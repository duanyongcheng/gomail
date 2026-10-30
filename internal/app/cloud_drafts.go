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
	"mime"
	"net/mail"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bary/pebble-go/internal/googleapi"
	"github.com/bary/pebble-go/internal/mailmime"
	"github.com/bary/pebble-go/internal/store"
)

const cloudDraftPurpose = "gmail_draft_copies.data"

type cloudDraftIdentity struct{ MessageID, Digest string }
type CloudDraftRequest struct {
	AccountID string `json:"accountId"`
	DraftID   string `json:"draftId"`
	Action    string `json:"action"`
}
type CloudDraftStatus struct {
	State   string `json:"state"`
	DraftID string `json:"draftId,omitempty"`
}
type draftSessionProvider interface {
	WithDraftSession(context.Context, string, func(*googleapi.API) error) error
}

func cloudCopyState(copy store.GmailDraftCopy, revision int64) string {
	if copy.State == "pending" {
		return "unknown"
	}
	if copy.State == "delete_pending" {
		return "deleting"
	}
	if copy.State == "synced" && copy.Revision != revision {
		return "changed"
	}
	return copy.State
}

// Compare decoded content, not transfer encodings or Gmail-added transport
// headers. Include attachment bytes explicitly (the parser excludes them from
// JSON). Any parser failure stays inconclusive, never permission to overwrite.
func cloudDigest(raw []byte) (string, error) {
	parsed, err := mailmime.ParseDraftWithLimit(raw, mailmime.MaxOutgoingBytes)
	if err != nil {
		return "", errors.New("cannot verify cloud draft MIME")
	}
	attachmentHashes := make([]string, 0, len(parsed.Attachments))
	for _, attachment := range parsed.Attachments {
		sum := sha256.Sum256(attachment.Data)
		attachmentHashes = append(attachmentHashes, hex.EncodeToString(sum[:]))
	}
	data, err := json.Marshal(struct {
		Message          mailmime.Message
		AttachmentHashes []string
	}{parsed, attachmentHashes})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// Cloud changes only an explicitly selected saved snapshot. Save stays local and
// offline. composeMu protects only snapshot building; network I/O must not block
// local autosave. The store checks the snapshot revision before recording intent.
// cloudDraftMu participates in account deletion/reconnection lock ordering.
func (s *ComposeService) Cloud(ctx context.Context, request CloudDraftRequest) (CloudDraftStatus, error) {
	if request.Action == "delete" || request.Action == "keep-local" || request.Action == "import-remote" {
		return s.cloudLifecycleAction(ctx, request)
	}
	if request.Action != "upload" && request.Action != "check" {
		return CloudDraftStatus{}, errors.New("invalid cloud draft action")
	}
	r := s.Runtime
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(r.Context, cancel)
	defer stop()
	r.cloudDraftMu.Lock()
	defer r.cloudDraftMu.Unlock()
	r.composeMu.Lock()
	composeLocked := true
	unlockCompose := func() {
		if composeLocked {
			r.composeMu.Unlock()
			composeLocked = false
		}
	}
	defer unlockCompose()
	stored, err := r.DB.ComposeDraft(ctx, request.AccountID, request.DraftID)
	if err != nil || stored.Deleted {
		return CloudDraftStatus{}, errors.New("local draft is unavailable in selected account")
	}
	account, err := r.DB.Account(ctx, request.AccountID)
	if err != nil || account.Provider != "gmail" {
		return CloudDraftStatus{}, errors.New("Gmail account required")
	}
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		return CloudDraftStatus{}, err
	}
	draft, err := decodeLocalDraft(cipher, stored)
	if err != nil {
		return CloudDraftStatus{}, err
	}
	copy, err := r.DB.GmailDraftCopy(ctx, account.ID, stored.ID)
	var previous *store.GmailDraftCopy
	var identity cloudDraftIdentity
	if err == nil {
		previous = &copy
		plain, e := cipher.Decrypt(cloudDraftPurpose, draftRecord(account.ID, stored.ID), copy.Data)
		if e != nil || json.Unmarshal(plain, &identity) != nil || (identity.MessageID == "" && copy.RemoteID == "") || len(identity.Digest) != 64 {
			return CloudDraftStatus{}, errors.New("cannot unlock cloud draft identity; data preserved")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return CloudDraftStatus{}, errors.New("cannot read cloud draft state")
	}
	if request.Action == "upload" && previous != nil && copy.State != "synced" {
		return CloudDraftStatus{}, errors.New("check the Gmail copy before further uploads; the last write is unconfirmed or conflicted")
	}
	if request.Action == "check" && previous == nil {
		return CloudDraftStatus{State: "local"}, nil
	}
	p, err := r.provider(account.ID)
	if err != nil {
		return CloudDraftStatus{}, err
	}
	provider, ok := p.(draftSessionProvider)
	if !ok {
		return CloudDraftStatus{}, errors.New("Gmail cloud drafts unavailable")
	}
	if request.Action == "check" {
		unlockCompose()
		if copy.State == "sending" || copy.State == "sent" {
			return CloudDraftStatus{State: copy.State}, errors.New("check the send status in Outbox")
		}
		if copy.State == "delete_pending" {
			err = provider.WithDraftSession(ctx, account.Email, func(api *googleapi.API) error {
				return s.checkCloudDeletion(ctx, api, copy)
			})
			state := "deleting"
			if err == nil {
				state = "local"
			}
			return CloudDraftStatus{State: state}, err
		}
		err = provider.WithDraftSession(ctx, account.Email, func(api *googleapi.API) error { return s.checkCloudCopy(ctx, api, &copy, identity) })
		return CloudDraftStatus{State: cloudCopyState(copy, stored.Revision)}, err
	}
	newIdentity := cloudDraftIdentity{MessageID: rand.Text() + "@pebble.invalid"}
	raw, thread, err := s.cloudDraftMIME(ctx, draft, newIdentity.MessageID)
	if err != nil {
		return CloudDraftStatus{}, err
	}
	newIdentity.Digest, err = cloudDigest(raw)
	if err != nil {
		return CloudDraftStatus{}, err
	}
	plain, err := json.Marshal(newIdentity)
	if err != nil {
		return CloudDraftStatus{}, err
	}
	encrypted, err := cipher.Encrypt(cloudDraftPurpose, draftRecord(account.ID, stored.ID), plain)
	if err != nil {
		return CloudDraftStatus{}, err
	}
	pending := store.GmailDraftCopy{DraftID: stored.ID, AccountID: account.ID, Revision: stored.Revision, State: "pending", Data: encrypted}
	if previous != nil {
		pending.RemoteID = copy.RemoteID
		pending.RemoteMessageID = copy.RemoteMessageID
	}
	unlockCompose()
	err = provider.WithDraftSession(ctx, account.Email, func(api *googleapi.API) error {
		if previous != nil {
			if err := s.checkCloudCopy(ctx, api, &copy, identity); err != nil {
				return err
			}
		}
		if err := r.DB.BeginGmailDraftCopy(ctx, pending); err != nil {
			return errors.New("cannot record cloud draft intent; nothing uploaded")
		}
		result, writeErr := api.WriteDraft(ctx, pending.RemoteID, raw, thread)
		if writeErr != nil {
			if !result.Unknown {
				// Use an independent bounded context so cancellation does not lose a known rejection.
				settle, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := r.DB.RejectGmailDraftCopy(settle, pending, previous); err != nil {
					return errors.New("cloud draft receipt unavailable; check Gmail before retrying")
				}
			}
			return writeErr
		}
		pending.RemoteID = result.Draft.ID
		pending.RemoteMessageID = result.Draft.Message.ID
		pending.State = "synced"
		if err := r.DB.SettleGmailDraftCopy(ctx, pending); err != nil {
			return errors.New("Gmail accepted the draft but its local receipt is unavailable; check Gmail before retrying")
		}
		return nil
	})
	return CloudDraftStatus{State: cloudCopyState(pending, stored.Revision)}, err
}

func (s *ComposeService) checkCloudCopy(ctx context.Context, api *googleapi.API, copy *store.GmailDraftCopy, identity cloudDraftIdentity) error {
	id := copy.RemoteID
	if id == "" {
		page, err := api.FindDraft(ctx, identity.MessageID)
		if err != nil {
			return err
		}
		if page.Next != "" || len(page.Drafts) != 1 {
			return errors.New("Gmail draft check is inconclusive; no upload was retried")
		}
		id = page.Drafts[0].ID
	}
	remote, err := api.Draft(ctx, id)
	if err != nil {
		var httpErr *googleapi.HTTPError
		if errors.As(err, &httpErr) && httpErr.Status == 404 && copy.State != "pending" {
			copy.State = "conflict"
			if e := s.Runtime.DB.SettleGmailDraftCopy(ctx, *copy); e != nil {
				return e
			}
		}
		return err
	}
	raw, err := base64.RawURLEncoding.DecodeString(remote.Message.Raw)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(remote.Message.Raw)
	}
	digest, digestErr := cloudDigest(raw)
	match := err == nil && digestErr == nil && slices.Contains(remote.Message.Labels, "DRAFT") && digest == identity.Digest
	if !match || (copy.State != "pending" && remote.Message.ID != copy.RemoteMessageID) {
		if copy.State != "pending" {
			copy.State = "conflict"
			if e := s.Runtime.DB.SettleGmailDraftCopy(ctx, *copy); e != nil {
				return e
			}
		}
		return errors.New("Gmail draft changed or check is inconclusive; remote content was not overwritten")
	}
	copy.RemoteID = remote.ID
	copy.RemoteMessageID = remote.Message.ID
	copy.State = "synced"
	return s.Runtime.DB.SettleGmailDraftCopy(ctx, *copy)
}

