package imap

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/store"
	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

type mutation struct {
	RemoteID       string   `json:"remote_id"`
	IsRead         *bool    `json:"is_read,omitempty"`
	IsStarred      *bool    `json:"is_starred,omitempty"`
	TargetRemoteID string   `json:"target_remote_id,omitempty"`
	AddLabels      []string `json:"add_labels,omitempty"`
	RemoveLabels   []string `json:"remove_labels,omitempty"`
}

// Flags queues the same atomic local intent as the typed application service.
func (p *Provider) Flags(ctx context.Context, id string, read, star *bool) error {
	m, err := p.DB.Message(ctx, id)
	if err != nil {
		return err
	}
	if m.AccountID != p.AccountID {
		return errors.New("message belongs to another account")
	}
	_, err = p.DB.QueueMailMutation(ctx, store.MailMutation{AccountID: p.AccountID, MessageIDs: []string{id}, OpType: "update_flags", IsRead: read, IsStarred: star})
	return err
}

func (p *Provider) Replay(ctx context.Context, opType string, raw json.RawMessage) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var request mutation
	if json.Unmarshal(raw, &request) != nil {
		return false, errors.New("invalid IMAP mutation")
	}
	mailbox, validity, uid, err := ParseRemoteID(request.RemoteID)
	if err != nil {
		return false, err
	}
	// Persisted identity must resolve to this account's cached message, even if
	// another account has the same remote mailbox name and UID.
	m, err := p.DB.Message(ctx, store.ScopedID(p.AccountID, request.RemoteID))
	if err != nil {
		return false, err
	}
	if m.AccountID != p.AccountID {
		return false, ErrStaleUID
	}
	p.gate.Lock()
	defer p.gate.Unlock()
	c, close, err := p.client(ctx, nil)
	if err != nil {
		return false, err
	}
	defer close()
	selected, err := c.Select(mailbox, nil).Wait()
	if err != nil {
		return false, errors.New("IMAP mailbox selection failed")
	}
	if selected.UIDValidity != validity {
		return false, ErrStaleUID
	}
	set := protocol.UIDSetNum(protocol.UID(uid))
	// Confirm existence without treating an empty FETCH as successful mutation.
	check := c.Fetch(set, &protocol.FetchOptions{UID: true})
	count := 0
	for message := check.Next(); message != nil; message = check.Next() {
		count++
		if count > 1 {
			_ = c.Close()
			return false, ErrStaleUID
		}
		found := false
		for item := message.Next(); item != nil; item = message.Next() {
			switch item := item.(type) {
			case imapclient.FetchItemDataUID:
				found = uint32(item.UID) == uid
			case imapclient.FetchItemDataBodySection, imapclient.FetchItemDataBinarySection:
				_ = c.Close()
				return false, errors.New("unexpected IMAP identity literal")
			}
		}
		if !found {
			_ = c.Close()
			return false, ErrStaleUID
		}
	}
	if err = check.Close(); err != nil {
		return false, errors.New("IMAP identity check failed")
	}
	if count != 1 {
		return false, ErrStaleUID
	}
	if p.BeforeRequest != nil {
		if err = p.BeforeRequest(ctx); err != nil {
			return false, err
		}
	}
	switch opType {
	case "update_flags":
		if request.IsRead == nil {
			for _, label := range request.AddLabels {
				if label == "UNREAD" {
					v := false
					request.IsRead = &v
				}
			}
			for _, label := range request.RemoveLabels {
				if label == "UNREAD" {
					v := true
					request.IsRead = &v
				}
			}
		}
		if request.IsStarred == nil {
			for _, label := range request.AddLabels {
				if label == "STARRED" {
					v := true
					request.IsStarred = &v
				}
			}
			for _, label := range request.RemoveLabels {
				if label == "STARRED" {
					v := false
					request.IsStarred = &v
				}
			}
		}
		for _, flag := range []struct {
			value *bool
			flag  protocol.Flag
		}{{request.IsRead, protocol.FlagSeen}, {request.IsStarred, protocol.FlagFlagged}} {
			if flag.value == nil {
				continue
			}
			op := protocol.StoreFlagsDel
			if *flag.value {
				op = protocol.StoreFlagsAdd
			}
			if err = c.Store(set, &protocol.StoreFlags{Op: op, Flags: []protocol.Flag{flag.flag}, Silent: true}, nil).Close(); err != nil {
				return false, errors.New("IMAP flag update failed")
			}
		}
		return false, nil
	case "archive", "restore", "unarchive", "delete", "move_to_folder":
		target := request.TargetRemoteID
		if target == "" {
			for _, label := range request.AddLabels {
				if label != "STARRED" && label != "UNREAD" {
					target = label
					break
				}
			}
		}
		if !validMailbox(target) || target == mailbox {
			return false, errors.New("IMAP target mailbox required")
		}
		folders, err := p.DB.Folders(ctx, p.AccountID)
		if err != nil {
			return false, err
		}
		found := false
		for _, f := range folders {
			if f.RemoteID == target && f.AccountID == p.AccountID {
				found = true
			}
		}
		if !found {
			return false, errors.New("IMAP target mailbox not owned by account")
		}
		if !c.Caps().Has(protocol.CapMove) {
			if !c.Caps().Has(protocol.CapUIDPlus) {
				return false, errors.New("server does not support safe UID MOVE")
			}
			// Do not use the client's automatic fallback: it pipelines deletion
			// before COPY acceptance and can fall back to global EXPUNGE.
			if _, err = c.Copy(set, target).Wait(); err != nil {
				return uncertain(err), errors.New("IMAP copy failed or requires reconciliation")
			}
			if err = c.Store(set, &protocol.StoreFlags{Op: protocol.StoreFlagsAdd, Flags: []protocol.Flag{protocol.FlagDeleted}, Silent: true}, nil).Close(); err != nil {
				return true, errors.New("IMAP copy accepted; source deletion requires reconciliation")
			}
			if err = c.UIDExpunge(set).Close(); err != nil {
				return true, errors.New("IMAP copy accepted; source expunge requires reconciliation")
			}
			return false, nil
		}
		_, err = c.Move(set, target).Wait()
		if err != nil {
			return uncertain(err), errors.New("IMAP move failed or requires reconciliation")
		}
		return false, nil
	default:
		return false, errors.New("unsupported IMAP mutation")
	}
}

