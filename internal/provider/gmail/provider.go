package gmail

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/googleapi"
	"github.com/bary/pebble-go/internal/mailmime"
	"github.com/bary/pebble-go/internal/store"
)

type Provider struct {
	DB        *store.DB
	API       *googleapi.API
	AccountID string
	gate      sync.Mutex
}

func New(db *store.DB, api *googleapi.API, account string) *Provider {
	return &Provider{DB: db, API: api, AccountID: account}
}
func (p *Provider) Capabilities() domain.Capabilities {
	return domain.Capabilities{Read: true, ModifyFlags: true}
}
func (p *Provider) Folders(ctx context.Context) ([]domain.Folder, error) {
	return p.DB.Folders(ctx, p.AccountID)
}
func validateHistory(id string) error {
	if id == "" {
		return errors.New("empty Gmail historyId")
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return errors.New("invalid Gmail historyId")
		}
	}
	return nil
}

func (p *Provider) fetch(ctx context.Context, id string) (store.CachedMessage, error) {
	remote, err := p.API.Message(ctx, id)
	if err != nil {
		return store.CachedMessage{}, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(remote.Raw)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(remote.Raw)
	}
	if err != nil {
		return store.CachedMessage{}, errors.New("invalid Gmail RAW encoding")
	}
	parsed, err := mailmime.Parse(raw)
	if err != nil {
		return store.CachedMessage{}, err
	}
	m := domain.Message{ID: store.ScopedID(p.AccountID, id), AccountID: p.AccountID, RemoteID: id, MessageIDHeader: &parsed.MessageID, ThreadID: &remote.ThreadID, Subject: parsed.Subject, Snippet: parsed.Snippet, FromAddress: parsed.From.Address, To: []domain.Address{}, CC: []domain.Address{}, BCC: []domain.Address{}, BodyText: parsed.BodyText, BodyHTML: parsed.BodyHTML, IsRead: true, Date: parsed.Date, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
	if parsed.From.Name != nil {
		m.FromName = *parsed.From.Name
	}
	for _, a := range parsed.To {
		m.To = append(m.To, domain.Address{Name: a.Name, Address: a.Address})
	}
	if millis, err := strconv.ParseInt(remote.Date, 10, 64); err == nil {
		m.Date = millis / 1000
	}
	cached := store.CachedMessage{Message: m, FolderIDs: []string{}, Attachments: []domain.Attachment{}}
	for _, label := range remote.Labels {
		cached.FolderIDs = append(cached.FolderIDs, store.ScopedID(p.AccountID, "label:"+label))
		if label == "UNREAD" {
			cached.Message.IsRead = false
		}
		if label == "STARRED" {
			cached.Message.IsStarred = true
		}
		if label == "DRAFT" {
			cached.Message.IsDraft = true
		}
	}
	for index, a := range parsed.Attachments {
		cid := a.ContentID
		cached.Attachments = append(cached.Attachments, domain.Attachment{ID: fmt.Sprintf("%s:attachment:%d", m.ID, index), MessageID: m.ID, Filename: a.Filename, MIMEType: a.MIMEType, ContentID: &cid, Inline: a.Inline, Data: a.Data})
	}
	cached.Message.HasAttachments = len(cached.Attachments) > 0
	return cached, nil
}

func (p *Provider) Flags(ctx context.Context, id string, read, star *bool) error {
	p.gate.Lock()
	defer p.gate.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	m, err := p.DB.Message(ctx, id)
	if err != nil {
		return err
	}
	if m.AccountID != p.AccountID {
		return errors.New("message does not belong to account")
	}
	add, remove := []string{}, []string{}
	for _, flag := range []struct {
		value   *bool
		label   string
		inverse bool
	}{{read, "UNREAD", true}, {star, "STARRED", false}} {
		if flag.value == nil {
			continue
		}
		value := *flag.value
		if flag.inverse {
			value = !value
		}
		if value {
			add = append(add, flag.label)
		} else {
			remove = append(remove, flag.label)
		}
	}
	if len(add)+len(remove) == 0 {
		return nil
	}
	if err := p.API.Modify(ctx, m.RemoteID, add, remove); err != nil {
		return err
	}
	fresh, err := p.fetch(ctx, m.RemoteID)
	if err != nil {
		return errors.New("Gmail flags changed but cache refresh failed; synchronize before retry")
	}
	return p.DB.CommitSnapshot(ctx, store.Snapshot{AccountID: p.AccountID, Messages: []store.CachedMessage{fresh}})
}