func (s *ComposeService) cloudDraftMIME(ctx context.Context, d LocalDraft, messageID string) ([]byte, string, error) {
	account, err := s.Runtime.DB.Account(ctx, d.AccountID)
	if err != nil {
		return nil, "", err
	}
	if d.EditorMode == "markdown" && (d.RenderedSource == nil || *d.RenderedSource != d.RawSource || (d.RawSource != "" && d.BodyHTML == "")) {
		return nil, "", errors.New("open and save this Markdown draft to render it before uploading to Gmail")
	}
	parse := func(values []string) ([]mail.Address, error) {
		out := []mail.Address{}
		for _, v := range values {
			if strings.ContainsAny(v, "\r\n") {
				return nil, errors.New("invalid cloud draft recipient")
			}
			a, e := mail.ParseAddress(v)
			if e != nil {
				return nil, errors.New("finish or remove incomplete recipients before uploading to Gmail")
			}
			out = append(out, *a)
		}
		return out, nil
	}
	to, err := parse(d.To)
	if err != nil {
		return nil, "", err
	}
	cc, err := parse(d.CC)
	if err != nil {
		return nil, "", err
	}
	bcc, err := parse(d.BCC)
	if err != nil {
		return nil, "", err
	}
	out := mailmime.Outgoing{From: mail.Address{Name: account.DisplayName, Address: account.Email}, To: to, CC: cc, BCC: bcc, IncludeBcc: true, Subject: d.Subject, Text: d.BodyText, HTML: d.BodyHTML, Date: time.Now(), MessageID: messageID, InReplyTo: d.InReplyTo, References: mailmime.ThreadHeaderIDs(d.References)}
	if d.QuotedReplyHTML != "" {
		out.HTML += "<br/><br/>" + d.QuotedReplyHTML
	}
	if d.InReplyTo != "" && len(out.References) == 0 {
		out.References = mailmime.ReplyReferences("", "", d.InReplyTo)
	}
	thread := ""
	if d.RemoteThreadID != "" && mailmime.NormalizeSubject(d.Subject) == mailmime.NormalizeSubject(d.ThreadSubject) {
		thread = d.RemoteThreadID
	}
	if d.ReplyMessageID != "" {
		parent, e := s.Runtime.DB.Message(ctx, d.ReplyMessageID)
		if e != nil || parent.AccountID != d.AccountID {
			return nil, "", errors.New("reply parent does not belong to selected account")
		}
		ids := mailmime.ThreadHeaderIDs(stringValue(parent.MessageIDHeader))
		if len(ids) != 1 {
			return nil, "", errors.New("reply parent has no usable Message-ID")
		}
		out.InReplyTo = ids[0]
		out.References = mailmime.ReplyReferences(stringValue(parent.ReferencesHeader), stringValue(parent.InReplyTo), ids[0])
		if mailmime.NormalizeSubject(d.Subject) == mailmime.NormalizeSubject(parent.Subject) {
			thread = stringValue(parent.ThreadID)
		}
	}
	cipher, err := s.Runtime.composeCrypto(ctx)
	if err != nil {
		return nil, "", err
	}
	for _, ref := range d.Attachments {
		a, e := s.attachmentData(ctx, cipher, ref.Path)
		if e != nil {
			return nil, "", e
		}
		typ := a.MIMEType
		if typ == "" {
			typ = mime.TypeByExtension(filepath.Ext(a.Name))
		}
		if typ == "" {
			typ = "application/octet-stream"
		}
		out.Attachments = append(out.Attachments, mailmime.Attachment{Filename: a.Name, MIMEType: typ, ContentID: a.ContentID, Inline: a.Inline, Data: a.Bytes})
	}
	raw, err := mailmime.BuildDraft(out, mailmime.MaxOutgoingBytes)
	if err != nil {
		return nil, "", errors.New("invalid cloud draft MIME or size limit exceeded")
	}
	return raw, thread, nil
}
