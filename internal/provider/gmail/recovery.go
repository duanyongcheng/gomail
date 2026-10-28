package gmail

import (
	"context"
	"database/sql"
	"errors"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/googleapi"
	"github.com/bary/pebble-go/internal/store"
)

const batchContentBytes = 8 << 20
const retriesPerPass = 10

// A page can contain many large messages. Commit bounded prefixes without
// advancing its checkpoint; a replay safely refreshes those committed prefixes.
type pageBatch struct {
	provider         *Provider
	ctx              context.Context
	folders          []domain.Folder
	snapshot         store.Snapshot
	announcements    []domain.NewMail
	bytes            int
	result           *domain.SyncResult
	counted          map[string]bool
	pendingWatermark int64
}

func (b *pageBatch) reset() {
	b.snapshot = store.Snapshot{AccountID: b.provider.AccountID, Folders: b.folders, PendingOpWatermark: &b.pendingWatermark}
	b.announcements = nil
	b.bytes = 0
}
func (b *pageBatch) flush(checkpoint *store.GmailCheckpoint, cursor *string) error {
	b.snapshot.Checkpoint, b.snapshot.Cursor = checkpoint, cursor
	if err := b.provider.DB.CommitSnapshot(b.ctx, b.snapshot); err != nil {
		return err
	}
	for _, cached := range b.snapshot.Messages {
		b.counted[cached.Message.ID] = true
	}
	b.result.Count = len(b.counted)
	b.result.NewMessages = append(b.result.NewMessages, b.announcements...)
	if cursor != nil {
		b.result.HistoryID = *cursor
	}
	b.reset()
	return nil
}
func cachedBytes(cached store.CachedMessage) int {
	m := cached.Message
	size := len(m.ID) + len(m.AccountID) + len(m.RemoteID) + len(m.BodyText) + len(m.BodyHTML) + len(m.Subject) + len(m.Snippet) + len(m.FromAddress) + len(m.FromName)
	for _, header := range []*string{m.MessageIDHeader, m.ThreadID, m.InReplyTo, m.ReferencesHeader} {
		if header != nil {
			size += len(*header)
		}
	}
	for _, addresses := range [][]domain.Address{m.To, m.CC, m.BCC} {
		for _, address := range addresses {
			size += len(address.Address)
			if address.Name != nil {
				size += len(*address.Name)
			}
		}
	}
	for _, folder := range cached.FolderIDs {
		size += len(folder)
	}
	for _, attachment := range cached.Attachments {
		size += len(attachment.Data) + len(attachment.ID) + len(attachment.Filename) + len(attachment.MIMEType)
		if attachment.ContentID != nil {
			size += len(*attachment.ContentID)
		}
	}
	return size
}

func (b *pageBatch) fetch(id string, notify, forceRetry bool) error {
	p := b.provider
	previous, err := p.DB.MessageFailure(b.ctx, p.AccountID, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if !forceRetry {
			// Existing failures are retried only by the bounded recovery batch.
			b.snapshot.ProcessedRemoteIDs = append(b.snapshot.ProcessedRemoteIDs, id)
			return nil
		}
		notify = previous.NotifyOnRecovery
	}
	cached, err := p.fetch(b.ctx, id)
	var content *googleapi.ContentError
	switch {
	case isHTTP(err, 404):
		b.snapshot.Removed = append(b.snapshot.Removed, store.ScopedID(p.AccountID, id))
		b.snapshot.RecoveredRemoteIDs = append(b.snapshot.RecoveredRemoteIDs, id)
	case errors.As(err, &content):
		b.snapshot.Failures = append(b.snapshot.Failures, store.MessageFailure{RemoteID: id, Kind: content.Kind, NotifyOnRecovery: notify})
	case err != nil:
		return err
	default:
		size := cachedBytes(cached)
		if b.bytes > 0 && b.bytes+size > batchContentBytes {
			if err := b.flush(nil, nil); err != nil {
				return err
			}
		}
		if notify && !cached.Message.IsRead && !cached.Message.IsDraft && uniqueInbox(cached, p.AccountID) {
			_, err := p.DB.Message(b.ctx, cached.Message.ID)
			if errors.Is(err, sql.ErrNoRows) {
				m := cached.Message
				b.announcements = append(b.announcements, domain.NewMail{AccountID: p.AccountID, MessageID: m.ID, FolderIDs: cached.FolderIDs, ThreadID: m.ThreadID, Subject: m.Subject, From: m.FromAddress, ReceivedAt: m.Date})
			} else if err != nil {
				return err
			}
		}
		b.snapshot.Messages = append(b.snapshot.Messages, cached)
		b.snapshot.RecoveredRemoteIDs = append(b.snapshot.RecoveredRemoteIDs, id)
		b.bytes += size
	}
	b.snapshot.ProcessedRemoteIDs = append(b.snapshot.ProcessedRemoteIDs, id)
	if b.bytes >= batchContentBytes {
		return b.flush(nil, nil)
	}
	return nil
}
