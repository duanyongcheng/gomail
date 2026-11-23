package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"mime"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/duanyongcheng/gomail/internal/crypto"
	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/googleapi"
	"github.com/duanyongcheng/gomail/internal/mailmime"
	"github.com/duanyongcheng/gomail/internal/store"
)

type CloudDraftListRequest struct {
	AccountID string `json:"accountId"`
	PageToken string `json:"pageToken"`
}
type RemoteDraftSummary struct {
	ID           string `json:"id"`
	MessageID    string `json:"messageId"`
	Subject      string `json:"subject"`
	UpdatedAt    int64  `json:"updatedAt"`
	LocalDraftID string `json:"localDraftId"`
}
type CloudDraftPage struct {
	Items         []RemoteDraftSummary `json:"items"`
	NextPageToken string               `json:"nextPageToken"`
}
type CloudDraftImportRequest struct {
	AccountID         string `json:"accountId"`
	RemoteDraftID     string `json:"remoteDraftId"`
	ExpectedMessageID string `json:"expectedMessageId"`
}

func (r *Runtime) cloudRequestContext(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	stop := context.AfterFunc(r.Context, cancel)
	return ctx, func() { stop(); cancel() }
}

func (s *ComposeService) cloudAccount(ctx context.Context, id string) (domain.Account, draftSessionProvider, error) {
	account, err := s.Runtime.DB.Account(ctx, id)
	if err != nil || account.Provider != "gmail" {
		return domain.Account{}, nil, errors.New("Gmail account required")
	}
	p, err := s.Runtime.provider(id)
	if err != nil {
		return account, nil, err
	}
	provider, ok := p.(draftSessionProvider)
	if !ok {
		return account, nil, errors.New("Gmail cloud drafts unavailable")
	}
	return account, provider, nil
}

// CloudList is an explicit bounded remote read. Opening local drafts never
// invokes it, and summaries fetch metadata rather than twenty large MIME blobs.
func (s *ComposeService) CloudList(ctx context.Context, request CloudDraftListRequest) (CloudDraftPage, error) {
	result := CloudDraftPage{Items: []RemoteDraftSummary{}}
	r := s.Runtime
	ctx, cancel := r.cloudRequestContext(ctx)
	defer cancel()
	r.cloudDraftMu.Lock()
	defer r.cloudDraftMu.Unlock()
	account, provider, err := s.cloudAccount(ctx, request.AccountID)
	if err != nil {
		return result, err
	}
	err = provider.WithDraftSession(ctx, account.Email, func(api *googleapi.API) error {
		page, err := api.ListDrafts(ctx, request.PageToken)
		if err != nil {
			return err
		}
		for _, ref := range page.Drafts {
			meta, err := api.DraftMetadata(ctx, ref.ID)
			if err != nil {
				return err
			}
			if !slices.Contains(meta.Message.Labels, "DRAFT") {
				return errors.New("Gmail draft list changed; refresh before importing")
			}
			item := RemoteDraftSummary{ID: meta.ID, MessageID: meta.Message.ID}
			for _, header := range meta.Message.Payload.Headers {
				if strings.EqualFold(header.Name, "Subject") {
					item.Subject = header.Value
					if decoded, err := new(mime.WordDecoder).DecodeHeader(header.Value); err == nil {
						item.Subject = decoded
					}
					if len(item.Subject) > 8192 {
						return errors.New("Gmail draft subject exceeds limit")
					}
					break
				}
			}
			if millis, err := strconv.ParseInt(meta.Message.Date, 10, 64); err == nil && millis > 0 {
				item.UpdatedAt = millis / 1000
			}
			item.LocalDraftID, err = r.DB.GmailDraftLocalID(ctx, account.ID, item.ID)
			if err != nil {
				return errors.New("cannot inspect local cloud draft associations")
			}
			result.Items = append(result.Items, item)
		}
		result.NextPageToken = page.Next
		return nil
	})
	return result, err
}

func cloudRaw(d googleapi.Draft) ([]byte, error) {
	if !slices.Contains(d.Message.Labels, "DRAFT") {
		return nil, errors.New("Gmail item is not an editable draft")
	}
	raw, err := base64.RawURLEncoding.DecodeString(d.Message.Raw)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(d.Message.Raw)
	}
	if err != nil || len(raw) == 0 || len(raw) > mailmime.MaxOutgoingBytes {
		return nil, errors.New("Gmail draft content is invalid or exceeds limit")
	}
	return raw, nil
}

