package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/mail"
	"path/filepath"
	"strings"
	"time"

	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/mailmime"
	"github.com/duanyongcheng/gomail/internal/store"
)

const outboxPurpose = "outbox.data"
const outboxSummaryPurpose = "outbox.summary"

type SendRequest struct {
	AccountID       string   `json:"accountId"`
	To              []string `json:"to"`
	CC              []string `json:"cc"`
	BCC             []string `json:"bcc"`
	Subject         string   `json:"subject"`
	BodyText        string   `json:"bodyText"`
	BodyHTML        string   `json:"bodyHtml"`
	InReplyTo       string   `json:"inReplyTo"`
	ReplyMessageID  string   `json:"replyMessageId,omitempty"`
	LocalDraftID    string   `json:"localDraftId,omitempty"`
	AttachmentPaths []string `json:"attachmentPaths"`
	ClientKey       string   `json:"clientKey"`
}
type outboxPayload struct {
	Email, Subject, MessageID, Fingerprint string
	Raw                                    []byte
	ThreadID                               string
	CloudDraft                             *outboxCloudDraft `json:",omitempty"`
	EnvelopeRecipients                     []string
}
type OutboxItem struct {
	ID          string `json:"id"`
	AccountID   string `json:"accountId"`
	Subject     string `json:"subject"`
	Status      string `json:"status"`
	Attempts    int    `json:"attempts"`
	NextRetryAt *int64 `json:"nextRetryAt"`
	LastError   string `json:"lastError"`
	CreatedAt   int64  `json:"createdAt"`
	CloudDraft  bool   `json:"cloudDraft,omitempty"`
}
type OutboxActionRequest struct {
	AccountID string `json:"accountId"`
	ID        string `json:"id"`
	Action    string `json:"action"`
}
type OutboxService struct{ Runtime *Runtime }

