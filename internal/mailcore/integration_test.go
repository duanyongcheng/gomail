package mailcore

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bary/pebble-go/internal/testmail"
	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/emersion/go-message/mail"
)

func setup(t *testing.T) (*Core, *testmail.Servers) {
	t.Helper()
	servers, err := testmail.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(servers.Close)
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "mail.sqlite"), sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	data, err := os.ReadFile("../../schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range strings.Split(string(data), ";") {
		if strings.TrimSpace(stmt) != "" {
			if _, err = db.Execute(context.Background(), stmt); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, err = db.Execute(context.Background(), "INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES(?,?,'imap',0,0)", AccountID, testmail.Username)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Execute(context.Background(), "INSERT INTO folders(id,account_id,remote_id,name,folder_type,role) VALUES('poc-inbox',?,'INBOX','Inbox','folder','inbox')", AccountID)
	if err != nil {
		t.Fatal(err)
	}
	core, err := New(context.Background(), db, Config{servers.IMAPAddr, servers.SMTPAddr, testmail.Username, testmail.Password, servers.ClientTLS})
	if err != nil {
		t.Fatal(err)
	}
	return core, servers
}
func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}
func fixture(t *testing.T, subject, body, id string) []byte {
	t.Helper()
	raw, err := Build("sender@example.invalid", subject, body, "<h2>中文 HTML 协议邮件</h2>", id, []*mail.Address{{Address: testmail.Username}}, nil, []Attachment{{Filename: "报告.txt", MIMEType: "text/plain", Data: []byte("附件内容")}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func count(t *testing.T, c *Core, sql string) int64 {
	t.Helper()
	r, e := c.DB.Query(ctx(t), sql)
	if e != nil {
		t.Fatal(e)
	}
	return r.Values[0][0].(int64)
}

func TestTLSIMAPSnapshotMIMEFlagsAndSearch(t *testing.T) {
	c, s := setup(t)
	first, err := s.Append(fixture(t, "协议测试中文主题", "中文搜索 and Hello world", "first@example.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.Sync(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 1 {
		t.Fatal(result)
	}
	if _, err = c.Sync(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if n := count(t, c, "SELECT count(*) FROM messages"); n != 1 {
		t.Fatal("duplicate", n)
	}
	r, err := c.DB.Query(ctx(t), "SELECT subject,body_text,is_read FROM messages")
	if err != nil {
		t.Fatal(err)
	}
	if r.Values[0][0] != "协议测试中文主题" || !strings.Contains(r.Values[0][1].(string), "中文搜索") || r.Values[0][2] != int64(0) {
		t.Fatal(r.Values)
	}
	if count(t, c, "SELECT count(*) FROM attachments WHERE filename='报告.txt'") != 1 {
		t.Fatal("MIME attachment missing")
	}
	for _, q := range []string{"中文搜索", "中文", "Hello", "HELLO"} {
		hits, e := c.Search(ctx(t), q, AccountID, 10)
		if e != nil || len(hits) != 1 {
			t.Fatalf("q=%q hits=%v err=%v", q, hits, e)
		}
	}
	if hits, e := c.Search(ctx(t), "Hello", "other-account", 10); e != nil || len(hits) != 0 {
		t.Fatal("account scope", hits, e)
	}
	truth := true
	if err = c.Flags(ctx(t), result.IDs[0], &truth, &truth); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Sync(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if count(t, c, "SELECT count(*) FROM messages WHERE is_read=1 AND is_starred=1") != 1 {
		t.Fatal("remote flags did not survive resync")
	}
	if !slices.Contains(s.Commands(), "UID STORE") {
		t.Fatal("no real STORE")
	}
	if err = s.Expunge(first); err != nil {
		t.Fatal(err)
	}
	if result, err = c.Sync(ctx(t)); err != nil || result.Count != 0 {
		t.Fatal(result, err)
	}
	if count(t, c, "SELECT count(*) FROM messages WHERE is_deleted=0") != 0 {
		t.Fatal("expunge not reconciled")
	}
	if hits, e := c.Search(ctx(t), "Hello", AccountID, 10); e != nil || len(hits) != 0 {
		t.Fatal("stale search", hits, e)
	}
}

func TestFailureDoesNotAdvanceCursorAndGenerationRejectsStaleUID(t *testing.T) {
	c, s := setup(t)
	_, _ = s.Append(fixture(t, "First", "initial", "one@example.invalid"))
	initial, err := c.Sync(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Append(fixture(t, "Second", "new", "two@example.invalid"))
	s.FailFetch.Store(true)
	if _, err = c.Sync(ctx(t)); err == nil {
		t.Fatal("expected FETCH failure")
	}
	cursor, err := c.Cursor(ctx(t))
	if err != nil || cursor.UIDNext != initial.UIDNext {
		t.Fatal("cursor advanced", cursor, err)
	}
	if count(t, c, "SELECT count(*) FROM messages") != 1 {
		t.Fatal("partial write")
	}
	if _, err = c.Sync(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if count(t, c, "SELECT count(*) FROM messages WHERE is_deleted=0") != 2 {
		t.Fatal("retry lost messages")
	}
	// A failed SQL statement rolls back mail/index changes and cursor together.
	bad := Message{ID: "bad", UID: 9, UIDValidity: 1, From: Address{Address: "a"}, To: []Address{}}
	stmts, err := snapshotStatements([]Message{bad}, SyncResult{UIDValidity: 1, UIDNext: 10})
	if err != nil {
		t.Fatal(err)
	}
	stmts = append(stmts, sqlite.Statement{SQL: "INSERT INTO missing_table VALUES(1)"})
	if _, err = c.DB.Transaction(ctx(t), stmts); err == nil {
		t.Fatal("expected rollback")
	}
	if count(t, c, "SELECT count(*) FROM messages WHERE is_deleted=0") != 2 {
		t.Fatal("rollback damaged messages")
	}
	before, _ := c.Cursor(ctx(t))
	if before.UIDNext != 3 {
		t.Fatal(before)
	}
	if err = s.ResetMailbox(); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Append(fixture(t, "New generation", "replacement", "replacement@example.invalid"))
	truth := true
	if err = c.Flags(ctx(t), initial.IDs[0], &truth, nil); !errors.Is(err, ErrStaleUID) {
		t.Fatal("old UID accepted", err)
	}
	newResult, err := c.Sync(ctx(t))
	if err != nil || newResult.UIDValidity == initial.UIDValidity {
		t.Fatal(newResult, err)
	}
	if count(t, c, "SELECT count(*) FROM messages WHERE is_deleted=0") != 1 {
		t.Fatal("generation reconciliation")
	}
}

func TestSMTPTLSAuthCaptureAndUnknownOutcome(t *testing.T) {
	c, s := setup(t)
	req := SendRequest{RequestID: "send-test", To: []string{"to@example.invalid"}, Cc: []string{"cc@example.invalid"}, Bcc: []string{"private@example.invalid"}, Subject: "中文发送测试", BodyText: "Go SMTP 正文", BodyHTML: "<b>HTML</b>"}
	res, err := c.Send(ctx(t), req)
	if err != nil || !res.Accepted {
		t.Fatal(res, err)
	}
	captured := s.Captures()
	if len(captured) != 1 || len(captured[0].To) != 3 {
		t.Fatal(captured)
	}
	r, err := mail.CreateReader(bytes.NewReader(captured[0].Raw))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	subject, _ := r.Header.Subject()
	if subject != req.Subject || r.Header.Get("Bcc") != "" {
		t.Fatal("headers", subject, r.Header)
	}
	if _, err = c.Send(ctx(t), req); err == nil || len(s.Captures()) != 1 {
		t.Fatal("duplicate send")
	}
	s.DropSMTPReply.Store(true)
	req.RequestID = "unknown-outcome"
	if _, err = c.Send(ctx(t), req); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatal("unknown outcome not modeled", err)
	}
	if len(s.Captures()) != 2 {
		t.Fatal("server did not receive DATA")
	}
	if _, err = c.Send(ctx(t), req); err == nil || len(s.Captures()) != 2 {
		t.Fatal("unknown result repeated")
	}
	if count(t, c, "SELECT count(*) FROM protocol_send WHERE status='outcome_unknown'") != 1 {
		t.Fatal("unknown state missing")
	}
}

func TestSafetyBadAuthTLSAndCancellation(t *testing.T) {
	c, s := setup(t)
	for _, addr := range []string{"mail.example.com:993", "192.0.2.1:993", "localhost:993", "127.0.0.1:0"} {
		if validateAddress(addr) == nil {
			t.Fatal("unsafe destination accepted", addr)
		}
	}
	c.Config.Password = "bad"
	if _, err := c.Sync(ctx(t)); err == nil {
		t.Fatal("wrong password accepted")
	}
	c.Config.Password = testmail.Password
	c.Config.TLS = &tls.Config{ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}
	if _, err := c.Sync(ctx(t)); err == nil {
		t.Fatal("untrusted cert accepted")
	}
	c.Config.TLS = s.ClientTLS
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Sync(cancelled); err == nil {
		t.Fatal("cancelled sync succeeded")
	}
	req := SendRequest{RequestID: "unsafe", To: []string{"a@example.invalid\r\nBcc: evil@example.invalid"}, Subject: "unsafe"}
	if _, err := c.Send(ctx(t), req); err == nil {
		t.Fatal("header injection accepted")
	}
	if len(s.Captures()) != 0 {
		t.Fatal("unsafe mail reached DATA")
	}
}