func readCloudIdentity(cipher *crypto.Service, copy store.GmailDraftCopy) (cloudDraftIdentity, error) {
	var identity cloudDraftIdentity
	plain, err := cipher.Decrypt(cloudDraftPurpose, draftRecord(copy.AccountID, copy.DraftID), copy.Data)
	if err != nil || json.Unmarshal(plain, &identity) != nil || len(identity.Digest) != 64 ||
		(copy.RemoteID == "" && identity.MessageID == "") {
		return identity, errors.New("cannot unlock cloud draft identity; data preserved")
	}
	return identity, nil
}

func cloudImportable(copy store.GmailDraftCopy) bool {
	return copy.OutboxID == "" && (copy.State == "synced" || copy.State == "conflict")
}

// CloudImport returns an already-linked local draft without overwriting it.
// A web-edited version requires the explicit preserve-both recovery action.
func (s *ComposeService) CloudImport(ctx context.Context, request CloudDraftImportRequest) (LocalDraft, error) {
	r := s.Runtime
	ctx, cancel := r.cloudRequestContext(ctx)
	defer cancel()
	r.cloudDraftMu.Lock()
	defer r.cloudDraftMu.Unlock()
	account, provider, err := s.cloudAccount(ctx, request.AccountID)
	if err != nil {
		return LocalDraft{}, err
	}
	var previous *store.GmailDraftCopy
	copy, err := r.DB.GmailDraftCopyByRemote(ctx, account.ID, request.RemoteDraftID)
	if err == nil {
		if !cloudImportable(copy) {
			return LocalDraft{}, errors.New("this Gmail draft has an unresolved operation; check its local draft or Outbox")
		}
		previous = &copy
	} else if !errors.Is(err, sql.ErrNoRows) {
		return LocalDraft{}, errors.New("cannot inspect cloud draft association")
	}
	return s.importCloudDraft(ctx, account, provider, request.RemoteDraftID, request.ExpectedMessageID, previous, false)
}

func (s *ComposeService) importCloudDraft(ctx context.Context, account domain.Account, provider draftSessionProvider, remoteID, expectedMessageID string, previous *store.GmailDraftCopy, preserveBoth bool) (LocalDraft, error) {
	r := s.Runtime
	var imported LocalDraft
	err := provider.WithDraftSession(ctx, account.Email, func(api *googleapi.API) error {
		remote, err := api.Draft(ctx, remoteID)
		if err != nil {
			return err
		}
		if expectedMessageID != "" && remote.Message.ID != expectedMessageID {
			return errors.New("Gmail draft changed since the list was opened; refresh before importing")
		}
		raw, err := cloudRaw(remote)
		if err != nil {
			return err
		}
		parsed, err := mailmime.ParseDraftWithLimit(raw, mailmime.MaxOutgoingBytes)
		if err != nil || !strings.EqualFold(parsed.From.Address, account.Email) {
			return errors.New("cannot import this draft; a valid primary-mailbox sender and supported MIME are required")
		}
		digest, err := cloudDigest(raw)
		if err != nil {
			return err
		}
		r.composeMu.Lock()
		defer r.composeMu.Unlock()
		cipher, err := r.composeCrypto(ctx)
		if err != nil {
			return err
		}
		if previous != nil {
			oldIdentity, err := readCloudIdentity(cipher, *previous)
			if err != nil {
				return err
			}
			old, err := r.DB.ComposeDraft(ctx, account.ID, previous.DraftID)
			if err != nil {
				return errors.New("cannot inspect the existing local draft")
			}
			if !old.Deleted && !preserveBoth {
				if previous.RemoteMessageID != remote.Message.ID || oldIdentity.Digest != digest {
					changed := *previous
					changed.State = "conflict"
					if err := r.DB.SettleGmailDraftCopy(ctx, changed); err != nil {
						return err
					}
					return errors.New("Gmail draft changed; use Keep both versions on the local draft to preserve your edits")
				}
				imported, err = decodeLocalDraft(cipher, old)
				return err
			}
		}
		d, attachments, err := s.prepareCloudImport(cipher, account.ID, remote, parsed)
		if err != nil {
			return err
		}
		plain, err := json.Marshal(d)
		if err != nil {
			return errors.New("cannot encode imported draft")
		}
		encrypted, err := cipher.Encrypt(draftPurpose, draftRecord(account.ID, d.ID), plain)
		if err != nil {
			return errors.New("cannot encrypt imported draft")
		}
		identityBytes, err := json.Marshal(cloudDraftIdentity{MessageID: parsed.MessageID, Digest: digest})
		if err != nil {
			return err
		}
		identityBytes, err = cipher.Encrypt(cloudDraftPurpose, draftRecord(account.ID, d.ID), identityBytes)
		if err != nil {
			return errors.New("cannot encrypt imported draft identity")
		}
		key := sha256.Sum256([]byte(rand.Text()))
		copy := store.GmailDraftCopy{DraftID: d.ID, AccountID: account.ID, RemoteID: remote.ID, RemoteMessageID: remote.Message.ID, Revision: 1, State: "synced", Data: identityBytes}
		if err := r.DB.ImportGmailDraft(ctx, store.EncryptedDraft{ID: d.ID, AccountID: account.ID, RequestKey: hex.EncodeToString(key[:]), Data: encrypted}, attachments, copy, previous); err != nil {
			return errors.New("cannot persist imported draft; existing drafts were preserved")
		}
		d.UpdatedAt = time.Now().Unix()
		imported = d
		return nil
	})
	return imported, err
}

