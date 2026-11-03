package imap

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/mailmime"
	"github.com/bary/pebble-go/internal/store"
	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const PageUIDs uint32 = 8
const PagesPerPass = 10
const MaxMessageBytes = 25 << 20

type checkpoint struct {
	Validity      uint32 `json:"uid_validity"`
	Next          uint32 `json:"next_uid"`
	Refresh       uint32 `json:"refresh_uid"`
	Backfilled    bool   `json:"backfilled"`
	Retire        bool   `json:"retire,omitempty"`
	RetireAfter   string `json:"retire_after,omitempty"`
	RetireMailbox string `json:"retire_mailbox,omitempty"`
}
type traversal struct {
	Folder int `json:"folder"`
}

func (p *Provider) loadCheckpoint(ctx context.Context, scope string, out any) error {
	b, err := p.DB.ProviderCheckpoint(ctx, p.AccountID, "imap", scope)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func cpRecord(scope string, value any) store.ProviderCheckpoint {
	return store.ProviderCheckpoint{Provider: "imap", Scope: scope, Data: encode(value)}
}

func (p *Provider) Sync(ctx context.Context) (domain.SyncResult, error) {
	return p.SyncWithProgress(ctx, nil)
}
func (p *Provider) SyncWithProgress(ctx context.Context, progress func(domain.SyncProgress)) (domain.SyncResult, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	p.gate.Lock()
	defer p.gate.Unlock()
	var result domain.SyncResult
	c, close, err := p.client(ctx, nil)
	if err != nil {
		return result, err
	}
	defer close()
	watermark, err := p.DB.PendingOpWatermark(ctx)
	if err != nil {
		return result, err
	}
	folders, err := p.list(ctx, c)
	if err != nil {
		return result, err
	}
	if err = p.DB.ReconcileProtocolFolders(ctx, p.AccountID, folders); err != nil {
		return result, err
	}
	available := map[string]bool{}
	for _, f := range folders {
		available[f.RemoteID] = true
	}
	var walk traversal
	if err = p.loadCheckpoint(ctx, "traversal", &walk); err != nil {
		return result, err
	}
	if walk.Folder < 0 || walk.Folder >= len(p.Config.SelectedFolders) {
		walk.Folder = 0
	}
	for page := 0; page < PagesPerPass; page++ {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		mailbox := p.Config.SelectedFolders[walk.Folder]
		if !available[mailbox] {
			return result, errors.New("selected IMAP mailbox unavailable; cached mail preserved")
		}
		selected, e := c.Select(mailbox, &protocol.SelectOptions{ReadOnly: true}).Wait()
		if e != nil {
			return result, errors.New("IMAP mailbox selection failed")
		}
		if selected.UIDValidity == 0 || selected.UIDNext == 0 {
			return result, errors.New("IMAP mailbox identity unavailable")
		}
		var state checkpoint
		if err = p.loadCheckpoint(ctx, "folder:"+mailbox, &state); err != nil {
			return result, err
		}
		if state.Validity == 0 {
			result.Initial = true
		}
		if state.Validity != selected.UIDValidity {
			// Old identities are retired in bounded local pages before the new epoch.
			old := state
			state = checkpoint{Validity: selected.UIDValidity, Next: 1, Refresh: 1}
			state.Retire = old.Validity != 0
			if old.RetireMailbox != "" {
				state.Retire = true
				state.RetireMailbox = old.RetireMailbox
			}
		}
		snapshot := store.Snapshot{AccountID: p.AccountID, PendingOpWatermark: &watermark}
		announcements := []domain.NewMail{}
		complete := false
		if state.Retire {
			retireMailbox := mailbox
			if state.RetireMailbox != "" {
				retireMailbox = state.RetireMailbox
			}
			prefix := "imap:" + base64.RawURLEncoding.EncodeToString([]byte(retireMailbox)) + ":"
			ids, e := p.DB.RemoteIDsPage(ctx, p.AccountID, prefix, state.RetireAfter, 64)
			if e != nil {
				return result, e
			}
			for _, id := range ids {
				_, validity, _, e := ParseRemoteID(id)
				if e != nil {
					return result, e
				}
				if retireMailbox != mailbox || validity != state.Validity {
					snapshot.Removed = append(snapshot.Removed, store.ScopedID(p.AccountID, id))
				}
				state.RetireAfter = id
			}
			if len(ids) < 64 {
				state.Retire = false
				state.RetireAfter = ""
				state.RetireMailbox = ""
			}
		} else {
			incremental := state.Next < uint32(selected.UIDNext)
			start := state.Next
			if !incremental {
				start = state.Refresh
			}
			if start == 0 {
				start = 1
			}
			until := uint32(selected.UIDNext)
			if start < until {
				end := uint32(min(uint64(start)+uint64(PageUIDs)-1, uint64(until)-1))
				messages, seen, failures, e := p.fetchPage(ctx, c, mailbox, state.Validity, start, end)
				if e != nil {
					return result, e
				}
				snapshot.Messages = messages
				snapshot.Failures = failures
				for uid := uint64(start); uid <= uint64(end); uid++ {
					remote := RemoteID(mailbox, state.Validity, uint32(uid))
					if !seen[uint32(uid)] {
						snapshot.Removed = append(snapshot.Removed, store.ScopedID(p.AccountID, remote))
					}
				}
				for _, m := range messages {
					snapshot.RecoveredRemoteIDs = append(snapshot.RecoveredRemoteIDs, m.Message.RemoteID)
					if state.Backfilled && incremental && strings.EqualFold(mailbox, "INBOX") && !m.Message.IsRead {
						if _, err := p.DB.Message(ctx, m.Message.ID); errors.Is(err, sql.ErrNoRows) {
							value := m.Message
							snapshotNew := domain.NewMail{AccountID: p.AccountID, MessageID: value.ID, FolderIDs: m.FolderIDs, ThreadID: value.ThreadID, Subject: value.Subject, From: value.FromAddress, ReceivedAt: value.Date}
							// Publish only after this page's transaction succeeds below.
							announcements = append(announcements, snapshotNew)
						} else if err != nil {
							return result, err
						}
					}
				}
				if incremental {
					state.Next = end + 1
				} else {
					state.Refresh = end + 1
				}
			}
			if state.Next >= until && (until <= 1 || (!incremental && state.Refresh >= until)) {
				state.Refresh = 1
				complete = true
			}
			// A freshly completed backfill needs a bounded flags/deletion sweep next.
			if incremental && state.Next >= until {
				complete = true
				state.Refresh = 1
			}
			if complete {
				state.Backfilled = true
			}
		}
		if complete {
			walk.Folder++
		}
		all := walk.Folder >= len(p.Config.SelectedFolders)
		if all {
			walk.Folder = 0
		}
		snapshot.ProviderCheckpoints = []store.ProviderCheckpoint{cpRecord("folder:"+mailbox, state), cpRecord("traversal", walk)}
		if err = p.DB.CommitSnapshot(ctx, snapshot); err != nil {
			return result, err
		}
		result.NewMessages = append(result.NewMessages, announcements...)
		result.Count += len(snapshot.Messages)
		result.DeferredCount += len(snapshot.Failures)
		if progress != nil {
			progress(domain.SyncProgress{AccountID: p.AccountID, Status: "syncing", Phase: "imap", Count: result.Count, Message: "Synchronizing selected folders"})
		}
		if all {
			return result, nil
		}
	}
	result.Limited = true
	return result, nil
}

// Streaming literals are limited before allocation, even if the server ignores
// BODY.PEEK's requested partial size or advertises a dishonest RFC822.SIZE.
func (p *Provider) fetchPage(ctx context.Context, c *imapclient.Client, mailbox string, validity, start, end uint32) ([]store.CachedMessage, map[uint32]bool, []store.MessageFailure, error) {
	set := protocol.UIDSet{{Start: protocol.UID(start), Stop: protocol.UID(end)}}
	command := c.Fetch(set, &protocol.FetchOptions{UID: true, RFC822Size: true})
	metadata := []*imapclient.FetchMessageBuffer{}
	for data := command.Next(); data != nil; data = command.Next() {
		if len(metadata) >= int(PageUIDs) {
			_ = c.Close()
			return nil, nil, nil, errors.New("IMAP metadata exceeds page bound")
		}
		item := &imapclient.FetchMessageBuffer{}
		for field := data.Next(); field != nil; field = data.Next() {
			switch value := field.(type) {
			case imapclient.FetchItemDataUID:
				item.UID = value.UID
			case imapclient.FetchItemDataRFC822Size:
				item.RFC822Size = value.Size
			case imapclient.FetchItemDataBodySection, imapclient.FetchItemDataBinarySection:
				_ = c.Close()
				return nil, nil, nil, errors.New("unexpected IMAP metadata literal")
			}
		}
		metadata = append(metadata, item)
	}
	if err := command.Close(); err != nil {
		return nil, nil, nil, errors.New("IMAP metadata fetch failed")
	}
	seen := map[uint32]bool{}
	messages := []store.CachedMessage{}
	failures := []store.MessageFailure{}
	for _, item := range metadata {
		uid := uint32(item.UID)
		if uid < start || uid > end || seen[uid] || item.RFC822Size < 0 {
			return nil, nil, nil, errors.New("invalid IMAP metadata page")
		}
		seen[uid] = true
		if item.RFC822Size > MaxMessageBytes {
			failures = append(failures, store.MessageFailure{RemoteID: RemoteID(mailbox, validity, uid), Kind: "too_large"})
			continue
		}
		cached, found, deferred, err := p.fetchBodies(ctx, c, mailbox, validity, uid, uid)
		if err != nil {
			return nil, nil, nil, err
		}
		// A UID that vanished between metadata and body reads has authoritative
		// absence evidence from the successful second UID FETCH.
		if !found[uid] {
			delete(seen, uid)
		}
		messages = append(messages, cached...)
		failures = append(failures, deferred...)
	}
	return messages, seen, failures, nil
}

func (p *Provider) fetchBodies(ctx context.Context, c *imapclient.Client, mailbox string, validity, start, end uint32) ([]store.CachedMessage, map[uint32]bool, []store.MessageFailure, error) {
	section := &protocol.FetchItemBodySection{Peek: true, Partial: &protocol.SectionPartial{Size: MaxMessageBytes + 1}}
	set := protocol.UIDSet{{Start: protocol.UID(start), Stop: protocol.UID(end)}}
	cmd := c.Fetch(set, &protocol.FetchOptions{UID: true, Flags: true, RFC822Size: true, BodySection: []*protocol.FetchItemBodySection{section}})
	seen := map[uint32]bool{}
	messages := []store.CachedMessage{}
	failures := []store.MessageFailure{}
	for data := cmd.Next(); data != nil; data = cmd.Next() {
		var uid uint32
		var flags []protocol.Flag
		var raw []byte
		var size int64
		body := false
		for item := data.Next(); item != nil; item = data.Next() {
			switch item := item.(type) {
			case imapclient.FetchItemDataUID:
				uid = uint32(item.UID)
			case imapclient.FetchItemDataFlags:
				flags = item.Flags
			case imapclient.FetchItemDataRFC822Size:
				size = item.Size
			case imapclient.FetchItemDataBodySection:
				if body || !item.MatchCommand(section) {
					_ = c.Close()
					return nil, nil, nil, errors.New("unexpected IMAP body section")
				}
				body = true
				var err error
				raw, err = io.ReadAll(io.LimitReader(item.Literal, MaxMessageBytes+1))
				if err != nil {
					_ = c.Close()
					return nil, nil, nil, errors.New("IMAP message read failed")
				}
				if len(raw) > MaxMessageBytes {
					_ = c.Close()
					return nil, nil, nil, errors.New("IMAP literal exceeds configured bound")
				}
			}
		}
		if uid < start || uid > end || seen[uid] || !body {
			_ = c.Close()
			return nil, nil, nil, errors.New("invalid IMAP UID page")
		}
		seen[uid] = true
		remote := RemoteID(mailbox, validity, uid)
		if size > MaxMessageBytes {
			failures = append(failures, store.MessageFailure{RemoteID: remote, Kind: "too_large"})
			continue
		}
		cached, err := p.parse(raw, mailbox, remote, flags)
		if err != nil {
			failures = append(failures, store.MessageFailure{RemoteID: remote, Kind: "invalid_content"})
			continue
		}
		messages = append(messages, cached)
		if ctx.Err() != nil {
			_ = c.Close()
			return nil, nil, nil, ctx.Err()
		}
	}
	if err := cmd.Close(); err != nil {
		return nil, nil, nil, errors.New("IMAP fetch failed; checkpoint preserved")
	}
	return messages, seen, failures, nil
}

func (p *Provider) parse(raw []byte, mailbox, remote string, flags []protocol.Flag) (store.CachedMessage, error) {
	parsed, err := mailmime.ParseWithLimit(raw, MaxMessageBytes)
	if err != nil {
		return store.CachedMessage{}, err
	}
	m := domain.Message{ID: store.ScopedID(p.AccountID, remote), AccountID: p.AccountID, RemoteID: remote, MessageIDHeader: &parsed.MessageID, InReplyTo: &parsed.InReplyTo, ReferencesHeader: &parsed.References, Subject: parsed.Subject, Snippet: parsed.Snippet, FromAddress: parsed.From.Address, To: []domain.Address{}, CC: []domain.Address{}, BCC: []domain.Address{}, BodyText: parsed.BodyText, BodyHTML: parsed.BodyHTML, Date: parsed.Date, IsRead: slices.Contains(flags, protocol.FlagSeen), IsStarred: slices.Contains(flags, protocol.FlagFlagged), IsDraft: slices.Contains(flags, protocol.FlagDraft), CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
	if parsed.From.Name != nil {
		m.FromName = *parsed.From.Name
	}
	for _, a := range parsed.To {
		m.To = append(m.To, domain.Address{Name: a.Name, Address: a.Address})
	}
	for _, a := range parsed.CC {
		m.CC = append(m.CC, domain.Address{Name: a.Name, Address: a.Address})
	}
	for _, a := range parsed.BCC {
		m.BCC = append(m.BCC, domain.Address{Name: a.Name, Address: a.Address})
	}
	folderID := p.folderIDs[mailbox]
	if folderID == "" {
		folderID = FolderID(p.AccountID, mailbox)
	}
	cached := store.CachedMessage{Message: m, FolderIDs: []string{folderID}, Attachments: []domain.Attachment{}}
	for i, a := range parsed.Attachments {
		cid := a.ContentID
		cached.Attachments = append(cached.Attachments, domain.Attachment{ID: fmt.Sprintf("%s:attachment:%d", m.ID, i), MessageID: m.ID, Filename: a.Filename, MIMEType: a.MIMEType, ContentID: &cid, Inline: a.Inline, Data: a.Data})
	}
	cached.Message.HasAttachments = len(cached.Attachments) > 0
	return cached, nil
}
