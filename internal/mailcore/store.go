package mailcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/egoist/mygo/plugins/sqlite"
)

type Core struct {
	DB     *sqlite.DB
	Config Config
	gate   sync.Mutex
}

func New(ctx context.Context, db *sqlite.DB, cfg Config) (*Core, error) {
	if err := validateAddress(cfg.IMAPAddr); err != nil {
		return nil, err
	}
	if err := validateAddress(cfg.SMTPAddr); err != nil {
		return nil, err
	}
	statements := []string{
		"CREATE TABLE IF NOT EXISTS protocol_cursor (account_id TEXT PRIMARY KEY, uid_validity INTEGER NOT NULL, uid_next INTEGER NOT NULL)",
		"CREATE TABLE IF NOT EXISTS protocol_map (message_id TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE, uid_validity INTEGER NOT NULL, uid INTEGER NOT NULL)",
		"CREATE VIRTUAL TABLE IF NOT EXISTS protocol_search USING fts5(message_id UNINDEXED,account_id UNINDEXED,subject,body,from_address, tokenize='trigram')",
		"CREATE TABLE IF NOT EXISTS protocol_send (id TEXT PRIMARY KEY,status TEXT NOT NULL)",
		"CREATE TABLE IF NOT EXISTS protocol_attachment_data (attachment_id TEXT PRIMARY KEY REFERENCES attachments(id) ON DELETE CASCADE,data BLOB NOT NULL)",
	}
	for _, s := range statements {
		if _, err := db.Execute(ctx, s); err != nil {
			return nil, err
		}
	}
	return &Core{DB: db, Config: cfg}, nil
}

func snapshotStatements(messages []Message, result SyncResult) ([]sqlite.Statement, error) {
	var stmts []sqlite.Statement
	// Only after a complete, bounded UID snapshot do we replace generation membership.
	stmts = append(stmts, sqlite.Statement{SQL: "UPDATE messages SET is_deleted=1 WHERE account_id=?", Args: []any{AccountID}}, sqlite.Statement{SQL: "DELETE FROM protocol_search WHERE account_id=?", Args: []any{AccountID}})
	for _, m := range messages {
		to, err := json.Marshal(m.To)
		if err != nil {
			return nil, err
		}
		fromName := ""
		if m.From.Name != nil {
			fromName = *m.From.Name
		}
		stmts = append(stmts,
			sqlite.Statement{SQL: `INSERT INTO messages(id,account_id,remote_id,subject,snippet,from_address,from_name,to_list,body_text,body_html_raw,has_attachments,is_read,is_starred,date,message_id_header,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET subject=excluded.subject,snippet=excluded.snippet,from_address=excluded.from_address,from_name=excluded.from_name,to_list=excluded.to_list,body_text=excluded.body_text,body_html_raw=excluded.body_html_raw,has_attachments=excluded.has_attachments,is_read=excluded.is_read,is_starred=excluded.is_starred,date=excluded.date,is_deleted=0`, Args: []any{m.ID, AccountID, fmt.Sprint(m.UID), m.Subject, m.Snippet, m.From.Address, fromName, string(to), m.BodyText, m.BodyHTML, len(m.Attachments) > 0, m.IsRead, m.IsStarred, m.Date, m.MessageID, m.Date, m.Date}},
			sqlite.Statement{SQL: "INSERT OR IGNORE INTO message_folders(message_id,folder_id) VALUES(?,?)", Args: []any{m.ID, "poc-inbox"}},
			sqlite.Statement{SQL: "INSERT INTO protocol_map VALUES(?,?,?) ON CONFLICT(message_id) DO UPDATE SET uid_validity=excluded.uid_validity,uid=excluded.uid", Args: []any{m.ID, m.UIDValidity, m.UID}},
			sqlite.Statement{SQL: "INSERT INTO protocol_search(message_id,account_id,subject,body,from_address) VALUES(?,?,?,?,?)", Args: []any{m.ID, AccountID, m.Subject, m.BodyText, m.From.Address}},
			sqlite.Statement{SQL: "DELETE FROM attachments WHERE message_id=?", Args: []any{m.ID}},
		)
		for n, a := range m.Attachments {
			id := fmt.Sprintf("%s:attachment:%d", m.ID, n)
			stmts = append(stmts,
				sqlite.Statement{SQL: "INSERT INTO attachments(id,message_id,filename,mime_type,size,content_id,is_inline) VALUES(?,?,?,?,?,?,?)", Args: []any{id, m.ID, a.Filename, a.MIMEType, len(a.Data), a.ContentID, a.Inline}},
				sqlite.Statement{SQL: "INSERT INTO protocol_attachment_data VALUES(?,?)", Args: []any{id, a.Data}},
			)
		}
	}
	stmts = append(stmts, sqlite.Statement{SQL: "INSERT INTO protocol_cursor VALUES(?,?,?) ON CONFLICT(account_id) DO UPDATE SET uid_validity=excluded.uid_validity,uid_next=excluded.uid_next", Args: []any{AccountID, result.UIDValidity, result.UIDNext}})
	return stmts, nil
}
func (c *Core) CommitSnapshot(ctx context.Context, messages []Message, result SyncResult) error {
	stmts, err := snapshotStatements(messages, result)
	if err != nil {
		return err
	}
	_, err = c.DB.Transaction(ctx, stmts)
	return err
}
func (c *Core) Cursor(ctx context.Context) (SyncResult, error) {
	rows, err := c.DB.Query(ctx, "SELECT uid_validity,uid_next FROM protocol_cursor WHERE account_id=?", AccountID)
	if err != nil || len(rows.Values) == 0 {
		return SyncResult{}, err
	}
	return SyncResult{UIDValidity: uint32(rows.Values[0][0].(int64)), UIDNext: uint32(rows.Values[0][1].(int64))}, nil
}

func (c *Core) Search(ctx context.Context, text, account string, limit int) ([]SearchHit, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []SearchHit{}, nil
	}
	if !utf8.ValidString(text) || len(text) > 512 {
		return nil, errors.New("invalid search text")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	query := `SELECT m.id,m.snippet,m.subject,m.from_address,m.date FROM protocol_search s JOIN messages m ON m.id=s.message_id WHERE m.is_deleted=0`
	var args []any
	if account != "" {
		query += " AND s.account_id=?"
		args = append(args, account)
	}
	if utf8.RuneCountInString(text) >= 3 {
		query += " AND protocol_search MATCH ?"
		args = append(args, "\""+strings.ReplaceAll(text, "\"", "\"\"")+"\"")
	} else {
		// Explicit PoC fallback for 1-2 character CJK query; not a performance claim.
		query += " AND (m.subject LIKE ? ESCAPE '\\' OR m.body_text LIKE ? ESCAPE '\\')"
		pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(text) + "%"
		args = append(args, pattern, pattern)
	}
	query += " ORDER BY m.date DESC LIMIT ?"
	args = append(args, limit)
	rows, err := c.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := make([]SearchHit, 0, len(rows.Values))
	for _, r := range rows.Values {
		out = append(out, SearchHit{r[0].(string), 1, r[1].(string), r[2].(string), r[3].(string), r[4].(int64)})
	}
	return out, nil
}