func (s *ComposeService) prepareCloudImport(cipher *crypto.Service, account string, remote googleapi.Draft, parsed mailmime.Message) (LocalDraft, []store.EncryptedComposeAttachment, error) {
	addresses := func(values []mailmime.Address) []string {
		result := make([]string, 0, len(values))
		for _, value := range values {
			result = append(result, (&mail.Address{Name: stringValue(value.Name), Address: value.Address}).String())
		}
		return result
	}
	d := LocalDraft{
		ID: "draft:" + rand.Text(), AccountID: account, To: addresses(parsed.To), CC: addresses(parsed.CC), BCC: addresses(parsed.BCC),
		Subject: parsed.Subject, BodyText: parsed.BodyText, BodyHTML: parsed.BodyHTML, InReplyTo: parsed.InReplyTo,
		References: parsed.References, RemoteThreadID: remote.Message.ThreadID, ThreadSubject: parsed.Subject,
		EditorMode: "html", RawSource: parsed.BodyHTML, Attachments: []DraftAttachment{},
	}
	if d.BodyHTML == "" && d.BodyText != "" {
		d.BodyHTML = "<pre>" + html.EscapeString(d.BodyText) + "</pre>"
		d.RawSource = d.BodyHTML
	}
	if len(d.References) > 32768 || len(d.RemoteThreadID) > 512 {
		return d, nil, errors.New("Gmail draft reply context exceeds limit")
	}
	if err := validateDraft(SaveDraftRequest{AccountID: account, To: d.To, CC: d.CC, BCC: d.BCC, Subject: d.Subject, BodyText: d.BodyText, BodyHTML: d.BodyHTML, InReplyTo: d.InReplyTo, EditorMode: d.EditorMode, RawSource: d.RawSource}); err != nil {
		return d, nil, err
	}
	if len(parsed.Attachments) > store.MaxComposeAttachments {
		return d, nil, errors.New("draft attachments exceed limit")
	}
	attachments := make([]store.EncryptedComposeAttachment, 0, len(parsed.Attachments))
	var total int
	for _, a := range parsed.Attachments {
		total += len(a.Data)
		if len(a.Data) > store.MaxComposeAttachmentBytes || total > store.MaxDraftAttachmentBytes {
			return d, nil, errors.New("draft attachments exceed limit")
		}
		id := rand.Text()
		name := SuggestedAttachmentFilename(a.Filename)
		data, err := json.Marshal(composeAttachmentData{Name: name, Bytes: a.Data, MIMEType: a.MIMEType, ContentID: a.ContentID, Inline: a.Inline})
		if err != nil {
			return d, nil, err
		}
		data, err = cipher.Encrypt(composeAttachmentPurpose, id, data)
		if err != nil {
			return d, nil, errors.New("cannot encrypt imported attachment")
		}
		attachments = append(attachments, store.EncryptedComposeAttachment{ID: id, Data: data, Size: int64(len(a.Data))})
		d.Attachments = append(d.Attachments, DraftAttachment{Name: name, Path: attachmentPrefix + id, Size: int64(len(a.Data))})
	}
	return d, attachments, nil
}

