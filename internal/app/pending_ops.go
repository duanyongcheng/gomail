package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/bary/pebble-go/internal/store"
)

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

func (s *MessageService) mutateMail(ctx context.Context, request store.MailMutation) (store.MailMutationResult, error) {
	result, err := s.Runtime.DB.QueueMailMutation(ctx, request)
	if err == nil && result.Count > 0 {
		s.Runtime.notifyPendingOps()
	}
	return result, err
}

// QueueFlags applies the local flag and persists the remote write atomically.
// Offline mail stays readable and the change survives restart.
func (s *MessageService) QueueFlags(ctx context.Context, request FlagsRequest) (Empty, error) {
	_, err := s.mutateMail(ctx, store.MailMutation{MessageIDs: []string{request.MessageID}, OpType: "update_flags", IsRead: request.IsRead, IsStarred: request.IsStarred})
	return Empty{}, err
}

// Archive keeps upstream's toggle semantics, decided inside the transaction.
func (s *MessageService) Archive(ctx context.Context, request ArchiveRequest) (string, error) {
	result, err := s.mutateMail(ctx, store.MailMutation{MessageIDs: []string{request.MessageID}, OpType: "archive", ToggleArchive: true})
	return result.ArchiveResult, err
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
	_, err := s.mutateMail(ctx, store.MailMutation{MessageIDs: []string{request.MessageID}, OpType: "move_to_folder", TargetFolderID: request.TargetFolderID})
	return Empty{}, err
}
func (s *MessageService) queueFolderChange(ctx context.Context, messageID, opType string) error {
	_, err := s.mutateMail(ctx, store.MailMutation{MessageIDs: []string{messageID}, OpType: opType})
	return err
}

// BatchArchive and BatchDelete queue one row per message in one transaction.
func (s *MessageService) BatchArchive(ctx context.Context, request BatchRequest) (int, error) {
	return s.batchMutation(ctx, store.MailMutation{MessageIDs: request.MessageIDs, OpType: "archive"})
}
func (s *MessageService) BatchDelete(ctx context.Context, request BatchRequest) (int, error) {
	return s.batchMutation(ctx, store.MailMutation{MessageIDs: request.MessageIDs, OpType: "delete"})
}
func (s *MessageService) BatchFlags(ctx context.Context, request BatchRequest) (int, error) {
	return s.batchMutation(ctx, store.MailMutation{MessageIDs: request.MessageIDs, OpType: "update_flags", IsRead: request.IsRead, IsStarred: request.Starred})
}
func (s *MessageService) batchMutation(ctx context.Context, request store.MailMutation) (int, error) {
	if len(request.MessageIDs) == 0 || len(request.MessageIDs) > 200 {
		return 0, errors.New("invalid batch size")
	}
	result, err := s.mutateMail(ctx, request)
	return result.Count, err
}
func (s *MessageService) MarkAccountAllRead(ctx context.Context, request AccountRequest) (int, error) {
	return s.markAllRead(ctx, request.AccountID, nil)
}
func (s *MessageService) MarkFolderAllRead(ctx context.Context, request AllReadRequest) (int, error) {
	if len(request.FolderIDs) == 0 {
		return 0, nil
	}
	return s.markAllRead(ctx, request.AccountID, request.FolderIDs)
}
func (s *MessageService) markAllRead(ctx context.Context, account string, folderIDs []string) (int, error) {
	if account == "" {
		return 0, errors.New("account required")
	}
	read := true
	result, err := s.mutateMail(ctx, store.MailMutation{AccountID: account, FolderIDs: folderIDs, AllUnread: true, OpType: "update_flags", IsRead: &read})
	return result.Count, err
}

// notifyPendingOps wakes the worker immediately instead of waiting for a poll.
func (r *Runtime) notifyPendingOps() {
	select {
	case r.pendingWake <- struct{}{}:
	default:
	}
	if r.OnPendingOpsChanged != nil {
		r.OnPendingOpsChanged()
	}
}

type PendingOpsService struct{ Runtime *Runtime }
type PendingOpsRequest struct {
	AccountID string `json:"accountId"`
	Limit     int    `json:"limit"`
}

