package store

import (
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

func ptr(value string) *string { return &value }
