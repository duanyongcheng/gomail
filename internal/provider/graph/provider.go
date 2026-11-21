// Package graph implements the experimental Microsoft Graph mail transport.
package graph

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/mailmime"
	"github.com/duanyongcheng/pebble-go/internal/store"
)

const wireLimit = 50 << 20

type Provider struct {
	DB            *store.DB
	API           *API
	SendingAPI    *API
	AccountID     string
	Email         string
	Subject       string
	BeforeRequest func(context.Context) error
	gate          sync.Mutex
}

func New(db *store.DB, api *API, account, email string) *Provider {
	return &Provider{DB: db, API: api, AccountID: account, Email: email}
}

func (p *Provider) Capabilities() domain.Capabilities {
	return domain.Capabilities{Read: true, ModifyFlags: true, Send: true, HTML: true, FolderManagement: true}
}

func (p *Provider) folderID(id string) string { return store.ScopedID(p.AccountID, "graph-folder:"+id) }

// Folders fetches a complete bounded tree. An incomplete listing never replaces
// the authoritative cached tree and cannot authorize a destructive action.
func (p *Provider) Folders(ctx context.Context) ([]domain.Folder, error) {
	p.gate.Lock()
	defer p.gate.Unlock()
	return p.folders(ctx)
}

func (p *Provider) folders(ctx context.Context) ([]domain.Folder, error) {
	queue := []string{""}
	folders := []domain.Folder{}
	ids, links := map[string]bool{}, map[string]bool{}
	pages := 0
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		link := ""
		for {
			pages++
			if pages > 100 || len(folders) > 5000 {
				return nil, errors.New("Graph folder traversal exceeds safety limit")
			}
			page, err := p.API.FolderPage(ctx, parent, link)
			if err != nil {
				return nil, err
			}
			for _, f := range page.Folders {
				if f.ID == "" || ids[f.ID] {
					return nil, errors.New("invalid Graph folder tree")
				}
				ids[f.ID] = true
				var parentID *string
				if parent != "" {
					id := p.folderID(parent)
					parentID = &id
				}
				folders = append(folders, domain.Folder{ID: p.folderID(f.ID), AccountID: p.AccountID, RemoteID: f.ID, Name: f.DisplayName, FolderType: "folder", ParentID: parentID})
				if f.ChildFolderCount > 0 {
					queue = append(queue, f.ID)
				}
			}
			if page.NextLink == "" {
				break
			}
			if links[page.NextLink] {
				return nil, errors.New("Graph folder pagination loop")
			}
			links[page.NextLink] = true
			link = page.NextLink
		}
	}
	for alias, role := range map[string]string{"inbox": "inbox", "sentitems": "sent", "drafts": "drafts", "deleteditems": "trash", "junkemail": "spam", "archive": "archive"} {
		known, err := p.API.WellKnownFolder(ctx, alias)
		if err != nil {
			var remote *HTTPError
			if errors.As(err, &remote) && remote.Status == 404 {
				continue
			}
			return nil, err
		}
		for i := range folders {
			if folders[i].RemoteID == known.ID {
				value := role
				folders[i].Role = &value
				folders[i].IsSystem = true
			}
		}
	}
	old, err := p.DB.Folders(ctx, p.AccountID)
	if err != nil {
		return nil, err
	}
	removed := []string{}
	for _, f := range old {
		if !ids[f.RemoteID] {
			removed = append(removed, f.ID)
		}
	}
	if err := p.DB.CommitSnapshot(ctx, store.Snapshot{AccountID: p.AccountID, Folders: folders, RemovedFolderIDs: removed}); err != nil {
		return nil, err
	}
	return folders, nil
}

type checkpoint struct {
	Link     string   `json:"link"`
	Seen     []string `json:"seen,omitempty"`
	Complete bool     `json:"complete"`
	// Reset reconciliation retains old memberships until the replacement scan
	// reaches its final deltaLink; list absence alone is never a deletion.
	Reset     bool   `json:"reset,omitempty"`
	SeenScope string `json:"seen_scope,omitempty"`
	Reconcile bool   `json:"reconcile,omitempty"`
	After     string `json:"after,omitempty"`
}

func (p *Provider) load(ctx context.Context, scope string, out any) error {
	raw, err := p.DB.ProviderCheckpoint(ctx, p.AccountID, "graph", scope)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if json.Unmarshal(raw, out) != nil {
		return errors.New("invalid Graph checkpoint")
	}
	return nil
}

func state(scope string, value any) store.ProviderCheckpoint {
	raw, _ := json.Marshal(value)
	return store.ProviderCheckpoint{Provider: "graph", Scope: scope, Data: raw}
}

