package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

type AttachmentFile struct {
	MIMEType string
	Path     string
	Size     int64
}

// AttachmentFile validates the attachment/message/account relationship before
// exposing a profile-relative file path to the desktop protocol adapter.
func (db *DB) AttachmentFile(ctx context.Context, accountID, messageID, attachmentID string) (AttachmentFile, error) {
	var file AttachmentFile
	var relative string
	err := db.read.QueryRowContext(ctx, `SELECT a.mime_type,a.local_path,a.size
		FROM attachments a JOIN messages m ON m.id=a.message_id
		WHERE a.id=? AND a.message_id=? AND m.account_id=? AND m.is_deleted=0`, attachmentID, messageID, accountID).Scan(&file.MIMEType, &relative, &file.Size)
	if err != nil {
		return file, err
	}
	if !filepath.IsLocal(relative) || strings.Contains(relative, "..") ||
		!strings.HasPrefix(filepath.ToSlash(relative), "attachments/") {
		return AttachmentFile{}, errors.New("invalid attachment path")
	}
	root := filepath.Dir(db.path)
	full := filepath.Clean(filepath.Join(root, relative))
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return AttachmentFile{}, err
	}
	cleanFull, err := filepath.Abs(full)
	if err != nil {
		return AttachmentFile{}, err
	}
	if cleanFull != cleanRoot && !strings.HasPrefix(cleanFull, cleanRoot+string(filepath.Separator)) {
		return AttachmentFile{}, errors.New("attachment path escapes profile")
	}
	file.Path = cleanFull
	return file, nil
}

// Attachment resolves an ID only while its owning message is live. Account and
// message IDs used by export come from this relationship, never from a path.
func (db *DB) Attachment(ctx context.Context, id string) (domain.Attachment, string, error) {
	var a domain.Attachment
	var account string
	err := db.read.QueryRowContext(ctx, `SELECT a.id,a.message_id,a.filename,a.mime_type,a.size,a.local_path,a.content_id,a.is_inline,m.account_id
		FROM attachments a JOIN messages m ON m.id=a.message_id WHERE a.id=? AND m.is_deleted=0`, id).
		Scan(&a.ID, &a.MessageID, &a.Filename, &a.MIMEType, &a.Size, &a.LocalPath, &a.ContentID, &a.Inline, &account)
	return a, account, err
}

// OpenAttachment is shared by export and the CID protocol. Lexical path checks
// alone do not stop symlinks: os.Root also constrains resolution during open.
// Only the attachment tree is readable, not other profile files (DB/tokens).
func (db *DB) OpenAttachment(ctx context.Context, accountID, messageID, attachmentID string) (*os.File, AttachmentFile, error) {
	file, err := db.AttachmentFile(ctx, accountID, messageID, attachmentID)
	if err != nil {
		return nil, file, err
	}
	profile, err := os.OpenRoot(filepath.Dir(db.path))
	if err != nil {
		return nil, file, err
	}
	defer profile.Close()
	rootInfo, err := profile.Lstat("attachments")
	if err != nil {
		return nil, file, err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, file, errors.New("invalid attachment directory")
	}
	root, err := profile.OpenRoot("attachments")
	if err != nil {
		return nil, file, err
	}
	defer root.Close()
	directory, err := filepath.Abs(filepath.Join(filepath.Dir(db.path), "attachments"))
	if err != nil {
		return nil, file, err
	}
	relative, err := filepath.Rel(directory, file.Path)
	if err != nil {
		return nil, file, err
	}
	// Reject known non-regular files before opening (in particular FIFOs).
	info, err := root.Stat(relative)
	if err != nil {
		return nil, file, err
	}
	if !info.Mode().IsRegular() {
		return nil, file, errors.New("attachment is not a regular file")
	}
	handle, err := root.Open(relative)
	if err != nil {
		return nil, file, err
	}
	info, err = handle.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != file.Size {
		_ = handle.Close()
		return nil, file, errors.New("attachment file is incomplete or unavailable")
	}
	return handle, file, nil
}

func IsMissingAttachment(err error) bool { return errors.Is(err, sql.ErrNoRows) }
