package mailcore

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func (c *Core) client(ctx context.Context) (*imapclient.Client, func(), error) {
	conn, close, err := dial(ctx, c.Config.IMAPAddr, c.Config.TLS)
	if err != nil {
		return nil, nil, err
	}
	client := imapclient.New(conn, nil)
	cleanup := func() { close(); _ = client.Close() }
	if err = client.Login(c.Config.Username, c.Config.Password).Wait(); err != nil {
		cleanup()
		return nil, nil, err
	}
	return client, cleanup, nil
}

// Sync is a bounded full UID snapshot, not production incremental synchronization.
func (c *Core) Sync(ctx context.Context) (SyncResult, error) {
	c.gate.Lock()
	defer c.gate.Unlock()
	client, close, err := c.client(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	defer close()
	if _, err = client.List("", "*", nil).Collect(); err != nil {
		return SyncResult{}, err
	}
	mbox, err := client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return SyncResult{}, err
	}
	search, err := client.UIDSearch(&imap.SearchCriteria{}, nil).Wait()
	if err != nil {
		return SyncResult{}, err
	}
	uids := search.AllUIDs()
	if len(uids) > MaxSnapshotMessages {
		return SyncResult{}, errors.New("snapshot exceeds PoC bound; cursor unchanged")
	}
	result := SyncResult{UIDValidity: mbox.UIDValidity, UIDNext: uint32(mbox.UIDNext), IDs: []string{}}
	messages := []Message{}
	if len(uids) > 0 {
		section := &imap.FetchItemBodySection{Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: MaxMessageBytes + 1}}
		fetched, err := client.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, Flags: true, RFC822Size: true, BodySection: []*imap.FetchItemBodySection{section}}).Collect()
		if err != nil {
			return SyncResult{}, err
		}
		if len(fetched) != len(uids) {
			return SyncResult{}, errors.New("incomplete snapshot: cursor unchanged")
		}
		seen := map[imap.UID]bool{}
		for _, f := range fetched {
			if !slices.Contains(uids, f.UID) || seen[f.UID] || f.RFC822Size > MaxMessageBytes {
				return SyncResult{}, errors.New("invalid or oversized fetched message")
			}
			seen[f.UID] = true
			m, err := Parse(f.FindBodySection(section))
			if err != nil {
				return SyncResult{}, fmt.Errorf("UID %d: %w", f.UID, err)
			}
			m.UID = uint32(f.UID)
			m.UIDValidity = mbox.UIDValidity
			m.ID = messageID(m.UIDValidity, m.UID)
			m.AccountID = AccountID
			m.IsRead = slices.Contains(f.Flags, imap.FlagSeen)
			m.IsStarred = slices.Contains(f.Flags, imap.FlagFlagged)
			messages = append(messages, m)
			result.IDs = append(result.IDs, m.ID)
		}
	}
	// No destructive reconciliation or cursor advance until every body parsed.
	result.Count = len(messages)
	if err = c.CommitSnapshot(ctx, messages, result); err != nil {
		return SyncResult{}, err
	}
	return result, nil
}

func (c *Core) Flags(ctx context.Context, id string, read, starred *bool) error {
	c.gate.Lock()
	defer c.gate.Unlock()
	rows, err := c.DB.Query(ctx, `SELECT p.uid_validity,p.uid FROM protocol_map p JOIN messages m ON m.id=p.message_id WHERE p.message_id=? AND m.is_deleted=0`, id)
	if err != nil {
		return err
	}
	if len(rows.Values) != 1 {
		return errors.New("message unavailable")
	}
	validity := uint32(rows.Values[0][0].(int64))
	uid := imap.UID(rows.Values[0][1].(int64))
	client, close, err := c.client(ctx)
	if err != nil {
		return err
	}
	defer close()
	selected, err := client.Select("INBOX", nil).Wait()
	if err != nil {
		return err
	}
	if selected.UIDValidity != validity {
		return ErrStaleUID
	}
	before, err := client.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil {
		return err
	}
	if len(before) != 1 {
		return errors.New("remote message vanished")
	}
	for _, item := range []struct {
		value *bool
		flag  imap.Flag
	}{{read, imap.FlagSeen}, {starred, imap.FlagFlagged}} {
		if item.value == nil {
			continue
		}
		op := imap.StoreFlagsDel
		if *item.value {
			op = imap.StoreFlagsAdd
		}
		if err = client.Store(imap.UIDSetNum(uid), &imap.StoreFlags{Op: op, Flags: []imap.Flag{item.flag}, Silent: true}, nil).Close(); err != nil {
			return fmt.Errorf("flag outcome may require reconciliation: %w", err)
		}
	}
	after, err := client.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil || len(after) != 1 {
		return errors.New("flag verification failed; synchronize before retry")
	}
	_, err = c.DB.Execute(ctx, "UPDATE messages SET is_read=?,is_starred=? WHERE id=?", slices.Contains(after[0].Flags, imap.FlagSeen), slices.Contains(after[0].Flags, imap.FlagFlagged), id)
	return err
}
