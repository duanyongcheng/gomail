package gmail

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/googleapi"
	"github.com/bary/pebble-go/internal/store"
)

const pageSize = 100
const pagesPerPass = 10
const maxPageTokens = 10000

func (p *Provider) Sync(ctx context.Context) (domain.SyncResult, error) {
	return p.SyncWithProgress(ctx, nil)
}

// Each page is a recovery boundary. A pass bounds memory and work; Limited
// tells the scheduler to continue promptly, without claiming the scan is done.
func (p *Provider) SyncWithProgress(ctx context.Context, progress func(domain.SyncProgress)) (result domain.SyncResult, err error) {
	p.gate.Lock()
	defer p.gate.Unlock()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	state, err := p.DB.GmailState(ctx, p.AccountID)
	if err != nil {
		return result, err
	}
	profile, err := p.API.Profile(ctx)
	if err != nil {
		return result, err
	}
	if err := validateHistory(profile.HistoryID); err != nil {
		return result, err
	}
	labels, err := p.API.Labels(ctx)
	if err != nil {
		return result, err
	}
	folders := []domain.Folder{}
	roles := map[string]string{"INBOX": "inbox", "SENT": "sent", "DRAFT": "drafts", "TRASH": "trash", "SPAM": "spam"}
	for _, label := range labels {
		var role *string
		if value, ok := roles[label.ID]; ok {
			role = &value
		}
		folders = append(folders, domain.Folder{ID: store.ScopedID(p.AccountID, "label:"+label.ID), AccountID: p.AccountID, RemoteID: label.ID, Name: label.Name, FolderType: "label", Role: role, IsSystem: label.Type == "system"})
	}
	checkpoint := state.Checkpoint
	reset := func() error {
		checkpoint = &store.GmailCheckpoint{Phase: "messages", HistoryID: profile.HistoryID, SuppressNotifications: true}
		return p.DB.CommitSnapshot(ctx, store.Snapshot{AccountID: p.AccountID, Folders: folders, Checkpoint: checkpoint, ResetBackfill: true})
	}
	if checkpoint == nil || state.Cursor == "" && checkpoint.Phase == "complete" {
		if err := reset(); err != nil {
			return result, err
		}
	} else if checkpoint.Phase == "complete" {
		checkpoint = &store.GmailCheckpoint{Phase: "history", HistoryID: state.Cursor}
	}
	result.Initial = checkpoint.SuppressNotifications
	result.HistoryID = state.Cursor
	counted := map[string]bool{}
	for page := 0; page < pagesPerPass; page++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := validateHistory(checkpoint.HistoryID); err != nil {
			return result, err
		}
		snapshot := store.Snapshot{AccountID: p.AccountID, Folders: folders}
		next := *checkpoint
		var ids []string
		switch checkpoint.Phase {
		case "messages":
			list, e := p.API.List(ctx, checkpoint.PageToken, pageSize)
			if isHTTP(e, 400) && checkpoint.PageToken != "" {
				if err := reset(); err != nil {
					return result, err
				}
				result.Limited = true
				return result, nil
			}
			if e != nil {
				return result, e
			}
			if len(list.Messages) > pageSize {
				return result, errors.New("Gmail message page exceeds requested size")
			}
			for _, ref := range list.Messages {
				ids = append(ids, ref.ID)
			}
			if e := advancePage(&next, list.Next); e != nil {
				return result, e
			}
			if list.Next == "" {
				next.Phase = "reconcile"
			}
		case "reconcile":
			ids, err = p.DB.BackfillPending(ctx, p.AccountID, pageSize)
			if err != nil {
				return result, err
			}
			if len(ids) == 0 {
				next = store.GmailCheckpoint{Phase: "history", HistoryID: checkpoint.HistoryID, SuppressNotifications: true}
				snapshot.Cursor = &next.HistoryID
			}
		case "history":
			history, e := p.API.History(ctx, checkpoint.HistoryID, checkpoint.PageToken)
			if isHTTP(e, 404) || isHTTP(e, 400) && checkpoint.PageToken != "" {
				if err := reset(); err != nil {
					return result, err
				}
				result.Initial = true
				continue
			}
			if e != nil {
				return result, e
			}
			if e := validateHistory(history.HistoryID); e != nil {
				return result, e
			}
			for _, entry := range history.Entries {
				for _, ref := range entry.Messages {
					ids = append(ids, ref.ID)
				}
				for _, ref := range entry.Added {
					ids = append(ids, ref.Message.ID)
				}
				for _, ref := range entry.Deleted {
					ids = append(ids, ref.Message.ID)
				}
				for _, ref := range entry.LabelsAdded {
					ids = append(ids, ref.Message.ID)
				}
				for _, ref := range entry.LabelsRemoved {
					ids = append(ids, ref.Message.ID)
				}
			}
			if e := advancePage(&next, history.Next); e != nil {
				return result, e
			}
			if history.Next == "" {
				next = store.GmailCheckpoint{Phase: "complete", HistoryID: history.HistoryID}
				snapshot.Cursor = &next.HistoryID
			}
		default:
			return result, errors.New("invalid Gmail sync checkpoint phase")
		}
		// Deduplicate a History page before fetching, while keeping memory
		// bounded by one server response rather than the entire mailbox.
		unique := map[string]bool{}
		for _, id := range ids {
			if id == "" {
				return result, errors.New("Gmail page contains an empty message ID")
			}
			unique[id] = true
		}
		ids = ids[:0]
		for id := range unique {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		newMail := []domain.NewMail{}
		for _, id := range ids {
			message, e := p.fetch(ctx, id)
			if isHTTP(e, 404) {
				snapshot.Removed = append(snapshot.Removed, store.ScopedID(p.AccountID, id))
			} else if e != nil {
				return result, e
			} else {
				if checkpoint.Phase == "history" && !checkpoint.SuppressNotifications && !message.Message.IsRead && !message.Message.IsDraft && uniqueInbox(message, p.AccountID) {
					_, e := p.DB.Message(ctx, message.Message.ID)
					if errors.Is(e, sql.ErrNoRows) {
						m := message.Message
						newMail = append(newMail, domain.NewMail{AccountID: p.AccountID, MessageID: m.ID, FolderIDs: message.FolderIDs, ThreadID: m.ThreadID, Subject: m.Subject, From: m.FromAddress, ReceivedAt: m.Date})
					} else if e != nil {
						return result, e
					}
				}
				snapshot.Messages = append(snapshot.Messages, message)
			}
			snapshot.ProcessedRemoteIDs = append(snapshot.ProcessedRemoteIDs, id)
		}
		snapshot.Checkpoint = &next
		if err := p.DB.CommitSnapshot(ctx, snapshot); err != nil {
			return result, err
		}
		for _, cached := range snapshot.Messages {
			counted[cached.Message.ID] = true
		}
		result.Count = len(counted)
		result.NewMessages = append(result.NewMessages, newMail...)
		if snapshot.Cursor != nil {
			result.HistoryID = *snapshot.Cursor
		}
		if progress != nil {
			progress(domain.SyncProgress{AccountID: p.AccountID, Status: "progress", Phase: checkpoint.Phase, Count: result.Count})
		}
		checkpoint = &next
		if checkpoint.Phase == "complete" {
			return result, nil
		}
	}
	result.Limited = true
	return result, nil
}

func advancePage(checkpoint *store.GmailCheckpoint, token string) error {
	if token != "" {
		if token == checkpoint.PageToken || len(checkpoint.PageTokens) >= maxPageTokens {
			return errors.New("Gmail pagination loop or safety limit")
		}
		for _, seen := range checkpoint.PageTokens {
			if seen == token {
				return errors.New("Gmail pagination loop")
			}
		}
		checkpoint.PageTokens = append(append([]string{}, checkpoint.PageTokens...), token)
	} else {
		checkpoint.PageTokens = nil
	}
	checkpoint.PageToken = token
	return nil
}

func isHTTP(err error, status int) bool {
	var remote *googleapi.HTTPError
	return errors.As(err, &remote) && remote.Status == status
}

func uniqueInbox(message store.CachedMessage, account string) bool {
	for _, folder := range message.FolderIDs {
		if folder == store.ScopedID(account, "label:INBOX") {
			return true
		}
	}
	return false
}