func (p *Provider) Sync(ctx context.Context) (domain.SyncResult, error) {
	return p.SyncWithProgress(ctx, nil)
}

func (p *Provider) SyncWithProgress(ctx context.Context, progress func(domain.SyncProgress)) (domain.SyncResult, error) {
	p.gate.Lock()
	defer p.gate.Unlock()
	result := domain.SyncResult{}
	folders, err := p.folders(ctx)
	if err != nil {
		return result, err
	}
	var position struct {
		Index int `json:"index"`
	}
	if err := p.load(ctx, "__position", &position); err != nil {
		return result, err
	}
	if position.Index < 0 || position.Index >= len(folders) {
		position.Index = 0
	}
	pages := 0
	start := position.Index
	for visited := 0; visited < len(folders); visited++ {
		index := (start + visited) % len(folders)
		folder := folders[index]
		var cp checkpoint
		if err := p.load(ctx, folder.RemoteID, &cp); err != nil {
			return result, err
		}
		for {
			if pages == 10 {
				result.Limited = true
				return result, nil
			}
			if cp.Reconcile {
				missing, err := p.DB.MissingProviderMappingsPage(ctx, p.AccountID, "graph", folder.RemoteID, cp.SeenScope, cp.After, 100)
				if err != nil {
					return result, err
				}
				s := store.Snapshot{AccountID: p.AccountID}
				for _, m := range missing {
					s.RemovedMemberships = append(s.RemovedMemberships, store.RemovedMembership{MessageID: m.MessageID, FolderID: folder.ID})
					m.Delete = true
					s.ProviderMappings = append(s.ProviderMappings, m)
					cp.After = m.RemoteID
				}
				if len(missing) < 100 {
					s.ProviderMappingScopesCleared = []store.ProviderMappingScope{{Provider: "graph", Scope: cp.SeenScope}}
					cp.Reset = false
					cp.Reconcile = false
					cp.SeenScope = ""
					cp.After = ""
					position.Index = (index + 1) % len(folders)
				}
				s.ProviderCheckpoints = []store.ProviderCheckpoint{state(folder.RemoteID, cp), state("__position", position)}
				if err := p.DB.CommitSnapshot(ctx, s); err != nil {
					return result, err
				}
				pages++
				if !cp.Reconcile {
					break
				}
				continue
			}
			watermark, err := p.DB.PendingOpWatermark(ctx)
			if err != nil {
				return result, err
			}
			page, err := p.API.Delta(ctx, folder.RemoteID, cp.Link)
			if errors.Is(err, ErrCursorExpired) && cp.Link != "" {
				cp = checkpoint{Reset: true, SeenScope: "reset:" + rand.Text()}
				if err := p.DB.CommitSnapshot(ctx, store.Snapshot{AccountID: p.AccountID, ProviderCheckpoints: []store.ProviderCheckpoint{state(folder.RemoteID, cp)}}); err != nil {
					return result, err
				}
				continue
			}
			if err != nil {
				return result, err
			}
			pages++
			next := cp
			if page.NextLink != "" {
				digest := fmt.Sprintf("%x", sha256.Sum256([]byte(page.NextLink)))
				if page.NextLink == cp.Link || contains(cp.Seen, digest) || contains(cp.Seen, page.NextLink) || len(cp.Seen) >= 10000 {
					return result, errors.New("Graph delta pagination loop")
				}
				next.Link = page.NextLink
				next.Seen = append(append([]string{}, cp.Seen...), digest)
				next.Complete = false
			} else {
				if page.DeltaLink == "" {
					return result, errors.New("Graph delta boundary missing")
				}
				next.Link = page.DeltaLink
				next.Seen = nil
				next.Complete = true
			}
			snapshot := store.Snapshot{AccountID: p.AccountID, PendingOpWatermark: &watermark}
			for _, change := range page.Messages {
				if change.ID == "" {
					return result, errors.New("Graph message identity missing")
				}
				id := store.ScopedID(p.AccountID, change.ID)
				mapping := store.ProviderMapping{Provider: "graph", Scope: folder.RemoteID, RemoteID: change.ID, MessageID: id, Delete: change.Removed}
				snapshot.ProviderMappings = append(snapshot.ProviderMappings, mapping)
				if cp.Reset && !change.Removed {
					seen := mapping
					seen.Scope = cp.SeenScope
					snapshot.ProviderMappings = append(snapshot.ProviderMappings, seen)
				}
				if change.Removed {
					snapshot.RemovedMemberships = append(snapshot.RemovedMemberships, store.RemovedMembership{MessageID: id, FolderID: folder.ID})
					continue
				}
				cached, err := p.fetch(ctx, change, folder)
				if err != nil {
					return result, err
				}
				snapshot.Messages = append(snapshot.Messages, cached)
				// Retain at most one large MIME message, or 8 MiB of hydrated data.
				// Prefix commits never advance the page boundary; replay is idempotent.
				if batchSize(snapshot.Messages) >= 8<<20 {
					if err := p.DB.CommitSnapshot(ctx, snapshot); err != nil {
						return result, err
					}
					result.Count += len(snapshot.Messages)
					snapshot = store.Snapshot{AccountID: p.AccountID, PendingOpWatermark: &watermark}
				}
			}
			if next.Complete && next.Reset {
				next.Reconcile = true
			}
			position.Index = index
			if next.Complete && !next.Reconcile {
				position.Index = (index + 1) % len(folders)
			}
			snapshot.ProviderCheckpoints = []store.ProviderCheckpoint{state(folder.RemoteID, next), state("__position", position)}
			if err := p.DB.CommitSnapshot(ctx, snapshot); err != nil {
				return result, err
			}
			result.Count += len(snapshot.Messages)
			if progress != nil {
				progress(domain.SyncProgress{AccountID: p.AccountID, Status: "syncing", Phase: "graph_delta", Count: result.Count})
			}
			cp = next
			if next.Complete && !next.Reconcile {
				break
			}
		}
	}
	return result, nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func without(values []string, value string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v != value {
			out = append(out, v)
		}
	}
	return out
}
func batchSize(messages []store.CachedMessage) int {
	size := 0
	for _, m := range messages {
		size += len(m.Message.BodyText) + len(m.Message.BodyHTML)
		for _, a := range m.Attachments {
			size += len(a.Data)
		}
	}
	return size
}

