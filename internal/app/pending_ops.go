package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/bary/pebble-go/internal/provider/gmail"
	"github.com/bary/pebble-go/internal/store"
)

func flagPayload(remote string, isRead, isStarred *bool) map[string]any {
	add, remove := gmail.LabelsForFlags(isRead, isStarred)
	return map[string]any{"remote_id": remote, "add_labels": add, "remove_labels": remove}
}

func labelPayload(remote string, add, remove []string) map[string]any {
	return map[string]any{"remote_id": remote, "add_labels": add, "remove_labels": remove}
}

// QueueFlags applies the local flag and persists the remote write atomically.
// Offline mail stays readable and the change survives restart.
func (s *MessageService) QueueFlags(ctx context.Context, request FlagsRequest) (Empty, error) {
	m, err := s.Runtime.DB.Message(ctx, request.MessageID)
	if err != nil {
		return Empty{}, err
	}
	if _, err := s.Runtime.provider(m.AccountID); err != nil {
		return Empty{}, err
	}
	op := store.PendingMailOpInput{AccountID: m.AccountID, MessageID: m.ID, OpType: "update_flags", Payload: flagPayload(m.RemoteID, request.IsRead, request.IsStarred)}
	change := store.LocalMailChange{MessageID: m.ID, IsRead: request.IsRead, IsStarred: request.IsStarred}
	err = s.Runtime.DB.QueueMessageOps(ctx, []store.PendingMailOpInput{op}, func(tx *sql.Tx) error {
		return store.ApplyLocalMailChanges(ctx, tx, []store.LocalMailChange{change})
	})
	if err == nil {
		s.Runtime.notifyPendingOps()
	}
	return Empty{}, err
}

type ArchiveRequest struct {
	MessageID string `json:"messageId"`
}
type MoveRequest struct {
	MessageID      string `json:"messageId"`
	TargetFolderID string `json:"targetFolderId"`
}
type BatchRequest struct {
	MessageIDs []string `json:"messageIds"`
	IsRead     *bool    `json:"isRead,omitempty"`
	Starred    *bool    `json:"starred,omitempty"`
}
type AllReadRequest struct {
	AccountID string   `json:"accountId"`
	FolderIDs []string `json:"folderIds"`
}

// Archive keeps upstream's toggle semantics: a message already in the inbox is
// archived, one outside it is restored.
func (s *MessageService) Archive(ctx context.Context, request ArchiveRequest) (string, error) {
	inInbox, err := s.Runtime.DB.MessageInFolderRemote(ctx, request.MessageID, "INBOX")
	if err != nil {
		return "", err
	}
	if inInbox {
		if err := s.queueFolderChange(ctx, request.MessageID, "archive"); err != nil {
			return "", err
		}
		return "archived", nil
	}
	if err := s.queueFolderChange(ctx, request.MessageID, "restore"); err != nil {
		return "", err
	}
	return "unarchived", nil
}
func (s *MessageService) Restore(ctx context.Context, request ArchiveRequest) (Empty, error) {
	return Empty{}, s.queueFolderChange(ctx, request.MessageID, "restore")
}
func (s *MessageService) Delete(ctx context.Context, request ArchiveRequest) (Empty, error) {
	return Empty{}, s.queueFolderChange(ctx, request.MessageID, "delete")
}

// Gmail modify scope cannot permanently delete mail.
func (s *MessageService) DeletePermanent(context.Context, ArchiveRequest) (Empty, error) {
	return Empty{}, errors.New("Permanent delete is not available with Gmail modify access")
}

func (s *MessageService) Move(ctx context.Context, request MoveRequest) (Empty, error) {
	m, err := s.Runtime.DB.Message(ctx, request.MessageID)
	if err != nil {
		return Empty{}, err
	}
	targetAccount, targetRemote, err := s.Runtime.DB.Folder(ctx, request.TargetFolderID)
	if err != nil {
		return Empty{}, err
	}
	if targetAccount != m.AccountID {
		return Empty{}, errors.New("target folder belongs to another account")
	}
	add, remove := gmail.MoveLabels("INBOX", targetRemote)
	if targetRemote == "INBOX" {
		add, remove = []string{"INBOX"}, nil
	}
	op := store.PendingMailOpInput{AccountID: m.AccountID, MessageID: m.ID, OpType: "move_to_folder", Payload: labelPayload(m.RemoteID, add, remove)}
	if err := s.Runtime.DB.QueueMessageOps(ctx, []store.PendingMailOpInput{op}, nil); err != nil {
		return Empty{}, err
	}
	s.Runtime.notifyPendingOps()
	return Empty{}, nil
}

