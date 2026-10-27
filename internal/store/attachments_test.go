package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bary/pebble-go/internal/domain"
)

func TestAttachmentFileRequiresAccountMessageAndStaysInsideProfile(t *testing.T) {
	db := openStore(t)
	if err := db.SaveAccount(t.Context(), domain.Account{ID: "a", Email: "a", Provider: "gmail", CreatedAt: 1, UpdatedAt: 1}, nil); err != nil {
		t.Fatal(err)
	}
	message := domain.Message{ID: "a:m", AccountID: "a", RemoteID: "m", BodyText: "body", CreatedAt: 1, UpdatedAt: 1}
	if err := db.CommitSnapshot(t.Context(), Snapshot{AccountID: "a", Messages: []CachedMessage{{Message: message, Attachments: []domain.Attachment{{ID: "a:m:attachment:0", MessageID: "a:m", Filename: "inline.png", MIMEType: "image/png", ContentID: ptr("logo"), Inline: true, Data: []byte("png")}}}}}); err != nil {
		t.Fatal(err)
	}
	file, err := db.AttachmentFile(t.Context(), "a", "a:m", "a:m:attachment:0")
	if err != nil || file.MIMEType != "image/png" || file.Size != 3 {
		t.Fatal(file, err)
	}
	if filepath.Dir(file.Path) != filepath.Join(filepath.Dir(db.Path()), "attachments") {
		t.Fatal("attachment escaped expected directory", file.Path)
	}
	if data, err := os.ReadFile(file.Path); err != nil || string(data) != "png" {
		t.Fatal(string(data), err)
	}
	if _, err := db.AttachmentFile(t.Context(), "other", "a:m", "a:m:attachment:0"); err == nil {
		t.Fatal("cross-account attachment accepted")
	}
	if _, err := db.AttachmentFile(t.Context(), "a", "other", "a:m:attachment:0"); err == nil {
		t.Fatal("cross-message attachment accepted")
	}
}

func TestOpenAttachmentRejectsTraversalSymlinksAndNonRegularFiles(t *testing.T) {
	db := openStore(t)
	ctx := t.Context()
	if err := db.SaveAccount(ctx, domain.Account{ID: "a", Email: "a", Provider: "gmail"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.CommitSnapshot(ctx, Snapshot{AccountID: "a", Messages: []CachedMessage{{Message: domain.Message{ID: "m", AccountID: "a", RemoteID: "r"}, Attachments: []domain.Attachment{{ID: "att", MessageID: "m", Data: []byte("ok")}}}}}); err != nil {
		t.Fatal(err)
	}
	file, err := db.AttachmentFile(ctx, "a", "m", "att")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret", "pebble.sqlite", file.Path, "attachments/../pebble.sqlite", "attachments/directory"} {
		if _, err := db.write.ExecContext(ctx, "UPDATE attachments SET local_path=? WHERE id='att'", path); err != nil {
			t.Fatal(err)
		}
		if f, _, err := db.OpenAttachment(ctx, "a", "m", "att"); err == nil {
			_ = f.Close()
			t.Fatalf("opened unsafe source %q", path)
		}
	}
	if err := os.Mkdir(filepath.Join(filepath.Dir(file.Path), "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	if f, _, err := db.OpenAttachment(ctx, "a", "m", "att"); err == nil {
		_ = f.Close()
		t.Fatal("opened directory")
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	for i, target := range []string{outside, "../pebble.sqlite"} {
		name := fmt.Sprintf("link%d", i)
		if err := os.Symlink(target, filepath.Join(filepath.Dir(file.Path), name)); err != nil {
			t.Skip("symlink unavailable:", err)
		}
		if _, err := db.write.ExecContext(ctx, "UPDATE attachments SET local_path=? WHERE id='att'", "attachments/"+name); err != nil {
			t.Fatal(err)
		}
		if f, _, err := db.OpenAttachment(ctx, "a", "m", "att"); err == nil {
			_ = f.Close()
			t.Fatal("opened source symlink outside attachments")
		}
	}
}

func ptr(value string) *string { return &value }
