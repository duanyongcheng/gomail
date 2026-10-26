package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/store"
)

func TestRenderedMessageEscapesPlainTextAndDoesNotExposeRawHTML(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SaveAccount(t.Context(), domain.Account{ID: "a", Email: "a", Provider: "gmail", CreatedAt: 1, UpdatedAt: 1}, nil); err != nil {
		t.Fatal(err)
	}
	message := domain.Message{ID: "a:m", AccountID: "a", RemoteID: "m", BodyText: "<script>plain text only</script>", BodyHTML: `<img src="https://tracker.invalid/pixel" onerror="danger()">`, Date: 1, CreatedAt: 1, UpdatedAt: 1}
	if err := db.CommitSnapshot(t.Context(), store.Snapshot{AccountID: "a", Messages: []store.CachedMessage{{Message: message}}}); err != nil {
		t.Fatal(err)
	}
	service := &MessageService{Runtime: New(t.Context(), db, nil, nil)}
	result, err := service.Rendered(t.Context(), MessageRequest{MessageID: message.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Rendered.HTML != "<pre>&lt;script&gt;plain text only&lt;/script&gt;</pre>" || strings.Contains(result.Rendered.HTML, "tracker.invalid") {
		t.Fatalf("unsafe rendering: %q", result.Rendered.HTML)
	}
	if result.Message.Capabilities.ModifyFlags {
		t.Fatal("offline account advertised remote flags capability")
	}
}
