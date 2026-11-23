package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/mailmime"
)

const accountColumns = "id,email,display_name,color,provider,account_label,provider_display_name,oauth_subject,created_at,updated_at,sort_order"

func (db *DB) Accounts(ctx context.Context) ([]domain.Account, error) {
	rows, err := db.read.QueryContext(ctx, "SELECT "+accountColumns+" FROM accounts ORDER BY sort_order,created_at,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Account{}
	for rows.Next() {
		var a domain.Account
		if err := rows.Scan(&a.ID, &a.Email, &a.DisplayName, &a.Color, &a.Provider, &a.AccountLabel, &a.ProviderDisplayName, &a.OAuthSubject, &a.CreatedAt, &a.UpdatedAt, &a.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (db *DB) SaveAccount(ctx context.Context, a domain.Account, encrypted []byte) error {
	_, err := db.write.ExecContext(ctx, `INSERT INTO accounts(id,email,display_name,color,provider,account_label,provider_display_name,created_at,updated_at,auth_data,sort_order)
 VALUES(?,?,?,?,?,?,?,?,?,?,(SELECT COALESCE(MAX(sort_order),-1)+1 FROM accounts))
 ON CONFLICT(id) DO UPDATE SET email=excluded.email,display_name=excluded.display_name,auth_data=excluded.auth_data,updated_at=excluded.updated_at`, a.ID, a.Email, a.DisplayName, a.Color, a.Provider, a.AccountLabel, a.ProviderDisplayName, a.CreatedAt, a.UpdatedAt, encrypted)
	return err
}

func (db *DB) HasAuthData(ctx context.Context) (bool, error) {
	var result bool
	err := db.read.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM accounts WHERE auth_data IS NOT NULL)").Scan(&result)
	return result, err
}
func (db *DB) DeleteAccount(ctx context.Context, id string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM pending_mail_ops WHERE account_id=?", id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE compose_attachments SET released=1 WHERE id IN
			(SELECT r.attachment_id FROM compose_draft_attachments r JOIN compose_drafts d ON d.id=r.draft_id WHERE d.account_id=?)`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM accounts WHERE id=?", id); err != nil {
			return err
		}
		return cleanReleasedComposeAttachments(ctx, tx)
	})
}

func (db *DB) Folders(ctx context.Context, account string) ([]domain.Folder, error) {
	rows, err := db.read.QueryContext(ctx, `SELECT id,account_id,remote_id,name,folder_type,role,parent_id,color,is_system,sort_order FROM folders WHERE account_id=? ORDER BY sort_order,name`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Folder{}
	for rows.Next() {
		var f domain.Folder
		if err := rows.Scan(&f.ID, &f.AccountID, &f.RemoteID, &f.Name, &f.FolderType, &f.Role, &f.ParentID, &f.Color, &f.IsSystem, &f.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

const messageColumns = `m.id,m.account_id,m.remote_id,m.message_id_header,m.in_reply_to,m.references_header,m.thread_id,m.subject,m.snippet,m.from_address,m.from_name,m.to_list,m.cc_list,m.bcc_list,m.body_text,m.body_html_raw,m.has_attachments,m.is_read,m.is_starred,m.is_draft,m.date,m.remote_version,m.is_deleted,m.deleted_at,m.created_at,m.updated_at,
EXISTS(SELECT 1 FROM message_folders mf JOIN folders f ON f.id=mf.folder_id
 WHERE mf.message_id=m.id AND f.account_id=m.account_id AND (f.role='trash' OR f.remote_id='TRASH'))`

func scanMessage(row interface{ Scan(...any) error }) (domain.Message, error) {
	var m domain.Message
	var to, cc, bcc string
	err := row.Scan(&m.ID, &m.AccountID, &m.RemoteID, &m.MessageIDHeader, &m.InReplyTo, &m.ReferencesHeader, &m.ThreadID, &m.Subject, &m.Snippet, &m.FromAddress, &m.FromName, &to, &cc, &bcc, &m.BodyText, &m.BodyHTML, &m.HasAttachments, &m.IsRead, &m.IsStarred, &m.IsDraft, &m.Date, &m.RemoteVersion, &m.IsDeleted, &m.DeletedAt, &m.CreatedAt, &m.UpdatedAt, &m.InTrash)
	if err != nil {
		return m, err
	}
	// Old cached headers may still contain quoted RFC 2047 names. Normalize the
	// read projection so offline views recover without a refetch or DB rewrite.
	m.FromName = mailmime.DecodeAddressName(m.FromName)
	for _, pair := range []struct {
		raw string
		out *[]domain.Address
	}{{to, &m.To}, {cc, &m.CC}, {bcc, &m.BCC}} {
		if err := json.Unmarshal([]byte(pair.raw), pair.out); err != nil {
			return m, errors.New("invalid cached recipient list")
		}
		if *pair.out == nil {
			*pair.out = []domain.Address{}
		}
		for i := range *pair.out {
			address := &(*pair.out)[i]
			if address.Name != nil {
				name := mailmime.DecodeAddressName(*address.Name)
				address.Name = &name
			}
		}
	}
	return m, nil
}

func (db *DB) Message(ctx context.Context, id string) (domain.Message, error) {
	return scanMessage(db.read.QueryRowContext(ctx, "SELECT "+messageColumns+" FROM messages m WHERE m.id=? AND m.is_deleted=0", id))
}
func (db *DB) Messages(ctx context.Context, folder, account string, starred bool, limit, offset int) ([]domain.Message, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	query := "SELECT " + messageColumns + " FROM messages m WHERE m.is_deleted=0"
	args := []any{}
	if folder != "" {
		query += " AND EXISTS(SELECT 1 FROM message_folders f WHERE f.message_id=m.id AND f.folder_id=?)"
		args = append(args, folder)
	}
	if account != "" {
		query += " AND m.account_id=?"
		args = append(args, account)
	}
	if starred {
		query += " AND m.is_starred=1"
	}
	query += " ORDER BY m.date DESC,m.id LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := db.read.QueryContext(ctx, query, args...)
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
		out = append(out, m)
	}
	return out, rows.Err()
}

func (db *DB) Cursor(ctx context.Context, account string) (string, error) {
	var state sql.NullString
	if err := db.read.QueryRowContext(ctx, "SELECT sync_state FROM accounts WHERE id=?", account).Scan(&state); err != nil {
		return "", err
	}
	if !state.Valid || strings.TrimSpace(state.String) == "" {
		return "", nil
	}
	var value struct {
		Cursor string `json:"last_sync_cursor"`
	}
	err := json.Unmarshal([]byte(state.String), &value)
	return value.Cursor, err
}
func (db *DB) KnownRemoteIDs(ctx context.Context, account string) ([]string, error) {
	rows, err := db.read.QueryContext(ctx, "SELECT remote_id FROM messages WHERE account_id=? AND is_deleted=0", account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

type CachedMessage struct {
	Message     domain.Message
	FolderIDs   []string
	Attachments []domain.Attachment
}
type Snapshot struct {
	ProviderMappingScopesCleared []ProviderMappingScope
	RemovedFolderIDs             []string
	RemovedMemberships           []RemovedMembership
	ProviderCheckpoints          []ProviderCheckpoint
	ProviderMappings             []ProviderMapping
	AccountID                    string
	Email                        string
	Folders                      []domain.Folder
	Messages                     []CachedMessage
	Removed                      []string
	Cursor                       *string
	Checkpoint                   *GmailCheckpoint
	ResetBackfill                bool
	ProcessedRemoteIDs           []string
	RecoveredRemoteIDs           []string
	Failures                     []MessageFailure
	PendingOpWatermark           *int64
}

// CommitSnapshot atomically updates the cache, FTS triggers, memberships, and
// cursor. Attachment objects are immutable and addressed by hash; a failed DB
// transaction may leave an orphan file, never a cursor pointing past lost mail.
func (db *DB) CommitSnapshot(ctx context.Context, s Snapshot) error {
	for i := range s.Messages {
		for j := range s.Messages[i].Attachments {
			a := &s.Messages[i].Attachments[j]
			digest := sha256.Sum256(a.Data)
			name := hex.EncodeToString(digest[:])
			directory := filepath.Join(filepath.Dir(db.path), "attachments")
			if err := os.MkdirAll(directory, 0700); err != nil {
				return err
			}
			path := filepath.Join(directory, name)
			file, err := os.CreateTemp(directory, ".attachment-")
			if err != nil {
				return err
			}
			tmp := file.Name()
			if _, err = file.Write(a.Data); err == nil {
				err = file.Sync()
			}
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(tmp, path)
			}
			_ = os.Remove(tmp)
			if err != nil {
				return err
			}
			relative := filepath.Join("attachments", name)
			a.LocalPath = &relative
			a.Size = int64(len(a.Data))
		}
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM accounts WHERE id=?)", s.AccountID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return sql.ErrNoRows
		}
		for _, f := range s.Folders {
			if f.AccountID != s.AccountID {
				return errors.New("folder account mismatch")
			}
			var owner string
			err := tx.QueryRowContext(ctx, `SELECT account_id FROM folders WHERE id=?`, f.ID).Scan(&owner)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if owner != "" && owner != s.AccountID {
				return errors.New("folder identity belongs to another account")
			}
			if f.ParentID != nil {
				var parentOwner string
				err := tx.QueryRowContext(ctx, `SELECT account_id FROM folders WHERE id=?`, *f.ParentID).Scan(&parentOwner)
				if errors.Is(err, sql.ErrNoRows) {
					for _, candidate := range s.Folders {
						if candidate.ID == *f.ParentID && candidate.AccountID == s.AccountID {
							parentOwner = s.AccountID
							break
						}
					}
				} else if err != nil {
					return err
				}
				if parentOwner != s.AccountID {
					return errors.New("folder parent account mismatch")
				}
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO folders(id,account_id,remote_id,name,folder_type,role,is_system,parent_id,sort_order,color) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET remote_id=excluded.remote_id,name=excluded.name,role=excluded.role,is_system=excluded.is_system,parent_id=excluded.parent_id,sort_order=excluded.sort_order,color=COALESCE(excluded.color,folders.color)`, f.ID, f.AccountID, f.RemoteID, f.Name, f.FolderType, f.Role, f.IsSystem, f.ParentID, f.SortOrder, f.Color)
			if err != nil {
				return err
			}
		}
		for _, id := range s.Removed {
			// A confirmed remote deletion supersedes local label intents. Clear
			// their FIFO blockers without trying to recreate the missing mail.
			if _, err := tx.ExecContext(ctx, "UPDATE pending_mail_ops SET status='done',last_error=NULL,next_retry_at=NULL WHERE account_id=? AND message_id=?", s.AccountID, id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM mail_op_sequence WHERE kind='succeeded' AND op_id IN (SELECT id FROM pending_mail_ops WHERE account_id=? AND message_id=?)", s.AccountID, id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE messages SET is_deleted=1 WHERE id=? AND account_id=?", id, s.AccountID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM message_folders WHERE message_id IN(SELECT id FROM messages WHERE id=? AND account_id=?)", id, s.AccountID); err != nil {
				return err
			}
		}
		rebuildThreads := false
		for _, cached := range s.Messages {
			m := cached.Message
			if m.AccountID != s.AccountID {
				return errors.New("message account mismatch")
			}
			var owner string
			var oldThread, oldMID, oldReply, oldRefs sql.NullString
			err := tx.QueryRowContext(ctx, "SELECT account_id,thread_id,message_id_header,in_reply_to,references_header FROM messages WHERE id=?", m.ID).Scan(&owner, &oldThread, &oldMID, &oldReply, &oldRefs)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil && owner != s.AccountID {
				return errors.New("message identity belongs to another account")
			}
			if nativeThread(m.ThreadID) == "" && (owner == "" || oldThread.String != "" || oldMID.String != textValue(m.MessageIDHeader) || oldReply.String != textValue(m.InReplyTo) || oldRefs.String != textValue(m.ReferencesHeader)) {
				rebuildThreads = true
			}
			to, _ := json.Marshal(m.To)
			cc, _ := json.Marshal(m.CC)
			bcc, _ := json.Marshal(m.BCC)
			if owner != "" {
				_, err = tx.ExecContext(ctx, `UPDATE messages SET remote_id=?,thread_id=?,message_id_header=?,in_reply_to=?,references_header=?,subject=?,snippet=?,from_address=?,from_name=?,to_list=?,cc_list=?,bcc_list=?,body_text=?,body_html_raw=?,has_attachments=?,is_read=?,is_starred=?,is_draft=?,date=?,is_deleted=0,updated_at=? WHERE id=? AND account_id=?`, m.RemoteID, m.ThreadID, m.MessageIDHeader, m.InReplyTo, m.ReferencesHeader, m.Subject, m.Snippet, m.FromAddress, m.FromName, string(to), string(cc), string(bcc), m.BodyText, m.BodyHTML, len(cached.Attachments) > 0, m.IsRead, m.IsStarred, m.IsDraft, m.Date, m.UpdatedAt, m.ID, s.AccountID)
			} else {
				_, err = tx.ExecContext(ctx, `INSERT INTO messages(id,account_id,remote_id,thread_id,message_id_header,in_reply_to,references_header,subject,snippet,from_address,from_name,to_list,cc_list,bcc_list,body_text,body_html_raw,has_attachments,is_read,is_starred,is_draft,date,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, m.ID, m.AccountID, m.RemoteID, m.ThreadID, m.MessageIDHeader, m.InReplyTo, m.ReferencesHeader, m.Subject, m.Snippet, m.FromAddress, m.FromName, string(to), string(cc), string(bcc), m.BodyText, m.BodyHTML, len(cached.Attachments) > 0, m.IsRead, m.IsStarred, m.IsDraft, m.Date, m.CreatedAt, m.UpdatedAt)
			}
			if err != nil {
				return err
			}
			changed, err := updateMessageThread(ctx, tx, m)
			if err != nil {
				return err
			}
			rebuildThreads = rebuildThreads || changed
			if _, err := tx.ExecContext(ctx, "DELETE FROM message_folders WHERE message_id=?", m.ID); err != nil {
				return err
			}
			for _, folder := range cached.FolderIDs {
				_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO message_folders(message_id,folder_id) SELECT ?,id FROM folders WHERE id=? AND account_id=?", m.ID, folder, s.AccountID)
				if err != nil {
					return err
				}
			}
			if err := overlayMailIntents(ctx, tx, m, s.PendingOpWatermark); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM attachments WHERE message_id=?", m.ID); err != nil {
				return err
			}
			for _, a := range cached.Attachments {
				if _, err := tx.ExecContext(ctx, "INSERT INTO attachments(id,message_id,filename,mime_type,size,local_path,content_id,is_inline) VALUES(?,?,?,?,?,?,?,?)", a.ID, m.ID, a.Filename, a.MIMEType, a.Size, a.LocalPath, a.ContentID, a.Inline); err != nil {
					return err
				}
			}
		}
		if err := saveProviderState(ctx, tx, s); err != nil {
			return err
		}
		if rebuildThreads {
			if err := rebuildHeaderThreads(ctx, tx, s.AccountID); err != nil {
				return err
			}
		}
		if s.ResetBackfill {
			if _, err := tx.ExecContext(ctx, "DELETE FROM gmail_backfill_pending WHERE account_id=?", s.AccountID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO gmail_backfill_pending(account_id,remote_id) SELECT account_id,remote_id FROM messages WHERE account_id=? AND is_deleted=0", s.AccountID); err != nil {
				return err
			}
		}
		for _, id := range s.ProcessedRemoteIDs {
			if _, err := tx.ExecContext(ctx, "DELETE FROM gmail_backfill_pending WHERE account_id=? AND remote_id=?", s.AccountID, id); err != nil {
				return err
			}
		}
		if err := saveMessageFailures(ctx, tx, s); err != nil {
			return err
		}
		if s.Checkpoint != nil {
			raw, err := json.Marshal(s.Checkpoint)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE accounts SET sync_state=json_set(COALESCE(NULLIF(sync_state,''),'{}'),'$.gmail_checkpoint',json(?)) WHERE id=?`, string(raw), s.AccountID); err != nil {
				return err
			}
		}
		if s.Cursor != nil {
			_, err := tx.ExecContext(ctx, `UPDATE accounts SET sync_state=json_set(COALESCE(NULLIF(sync_state,''),'{}'),'$.provider','gmail','$.last_sync_cursor',?),updated_at=? WHERE id=?`, *s.Cursor, time.Now().Unix(), s.AccountID)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (db *DB) Attachments(ctx context.Context, message string) ([]domain.Attachment, error) {
	rows, err := db.read.QueryContext(ctx, "SELECT id,message_id,filename,mime_type,size,local_path,content_id,is_inline FROM attachments WHERE message_id=?", message)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Attachment{}
	for rows.Next() {
		var a domain.Attachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Filename, &a.MIMEType, &a.Size, &a.LocalPath, &a.ContentID, &a.Inline); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (db *DB) Search(ctx context.Context, text, account string, limit int) ([]domain.SearchHit, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return []domain.SearchHit{}, nil
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
	query := `SELECT m.id,m.snippet,m.subject,m.from_address,m.date FROM message_search s JOIN messages m ON m.id=s.message_id WHERE m.is_deleted=0`
	args := []any{}
	if account != "" {
		query += " AND m.account_id=?"
		args = append(args, account)
	}
	if utf8.RuneCountInString(text) >= 3 {
		query += " AND message_search MATCH ?"
		args = append(args, "\""+strings.ReplaceAll(text, "\"", "\"\"")+"\"")
	} else {
		query += ` AND (m.subject LIKE ? ESCAPE '\' OR m.body_text LIKE ? ESCAPE '\')`
		pattern := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(text) + "%"
		args = append(args, pattern, pattern)
	}
	query += " ORDER BY m.date DESC LIMIT ?"
	args = append(args, limit)
	rows, err := db.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SearchHit{}
	for rows.Next() {
		var h domain.SearchHit
		h.Score = 1
		if err := rows.Scan(&h.MessageID, &h.Snippet, &h.Subject, &h.FromAddress, &h.Date); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (db *DB) UnreadCounts(ctx context.Context, account string, byFolder bool) (map[string]int, error) {
	query := "SELECT account_id,count(*) FROM messages WHERE is_read=0 AND is_deleted=0 GROUP BY account_id"
	args := []any{}
	if byFolder {
		query = "SELECT f.folder_id,count(*) FROM message_folders f JOIN messages m ON m.id=f.message_id WHERE m.is_read=0 AND m.is_deleted=0 AND m.account_id=? GROUP BY f.folder_id"
		args = append(args, account)
	}
	rows, err := db.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		out[id] = count
	}
	return out, rows.Err()
}

func ScopedID(account, remote string) string { return fmt.Sprintf("%s:%s", account, remote) }