func (s *MessageService) queueFolderChange(ctx context.Context, messageID, opType string) error {
	m, err := s.Runtime.DB.Message(ctx, messageID)
	if err != nil {
		return err
	}
	add, remove := gmail.ArchiveLabels()
	switch opType {
	case "restore":
		add, remove = gmail.RestoreLabels()
	case "delete":
		add, remove = gmail.TrashLabels()
	case "archive":
	default:
		return errors.New("unsupported folder change")
	}
	op := store.PendingMailOpInput{AccountID: m.AccountID, MessageID: m.ID, OpType: opType, Payload: labelPayload(m.RemoteID, add, remove)}
	if err := s.Runtime.DB.QueueMessageOps(ctx, []store.PendingMailOpInput{op}, nil); err != nil {
		return err
	}
	s.Runtime.notifyPendingOps()
	return nil
}

// BatchArchive and BatchDelete queue one row per message in a single
// transaction, so a partial failure cannot leave the selection half applied.
func (s *MessageService) BatchArchive(ctx context.Context, request BatchRequest) (int, error) {
	return s.queueBatchFolder(ctx, request.MessageIDs, "archive")
}
func (s *MessageService) BatchDelete(ctx context.Context, request BatchRequest) (int, error) {
	return s.queueBatchFolder(ctx, request.MessageIDs, "delete")
}

func (s *MessageService) queueBatchFolder(ctx context.Context, messageIDs []string, opType string) (int, error) {
	if len(messageIDs) == 0 || len(messageIDs) > 200 {
		return 0, errors.New("invalid batch size")
	}
	add, remove := gmail.ArchiveLabels()
	if opType == "delete" {
		add, remove = gmail.TrashLabels()
	}
	ops := make([]store.PendingMailOpInput, 0, len(messageIDs))
	for _, id := range messageIDs {
		m, err := s.Runtime.DB.Message(ctx, id)
		if err != nil {
			return 0, err
		}
		ops = append(ops, store.PendingMailOpInput{AccountID: m.AccountID, MessageID: m.ID, OpType: opType, Payload: labelPayload(m.RemoteID, add, remove)})
	}
	if err := s.Runtime.DB.QueueMessageOps(ctx, ops, nil); err != nil {
		return 0, err
	}
	s.Runtime.notifyPendingOps()
	return len(ops), nil
}

func (s *MessageService) BatchFlags(ctx context.Context, request BatchRequest) (int, error) {
	if len(request.MessageIDs) == 0 || len(request.MessageIDs) > 200 {
		return 0, errors.New("invalid batch size")
	}
	ops := make([]store.PendingMailOpInput, 0, len(request.MessageIDs))
	changes := make([]store.LocalMailChange, 0, len(request.MessageIDs))
	for _, id := range request.MessageIDs {
		m, err := s.Runtime.DB.Message(ctx, id)
		if err != nil {
			return 0, err
		}
		ops = append(ops, store.PendingMailOpInput{AccountID: m.AccountID, MessageID: m.ID, OpType: "update_flags", Payload: flagPayload(m.RemoteID, request.IsRead, request.Starred)})
		changes = append(changes, store.LocalMailChange{MessageID: m.ID, IsRead: request.IsRead, IsStarred: request.Starred})
	}
	err := s.Runtime.DB.QueueMessageOps(ctx, ops, func(tx *sql.Tx) error {
		return store.ApplyLocalMailChanges(ctx, tx, changes)
	})
	if err == nil && s.Runtime.OnPendingOpsChanged != nil {
		s.Runtime.OnPendingOpsChanged()
	}
	return len(ops), err
}

func (s *MessageService) MarkAccountAllRead(ctx context.Context, request AccountRequest) (int, error) {
	return s.markAllRead(ctx, request.AccountID, nil)
}
func (s *MessageService) MarkFolderAllRead(ctx context.Context, request AllReadRequest) (int, error) {
	if len(request.FolderIDs) == 0 {
		return 0, nil
	}
	for _, id := range request.FolderIDs {
		account, _, err := s.Runtime.DB.Folder(ctx, id)
		if err != nil {
			return 0, err
		}
		if account != request.AccountID {
			return 0, errors.New("folder belongs to another account")
		}
	}
	return s.markAllRead(ctx, request.AccountID, request.FolderIDs)
}

