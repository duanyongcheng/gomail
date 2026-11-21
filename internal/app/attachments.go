package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/duanyongcheng/pebble-go/internal/store"
)

type AttachmentRequest struct {
	AttachmentID string `json:"attachmentId"`
}
type DownloadAttachmentRequest struct {
	AttachmentID string `json:"attachmentId"`
	SaveTo       string `json:"saveTo"`
}
type AttachmentProgress struct {
	AttachmentID string `json:"attachment_id"`
	BytesCopied  int64  `json:"bytes_copied"`
	TotalBytes   int64  `json:"total_bytes"`
}
type AttachmentService struct{ Runtime *Runtime }

// Path preserves the legacy nullable result without returning stale/missing
// paths. Downloads must still use OpenAttachment rather than reopen this path.
func (s *AttachmentService) Path(ctx context.Context, request AttachmentRequest) (*string, error) {
	a, account, err := s.Runtime.DB.Attachment(ctx, request.AttachmentID)
	if err != nil {
		if store.IsMissingAttachment(err) {
			return nil, nil
		}
		return nil, errors.New("attachment lookup failed")
	}
	if a.LocalPath == nil || *a.LocalPath == "" {
		return nil, nil
	}
	handle, file, err := s.Runtime.DB.OpenAttachment(ctx, account, a.MessageID, a.ID)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("attachment file is unavailable; sync the account to recover it")
	}
	_ = handle.Close()
	return &file.Path, nil
}

func (s *AttachmentService) Download(ctx context.Context, request DownloadAttachmentRequest) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.Runtime.Context, cancel)
	defer stop()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	a, account, err := s.Runtime.DB.Attachment(ctx, request.AttachmentID)
	if err != nil {
		return "", errors.New("attachment not found")
	}
	source, _, err := s.Runtime.DB.OpenAttachment(ctx, account, a.MessageID, a.ID)
	if err != nil {
		return "", errors.New("attachment file is unavailable; sync the account to recover it")
	}
	defer source.Close()
	return copyAttachment(ctx, source, request.SaveTo, func(copied, total int64) {
		if s.Runtime.OnAttachmentProgress != nil {
			s.Runtime.OnAttachmentProgress(AttachmentProgress{AttachmentID: a.ID, BytesCopied: copied, TotalBytes: total})
		}
	})
}

// SuggestedAttachmentFilename is UX normalization, not the write security gate.
// Keep its portable character set aligned with src/lib/sanitizeFilename.ts.
func SuggestedAttachmentFilename(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) == 0 {
		return "download"
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		if strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, parts[len(parts)-1])
	name = strings.Trim(name, ". \t\r\n")
	for strings.Contains(name, "..") {
		name = strings.ReplaceAll(name, "..", ".")
	}
	if !validAttachmentFilename(name) {
		return "download"
	}
	return name
}

func validAttachmentFilename(name string) bool {
	if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `/\<>:"|?*`) || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	stem, _, _ := strings.Cut(name, ".")
	stem = strings.ToUpper(strings.TrimSpace(stem))
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return false
	}
	if len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9' {
		return false
	}
	return true
}

// copyAttachment never truncates existing files, including symlinks. The
// destination directory is pinned for creation and cleanup. Any incomplete
// output is removed; the immutable cache source is never modified.
func copyAttachment(ctx context.Context, source *os.File, saveTo string, progress func(int64, int64)) (result string, err error) {
	if !filepath.IsAbs(saveTo) || filepath.Clean(saveTo) != saveTo || !validAttachmentFilename(filepath.Base(saveTo)) {
		return "", errors.New("invalid attachment save path or filename")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	info, err := source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("attachment file is unavailable")
	}
	directory, err := filepath.EvalSymlinks(filepath.Dir(saveTo))
	if err != nil {
		return "", errors.New("attachment save directory is unavailable")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", errors.New("attachment save directory is unavailable")
	}
	defer root.Close()
	base := filepath.Base(saveTo)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	var target *os.File
	var name string
	for attempt := 0; attempt < 1000; attempt++ {
		name = base
		if attempt > 0 {
			name = fmt.Sprintf("%s (%d)%s", stem, attempt, ext)
		}
		target, err = root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return "", errors.New("cannot create attachment destination")
		}
	}
	if err != nil {
		return "", errors.New("cannot choose an unused attachment filename")
	}
	defer func() {
		_ = target.Close()
		if result == "" {
			_ = root.Remove(name)
		}
	}()
	var copied int64
	buffer := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := source.Read(buffer)
		if n > 0 {
			written, writeErr := target.Write(buffer[:n])
			if writeErr != nil || written != n {
				return "", errors.New("cannot write attachment destination")
			}
			copied += int64(n)
			if copied > info.Size() {
				return "", errors.New("attachment changed during download")
			}
			if copied < info.Size() {
				progress(copied, info.Size())
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", errors.New("cannot read attachment file")
		}
	}
	if copied != info.Size() {
		return "", errors.New("attachment changed during download")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := target.Sync(); err != nil {
		return "", errors.New("cannot flush attachment destination")
	}
	if err := target.Close(); err != nil {
		return "", errors.New("cannot close attachment destination")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	progress(copied, info.Size())
	return filepath.Join(directory, name), nil
}