// Queue returns only after encrypted MIME is durable. It does NOT mean sent.
func (s *OutboxService) Queue(ctx context.Context, request SendRequest) (Empty, error) {
	r := s.Runtime
	if err := ctx.Err(); err != nil {
		return Empty{}, err
	}
	if request.AccountID == "" || len(request.ClientKey) > 200 || len(request.LocalDraftID) > 200 {
		return Empty{}, errors.New("invalid send identity")
	}
	if err := validateDraft(SaveDraftRequest{AccountID: request.AccountID, To: request.To, CC: request.CC, BCC: request.BCC, Subject: request.Subject, BodyText: request.BodyText, BodyHTML: request.BodyHTML, InReplyTo: request.InReplyTo, ReplyMessageID: request.ReplyMessageID, AttachmentPaths: request.AttachmentPaths}); err != nil {
		return Empty{}, err
	}
	r.composeMu.Lock()
	defer r.composeMu.Unlock()
	account, err := r.DB.Account(ctx, request.AccountID)
	if err != nil {
		return Empty{}, errors.New("account required")
	}
	if account.Provider != "gmail" {
		p, providerErr := r.provider(account.ID)
		if providerErr != nil || !p.Capabilities().Send {
			return Empty{}, errors.New("account does not support sending")
		}
	}
	key := request.ClientKey
	if key == "" {
		key = rand.Text()
	}
	digest := sha256.Sum256([]byte(key))
	key = hex.EncodeToString(digest[:])
	content := request
	content.ClientKey = ""
	contentJSON, err := json.Marshal(content)
	if err != nil {
		return Empty{}, errors.New("cannot encode send request")
	}
	contentHash := sha256.Sum256(contentJSON)
	fingerprint := hex.EncodeToString(contentHash[:])
	if existing, err := r.DB.OutboxByKey(ctx, account.ID, key); err == nil {
		if existing.Data == nil {
			return Empty{}, errors.New("This send request was already removed; start a new compose if needed.")
		}
		previous, err := r.decodeOutbox(ctx, existing)
		if err != nil {
			return Empty{}, err
		}
		if previous.Fingerprint != fingerprint {
			return Empty{}, errors.New("This request already queued different content; check Outbox before composing another copy.")
		}
		r.notifyOutbox()
		return Empty{}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Empty{}, errors.New("cannot inspect outbox")
	}
	addresses := func(values []string) ([]mail.Address, error) {
		out := []mail.Address{}
		for _, v := range values {
			if strings.ContainsAny(v, "\r\n") {
				return nil, errors.New("invalid recipient address")
			}
			a, err := mail.ParseAddress(v)
			if err != nil {
				return nil, errors.New("invalid recipient address")
			}
			out = append(out, *a)
		}
		return out, nil
	}
	to, err := addresses(request.To)
	if err != nil {
		return Empty{}, err
	}
	cc, err := addresses(request.CC)
	if err != nil {
		return Empty{}, err
	}
	bcc, err := addresses(request.BCC)
	if err != nil {
		return Empty{}, err
	}
	id := "outbox:" + rand.Text()
	messageID := rand.Text() + "@gomail.invalid"
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		return Empty{}, err
	}
	local, reservation, cloud, err := r.outboxDraft(ctx, cipher, request.AccountID, request.LocalDraftID)
	if err != nil {
		return Empty{}, err
	}
	outgoing := mailmime.Outgoing{From: mail.Address{Name: account.DisplayName, Address: account.Email}, To: to, CC: cc, BCC: bcc, IncludeBcc: true, Subject: request.Subject, Text: request.BodyText, HTML: request.BodyHTML, MessageID: messageID, InReplyTo: request.InReplyTo, Date: time.Now()}
	// Graph and Gmail submit raw recipient headers; SMTP freezes its envelope separately.
	outgoing.IncludeBcc = account.Provider == "gmail" || account.Provider == "outlook" || account.Provider == "graph"
	envelope := []string{}
	for _, group := range [][]mail.Address{to, cc, bcc} {
		for _, address := range group {
			envelope = append(envelope, address.Address)
		}
	}
	if request.InReplyTo != "" {
		outgoing.References = mailmime.ReplyReferences("", "", request.InReplyTo)
	}
	threadID := ""
	if local != nil {
		if outgoing.InReplyTo == "" {
			outgoing.InReplyTo = local.InReplyTo
		}
		if outgoing.InReplyTo == local.InReplyTo && local.References != "" {
			outgoing.References = mailmime.ThreadHeaderIDs(local.References)
		}
		if local.RemoteThreadID != "" && outgoing.InReplyTo == local.InReplyTo && mailmime.NormalizeSubject(request.Subject) == mailmime.NormalizeSubject(local.ThreadSubject) {
			threadID = local.RemoteThreadID
		}
	}
	if request.ReplyMessageID != "" {
		threadID = ""
		parent, err := r.DB.Message(ctx, request.ReplyMessageID)
		if err != nil || parent.AccountID != account.ID {
			return Empty{}, errors.New("reply message does not belong to selected account")
		}
		ids := mailmime.ThreadHeaderIDs(stringValue(parent.MessageIDHeader))
		if len(ids) != 1 {
			return Empty{}, errors.New("original message has no usable Message-ID")
		}
		outgoing.InReplyTo = ids[0]
		outgoing.References = mailmime.ReplyReferences(stringValue(parent.ReferencesHeader), stringValue(parent.InReplyTo), ids[0])
		if mailmime.NormalizeSubject(request.Subject) == mailmime.NormalizeSubject(parent.Subject) {
			threadID = stringValue(parent.ThreadID)
		}
	}
	compose := &ComposeService{Runtime: r}
	var total int
	seen := map[string]bool{}
	for _, path := range request.AttachmentPaths {
		if seen[path] {
			return Empty{}, errors.New("duplicate outgoing attachment")
		}
		seen[path] = true
		a, err := compose.attachmentData(ctx, cipher, path)
		if err != nil {
			return Empty{}, err
		}
		total += len(a.Bytes)
		if total > store.MaxDraftAttachmentBytes {
			return Empty{}, errors.New("outgoing attachments exceed 25 MiB")
		}
		typ := a.MIMEType
		if typ == "" {
			typ = mime.TypeByExtension(filepath.Ext(a.Name))
		}
		if typ == "" {
			typ = "application/octet-stream"
		}
		outgoing.Attachments = append(outgoing.Attachments, mailmime.Attachment{Filename: a.Name, MIMEType: typ, ContentID: a.ContentID, Inline: a.Inline, Data: a.Bytes})
	}
	raw, err := mailmime.BuildOutgoing(outgoing, mailmime.MaxOutgoingBytes)
	if err != nil {
		return Empty{}, errors.New("invalid outgoing message or size limit exceeded")
	}
	plain, err := json.Marshal(outboxPayload{Email: account.Email, Subject: request.Subject, MessageID: messageID, Fingerprint: fingerprint, Raw: raw, ThreadID: threadID, CloudDraft: cloud, EnvelopeRecipients: envelope})
	if err != nil {
		return Empty{}, err
	}
	encrypted, err := cipher.Encrypt(outboxPurpose, draftRecord(account.ID, id), plain)
	if err != nil {
		return Empty{}, errors.New("cannot encrypt outbox message")
	}
	summary, err := cipher.Encrypt(outboxSummaryPurpose, draftRecord(account.ID, id), []byte(request.Subject))
	if err != nil {
		return Empty{}, errors.New("cannot encrypt outbox summary")
	}
	if _, err := r.DB.QueueOutbox(ctx, store.OutboxRecord{ID: id, AccountID: account.ID, RequestKey: key, Data: encrypted, Summary: summary, CloudDraft: reservation}); err != nil {
		return Empty{}, errors.New("cannot queue message; check outbox storage")
	}
	r.notifyOutbox()
	return Empty{}, nil
}
func (r *Runtime) decodeOutbox(ctx context.Context, record store.OutboxRecord) (outboxPayload, error) {
	var payload outboxPayload
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		return payload, err
	}
	plain, err := cipher.Decrypt(outboxPurpose, draftRecord(record.AccountID, record.ID), record.Data)
	if err != nil || json.Unmarshal(plain, &payload) != nil || len(payload.Raw) == 0 {
		return payload, errors.New("cannot decrypt queued message; data preserved")
	}
	return payload, nil
}
func (s *OutboxService) List(ctx context.Context, request AccountRequest) ([]OutboxItem, error) {
	rows, err := s.Runtime.DB.OutboxList(ctx, request.AccountID)
	if err != nil {
		return nil, errors.New("cannot list outbox")
	}
	out := []OutboxItem{}
	for _, row := range rows {
		item := OutboxItem{ID: row.ID, AccountID: row.AccountID, Status: row.Status, Attempts: row.Attempts, NextRetryAt: row.NextRetryAt, LastError: row.LastError, CreatedAt: row.CreatedAt, CloudDraft: row.HasCloudDraft}
		cipher, keyErr := s.Runtime.composeCrypto(ctx)
		if keyErr == nil {
			if subject, err := cipher.Decrypt(outboxSummaryPurpose, draftRecord(row.AccountID, row.ID), row.Summary); err == nil {
				item.Subject = string(subject)
			} else {
				item.LastError = "Encrypted content unavailable; unlock the profile keyring."
			}
		} else {
			item.LastError = "Encrypted content unavailable; unlock the profile keyring."
		}
		out = append(out, item)
	}
	return out, nil
}
func (s *OutboxService) Action(ctx context.Context, request OutboxActionRequest) (Empty, error) {
	r := s.Runtime
	if request.Action == "check" {
		r.outboxMu.Lock()
		defer r.outboxMu.Unlock()
		record, err := r.DB.Outbox(ctx, request.AccountID, request.ID)
		if err != nil || record.Status != "outcome_unknown" {
			return Empty{}, errors.New("only unknown sends can be checked")
		}
		payload, err := r.decodeOutbox(ctx, record)
		if err != nil {
			return Empty{}, err
		}
		provider, err := r.provider(record.AccountID)
		if err != nil {
			return Empty{}, err
		}
		sender, ok := provider.(domain.SendTransport)
		if !ok {
			return Empty{}, errors.New("provider cannot check Sent")
		}
		id, err := sender.CheckSent(ctx, payload.Email, payload.MessageID)
		if err != nil {
			return Empty{}, errors.New("Sent check is inconclusive; no resend was attempted")
		}
		if id == "" {
			return Empty{}, errors.New("Sent check is inconclusive; no resend was attempted")
		}
		if err := r.DB.ConfirmOutboxSent(ctx, record.AccountID, record.ID, id); err != nil {
			return Empty{}, errors.New("cannot save Sent confirmation")
		}
	} else if err := r.DB.OutboxAction(ctx, request.AccountID, request.ID, request.Action); err != nil {
		if errors.Is(err, store.ErrOutboxDraftUnavailable) {
			return Empty{}, err
		}
		return Empty{}, errors.New("outbox state does not allow this action")
	}
	r.notifyOutbox()
	return Empty{}, nil
}
func (r *Runtime) notifyOutbox() {
	select {
	case r.outboxWake <- struct{}{}:
	default:
	}
	if r.OnOutboxChanged != nil {
		r.OnOutboxChanged()
	}
}