func (s *MessageService) markAllRead(ctx context.Context, account string, folderIDs []string) (int, error) {
	ids, err := s.Runtime.DB.UnreadMessageIDs(ctx, account, folderIDs)
	if err != nil {
		return 0, err
	}
	read := true
	ops := make([]store.PendingMailOpInput, 0, len(ids))
	changes := make([]store.LocalMailChange, 0, len(ids))
	for _, id := range ids {
		m, err := s.Runtime.DB.Message(ctx, id)
		if err != nil {
			return 0, err
		}
		ops = append(ops, store.PendingMailOpInput{AccountID: account, MessageID: id, OpType: "update_flags", Payload: flagPayload(m.RemoteID, &read, nil)})
		changes = append(changes, store.LocalMailChange{MessageID: id, IsRead: &read})
	}
	if len(ops) == 0 {
		return 0, nil
	}
	err = s.Runtime.DB.QueueMessageOps(ctx, ops, func(tx *sql.Tx) error {
		return store.ApplyLocalMailChanges(ctx, tx, changes)
	})
	if err == nil && s.Runtime.OnPendingOpsChanged != nil {
		s.Runtime.OnPendingOpsChanged()
	}
	return len(ops), err
}

// notifyPendingOps wakes the worker immediately instead of waiting for the
// polling tick. It never blocks the caller that just queued work.
func (r *Runtime) notifyPendingOps() {
	select {
	case r.pendingWake <- struct{}{}:
	default:
	}
	if r.OnPendingOpsChanged != nil {
		r.OnPendingOpsChanged()
	}
}

// PendingOpsService exposes the durable queue to the existing settings UI.
type PendingOpsService struct{ Runtime *Runtime }

type PendingOpsRequest struct {
	AccountID string `json:"accountId"`
	Limit     int    `json:"limit"`
}

func (s *PendingOpsService) Summary(_ context.Context, request PendingOpsRequest) (store.PendingMailOpsSummary, error) {
	return s.Runtime.DB.PendingMailOpsSummary(s.Runtime.Context, request.AccountID)
}
func (s *PendingOpsService) List(_ context.Context, request PendingOpsRequest) ([]store.PendingMailOp, error) {
	return s.Runtime.DB.PendingMailOps(s.Runtime.Context, request.AccountID, request.Limit)
}
func (s *PendingOpsService) Dismiss(ctx context.Context, request PendingOpsRequest) (int, error) {
	n, err := s.Runtime.DB.DismissFailedPendingOps(ctx, request.AccountID)
	if err == nil && n > 0 && s.Runtime.OnPendingOpsChanged != nil {
		s.Runtime.OnPendingOpsChanged()
	}
	return int(n), err
}

// RunPendingOpsWorker replays queued writes on the application context. A
// failed startup recovery keeps the queue intact for the next launch.
func (r *Runtime) RunPendingOpsWorker(ctx context.Context) {
	if r.DB == nil {
		return
	}
	if err := r.DB.ResetInterruptedPendingOps(ctx); err != nil {
		slog.Warn("pending write recovery deferred", "error", err)
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		r.processPendingOps(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-r.pendingWake:
		}
	}
}

func (r *Runtime) processPendingOps(ctx context.Context) {
	ops, payloads, err := r.DB.ClaimPendingOps(ctx, 20, time.Now().Unix())
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("pending write claim failed", "error", err)
		}
		return
	}
	for _, op := range ops {
		if ctx.Err() != nil {
			return
		}
		outcomeUnknown, err := r.replayPendingOp(ctx, op, payloads[op.ID])
		switch {
		case err == nil && !outcomeUnknown:
			_ = r.DB.CompletePendingOp(ctx, op.ID)
		case ctx.Err() != nil:
			return
		case outcomeUnknown:
			message := "Remote write outcome is unknown; it will not be retried automatically."
			if err != nil {
				message = err.Error()
			}
			_ = r.DB.FailPendingOp(ctx, op.ID, message, true)
		default:
			_ = r.DB.FailPendingOp(ctx, op.ID, err.Error(), false)
		}
	}
	if len(ops) > 0 && r.OnPendingOpsChanged != nil {
		r.OnPendingOpsChanged()
	}
}

func (r *Runtime) replayPendingOp(ctx context.Context, op store.PendingMailOp, payload json.RawMessage) (bool, error) {
	p, err := r.provider(op.AccountID)
	if err != nil {
		return false, err
	}
	write, ok := p.(interface {
		Replay(context.Context, string, json.RawMessage) (bool, error)
	})
	if !ok {
		return false, errors.New("provider does not support queued writes")
	}
	return write.Replay(ctx, op.OpType, payload)
}
