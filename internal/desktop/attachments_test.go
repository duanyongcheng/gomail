package desktop

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/privacy"
	"github.com/duanyongcheng/pebble-go/internal/store"
)

func TestAttachmentHandlerServesOnlyAuthorizedOpaqueURL(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SaveAccount(t.Context(), domain.Account{ID: "a", Email: "a", Provider: "gmail", CreatedAt: 1, UpdatedAt: 1}, nil); err != nil {
		t.Fatal(err)
	}
	contentID := "logo"
	if err := db.CommitSnapshot(t.Context(), store.Snapshot{AccountID: "a", Messages: []store.CachedMessage{{Message: domain.Message{ID: "a:m", AccountID: "a", RemoteID: "m", BodyText: "body", CreatedAt: 1, UpdatedAt: 1}, Attachments: []domain.Attachment{{ID: "a:m:attachment:0", MessageID: "a:m", MIMEType: "image/png", ContentID: &contentID, Data: []byte("png")}}}}}); err != nil {
		t.Fatal(err)
	}
	handler := AttachmentHandler(db)
	request := httptest.NewRequest("GET", privacy.AttachmentURL("a", "a:m", "a:m:attachment:0"), nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != 200 || recorder.Body.String() != "png" || recorder.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("attachment response: %d %q %s", recorder.Code, recorder.Body.String(), recorder.Header().Get("Content-Type"))
	}
	request = httptest.NewRequest("GET", privacy.AttachmentURL("other", "a:m", "a:m:attachment:0"), nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != 404 {
		t.Fatalf("cross-account attachment returned %d", recorder.Code)
	}
	request = httptest.NewRequest("GET", "pebble-attachment://localhost/not-a-token", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != 404 {
		t.Fatalf("invalid attachment returned %d", recorder.Code)
	}
}
