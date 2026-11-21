package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/domain"
)

func threadFixture(t *testing.T) *DB {
	t.Helper()
	db := openStore(t)
	for _, account := range []string{"a", "b"} {
		if err := db.SaveAccount(t.Context(), domain.Account{ID: account, Email: account, Provider: "gmail"}, nil); err != nil {
			t.Fatal(err)
		}
		if err := db.CommitSnapshot(t.Context(), Snapshot{AccountID: account, Folders: []domain.Folder{{ID: account + "-inbox", AccountID: account, RemoteID: "INBOX", Name: "Inbox", FolderType: "label"}, {ID: account + "-sent", AccountID: account, RemoteID: "SENT", Name: "Sent", FolderType: "label"}}}); err != nil {
			t.Fatal(err)
		}
	}
	return db
}
func cachedThread(account, id, thread string, date int64, folders ...string) CachedMessage {
	m := domain.Message{ID: account + ":" + id, AccountID: account, RemoteID: id, Subject: "Same subject", Snippet: "snippet-" + id, Date: date, FromAddress: "sender@example.invalid", MessageIDHeader: ptr(id + "@example.invalid"), BodyText: "body"}
	if thread != "" {
		m.ThreadID = ptr(thread)
	}
	return CachedMessage{Message: m, FolderIDs: folders}
}
func commitThreads(t *testing.T, db *DB, account string, messages ...CachedMessage) {
	t.Helper()
	if err := db.CommitSnapshot(t.Context(), Snapshot{AccountID: account, Messages: messages}); err != nil {
		t.Fatal(err)
	}
}
func TestThreadsScopeDedupLatestMetadataAndDetail(t *testing.T) {
	db := threadFixture(t)
	first := cachedThread("a", "first", "shared", 1, "a-inbox", "a-sent")
	second := cachedThread("a", "second", "shared", 2, "a-inbox")
	second.Message.Subject = "Newest selected"
	second.Message.IsRead = true
	second.Message.FromAddress = "other||sender@example.invalid"
	second.Attachments = []domain.Attachment{{ID: "file", Filename: "x", MIMEType: "text/plain", Data: []byte("x")}}
	third := cachedThread("a", "third", "shared", 3, "a-sent")
	third.Message.Subject = "Outside selected folder"
	commitThreads(t, db, "a", first, second, third, cachedThread("a", "separate", "different", 1, "a-inbox"))
	commitThreads(t, db, "b", cachedThread("b", "first", "shared", 4, "b-inbox"))
	rows, err := db.Threads(t.Context(), ThreadsFilter{FolderIDs: []string{"a-inbox", "b-inbox"}, Limit: 50})
	if err != nil || len(rows) != 3 {
		t.Fatal(rows, err)
	}
	var a domain.ThreadSummary
	for _, r := range rows {
		if r.AccountID == "a" && r.MessageCount == 2 {
			a = r
		}
	}
	if a.Subject != "Newest selected" || a.Snippet != "snippet-second" || a.UnreadCount != 1 || !a.HasAttachments || len(a.Participants) != 2 {
		t.Fatal(a)
	}
	if a.ThreadID == rows[0].ThreadID {
		t.Fatal("same remote ID crossed accounts")
	}
	detail, err := db.ThreadMessages(t.Context(), a.ThreadID)
	if err != nil || len(detail) != 3 || detail[0].ID != "a:first" || detail[2].ID != "a:third" || detail[0].BodyText != "" {
		t.Fatal(detail, err)
	}
	for _, m := range detail {
		if m.AccountID != "a" {
			t.Fatal("cross-account detail")
		}
	}
	multi, err := db.Threads(t.Context(), ThreadsFilter{FolderIDs: []string{"a-inbox", "a-sent", "a-inbox"}})
	if err != nil || len(multi) != 2 || multi[0].MessageCount != 3 {
		t.Fatal("duplicate folder memberships counted", multi, err)
	}
	if _, err := db.ThreadMessages(t.Context(), "shared"); err == nil {
		t.Fatal("bare remote thread accepted")
	}
	if err := db.CommitSnapshot(t.Context(), Snapshot{AccountID: "a", Removed: []string{"a:first"}}); err != nil {
		t.Fatal(err)
	}
	detail, err = db.ThreadMessages(t.Context(), a.ThreadID)
	if err != nil || len(detail) != 2 {
		t.Fatal(detail, err)
	}
}
func TestHeaderThreadsOutOfOrderMissingAncestorCycleAndNativeAuthority(t *testing.T) {
	db := threadFixture(t)
	child := cachedThread("a", "child", "", 2, "a-inbox")
	child.Message.InReplyTo = ptr("<root@example.invalid>")
	commitThreads(t, db, "a", child)
	initial, err := db.Threads(t.Context(), ThreadsFilter{FolderIDs: []string{"a-inbox"}})
	if err != nil || len(initial) != 1 {
		t.Fatal(initial, err)
	}
	root := cachedThread("a", "root", "", 1, "a-inbox")
	grandchild := cachedThread("a", "last", "", 3, "a-inbox")
	grandchild.Message.ReferencesHeader = ptr("<root@example.invalid> <child@example.invalid>")
	commitThreads(t, db, "a", root, grandchild, cachedThread("a", "unrelated", "", 4, "a-inbox"))
	rows, err := db.Threads(t.Context(), ThreadsFilter{FolderIDs: []string{"a-inbox"}})
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	var group domain.ThreadSummary
	for _, r := range rows {
		if r.MessageCount == 3 {
			group = r
		}
	}
	if group.ThreadID == "" {
		t.Fatal("reply chain did not join", rows)
	}
	if err := db.CommitSnapshot(t.Context(), Snapshot{AccountID: "a", Removed: []string{"a:root"}}); err != nil {
		t.Fatal(err)
	}
	messages, err := db.ThreadMessages(t.Context(), group.ThreadID)
	if err != nil || len(messages) != 2 {
		t.Fatal(messages, err)
	}
	x := cachedThread("a", "x", "", 7, "a-inbox")
	x.Message.InReplyTo = ptr("y@example.invalid")
	y := cachedThread("a", "y", "", 8, "a-inbox")
	y.Message.InReplyTo = ptr("x@example.invalid")
	commitThreads(t, db, "a", x, y)
	one := cachedThread("a", "native-one", "one", 9, "a-inbox")
	one.Message.InReplyTo = ptr("native-two@example.invalid")
	two := cachedThread("a", "native-two", "two", 10, "a-inbox")
	two.Message.InReplyTo = ptr("native-one@example.invalid")
	commitThreads(t, db, "a", one, two)
	rows, err = db.Threads(t.Context(), ThreadsFilter{FolderIDs: []string{"a-inbox"}})
	if err != nil || len(rows) != 5 {
		t.Fatal(rows, err)
	}
	if rows[0].ThreadID == rows[1].ThreadID {
		t.Fatal("native groups merged by headers")
	}
}
func TestThreadPaginationAndCheckpointRollback(t *testing.T) {
	db := threadFixture(t)
	messages := []CachedMessage{}
	for i := 0; i < 61; i++ {
		messages = append(messages, cachedThread("a", fmt.Sprint(i), fmt.Sprint(i), 1, "a-inbox"))
	}
	commitThreads(t, db, "a", messages...)
	first, err := db.Threads(t.Context(), ThreadsFilter{FolderIDs: []string{"a-inbox"}, Limit: 50})
	if err != nil || len(first) != 50 {
		t.Fatal(len(first), err)
	}
	second, err := db.Threads(t.Context(), ThreadsFilter{FolderIDs: []string{"a-inbox"}, Limit: 50, Offset: 50})
	if err != nil || len(second) != 11 {
		t.Fatal(len(second), err)
	}
	seen := map[string]bool{}
	for _, r := range append(first, second...) {
		if seen[r.ThreadID] {
			t.Fatal("unstable paging")
		}
		seen[r.ThreadID] = true
	}
	execute(t, db.write, `CREATE TRIGGER reject_thread_checkpoint BEFORE UPDATE OF sync_state ON accounts BEGIN SELECT RAISE(ABORT,'injected'); END`)
	cursor := "10"
	m := cachedThread("a", "new", "new", 3, "a-inbox")
	if err := db.CommitSnapshot(t.Context(), Snapshot{AccountID: "a", Messages: []CachedMessage{m}, Cursor: &cursor}); err == nil {
		t.Fatal("failure ignored")
	}
	if integer(t, db.write, "SELECT COUNT(*) FROM message_threads") != 61 {
		t.Fatal("thread index not rolled back")
	}
	for _, filter := range []ThreadsFilter{{FolderIDs: []string{"a-inbox"}, Limit: 201}, {FolderIDs: []string{"a-inbox"}, Offset: -1}} {
		if _, err := db.Threads(t.Context(), filter); err == nil {
			t.Fatal("bad pagination accepted")
		}
	}
}
func TestV27MigratesExistingThreadsAndAccountCascade(t *testing.T) {
	db := openRaw(t)
	ctx := t.Context()
	if err := migrate(ctx, db, 26); err != nil {
		t.Fatal(err)
	}
	execute(t, db, `INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES('a','a','gmail',1,1)`)
	execute(t, db, `INSERT INTO messages(id,account_id,remote_id,thread_id,message_id_header,in_reply_to,subject,date,created_at,updated_at) VALUES('root','a','r',NULL,'root@example.invalid',NULL,'s',1,1,1),('child','a','c',NULL,'child@example.invalid','root@example.invalid','s',2,1,1),('native','a','n','native',NULL,NULL,'s',3,1,1)`)
	if err := migrate(ctx, db, 27); err != nil {
		t.Fatal(err)
	}
	checkLegacySchema(t, db)
	if integer(t, db, "SELECT COUNT(*) FROM message_threads") != 3 || integer(t, db, "SELECT COUNT(DISTINCT thread_key) FROM message_threads") != 2 {
		t.Fatal("migration grouping failed")
	}
	execute(t, db, "DELETE FROM accounts WHERE id='a'")
	if integer(t, db, "SELECT COUNT(*) FROM message_threads") != 0 {
		t.Fatal("index did not cascade")
	}
}
func TestThreadDetailBoundAndCancellation(t *testing.T) {
	db := threadFixture(t)
	messages := []CachedMessage{}
	for i := 0; i < 501; i++ {
		messages = append(messages, cachedThread("a", fmt.Sprint(i), "large", int64(i), "a-inbox"))
	}
	commitThreads(t, db, "a", messages...)
	if _, err := db.ThreadMessages(t.Context(), threadHandle("a", "native", "large")); err == nil {
		t.Fatal("large thread silently returned")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.Threads(ctx, ThreadsFilter{FolderIDs: []string{"a-inbox"}}); err == nil {
		t.Fatal("cancel ignored")
	}
}
