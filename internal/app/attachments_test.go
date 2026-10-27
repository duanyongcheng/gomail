package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/store"
)

func attachmentFixture(t *testing.T, data []byte) *AttachmentService {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "pebble.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.SaveAccount(t.Context(), domain.Account{ID: "a", Provider: "gmail", Email: "a"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.CommitSnapshot(t.Context(), store.Snapshot{AccountID: "a", Messages: []store.CachedMessage{{Message: domain.Message{ID: "m", AccountID: "a", RemoteID: "remote"}, Attachments: []domain.Attachment{{ID: "att", MessageID: "m", Filename: "report.txt", MIMEType: "text/plain", Data: data}}}}}); err != nil {
		t.Fatal(err)
	}
	r := New(t.Context(), db, nil, nil)
	t.Cleanup(r.Scheduler.Close)
	return &AttachmentService{Runtime: r}
}

func TestAttachmentExportConflictProgressAndCachePreserved(t *testing.T) {
	data := bytes.Repeat([]byte("mail attachment\n"), 15000)
	s := attachmentFixture(t, data)
	source, err := s.Path(t.Context(), AttachmentRequest{AttachmentID: "att"})
	if err != nil || source == nil {
		t.Fatal(source, err)
	}
	directory := t.TempDir()
	destination := filepath.Join(directory, "report.txt")
	if err := os.WriteFile(destination, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	var events []AttachmentProgress
	s.Runtime.OnAttachmentProgress = func(p AttachmentProgress) { events = append(events, p) }
	actual, err := s.Download(t.Context(), DownloadAttachmentRequest{AttachmentID: "att", SaveTo: destination})
	canonical, _ := filepath.EvalSymlinks(directory)
	if err != nil || actual != filepath.Join(canonical, "report (1).txt") {
		t.Fatal(actual, err)
	}
	for path, want := range map[string][]byte{actual: data, *source: data, destination: []byte("existing")} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("unexpected content at %s: %v", path, err)
		}
	}
	if len(events) < 2 || events[len(events)-1] != (AttachmentProgress{AttachmentID: "att", BytesCopied: int64(len(data)), TotalBytes: int64(len(data))}) {
		t.Fatal(events)
	}
	for i, p := range events {
		if p.AttachmentID != "att" || p.TotalBytes != int64(len(data)) || (i > 0 && p.BytesCopied <= events[i-1].BytesCopied) {
			t.Fatal("non-monotonic progress", events)
		}
	}
}

