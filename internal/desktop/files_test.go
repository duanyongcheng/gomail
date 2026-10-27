package desktop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bary/pebble-go/internal/app"
	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/store"
	"github.com/egoist/mygo"
)

func TestSaveAttachmentDialogDefaultsCancelAndExport(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "pebble.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SaveAccount(t.Context(), domain.Account{ID: "a", Email: "a", Provider: "gmail"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.CommitSnapshot(t.Context(), store.Snapshot{AccountID: "a", Messages: []store.CachedMessage{{Message: domain.Message{ID: "m", AccountID: "a", RemoteID: "r"}, Attachments: []domain.Attachment{{ID: "att", MessageID: "m", Filename: `../../CON.txt`, Data: []byte("attachment")}}}}}); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "Downloads")
	mygo.App.SetPath(mygo.PathDownloads, directory)
	defer mygo.App.SetPath(mygo.PathDownloads, "")
	runtime := app.New(t.Context(), db, nil, nil)
	defer runtime.Scheduler.Close()
	var answer string
	calls := 0
	s := &FileService{Runtime: runtime, SaveDialog: func(options mygo.SaveDialogOptions) (string, error) {
		calls++
		if options.DefaultPath != filepath.Join(directory, "download") || !options.CreateDirectories {
			t.Fatal(options)
		}
		return answer, nil
	}}
	request := app.AttachmentRequest{AttachmentID: "att"}
	path, err := s.SaveAttachment(t.Context(), request)
	if path != nil || err != nil || calls != 1 {
		t.Fatal("cancel should be null", path, err, calls)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("cancel created file", entries, err)
	}
	answer = filepath.Join(directory, "saved.txt")
	path, err = s.SaveAttachment(t.Context(), request)
	canonical, _ := filepath.EvalSymlinks(answer)
	if path == nil || *path != canonical || err != nil {
		t.Fatal(path, err)
	}
	data, err := os.ReadFile(answer)
	if err != nil || string(data) != "attachment" {
		t.Fatal(string(data), err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.SaveAttachment(ctx, request); err == nil || calls != 2 {
		t.Fatal("cancelled request opened dialog", err, calls)
	}
	if _, err := s.SaveAttachment(t.Context(), app.AttachmentRequest{AttachmentID: "missing"}); err == nil || calls != 2 {
		t.Fatal("missing attachment opened dialog", err, calls)
	}
}