func (s *ComposeService) cloudLifecycleAction(ctx context.Context, request CloudDraftRequest) (CloudDraftStatus, error) {
	r := s.Runtime
	ctx, cancel := r.cloudRequestContext(ctx)
	defer cancel()
	r.cloudDraftMu.Lock()
	defer r.cloudDraftMu.Unlock()
	r.composeMu.Lock()
	local, err := r.DB.ComposeDraft(ctx, request.AccountID, request.DraftID)
	if err != nil || local.Deleted {
		r.composeMu.Unlock()
		return CloudDraftStatus{}, errors.New("local draft is unavailable in selected account")
	}
	copy, err := r.DB.GmailDraftCopy(ctx, request.AccountID, request.DraftID)
	if err != nil || !cloudImportable(copy) {
		r.composeMu.Unlock()
		return CloudDraftStatus{}, errors.New("cloud draft operation is unavailable; check the pending operation first")
	}
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		r.composeMu.Unlock()
		return CloudDraftStatus{}, err
	}
	identity, err := readCloudIdentity(cipher, copy)
	r.composeMu.Unlock()
	if err != nil {
		return CloudDraftStatus{}, err
	}
	if request.Action == "keep-local" {
		if copy.State != "conflict" {
			return CloudDraftStatus{}, errors.New("only a known conflict can be detached")
		}
		if err := r.DB.RemoveGmailDraftCopy(ctx, copy); err != nil {
			return CloudDraftStatus{}, errors.New("cannot detach cloud identity; local content was preserved")
		}
		return CloudDraftStatus{State: "local", DraftID: local.ID}, nil
	}
	account, provider, err := s.cloudAccount(ctx, request.AccountID)
	if err != nil {
		return CloudDraftStatus{}, err
	}
	if request.Action == "import-remote" {
		if copy.State != "conflict" {
			return CloudDraftStatus{}, errors.New("check the Gmail copy to identify the conflict before keeping both versions")
		}
		imported, err := s.importCloudDraft(ctx, account, provider, copy.RemoteID, "", &copy, true)
		if err != nil {
			return CloudDraftStatus{}, err
		}
		return CloudDraftStatus{State: "synced", DraftID: imported.ID}, nil
	}
	if copy.State != "synced" {
		return CloudDraftStatus{}, errors.New("resolve the cloud conflict before deleting remotely")
	}
	err = provider.WithDraftSession(ctx, account.Email, func(api *googleapi.API) error {
		if err := s.checkCloudCopy(ctx, api, &copy, identity); err != nil {
			return err
		}
		if err := r.DB.BeginGmailDraftDelete(ctx, copy); err != nil {
			return errors.New("cannot record cloud deletion intent; nothing was deleted")
		}
		copy.State = "delete_pending"
		result, deleteErr := api.DeleteDraft(ctx, copy.RemoteID)
		if deleteErr != nil {
			var httpErr *googleapi.HTTPError
			if errors.As(deleteErr, &httpErr) && httpErr.Status == 404 {
				return r.DB.RemoveGmailDraftCopy(ctx, copy)
			}
			if !result.Unknown {
				settle, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := r.DB.RejectGmailDraftDelete(settle, copy); err != nil {
					return errors.New("cloud deletion receipt unavailable; check Gmail before further actions")
				}
			}
			return deleteErr
		}
		if err := r.DB.RemoveGmailDraftCopy(ctx, copy); err != nil {
			return errors.New("Gmail deletion receipt unavailable; check Gmail before further actions")
		}
		return nil
	})
	state := cloudCopyState(copy, local.Revision)
	if err == nil {
		state = "local"
	}
	return CloudDraftStatus{State: state}, err
}

func (s *ComposeService) checkCloudDeletion(ctx context.Context, api *googleapi.API, copy store.GmailDraftCopy) error {
	_, err := api.Draft(ctx, copy.RemoteID)
	var httpErr *googleapi.HTTPError
	if errors.As(err, &httpErr) && httpErr.Status == 404 {
		return s.Runtime.DB.RemoveGmailDraftCopy(ctx, copy)
	}
	if err != nil {
		return err
	}
	return errors.New("Gmail deletion is still unconfirmed; no delete was retried")
}