func TestAttachmentExportCancelCleansPartialAndMissingIsNullable(t *testing.T) {
	s := attachmentFixture(t, bytes.Repeat([]byte("x"), 200000))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.Runtime.OnAttachmentProgress = func(p AttachmentProgress) {
		if p.BytesCopied == p.TotalBytes {
			t.Error("cancelled download reported complete")
		}
		cancel()
	}
	directory := t.TempDir()
	actual, err := s.Download(ctx, DownloadAttachmentRequest{AttachmentID: "att", SaveTo: filepath.Join(directory, "cancel.txt")})
	if !errors.Is(err, context.Canceled) || actual != "" {
		t.Fatal(actual, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("partial file retained", entries, err)
	}
	path, err := s.Path(t.Context(), AttachmentRequest{AttachmentID: "missing"})
	if err != nil || path != nil {
		t.Fatal(path, err)
	}
	path, err = s.Path(t.Context(), AttachmentRequest{AttachmentID: "att"})
	if err != nil || path == nil {
		t.Fatal(path, err)
	}
	if err := os.Remove(*path); err != nil {
		t.Fatal(err)
	}
	path, err = s.Path(t.Context(), AttachmentRequest{AttachmentID: "att"})
	if err != nil || path != nil {
		t.Fatal("missing cache should return null", path, err)
	}
	if _, err := s.Download(t.Context(), DownloadAttachmentRequest{AttachmentID: "att", SaveTo: filepath.Join(directory, "missing.txt")}); err == nil {
		t.Fatal("missing file exported")
	}
}

func TestAttachmentExportEmptyAndConcurrentUniqueFiles(t *testing.T) {
	s := attachmentFixture(t, nil)
	s.Runtime.OnAttachmentProgress = func(p AttachmentProgress) {
		if p.BytesCopied != 0 || p.TotalBytes != 0 {
			t.Error(p)
		}
	}
	directory := t.TempDir()
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			actual, err := s.Download(t.Context(), DownloadAttachmentRequest{AttachmentID: "att", SaveTo: filepath.Join(directory, "empty.txt")})
			if err != nil {
				t.Error(err)
				return
			}
			info, err := os.Stat(actual)
			if err != nil || info.Size() != 0 {
				t.Error(info, err)
			}
		})
	}
	wg.Wait()
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 5 {
		t.Fatal(entries, err)
	}
	if err := s.Runtime.DB.CommitSnapshot(t.Context(), store.Snapshot{AccountID: "a", Removed: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	path, err := s.Path(t.Context(), AttachmentRequest{AttachmentID: "att"})
	if path != nil || err != nil {
		t.Fatal("soft deleted message still accessible", path, err)
	}
}

func TestAttachmentExportRejectsUnsafeSavePathsAndSymlinkDoesNotOverwrite(t *testing.T) {
	s := attachmentFixture(t, []byte("new"))
	directory := t.TempDir()
	for _, name := range []string{"CON.txt", "nul.tar.gz", "LPT1", "bad?.txt", "tail.", "tail ", "bad\n.txt", "a..b", "..", "a:b"} {
		if _, err := s.Download(t.Context(), DownloadAttachmentRequest{AttachmentID: "att", SaveTo: directory + string(filepath.Separator) + name}); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	for _, path := range []string{"relative.txt", directory + "/../escape.txt", directory + "/", filepath.Join(directory, "absent", "file.txt")} {
		if _, err := s.Download(t.Context(), DownloadAttachmentRequest{AttachmentID: "att", SaveTo: path}); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	outside := filepath.Join(t.TempDir(), "precious.txt")
	if err := os.WriteFile(outside, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symlink unavailable:", err)
	}
	actual, err := s.Download(t.Context(), DownloadAttachmentRequest{AttachmentID: "att", SaveTo: link})
	canonical, _ := filepath.EvalSymlinks(directory)
	if err != nil || actual != filepath.Join(canonical, "link (1).txt") {
		t.Fatal(actual, err)
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "old" {
		t.Fatal("symlink target overwritten", err)
	}
}

func TestSuggestedAttachmentFilename(t *testing.T) {
	for input, want := range map[string]string{"../../hello.txt": "hello.txt", `C:\mail\测试.pdf`: "测试.pdf", "a:b?.txt": "a_b_.txt", "CON.txt": "download", "NUL.tar.gz": "download", "..": "download", "report. ": "report", "a\nb.txt": "ab.txt"} {
		if got := SuggestedAttachmentFilename(input); got != want || !validAttachmentFilename(got) {
			t.Errorf("%q: %q != %q", input, got, want)
		}
	}
}

func TestAttachmentExportReadFailureRemovesOutputAndHidesPaths(t *testing.T) {
	s := attachmentFixture(t, bytes.Repeat([]byte("x"), 200000))
	path, err := s.Path(t.Context(), AttachmentRequest{AttachmentID: "att"})
	if err != nil || path == nil {
		t.Fatal(path, err)
	}
	f, err := os.Open(*path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	directory := t.TempDir()
	_, err = copyAttachment(t.Context(), f, filepath.Join(directory, "broken.txt"), func(_, _ int64) { _ = f.Close() })
	if err == nil || strings.Contains(err.Error(), directory) || strings.Contains(err.Error(), *path) {
		t.Fatal("expected static read error", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}
