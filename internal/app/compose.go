package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/bary/pebble-go/internal/crypto"
	"github.com/bary/pebble-go/internal/store"
)

const maxDraftBodyBytes = 2 << 20
const attachmentPrefix = "compose-attachment:"
const draftPurpose = "compose_drafts.data"
const composeAttachmentPurpose = "compose_attachments.data"

type SaveDraftRequest struct {
	AccountID       string   `json:"accountId"`
	To              []string `json:"to"`
	CC              []string `json:"cc"`
	BCC             []string `json:"bcc"`
	Subject         string   `json:"subject"`
	BodyText        string   `json:"bodyText"`
	BodyHTML        string   `json:"bodyHtml"`
	InReplyTo       string   `json:"inReplyTo"`
	ReplyMessageID  string   `json:"replyMessageId,omitempty"`
	QuotedReplyHTML string   `json:"quotedReplyHtml"`
	ExistingDraftID string   `json:"existingDraftId"`
	AttachmentPaths []string `json:"attachmentPaths"`
	ClientKey       string   `json:"clientKey"`
	EditorMode      string   `json:"editorMode"`
	RawSource       string   `json:"rawSource"`
}
type DraftRequest struct {
	AccountID string `json:"accountId"`
	DraftID   string `json:"draftId"`
}
type StageComposeRequest struct {
	Filename string `json:"filename"`
	Bytes    []int  `json:"bytes"`
}
type CleanupComposeRequest struct {
	Path string `json:"path"`
}
type DraftAttachment struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}
type LocalDraft struct {
	ID              string            `json:"id"`
	AccountID       string            `json:"accountId"`
	To              []string          `json:"to"`
	CC              []string          `json:"cc"`
	BCC             []string          `json:"bcc"`
	Subject         string            `json:"subject"`
	BodyText        string            `json:"bodyText"`
	BodyHTML        string            `json:"bodyHtml"`
	InReplyTo       string            `json:"inReplyTo"`
	ReplyMessageID  string            `json:"replyMessageId,omitempty"`
	QuotedReplyHTML string            `json:"quotedReplyHtml"`
	EditorMode      string            `json:"editorMode"`
	RawSource       string            `json:"rawSource"`
	Attachments     []DraftAttachment `json:"attachments"`
	UpdatedAt       int64             `json:"updatedAt"`
}
type DraftSummary struct {
	ID              string `json:"id"`
	AccountID       string `json:"accountId"`
	Subject         string `json:"subject"`
	UpdatedAt       int64  `json:"updatedAt"`
	AttachmentCount int    `json:"attachmentCount"`
}
type composeAttachmentData struct {
	Name  string `json:"name"`
	Bytes []byte `json:"bytes"`
}
type ComposeService struct{ Runtime *Runtime }

