package gmail

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/duanyongcheng/gomail/internal/mailcore"
	"github.com/egoist/mygo/plugins/sqlite"
)

const AccountID = "poc-account"
const InitialLimit = 100
const ChangeLimit = 200

type Core struct {
	DB            *sqlite.DB
	API           *API
	ModifyEnabled bool
	gate          sync.Mutex
}
type SyncResult struct {
	Count     int    `json:"count"`
	HistoryID string `json:"history_id"`
	Initial   bool   `json:"initial"`
	Limited   bool   `json:"limited"`
}
type fetched struct {
	api  Message
	mail mailcore.Message
}

func New(ctx context.Context, db *sqlite.DB, api *API, modify bool) (*Core, error) {
	for _, sql := range []string{
		"CREATE TABLE IF NOT EXISTS gmail_cursor(account_id TEXT PRIMARY KEY,history_id TEXT NOT NULL)",
		"CREATE VIRTUAL TABLE IF NOT EXISTS protocol_search USING fts5(message_id UNINDEXED,account_id UNINDEXED,subject,body,from_address,tokenize='trigram')",
		"CREATE TABLE IF NOT EXISTS protocol_attachment_data(attachment_id TEXT PRIMARY KEY REFERENCES attachments(id) ON DELETE CASCADE,data BLOB NOT NULL)",
	} {
		if _, err := db.Execute(ctx, sql); err != nil {
			return nil, err
		}
	}
	return &Core{DB: db, API: api, ModifyEnabled: modify}, nil
}
func (c *Core) Cursor(ctx context.Context) (string, error) {
	r, err := c.DB.Query(ctx, "SELECT history_id FROM gmail_cursor WHERE account_id=?", AccountID)
	if err != nil || len(r.Values) == 0 {
		return "", err
	}
	return r.Values[0][0].(string), nil
}
func (c *Core) fetch(ctx context.Context, id string) (fetched, error) {
	m, err := c.API.Message(ctx, id)
	if err != nil {
		return fetched{}, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(m.Raw)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(m.Raw)
	}
	if err != nil {
		return fetched{}, errors.New("Gmail raw message invalid")
	}
	parsed, err := mailcore.Parse(raw)
	if err != nil {
		return fetched{}, err
	}
	parsed.ID = "gmail:" + id
	parsed.AccountID = AccountID
	parsed.IsRead = true
	for _, label := range m.Labels {
		if label == "UNREAD" {
			parsed.IsRead = false
		}
		if label == "STARRED" {
			parsed.IsStarred = true
		}
	}
	if millis, err := strconv.ParseInt(m.Date, 10, 64); err == nil {
		parsed.Date = millis / 1000
	}
	return fetched{m, parsed}, nil
}
func (c *Core) Sync(ctx context.Context) (SyncResult, error) {
	c.gate.Lock()
	defer c.gate.Unlock()
	cursor, err := c.Cursor(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	profile, err := c.API.Profile(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	labels, err := c.API.Labels(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	if err := validateHistory(profile.HistoryID); err != nil {
		return SyncResult{}, err
	}
	initial := cursor == ""
	limited := false
	target := profile.HistoryID
	ids := map[string]bool{}
	if !initial {
		page := ""
		seen := map[string]bool{}
		for i := 0; i < 20; i++ {
			p, e := c.API.History(ctx, cursor, page)
			var httpErr *HTTPError
			if errors.As(e, &httpErr) && httpErr.Status == 404 {
				initial = true
				ids = map[string]bool{}
				break
			}
			if e != nil {
				return SyncResult{}, e
			}
			for _, entry := range p.Entries {
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
			if len(ids) > ChangeLimit {
				return SyncResult{}, errors.New("Gmail history exceeds prototype bound; cursor unchanged")
			}
			if p.Next == "" {
				if p.HistoryID == "" {
					return SyncResult{}, errors.New("terminal Gmail history cursor missing")
				}
				target = p.HistoryID
				break
			}
			if seen[p.Next] || i == 19 {
				return SyncResult{}, errors.New("Gmail history page loop/limit")
			}
			seen[p.Next] = true
			page = p.Next
		}
	}
	if initial {
		page := ""
		seen := map[string]bool{}
		for i := 0; i < 20; i++ {
			p, e := c.API.List(ctx, page, InitialLimit-len(ids))
			if e != nil {
				return SyncResult{}, e
			}
			for _, m := range p.Messages {
				if len(ids) >= InitialLimit {
					limited = true
					break
				}
				ids[m.ID] = true
			}
			if p.Next == "" {
				break
			}
			if len(ids) >= InitialLimit {
				limited = true
				break
			}
			if seen[p.Next] || i == 19 {
				return SyncResult{}, errors.New("Gmail message page loop/limit")
			}
			seen[p.Next] = true
			page = p.Next
		}
		// A bounded recent list is not a deletion snapshot. Re-fetch known cache
		// IDs on fallback so absent older mail is not blindly removed.
		known, e := c.DB.Query(ctx, "SELECT remote_id FROM messages WHERE account_id=? AND is_deleted=0", AccountID)
		if e != nil {
			return SyncResult{}, e
		}
		for _, row := range known.Values {
			ids[row[0].(string)] = true
		}
		if len(ids) > ChangeLimit {
			return SyncResult{}, errors.New("Gmail cache reconciliation exceeds bound")
		}
	}
	var messages []fetched
	var removed []string
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		if id != "" {
			ordered = append(ordered, id)
		}
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		m, e := c.fetch(ctx, id)
		var httpErr *HTTPError
		if errors.As(e, &httpErr) && httpErr.Status == 404 {
			removed = append(removed, "gmail:"+id)
			continue
		}
		if e != nil {
			return SyncResult{}, e
		}
		messages = append(messages, m)
	}
	stmts, err := c.statements(messages, removed, labels, profile, target, true)
	if err != nil {
		return SyncResult{}, err
	}
	if _, err = c.DB.Transaction(ctx, stmts); err != nil {
		return SyncResult{}, err
	}
	return SyncResult{len(messages), target, initial, limited}, nil
}
func (c *Core) Flags(ctx context.Context, id string, read, star *bool) error {
	if !c.ModifyEnabled {
		return errors.New("Gmail readonly mode; opt in modify scope and reauthorize")
	}
	c.gate.Lock()
	defer c.gate.Unlock()
	rows, err := c.DB.Query(ctx, "SELECT remote_id FROM messages WHERE id=? AND account_id=? AND is_deleted=0", id, AccountID)
	if err != nil {
		return err
	}
	if len(rows.Values) != 1 {
		return errors.New("Gmail message not cached")
	}
	remote := rows.Values[0][0].(string)
	add, remove := []string{}, []string{}
	for _, flag := range []struct {
		v      *bool
		label  string
		invert bool
	}{{read, "UNREAD", true}, {star, "STARRED", false}} {
		if flag.v == nil {
			continue
		}
		value := *flag.v
		if flag.invert {
			value = !value
		}
		if value {
			add = append(add, flag.label)
		} else {
			remove = append(remove, flag.label)
		}
	}
	if err = c.API.Modify(ctx, remote, add, remove); err != nil {
		return err
	}
	m, err := c.fetch(ctx, remote)
	if err != nil {
		return errors.New("Gmail modify sent but refresh failed; synchronize before retry")
	}
	stmts, err := c.statements([]fetched{m}, nil, nil, Profile{}, "", false)
	if err != nil {
		return err
	}
	_, err = c.DB.Transaction(ctx, stmts)
	return err
}
func (c *Core) Search(ctx context.Context, q, account string, limit int) ([]mailcore.SearchHit, error) {
	return (&mailcore.Core{DB: c.DB}).Search(ctx, q, account, limit)
}
func validateHistory(id string) error {
	if id == "" {
		return errors.New("empty Gmail historyId")
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return fmt.Errorf("invalid Gmail historyId")
		}
	}
	return nil
}