func (p *Provider) fetch(ctx context.Context, remote Message, folder domain.Folder) (store.CachedMessage, error) {
	raw, err := p.API.Raw(ctx, remote.ID)
	if err != nil {
		return store.CachedMessage{}, err
	}
	var parsed mailmime.Message
	draft := remote.IsDraft != nil && *remote.IsDraft
	if remote.IsDraft == nil {
		if existing, e := p.DB.Message(ctx, store.ScopedID(p.AccountID, remote.ID)); e == nil {
			draft = existing.IsDraft
		}
	}
	if draft {
		parsed, err = mailmime.ParseDraftWithLimit(raw, wireLimit)
	} else {
		parsed, err = mailmime.ParseWithLimit(raw, wireLimit)
	}
	if err != nil {
		return store.CachedMessage{}, errors.New("invalid or oversized Graph MIME")
	}
	now := time.Now().Unix()
	id := store.ScopedID(p.AccountID, remote.ID)
	m := domain.Message{ID: id, AccountID: p.AccountID, RemoteID: remote.ID, Subject: parsed.Subject, Snippet: parsed.Snippet, MessageIDHeader: &parsed.MessageID, InReplyTo: &parsed.InReplyTo, ReferencesHeader: &parsed.References, FromAddress: parsed.From.Address, BodyText: parsed.BodyText, BodyHTML: parsed.BodyHTML, Date: parsed.Date, CreatedAt: now, UpdatedAt: now, To: []domain.Address{}, CC: []domain.Address{}, BCC: []domain.Address{}}
	if parsed.From.Name != nil {
		m.FromName = *parsed.From.Name
	}
	if existing, e := p.DB.Message(ctx, id); e == nil {
		m.IsRead = existing.IsRead
		m.IsStarred = existing.IsStarred
		m.IsDraft = existing.IsDraft
		m.CreatedAt = existing.CreatedAt
	}
	if remote.IsRead != nil {
		m.IsRead = *remote.IsRead
	}
	if remote.IsDraft != nil {
		m.IsDraft = *remote.IsDraft
	}
	if remote.Flag != nil {
		m.IsStarred = remote.Flag.FlagStatus == "flagged"
	}
	for _, pair := range []struct {
		source []mailmime.Address
		target *[]domain.Address
	}{{parsed.To, &m.To}, {parsed.CC, &m.CC}, {parsed.BCC, &m.BCC}} {
		for _, a := range pair.source {
			*pair.target = append(*pair.target, domain.Address{Name: a.Name, Address: a.Address})
		}
	}
	cached := store.CachedMessage{Message: m, FolderIDs: []string{folder.ID}, Attachments: []domain.Attachment{}}
	for i, a := range parsed.Attachments {
		cid := a.ContentID
		cached.Attachments = append(cached.Attachments, domain.Attachment{ID: fmt.Sprintf("%s:attachment:%d", id, i), MessageID: id, Filename: a.Filename, MIMEType: a.MIMEType, ContentID: &cid, Inline: a.Inline, Data: a.Data})
	}
	cached.Message.HasAttachments = len(cached.Attachments) > 0
	return cached, nil
}