func (r *Runtime) composeCrypto(ctx context.Context) (*crypto.Service, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.crypto != nil {
		return r.crypto, nil
	}
	has, err := r.DB.HasEncryptedData(ctx)
	if err != nil {
		return nil, errors.New("cannot inspect encrypted profile data")
	}
	if r.Keyring == nil {
		return nil, errors.New("keyring unavailable; draft was not saved")
	}
	r.crypto, err = crypto.Load(r.Keyring, has)
	return r.crypto, err
}
func composeID(path string) (string, error) {
	id, ok := strings.CutPrefix(path, attachmentPrefix)
	if !ok || len(id) != 26 {
		return "", errors.New("invalid compose attachment reference")
	}
	for _, r := range id {
		if !(r >= 'A' && r <= 'Z' || r >= '2' && r <= '7') {
			return "", errors.New("invalid compose attachment reference")
		}
	}
	return id, nil
}
func draftRecord(account, id string) string { return account + "\x00" + id }
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *ComposeService) Stage(ctx context.Context, request StageComposeRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(request.Bytes) > store.MaxComposeAttachmentBytes {
		return "", errors.New("compose attachment exceeds 10 MiB")
	}
	if !utf8.ValidString(request.Filename) || len(request.Filename) > 1024 {
		return "", errors.New("invalid attachment filename")
	}
	data := make([]byte, len(request.Bytes))
	for i, value := range request.Bytes {
		if value < 0 || value > 255 {
			return "", errors.New("invalid attachment byte")
		}
		data[i] = byte(value)
	}
	r := s.Runtime
	r.composeMu.Lock()
	defer r.composeMu.Unlock()
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		return "", err
	}
	id := rand.Text()
	plain, err := json.Marshal(composeAttachmentData{Name: SuggestedAttachmentFilename(request.Filename), Bytes: data})
	if err != nil {
		return "", err
	}
	encrypted, err := cipher.Encrypt(composeAttachmentPurpose, id, plain)
	if err != nil {
		return "", errors.New("cannot encrypt compose attachment")
	}
	if err := r.DB.StageComposeAttachment(ctx, store.EncryptedComposeAttachment{ID: id, Data: encrypted, Size: int64(len(data))}); err != nil {
		return "", errors.New("cannot stage attachment; check available storage and compose limits")
	}
	return attachmentPrefix + id, nil
}
func (s *ComposeService) Cleanup(ctx context.Context, request CleanupComposeRequest) (Empty, error) {
	id, err := composeID(request.Path)
	if err != nil {
		return Empty{}, err
	}
	r := s.Runtime
	r.composeMu.Lock()
	defer r.composeMu.Unlock()
	if err := r.DB.ReleaseComposeAttachment(ctx, id); err != nil {
		return Empty{}, errors.New("cannot release compose attachment")
	}
	return Empty{}, nil
}
func (s *ComposeService) attachment(ctx context.Context, cipher *crypto.Service, path string) (DraftAttachment, error) {
	data, err := s.attachmentData(ctx, cipher, path)
	if err != nil {
		return DraftAttachment{}, err
	}
	return DraftAttachment{Name: data.Name, Path: path, Size: int64(len(data.Bytes))}, nil
}
func (s *ComposeService) attachmentData(ctx context.Context, cipher *crypto.Service, path string) (composeAttachmentData, error) {
	id, err := composeID(path)
	if err != nil {
		return composeAttachmentData{}, err
	}
	stored, err := s.Runtime.DB.ComposeAttachment(ctx, id)
	if err != nil {
		return composeAttachmentData{}, errors.New("compose attachment is unavailable")
	}
	plain, err := cipher.Decrypt(composeAttachmentPurpose, id, stored.Data)
	if err != nil {
		return composeAttachmentData{}, errors.New("cannot decrypt compose attachment; data preserved")
	}
	var data composeAttachmentData
	if json.Unmarshal(plain, &data) != nil || int64(len(data.Bytes)) != stored.Size {
		return composeAttachmentData{}, errors.New("invalid encrypted compose attachment")
	}
	return data, nil
}
func validateDraft(request SaveDraftRequest) error {
	if request.AccountID == "" || len(request.ClientKey) > 200 || len(request.ExistingDraftID) > 200 || len(request.ReplyMessageID) > 1024 {
		return errors.New("invalid draft identity")
	}
	if len(request.Subject) > 8192 || len(request.InReplyTo) > 2048 || len(request.RawSource)+len(request.BodyText)+len(request.BodyHTML)+len(request.QuotedReplyHTML) > maxDraftBodyBytes {
		return errors.New("draft text exceeds limit")
	}
	if len(request.To)+len(request.CC)+len(request.BCC) > 1000 || len(request.AttachmentPaths) > store.MaxComposeAttachments {
		return errors.New("draft recipients or attachments exceed limit")
	}
	for _, list := range [][]string{request.To, request.CC, request.BCC} {
		for _, address := range list {
			if len(address) > 1024 || !utf8.ValidString(address) {
				return errors.New("invalid draft recipient")
			}
		}
	}
	for _, text := range []string{request.Subject, request.BodyText, request.BodyHTML, request.RawSource, request.InReplyTo, request.QuotedReplyHTML} {
		if !utf8.ValidString(text) {
			return errors.New("invalid draft text")
		}
	}
	if request.EditorMode != "" && request.EditorMode != "rich" && request.EditorMode != "html" && request.EditorMode != "markdown" {
		return errors.New("invalid draft editor mode")
	}
	return nil
}

