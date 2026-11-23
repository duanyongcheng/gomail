package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/mailmime"
)

func threadHandle(account, kind, key string) string {
	tuple, _ := json.Marshal([]string{account, kind, key})
	return "thread:" + base64.RawURLEncoding.EncodeToString(tuple)
}
func decodeThread(handle string) (account, kind, key string, err error) {
	if len(handle) > 32768 {
		return "", "", "", errors.New("invalid thread id")
	}
	encoded, ok := strings.CutPrefix(handle, "thread:")
	if !ok {
		return "", "", "", errors.New("invalid thread id")
	}
	raw, e := base64.RawURLEncoding.DecodeString(encoded)
	var tuple []string
	if e != nil || json.Unmarshal(raw, &tuple) != nil || len(tuple) != 3 || tuple[0] == "" || tuple[2] == "" || (tuple[1] != "native" && tuple[1] != "headers") {
		return "", "", "", errors.New("invalid thread id")
	}
	return tuple[0], tuple[1], tuple[2], nil
}
func nativeThread(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
func textValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func migrateThreads(ctx context.Context, tx *sql.Tx) error {
	if err := sqlMigration("027.sql")(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO message_threads(message_id,account_id,kind,thread_key)
		SELECT id,account_id,'native',trim(thread_id) FROM messages WHERE trim(COALESCE(thread_id,''))!=''`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT account_id FROM messages WHERE trim(COALESCE(thread_id,''))=''`)
	if err != nil {
		return err
	}
	var accounts []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		accounts = append(accounts, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, account := range accounts {
		if err := rebuildHeaderThreads(ctx, tx, account); err != nil {
			return err
		}
	}
	return nil
}

// Missing-ancestor nodes make header grouping independent of arrival order.
// Native provider threads are authoritative and never joined by these edges.
func rebuildHeaderThreads(ctx context.Context, tx *sql.Tx, account string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,COALESCE(message_id_header,''),COALESCE(in_reply_to,''),COALESCE(references_header,'')
		FROM messages WHERE account_id=? AND trim(COALESCE(thread_id,''))='' ORDER BY id`, account)
	if err != nil {
		return err
	}
	type item struct{ id, node string }
	var items []item
	parent := map[string]string{}
	find := func(node string) string {
		if _, ok := parent[node]; !ok {
			parent[node] = node
		}
		root := node
		for parent[root] != root {
			root = parent[root]
		}
		for parent[node] != node {
			next := parent[node]
			parent[node] = root
			node = next
		}
		return root
	}
	join := func(a, b string) {
		a, b = find(a), find(b)
		if a > b {
			a, b = b, a
		}
		parent[b] = a
	}
	for rows.Next() {
		var id, mid, reply, refs string
		if err := rows.Scan(&id, &mid, &reply, &refs); err != nil {
			rows.Close()
			return err
		}
		node := "m:" + id
		if mids := mailmime.ThreadHeaderIDs(mid); len(mids) == 1 {
			node = "h:" + mids[0]
		}
		links := append(mailmime.ThreadHeaderIDs(reply), mailmime.ThreadHeaderIDs(refs)...)
		find(node)
		for _, link := range links {
			join(node, "h:"+link)
		}
		items = append(items, item{id: id, node: node})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, i := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO message_threads(message_id,account_id,kind,thread_key) VALUES(?,?,'headers',?)
			ON CONFLICT(message_id) DO UPDATE SET kind=excluded.kind,thread_key=excluded.thread_key`, i.id, account, find(i.node)); err != nil {
			return err
		}
	}
	return nil
}

