package gmail

import (
	"encoding/json"
	"fmt"

	"github.com/egoist/mygo/plugins/sqlite"
)

func (c *Core) statements(messages []fetched, removed []string, labels []Label, profile Profile, cursor string, sync bool) ([]sqlite.Statement, error) {
	stmts := []sqlite.Statement{}
	if sync {
		if err := validateHistory(cursor); err != nil {
			return nil, err
		}
		stmts = append(stmts, sqlite.Statement{SQL: "UPDATE accounts SET email=?,display_name=?,provider='gmail' WHERE id=?", Args: []any{profile.Email, profile.Email, AccountID}})
	}
	for _, label := range labels {
		role := any(nil)
		switch label.ID {
		case "INBOX":
			role = "inbox"
		case "SENT":
			role = "sent"
		case "DRAFT":
			role = "drafts"
		case "TRASH":
			role = "trash"
		case "SPAM":
			role = "spam"
		}
		stmts = append(stmts, sqlite.Statement{SQL: `INSERT INTO folders(id,account_id,remote_id,name,folder_type,role,is_system) VALUES(?,?,?,?,'label',?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,role=excluded.role`, Args: []any{"gmail-label:" + label.ID, AccountID, label.ID, label.Name, role, label.Type == "system"}})
	}
	for _, id := range removed {
		stmts = append(stmts, sqlite.Statement{SQL: "UPDATE messages SET is_deleted=1 WHERE id=?", Args: []any{id}}, sqlite.Statement{SQL: "DELETE FROM message_folders WHERE message_id=?", Args: []any{id}}, sqlite.Statement{SQL: "DELETE FROM protocol_search WHERE message_id=?", Args: []any{id}})
	}
	for _, f := range messages {
		m := f.mail
		to, err := json.Marshal(m.To)
		if err != nil {
			return nil, err
		}
		name := ""
		if m.From.Name != nil {
			name = *m.From.Name
		}
		stmts = append(stmts, sqlite.Statement{SQL: `INSERT INTO messages(id,account_id,remote_id,thread_id,message_id_header,subject,snippet,from_address,from_name,to_list,body_text,body_html_raw,has_attachments,is_read,is_starred,date,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET subject=excluded.subject,snippet=excluded.snippet,from_address=excluded.from_address,from_name=excluded.from_name,to_list=excluded.to_list,body_text=excluded.body_text,body_html_raw=excluded.body_html_raw,has_attachments=excluded.has_attachments,is_read=excluded.is_read,is_starred=excluded.is_starred,is_deleted=0`, Args: []any{m.ID, AccountID, f.api.ID, f.api.ThreadID, m.MessageID, m.Subject, m.Snippet, m.From.Address, name, string(to), m.BodyText, m.BodyHTML, len(m.Attachments) > 0, m.IsRead, m.IsStarred, m.Date, m.Date, m.Date}},
			sqlite.Statement{SQL: "DELETE FROM message_folders WHERE message_id=?", Args: []any{m.ID}},
			sqlite.Statement{SQL: "DELETE FROM protocol_search WHERE message_id=?", Args: []any{m.ID}},
			sqlite.Statement{SQL: "INSERT INTO protocol_search(message_id,account_id,subject,body,from_address) VALUES(?,?,?,?,?)", Args: []any{m.ID, AccountID, m.Subject, m.BodyText, m.From.Address}},
			sqlite.Statement{SQL: "DELETE FROM attachments WHERE message_id=?", Args: []any{m.ID}},
		)
		for _, label := range f.api.Labels {
			stmts = append(stmts, sqlite.Statement{SQL: "INSERT OR IGNORE INTO message_folders(message_id,folder_id) SELECT ?,id FROM folders WHERE id=?", Args: []any{m.ID, "gmail-label:" + label}})
		}
		for i, a := range m.Attachments {
			id := fmt.Sprintf("%s:attachment:%d", m.ID, i)
			stmts = append(stmts, sqlite.Statement{SQL: "INSERT INTO attachments(id,message_id,filename,mime_type,size,content_id,is_inline) VALUES(?,?,?,?,?,?,?)", Args: []any{id, m.ID, a.Filename, a.MIMEType, len(a.Data), a.ContentID, a.Inline}}, sqlite.Statement{SQL: "INSERT INTO protocol_attachment_data VALUES(?,?)", Args: []any{id, a.Data}})
		}
	}
	if sync {
		stmts = append(stmts, sqlite.Statement{SQL: "INSERT INTO gmail_cursor VALUES(?,?) ON CONFLICT(account_id) DO UPDATE SET history_id=excluded.history_id", Args: []any{AccountID, cursor}})
	}
	return stmts, nil
}
