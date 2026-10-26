package gmail

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/googleapi"
	"github.com/bary/pebble-go/internal/mailmime"
	"github.com/bary/pebble-go/internal/store"
)

const initialLimit = 100
const changeLimit = 200

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

func (p *Provider) Sync(ctx context.Context) (domain.SyncResult, error) {
	p.gate.Lock()
	defer p.gate.Unlock()
	cursor, err := p.DB.Cursor(ctx, p.AccountID)
	if err != nil {
		return domain.SyncResult{}, err
	}
	profile, err := p.API.Profile(ctx)
	if err != nil {
		return domain.SyncResult{}, err
	}
	if err := validateHistory(profile.HistoryID); err != nil {
		return domain.SyncResult{}, err
	}
	labels, err := p.API.Labels(ctx)
	if err != nil {
		return domain.SyncResult{}, err
	}
	initial := cursor == ""
	limited := false
	target := profile.HistoryID
	ids := map[string]bool{}
	if !initial {
		page := ""
		seen := map[string]bool{}
		for i := 0; i < 20; i++ {
			history, e := p.API.History(ctx, cursor, page)
			var httpErr *googleapi.HTTPError
			if errors.As(e, &httpErr) && httpErr.Status == 404 {
				initial = true
				ids = map[string]bool{}
				break
			}
			if e != nil {
				return domain.SyncResult{}, e
			}
			for _, entry := range history.Entries {
				for _, m := range entry.Messages {
					ids[m.ID] = true
				}
				for _, m := range entry.Added {
					ids[m.Message.ID] = true
				}
				for _, m := range entry.Deleted {
					ids[m.Message.ID] = true
				}
				for _, m := range entry.LabelsAdded {
					ids[m.Message.ID] = true
				}
				for _, m := range entry.LabelsRemoved {
					ids[m.Message.ID] = true
				}
			}
			if len(ids) > changeLimit {
				return domain.SyncResult{}, errors.New("Gmail change limit exceeded; cursor unchanged")
			}
			if history.Next == "" {
				if err := validateHistory(history.HistoryID); err != nil {
					return domain.SyncResult{}, err
				}
				target = history.HistoryID
				break
			}
			if seen[history.Next] || i == 19 {
				return domain.SyncResult{}, errors.New("Gmail history page loop or limit")
			}
			seen[history.Next] = true
			page = history.Next
		}
	}
	if initial {
		page := ""
		seen := map[string]bool{}
		for i := 0; i < 20; i++ {
			list, e := p.API.List(ctx, page, initialLimit-len(ids))
			if e != nil {
				return domain.SyncResult{}, e
			}
			for _, m := range list.Messages {
				if len(ids) >= initialLimit {
					limited = true
					break
				}
				ids[m.ID] = true
			}
			if list.Next == "" {
				break
			}
			if len(ids) >= initialLimit {
				limited = true
				break
			}
			if seen[list.Next] || i == 19 {
				return domain.SyncResult{}, errors.New("Gmail message page loop or limit")
			}
			seen[list.Next] = true
			page = list.Next
		}
		known, e := p.DB.KnownRemoteIDs(ctx, p.AccountID)
		if e != nil {
			return domain.SyncResult{}, e
		}
		for _, id := range known {
			ids[id] = true
		}
		if len(ids) > changeLimit {
			return domain.SyncResult{}, errors.New("Gmail cache reconciliation limit exceeded")
		}
	}
	snapshot := store.Snapshot{AccountID: p.AccountID, Email: profile.Email, Cursor: &target, Messages: []store.CachedMessage{}}
	for _, label := range labels {
		var role *string
		roles := map[string]string{"INBOX": "inbox", "SENT": "sent", "DRAFT": "drafts", "TRASH": "trash", "SPAM": "spam"}
		if value, ok := roles[label.ID]; ok {
			role = &value
		}
		snapshot.Folders = append(snapshot.Folders, domain.Folder{ID: store.ScopedID(p.AccountID, "label:"+label.ID), AccountID: p.AccountID, RemoteID: label.ID, Name: label.Name, FolderType: "label", Role: role, IsSystem: label.Type == "system"})
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		if id != "" {
			ordered = append(ordered, id)
		}
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		message, e := p.fetch(ctx, id)
		var httpErr *googleapi.HTTPError
		if errors.As(e, &httpErr) && httpErr.Status == 404 {
			snapshot.Removed = append(snapshot.Removed, store.ScopedID(p.AccountID, id))
			continue
		}
		if e != nil {
			return domain.SyncResult{}, e
		}
		snapshot.Messages = append(snapshot.Messages, message)
	}
	if err := p.DB.CommitSnapshot(ctx, snapshot); err != nil {
		return domain.SyncResult{}, err
	}
	return domain.SyncResult{Count: len(snapshot.Messages), HistoryID: target, Initial: initial, Limited: limited}, nil
}

func (p *Provider) Flags(ctx context.Context, id string, read, star *bool) error {
	p.gate.Lock()
	defer p.gate.Unlock()
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