func updateMessageThread(ctx context.Context, tx *sql.Tx, m domain.Message) (bool, error) {
	var previousKind string
	err := tx.QueryRowContext(ctx, "SELECT kind FROM message_threads WHERE message_id=?", m.ID).Scan(&previousKind)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	native := nativeThread(m.ThreadID)
	if native == "" {
		return errors.Is(err, sql.ErrNoRows), nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO message_threads(message_id,account_id,kind,thread_key) VALUES(?,?,'native',?)
		ON CONFLICT(message_id) DO UPDATE SET kind=excluded.kind,thread_key=excluded.thread_key`, m.ID, m.AccountID, native)
	return previousKind == "headers", err
}

type ThreadsFilter struct {
	FolderIDs     []string
	Limit, Offset int
}

func (db *DB) Threads(ctx context.Context, filter ThreadsFilter) ([]domain.ThreadSummary, error) {
	out := []domain.ThreadSummary{}
	if len(filter.FolderIDs) == 0 {
		return out, nil
	}
	if len(filter.FolderIDs) > 200 || filter.Limit < 0 || filter.Limit > 200 || filter.Offset < 0 {
		return nil, errors.New("invalid thread pagination or folder selection")
	}
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	args := []any{}
	for _, id := range filter.FolderIDs {
		args = append(args, id)
	}
	args = append(args, filter.Limit, filter.Offset)
	query := `WITH selected AS (
		SELECT m.id,m.account_id,t.kind,t.thread_key,m.subject,m.snippet,m.date,m.is_read,m.is_starred,m.has_attachments,m.from_address,
		ROW_NUMBER() OVER (PARTITION BY m.account_id,t.kind,t.thread_key ORDER BY m.date DESC,m.id DESC) AS newest
		FROM messages m JOIN message_threads t ON t.message_id=m.id AND t.account_id=m.account_id
		WHERE m.is_deleted=0 AND EXISTS (SELECT 1 FROM message_folders mf WHERE mf.message_id=m.id AND mf.folder_id IN (` + placeholders(len(filter.FolderIDs)) + `))
	)
	SELECT s.account_id,s.kind,s.thread_key,MAX(CASE WHEN newest=1 THEN subject END),MAX(CASE WHEN newest=1 THEN snippet END),MAX(date),
	COUNT(*),SUM(is_read=0),MAX(is_starred),MAX(has_attachments),
	(SELECT json_group_array(from_address) FROM (SELECT DISTINCT from_address FROM selected p WHERE p.account_id=s.account_id AND p.kind=s.kind AND p.thread_key=s.thread_key AND from_address!='' ORDER BY from_address))
	FROM selected s GROUP BY s.account_id,s.kind,s.thread_key ORDER BY MAX(date) DESC,s.account_id,s.kind,s.thread_key LIMIT ? OFFSET ?`
	rows, err := db.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.ThreadSummary
		var kind, key, participants string
		if err := rows.Scan(&item.AccountID, &kind, &key, &item.Subject, &item.Snippet, &item.LastDate, &item.MessageCount, &item.UnreadCount, &item.IsStarred, &item.HasAttachments, &participants); err != nil {
			return nil, err
		}
		item.ThreadID = threadHandle(item.AccountID, kind, key)
		if err := json.Unmarshal([]byte(participants), &item.Participants); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ThreadMessages omits bodies: each expanded bubble fetches rendered content
// by ID, so a 500-message conversation cannot materialize 500 large MIME bodies.
func (db *DB) ThreadMessages(ctx context.Context, handle string) ([]domain.Message, error) {
	account, kind, key, err := decodeThread(handle)
	if err != nil {
		return nil, err
	}
	columns := strings.Replace(messageColumns, "m.body_text,m.body_html_raw", "'',''", 1)
	rows, err := db.read.QueryContext(ctx, "SELECT "+columns+` FROM messages m JOIN message_threads t ON t.message_id=m.id AND t.account_id=m.account_id
		WHERE m.account_id=? AND t.kind=? AND t.thread_key=? AND m.is_deleted=0 ORDER BY m.date,m.id LIMIT 501`, account, kind, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		m.ThreadID = &handle
		out = append(out, m)
	}
	if len(out) > 500 {
		return nil, errors.New("thread exceeds 500 cached messages; use message view")
	}
	return out, rows.Err()
}

func (db *DB) PublicThreadID(ctx context.Context, messageID string) (string, error) {
	var account, kind, key string
	err := db.read.QueryRowContext(ctx, "SELECT account_id,kind,thread_key FROM message_threads WHERE message_id=?", messageID).Scan(&account, &kind, &key)
	if err != nil {
		return "", err
	}
	return threadHandle(account, kind, key), nil
}