type outboxSettlement struct {
	ID     string
	Result domain.SendResult
	Paused bool
}

func (r *Runtime) settleOutbox(ctx context.Context) error {
	pending := r.outboxSettlement
	if pending == nil {
		return nil
	}
	status := "failed"
	if pending.Result.Unknown {
		status = "outcome_unknown"
		pending.Result.Retryable = false
	} else if pending.Result.Accepted || pending.Result.RemoteID != "" {
		status = "sent"
		pending.Result.Retryable = false
	}
	var err error
	if pending.Result.AccessState != "" && pending.Result.AccessState.Blocked() && !pending.Result.Unknown && !pending.Result.Accepted && pending.Result.RemoteID == "" {
		err = r.DB.PauseOutboxResource(ctx, pending.ID, pending.Result.AccessState)
	} else if pending.Paused {
		err = r.DB.ReleaseOutboxAccessClaim(ctx, pending.ID)
	} else {
		err = r.DB.SettleOutbox(ctx, pending.ID, status, pending.Result.Message, pending.Result.RemoteID, pending.Result.Retryable)
	}
	if err != nil {
		return err
	}
	r.outboxSettlement = nil
	if r.OnOutboxChanged != nil {
		r.OnOutboxChanged()
	}
	return nil
}
func (r *Runtime) processOutbox(ctx context.Context) (bool, error) {
	r.outboxMu.Lock()
	defer r.outboxMu.Unlock()
	if err := r.settleOutbox(ctx); err != nil {
		return false, err
	}
	for i := 0; i < 10; i++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		record, err := r.DB.ClaimOutbox(ctx, time.Now().Unix(), r.blockedAccounts()...)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		result := r.sendOutbox(ctx, record)
		r.outboxSettlement = &outboxSettlement{ID: record.ID, Result: result, Paused: !result.Unknown && !result.Accepted && result.RemoteID == "" && r.accountBlocked(record.AccountID)}
		if err := r.settleOutbox(ctx); err != nil {
			return false, err
		}
	}
	return true, nil
}
func (r *Runtime) sendOutbox(ctx context.Context, record store.OutboxRecord) domain.SendResult {
	payload, err := r.decodeOutbox(ctx, record)
	if err != nil {
		return domain.SendResult{Message: "Cannot decrypt queued message; unlock the profile keyring."}
	}
	p, err := r.provider(record.AccountID)
	if err != nil {
		return domain.SendResult{Retryable: true, Message: "Gmail sign-in required; queued message is retained."}
	}
	if payload.CloudDraft != nil {
		return r.sendCloudOutbox(ctx, record, payload, p)
	}
	sender, ok := p.(domain.SendTransport)
	if !ok {
		return domain.SendResult{Message: "Provider does not support sending."}
	}
	return sender.Send(ctx, domain.OutgoingMail{Email: payload.Email, Raw: payload.Raw, ThreadID: payload.ThreadID, EnvelopeRecipients: append([]string(nil), payload.EnvelopeRecipients...)}, func() error { return r.DB.BeginOutboxSend(ctx, record.ID) })
}
func (r *Runtime) RunOutboxWorker(ctx context.Context) {
	r.outboxWorkerMu.Lock()
	defer r.outboxWorkerMu.Unlock()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	recovered := false
	for ctx.Err() == nil {
		if !recovered {
			r.outboxMu.Lock()
			err := r.settleOutbox(ctx)
			if err == nil {
				err = r.DB.RecoverOutbox(ctx)
			}
			r.outboxMu.Unlock()
			if err == nil {
				recovered = true
			} else if ctx.Err() == nil {
				slog.Warn("outbox recovery deferred")
			}
		}
		if recovered {
			more, err := r.processOutbox(ctx)
			if err == nil && more {
				continue
			}
			if err != nil && ctx.Err() == nil {
				slog.Warn("outbox persistence deferred")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-r.outboxWake:
		}
	}
}
