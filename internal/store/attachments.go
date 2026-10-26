package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
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
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "..") {
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

func IsMissingAttachment(err error) bool { return errors.Is(err, sql.ErrNoRows) }
