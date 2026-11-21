package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/mail"
	"slices"
	"strings"

	"github.com/duanyongcheng/pebble-go/internal/crypto"
	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/googleapi"
	"github.com/duanyongcheng/pebble-go/internal/store"
)

type outboxCloudDraft struct {
	DraftID, RemoteID, RemoteMessageID, Digest string
}

func (r *Runtime) outboxDraft(ctx context.Context, cipher *crypto.Service, account, id string) (*LocalDraft, *store.OutboxDraftReservation, *outboxCloudDraft, error) {
	if id == "" {
		return nil, nil, nil, nil
	}
	stored, err := r.DB.ComposeDraft(ctx, account, id)
	if err != nil || stored.Deleted {
		return nil, nil, nil, errors.New("saved draft is unavailable in the selected account")
	}
	local, err := decodeLocalDraft(cipher, stored)
	if err != nil {
		return nil, nil, nil, err
	}
	copy, err := r.DB.GmailDraftCopy(ctx, account, id)
	if errors.Is(err, sql.ErrNoRows) {
		return &local, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, errors.New("cannot read the saved Gmail draft identity")
	}
	if copy.State != "synced" || copy.RemoteID == "" || copy.RemoteMessageID == "" {
		return nil, nil, nil, errors.New("resolve the Gmail draft state or check Outbox before sending")
	}
	plain, err := cipher.Decrypt(cloudDraftPurpose, draftRecord(account, id), copy.Data)
	var identity cloudDraftIdentity
	if err != nil || json.Unmarshal(plain, &identity) != nil || len(identity.Digest) != 64 {
		return nil, nil, nil, errors.New("cannot unlock the Gmail draft identity; data preserved")
	}
	reservation := &store.OutboxDraftReservation{Copy: copy, LocalRevision: stored.Revision}
	cloud := &outboxCloudDraft{DraftID: id, RemoteID: copy.RemoteID, RemoteMessageID: copy.RemoteMessageID, Digest: identity.Digest}
	return &local, reservation, cloud, nil
}

// The provider gate pins mailbox validation, preflight and the one submission.
// Reservation CAS replaces a cloudDraftMu acquisition here: the caller already
// owns outboxMu, which follows cloudDraftMu in the account-quiescence lock order.
func (r *Runtime) sendCloudOutbox(ctx context.Context, record store.OutboxRecord, payload outboxPayload, transport domain.Transport) domain.SendResult {
	provider, ok := transport.(draftSessionProvider)
	if !ok {
		return domain.SendResult{Message: "Provider cannot send Gmail drafts."}
	}
	cloud := payload.CloudDraft
	if cloud == nil || cloud.DraftID == "" || cloud.RemoteID == "" || cloud.RemoteMessageID == "" || len(cloud.Digest) != 64 {
		return domain.SendResult{Message: "Queued Gmail draft identity is invalid; nothing was sent."}
	}
	reader, err := mail.ReadMessage(bytes.NewReader(payload.Raw))
	if err != nil {
		return domain.SendResult{Message: "Invalid outgoing MIME."}
	}
	from, err := reader.Header.AddressList("From")
	if err != nil || len(from) != 1 || !strings.EqualFold(from[0].Address, payload.Email) {
		return domain.SendResult{Message: "Outgoing sender does not match the account."}
	}
	result := domain.SendResult{Retryable: true, Message: "Cannot verify the Gmail draft; check connection and authorization."}
	err = provider.WithDraftSession(ctx, payload.Email, func(api *googleapi.API) error {
		remote, err := api.Draft(ctx, cloud.RemoteID)
		if err != nil {
			var httpErr *googleapi.HTTPError
			if errors.As(err, &httpErr) && httpErr.Status == 404 {
				result = domain.SendResult{Message: "The Gmail draft is missing; cancel this send and check Gmail. No fallback message was sent."}
			}
			return err
		}
		raw, decodeErr := base64.RawURLEncoding.DecodeString(remote.Message.Raw)
		if decodeErr != nil {
			raw, decodeErr = base64.URLEncoding.DecodeString(remote.Message.Raw)
		}
		digest, digestErr := cloudDigest(raw)
		if decodeErr != nil || digestErr != nil || remote.Message.ID != cloud.RemoteMessageID || digest != cloud.Digest || !slices.Contains(remote.Message.Labels, "DRAFT") {
			result = domain.SendResult{Message: "The Gmail draft changed; cancel this send and resolve the draft conflict. Nothing was sent."}
			return errors.New("Gmail draft preflight mismatch")
		}
		if err := r.DB.BeginCloudOutboxSend(ctx, record.AccountID, record.ID, cloud.DraftID, cloud.RemoteID, cloud.RemoteMessageID); err != nil {
			result = domain.SendResult{Retryable: true, Message: "Could not persist draft send start; nothing was submitted."}
			return err
		}
		receipt := api.SendDraftRaw(ctx, cloud.RemoteID, payload.Raw, payload.ThreadID)
		result = domain.SendResult{RemoteID: receipt.RemoteID, Unknown: receipt.Unknown, Retryable: receipt.Retryable, Message: receipt.Message}
		return nil
	})
	if err != nil {
		return result
	}
	return result
}