func (p *Provider) Flags(ctx context.Context, id string, read, star *bool) error {
	return errors.New("Graph flags require durable pending-operation replay")
}

// Replay is called only after the app has durably claimed the pending intent.
func (p *Provider) Replay(ctx context.Context, operation string, raw json.RawMessage) (unknown bool, replayErr error) {
	p.gate.Lock()
	defer p.gate.Unlock()
	defer func() {
		var remote *HTTPError
		if !unknown && errors.As(replayErr, &remote) {
			switch remote.Status {
			case 401:
				replayErr = &domain.AccessError{State: domain.ConnectionReauthRequired}
			case 403:
				replayErr = &domain.AccessError{State: domain.ConnectionPermissionRequired}
			}
		}
	}()
	switch operation {
	case "update_flags", "archive", "unarchive", "restore", "delete", "move_to_folder":
	default:
		return false, errors.New("unsupported Graph pending operation")
	}
	var payload struct {
		RemoteID       string   `json:"remote_id"`
		IsRead         *bool    `json:"is_read"`
		IsStarred      *bool    `json:"is_starred"`
		TargetRemoteID string   `json:"target_remote_id"`
		AddLabels      []string `json:"add_labels"`
		RemoveLabels   []string `json:"remove_labels"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.RemoteID == "" {
		return false, errors.New("invalid Graph pending intent")
	}
	m, err := p.DB.Message(ctx, store.ScopedID(p.AccountID, payload.RemoteID))
	if err != nil || m.AccountID != p.AccountID {
		return false, errors.New("Graph message does not belong to account")
	}
	if operation == "update_flags" {
		for _, v := range payload.AddLabels {
			if v == "UNREAD" {
				x := false
				payload.IsRead = &x
			}
			if v == "STARRED" {
				x := true
				payload.IsStarred = &x
			}
		}
		for _, v := range payload.RemoveLabels {
			if v == "UNREAD" {
				x := true
				payload.IsRead = &x
			}
			if v == "STARRED" {
				x := false
				payload.IsStarred = &x
			}
		}
		if payload.IsRead == nil && payload.IsStarred == nil {
			return false, errors.New("empty Graph flag intent")
		}
		return p.API.PatchFlags(ctx, payload.RemoteID, payload.IsRead, payload.IsStarred)
	}
	destination := payload.TargetRemoteID
	if destination == "" {
		switch operation {
		case "delete":
			destination = "deleteditems"
		case "restore", "unarchive":
			destination = "inbox"
		case "archive":
			destination = "archive"
		}
	}
	if destination == "" {
		return false, errors.New("Graph move destination missing")
	}
	_, unknown, replayErr = p.API.Move(ctx, payload.RemoteID, destination)
	return unknown, replayErr
}

// ChangeFolder's caller owns the durable journal; the callback is the final
// local barrier before a non-idempotent remote folder request.
func (p *Provider) ChangeFolder(ctx context.Context, action, source, name string, beforeWrite func() error) (bool, error) {
	p.gate.Lock()
	defer p.gate.Unlock()
	if action != "create" && action != "rename" && action != "delete" {
		return false, errors.New("invalid Graph folder action")
	}
	if action != "delete" && (strings.TrimSpace(name) == "" || len(name) > 255 || strings.ContainsAny(name, "\r\n\x00")) {
		return false, errors.New("invalid Graph folder name")
	}
	folders, err := p.folders(ctx)
	if err != nil {
		return false, err
	}
	if action != "create" {
		found := false
		for _, f := range folders {
			if f.RemoteID == source && !f.IsSystem {
				found = true
			}
		}
		if !found {
			return false, errors.New("Graph folder cannot be changed")
		}
	}
	if beforeWrite == nil {
		return false, errors.New("Graph folder durable barrier required")
	}
	if err := beforeWrite(); err != nil {
		return false, err
	}
	switch action {
	case "create":
		_, u, e := p.API.CreateFolder(ctx, "", name)
		return u, e
	case "rename":
		return p.API.RenameFolder(ctx, source, name)
	default:
		return p.API.DeleteFolder(ctx, source)
	}
}
