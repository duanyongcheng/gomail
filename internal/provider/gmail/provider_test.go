package gmail_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/mailmime"
	"github.com/bary/pebble-go/internal/provider/gmail"
	"github.com/bary/pebble-go/internal/store"
	"github.com/bary/pebble-go/internal/testgmail"
	"github.com/emersion/go-message/mail"
)

type tokens struct{ fail bool }

func (t *tokens) Token(context.Context) (string, error) {
	if t.fail {
		return "", errors.New("injected token failure")
	}
	return "demo-access", nil
}
func fixture(t *testing.T, db *store.DB, id string) (*gmail.Provider, *testgmail.Mock, *tokens) {
	t.Helper()
	mock := testgmail.NewMock()
	t.Cleanup(mock.Close)
	for _, remote := range []string{"m1", "m2"} {
		raw, err := mailmime.Build("sender@example.invalid", "中文邮件 "+id, "中文搜索 body", "<script>unsafe()</script>", remote+"@example.invalid", []*mail.Address{{Address: "to@example.invalid"}}, nil, []mailmime.Attachment{{Filename: "report.txt", MIMEType: "text/plain", Data: []byte("attachment content")}})
		if err != nil {
			t.Fatal(err)
		}
		mock.Set(remote, raw, []string{"INBOX", "UNREAD"})
	}
	if err := db.SaveAccount(t.Context(), domain.Account{ID: id, Email: id + "@example.invalid", DisplayName: id, Provider: "gmail", CreatedAt: 1, UpdatedAt: 1}, nil); err != nil {
		t.Fatal(err)
	}
	source := &tokens{}
	return gmail.New(db, mock.API(source), id), mock, source
}
func TestFetchFailureRollsBackCacheAndOpaqueCursor(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider, mock, _ := fixture(t, db, "a")
	if _, err := provider.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := db.Cursor(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	mock.ChangeHistory("900719925474099312399")
	mock.Lock()
	mock.FailMessage = "m2"
	mock.Unlock()
	if _, err := provider.Sync(t.Context()); err == nil {
		t.Fatal("remote fetch failure accepted")
	}
	after, err := db.Cursor(t.Context(), "a")
	if err != nil || before != after {
		t.Fatal("partial fetch advanced cursor", before, after, err)
	}
	hits, err := db.Search(t.Context(), "中文搜索", "a", 20)
	if err != nil || len(hits) != 2 {
		t.Fatal("partial fetch damaged cache/index", hits, err)
	}
}

func TestMultiAccountSyncHistoryFlagsAttachmentsAndIsolation(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, _, source := fixture(t, db, "a")
	b, _, _ := fixture(t, db, "b")
	for _, p := range []*gmail.Provider{a, b, a} {
		result, err := p.Sync(t.Context())
		if err != nil || result.Count != 2 {
			t.Fatal("sync or idempotence failed", result, err)
		}
	}
	messages, err := db.Messages(t.Context(), "", "", false, 50, 0)
	if err != nil || len(messages) != 4 {
		t.Fatal("account collision", len(messages), err)
	}
	for _, id := range []string{"a", "b"} {
		folders, err := db.Folders(t.Context(), id)
		if err != nil || len(folders) == 0 || folders[0].AccountID != id {
			t.Fatal("folder scope", folders, err)
		}
	}
	truth := true
	if err := a.Flags(t.Context(), "a:m1", &truth, &truth); err != nil {
		t.Fatal(err)
	}
	first, err := db.Message(t.Context(), "a:m1")
	if err != nil || !first.IsRead || !first.IsStarred {
		t.Fatal("flags missing", err)
	}
	second, err := db.Message(t.Context(), "b:m1")
	if err != nil || second.IsRead || second.IsStarred {
		t.Fatal("flags leaked across accounts", err)
	}
	if err := a.Flags(t.Context(), "b:m1", &truth, nil); err == nil {
		t.Fatal("cross account modification accepted")
	}
	attachments, err := db.Attachments(t.Context(), "a:m1")
	if err != nil || len(attachments) != 1 || attachments[0].LocalPath == nil {
		t.Fatal("attachment metadata missing", attachments, err)
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(db.Path()), *attachments[0].LocalPath))
	if err != nil || string(content) != "attachment content" {
		t.Fatal("attachment bytes missing", err)
	}
	before, err := db.Cursor(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	source.fail = true
	if _, err := a.Sync(t.Context()); err == nil {
		t.Fatal("failure ignored")
	}
	after, err := db.Cursor(t.Context(), "a")
	if err != nil || after != before {
		t.Fatal("failure advanced cursor", err)
	}
	hits, err := db.Search(t.Context(), "中文搜索", "b", 20)
	if err != nil || len(hits) != 2 {
		t.Fatal("search account scope", hits, err)
	}
	if err := db.DeleteAccount(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	hits, err = db.Search(t.Context(), "中文搜索", "", 20)
	if err != nil || len(hits) != 2 {
		t.Fatal("account deletion lost another account's search", hits, err)
	}
}