func (s *PendingOpsService) Summary(ctx context.Context, request PendingOpsRequest) (store.PendingMailOpsSummary, error) {
	return s.Runtime.DB.PendingMailOpsSummary(ctx, request.AccountID)
}
func (s *PendingOpsService) List(ctx context.Context, request PendingOpsRequest) ([]store.PendingMailOp, error) {
	return s.Runtime.DB.PendingMailOps(ctx, request.AccountID, request.Limit)
}
func (s *PendingOpsService) Dismiss(ctx context.Context, request PendingOpsRequest) (int, error) {
	n, err := s.Runtime.DB.DismissFailedPendingOps(ctx, request.AccountID)
	if err == nil && n > 0 {
		s.Runtime.notifyPendingOps()
	}
	return int(n), err
}

// pendingSettlement is retained until SQLite accepts the replay result. A local
// disk/transaction failure must never cause another HTTP call in this process.
type pendingSettlement struct {
	id      string
	unknown bool
	message string
}

func (r *Runtime) settlePendingOp(ctx context.Context) error {
	result := r.pendingSettlement
	if result == nil {
		return nil
	}
	var err error
	if result.message == "" && !result.unknown {
		err = r.DB.CompletePendingOp(ctx, result.id)
	} else {
		err = r.DB.FailPendingOp(ctx, result.id, result.message, result.unknown)
	}
	if err == nil {
		r.pendingSettlement = nil
		if r.OnPendingOpsChanged != nil {
			r.OnPendingOpsChanged()
		}
	}
	return err
}

// RunPendingOpsWorker is the sole startup recovery owner. On recovery failure
// it retries locally without claiming any remote work. The app context owns it.
func (r *Runtime) RunPendingOpsWorker(ctx context.Context) {
	r.pendingWorkerMu.Lock()
	defer r.pendingWorkerMu.Unlock()
	if r.DB == nil {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	recovered := false
	for ctx.Err() == nil {
		if !recovered {
			r.pendingMu.Lock()
			err := r.settlePendingOp(ctx)
			if err == nil {
				err = r.DB.ResetInterruptedPendingOps(ctx)
			}
			r.pendingMu.Unlock()
			if err == nil {
				recovered = true
				if r.OnPendingOpsChanged != nil {
					r.OnPendingOpsChanged()
				}
			} else if ctx.Err() == nil {
				slog.Warn("pending write recovery deferred")
			}
		}
		if recovered {
			more, err := r.processPendingOps(ctx)
			if err != nil && ctx.Err() == nil {
				slog.Warn("pending write processing deferred")
			}
			if err == nil && more {
				continue
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-r.pendingWake:
		}
	}
}

// Claim one row just before each call, so a failed settlement never strands an
// unstarted batch. FIFO blocking is per message; other messages can progress.
func (r *Runtime) processPendingOps(ctx context.Context) (bool, error) {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	if err := r.settlePendingOp(ctx); err != nil {
		return false, err
	}
	for i := 0; i < 20; i++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		ops, payloads, err := r.DB.ClaimPendingOps(ctx, 1, time.Now().Unix())
		if err != nil {
			return false, err
		}
		if len(ops) == 0 {
			return false, nil
		}
		op := ops[0]
		unknown, replayErr := r.replayPendingOp(ctx, op, payloads[op.ID])
		if ctx.Err() != nil && replayErr != nil && !unknown {
			return false, ctx.Err()
		}
		result := &pendingSettlement{id: op.ID, unknown: unknown}
		if unknown {
			result.message = "Remote write outcome is unknown; it will not be retried automatically."
		} else if replayErr != nil {
			result.message = "Remote write failed; check the account connection and retry status."
		}
		r.pendingSettlement = result
		if err := r.settlePendingOp(ctx); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (r *Runtime) replayPendingOp(ctx context.Context, op store.PendingMailOp, payload json.RawMessage) (bool, error) {
	message, err := r.DB.Message(ctx, op.MessageID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if message.AccountID != op.AccountID {
		return false, errors.New("pending operation crosses account boundary")
	}
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