// Save is deliberately local-only; it never interprets an unknown draft ID as
// a Gmail ID or calls the network. Partial recipient input is valid in a draft.
func (s *ComposeService) Save(ctx context.Context, request SaveDraftRequest) (string, error) {
	if err := validateDraft(request); err != nil {
		return "", err
	}
	r := s.Runtime
	r.composeMu.Lock()
	defer r.composeMu.Unlock()
	exists, err := r.DB.ComposeAccountExists(ctx, request.AccountID)
	if err != nil || !exists {
		return "", errors.New("draft account is unavailable")
	}
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		return "", err
	}
	var stored store.EncryptedDraft
	if request.ExistingDraftID != "" {
		stored, err = r.DB.ComposeDraft(ctx, request.AccountID, request.ExistingDraftID)
		if err != nil {
			return "", errors.New("draft not found in selected account")
		}
	} else {
		key := request.ClientKey
		if key == "" {
			key = rand.Text()
		}
		// Hash the client key so arbitrary client text is not stored in clear.
		digest := sha256.Sum256([]byte(key))
		key = hex.EncodeToString(digest[:])
		stored, err = r.DB.ComposeDraftByKey(ctx, request.AccountID, key)
		if errors.Is(err, sql.ErrNoRows) {
			stored = store.EncryptedDraft{ID: "local-draft:" + rand.Text(), AccountID: request.AccountID, RequestKey: key}
		} else if err != nil {
			return "", errors.New("cannot read draft identity")
		}
	}
	if stored.Deleted {
		return "", store.ErrDraftConflict
	}
	if stored.Data != nil {
		if _, err := cipher.Decrypt(draftPurpose, draftRecord(stored.AccountID, stored.ID), stored.Data); err != nil {
			return "", errors.New("cannot decrypt existing draft; data preserved")
		}
	}
	draft := LocalDraft{ID: stored.ID, AccountID: request.AccountID, To: request.To, CC: request.CC, BCC: request.BCC, Subject: request.Subject, BodyText: request.BodyText, BodyHTML: request.BodyHTML, InReplyTo: request.InReplyTo, ReplyMessageID: request.ReplyMessageID, QuotedReplyHTML: request.QuotedReplyHTML, EditorMode: request.EditorMode, RawSource: request.RawSource, Attachments: []DraftAttachment{}}
	if draft.EditorMode == "" {
		draft.EditorMode = "rich"
	}
	ids := []string{}
	var total int64
	for _, path := range request.AttachmentPaths {
		a, err := s.attachment(ctx, cipher, path)
		if err != nil {
			return "", err
		}
		total += a.Size
		if total > store.MaxDraftAttachmentBytes {
			return "", errors.New("draft attachments exceed 25 MiB")
		}
		draft.Attachments = append(draft.Attachments, a)
		id, _ := composeID(path)
		ids = append(ids, id)
	}
	plain, err := json.Marshal(draft)
	if err != nil {
		return "", errors.New("cannot encode draft")
	}
	stored.Data, err = cipher.Encrypt(draftPurpose, draftRecord(stored.AccountID, stored.ID), plain)
	if err != nil {
		return "", errors.New("cannot encrypt draft")
	}
	if err = r.DB.SaveComposeDraft(ctx, stored, ids); err != nil {
		if errors.Is(err, store.ErrDraftConflict) {
			return "", err
		}
		return "", errors.New("cannot save draft; previous data preserved")
	}
	return stored.ID, nil
}
func decodeLocalDraft(cipher *crypto.Service, stored store.EncryptedDraft) (LocalDraft, error) {
	var draft LocalDraft
	if stored.Deleted {
		return draft, errors.New("draft was deleted")
	}
	plain, err := cipher.Decrypt(draftPurpose, draftRecord(stored.AccountID, stored.ID), stored.Data)
	if err != nil || json.Unmarshal(plain, &draft) != nil {
		return draft, errors.New("cannot decrypt draft; data preserved")
	}
	draft.ID, draft.AccountID, draft.UpdatedAt = stored.ID, stored.AccountID, stored.UpdatedAt
	if draft.To == nil {
		draft.To = []string{}
	}
	if draft.CC == nil {
		draft.CC = []string{}
	}
	if draft.BCC == nil {
		draft.BCC = []string{}
	}
	return draft, nil
}
func (s *ComposeService) Get(ctx context.Context, request DraftRequest) (LocalDraft, error) {
	r := s.Runtime
	r.composeMu.Lock()
	defer r.composeMu.Unlock()
	stored, err := r.DB.ComposeDraft(ctx, request.AccountID, request.DraftID)
	if err != nil {
		return LocalDraft{}, errors.New("draft not found in selected account")
	}
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		return LocalDraft{}, err
	}
	draft, err := decodeLocalDraft(cipher, stored)
	if err != nil {
		return LocalDraft{}, err
	}
	for _, a := range draft.Attachments {
		if _, err := s.attachment(ctx, cipher, a.Path); err != nil {
			return LocalDraft{}, err
		}
	}
	return draft, nil
}
func (s *ComposeService) List(ctx context.Context, request AccountRequest) ([]DraftSummary, error) {
	r := s.Runtime
	r.composeMu.Lock()
	defer r.composeMu.Unlock()
	rows, err := r.DB.ComposeDrafts(ctx, request.AccountID)
	if err != nil {
		return nil, errors.New("cannot list local drafts")
	}
	out := []DraftSummary{}
	if len(rows) == 0 {
		return out, nil
	}
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		d, err := decodeLocalDraft(cipher, row)
		if err != nil {
			return nil, err
		}
		out = append(out, DraftSummary{ID: d.ID, AccountID: d.AccountID, Subject: d.Subject, UpdatedAt: d.UpdatedAt, AttachmentCount: len(d.Attachments)})
	}
	return out, nil
}
func (s *ComposeService) Delete(ctx context.Context, request DraftRequest) (Empty, error) {
	r := s.Runtime
	r.composeMu.Lock()
	defer r.composeMu.Unlock()
	if err := r.DB.DeleteComposeDraft(ctx, request.AccountID, request.DraftID); err != nil {
		return Empty{}, errors.New("cannot delete draft in selected account")
	}
	return Empty{}, nil
}