func uncertain(err error) bool { var status *protocol.Error; return !errors.As(err, &status) }

// ChangeFolder is called only after explicit UI confirmation. The application
// supplies a durable barrier and owns receipt settlement, never automatic replay.
func (p *Provider) ChangeFolder(ctx context.Context, action, source, name string, beforeWrite func() error) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	p.gate.Lock()
	defer p.gate.Unlock()
	if action != "create" && action != "rename" && action != "delete" {
		return false, errors.New("unsupported IMAP folder action")
	}
	if action != "delete" && !validMailbox(name) {
		return false, errors.New("invalid IMAP mailbox name")
	}
	if action != "create" {
		folders, err := p.DB.Folders(ctx, p.AccountID)
		if err != nil {
			return false, err
		}
		found := false
		for _, f := range folders {
			if f.RemoteID == source {
				if f.IsSystem || strings.EqualFold(source, "INBOX") {
					return false, errors.New("system mailbox is protected")
				}
				found = true
			}
		}
		if !found {
			return false, errors.New("IMAP source mailbox not owned by account")
		}
	}
	c, close, err := p.client(ctx, nil)
	if err != nil {
		return false, err
	}
	defer close()
	if beforeWrite == nil {
		return false, errors.New("durable IMAP folder barrier required")
	}
	if p.BeforeRequest != nil {
		if err = p.BeforeRequest(ctx); err != nil {
			return false, err
		}
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	if err = beforeWrite(); err != nil {
		return false, err
	}
	switch action {
	case "create":
		err = c.Create(name, nil).Wait()
	case "rename":
		err = c.Rename(source, name, nil).Wait()
	case "delete":
		err = c.Delete(source).Wait()
	}
	if err != nil {
		return uncertain(err), errors.New("IMAP folder change failed or requires reconciliation")
	}
	return false, nil
}

var _ domain.Transport = (*Provider)(nil)
var _ domain.ProgressTransport = (*Provider)(nil)
