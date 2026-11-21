package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/store"
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
	if strings.Contains(result.Rendered.HTML, "tracker.invalid") || strings.Contains(result.Rendered.HTML, "onerror") || !strings.Contains(result.Rendered.HTML, "blocked-image") {
		t.Fatalf("unsafe rendering: %q", result.Rendered.HTML)
	}
	if result.Message.Capabilities.ModifyFlags {
		t.Fatal("offline account advertised remote flags capability")
	}
}

func TestRenderedMessageUsesPrivacyModeAndResolvesCIDThroughOpaqueURL(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SaveAccount(t.Context(), domain.Account{ID: "a", Email: "a", Provider: "gmail", CreatedAt: 1, UpdatedAt: 1}, nil); err != nil {
		t.Fatal(err)
	}
	message := domain.Message{ID: "a:m", AccountID: "a", RemoteID: "m", BodyHTML: `<p>Hi</p><script>bad()</script><img src="cid:logo">`, BodyText: "fallback", Date: 1, CreatedAt: 1, UpdatedAt: 1}
	contentID := "logo"
	if err := db.CommitSnapshot(t.Context(), store.Snapshot{AccountID: "a", Messages: []store.CachedMessage{{Message: message, Attachments: []domain.Attachment{{ID: "a:m:attachment:0", MessageID: "a:m", MIMEType: "image/png", ContentID: &contentID, Inline: true, Data: []byte("png")}}}}}); err != nil {
		t.Fatal(err)
	}
	service := &MessageService{Runtime: New(t.Context(), db, nil, nil)}
	result, err := service.Rendered(t.Context(), MessageRequest{MessageID: message.ID, PrivacyMode: json.RawMessage(`"Strict"`)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Rendered.HTML, "script") || !strings.Contains(result.Rendered.HTML, "pebble-attachment://") {
		t.Fatalf("privacy/CID render: %s", result.Rendered.HTML)
	}
}
