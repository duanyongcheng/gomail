package imap

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/store"
	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

type fixture struct {
	user      *imapmemserver.User
	server    *imapserver.Server
	endpoint  domain.ProtocolEndpoint
	failFetch atomic.Bool
	oversize  atomic.Bool
	dropMove  atomic.Bool
	fetches   atomic.Int32
	writes    atomic.Int32
	starttls  bool
}
type fixtureSession struct {
	*imapmemserver.UserSession
	f    *fixture
	conn *imapserver.Conn
}

func (s *fixtureSession) Fetch(w *imapserver.FetchWriter, set protocol.NumSet, o *protocol.FetchOptions) error {
	s.f.fetches.Add(1)
	if s.f.failFetch.Swap(false) {
		return &protocol.Error{Type: protocol.StatusResponseTypeNo, Text: "fixture failure"}
	}
	if s.f.oversize.Load() && len(o.BodySection) == 0 {
		message := w.CreateMessage(1)
		message.WriteUID(1)
		message.WriteRFC822Size(MaxMessageBytes + 1)
		return message.Close()
	}
	return s.UserSession.Fetch(w, set, o)
}
func (s *fixtureSession) Move(w *imapserver.MoveWriter, set protocol.NumSet, mailbox string) error {
	s.f.writes.Add(1)
	if s.f.dropMove.Swap(false) {
		_ = s.conn.NetConn().Close()
		return errors.New("fixture disconnect")
	}
	return s.UserSession.Move(w, set, mailbox)
}
func (s *fixtureSession) Create(name string, o *protocol.CreateOptions) error {
	s.f.writes.Add(1)
	return s.UserSession.Create(name, o)
}
func (s *fixtureSession) Rename(name, target string, o *protocol.RenameOptions) error {
	s.f.writes.Add(1)
	return s.UserSession.Rename(name, target, o)
}
func (s *fixtureSession) Delete(name string) error {
	s.f.writes.Add(1)
	return s.UserSession.Delete(name)
}
func newFixture(t *testing.T, starttls bool, noMove ...bool) *fixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(55), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	f := &fixture{user: imapmemserver.NewUser("fixture@example.invalid", "fixture-password"), starttls: starttls}
	for _, name := range []string{"INBOX", "Archive", "Trash", "Projects/One"} {
		if err = f.user.Create(name, nil); err != nil {
			t.Fatal(err)
		}
	}
	caps := protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapUIDPlus: {}, protocol.CapMove: {}, protocol.CapIdle: {}}
	if len(noMove) > 0 && noMove[0] {
		delete(caps, protocol.CapMove)
	}
	if len(noMove) > 1 && noMove[1] {
		delete(caps, protocol.CapUIDPlus)
	}
	f.server = imapserver.New(&imapserver.Options{TLSConfig: config, Logger: log.New(io.Discard, "", 0), Caps: caps, NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return &fixtureSession{imapmemserver.NewUserSession(f.user), f, conn}, nil, nil
	}})
	var ln net.Listener
	if starttls {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	} else {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", config)
	}
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = f.server.Serve(ln) }()
	t.Cleanup(func() { _ = ln.Close(); _ = f.server.Close(); <-done })
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	n, _ := strconv.Atoi(port)
	security := "tls"
	if starttls {
		security = "starttls"
	}
	f.endpoint = domain.ProtocolEndpoint{Host: host, Port: n, Security: security, Username: "fixture@example.invalid", Password: "fixture-password", AuthMode: "password", RootCAPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
	return f
}

type literal struct{ *bytes.Reader }

func (l literal) Size() int64 { return l.Reader.Size() }
func (f *fixture) append(t *testing.T, mailbox string, index int) protocol.UID {
	t.Helper()
	raw := []byte(fmt.Sprintf("From: =?UTF-8?B?5rWL6K+V?= <sender@example.invalid>\r\nTo: fixture@example.invalid\r\nDate: Mon, 02 Nov 2026 10:00:00 +0800\r\nMessage-ID: <%s-%d@example.invalid>\r\nSubject: Fixture %d\r\n\r\nBounded body %d\r\n", mailbox, index, index, index))
	result, err := f.user.Append(mailbox, literal{bytes.NewReader(raw)}, &protocol.AppendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return result.UID
}
func newProvider(t *testing.T, f *fixture, folders ...string) (*Provider, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mail.sqlite")
	db, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.SaveAccount(context.Background(), domain.Account{ID: "a", Email: f.endpoint.Username, Provider: "imap"}, []byte("synthetic-only")); err != nil {
		t.Fatal(err)
	}
	p, err := New(db, "a", domain.ProtocolConfig{Version: 1, IMAP: f.endpoint, SelectedFolders: folders}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p, path
}
func syncAll(t *testing.T, p *Provider) domain.SyncResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < 50; i++ {
		result, err := p.Sync(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Limited {
			return result
		}
	}
	t.Fatal("unbounded continuation")
	return domain.SyncResult{}
}

func TestSelectedFoldersBoundedBackfillReopenIncrementalAndDeletion(t *testing.T) {
	f := newFixture(t, false)
	for i := 0; i < 93; i++ {
		f.append(t, "INBOX", i)
	}
	for i := 0; i < 13; i++ {
		f.append(t, "Projects/One", i)
	}
	f.append(t, "Trash", 1)
	p, path := newProvider(t, f, "INBOX", "Projects/One")
	first, err := p.Sync(context.Background())
	if err != nil || !first.Limited || first.Count != int(PageUIDs)*PagesPerPass {
		t.Fatal(first, err)
	}
	if f.fetches.Load() != PagesPerPass*(int32(PageUIDs)+1) {
		t.Fatal("unexpected fetch bound", f.fetches.Load())
	}
	if err = p.DB.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p.DB = db
	syncAll(t, p)
	messages, err := db.Messages(context.Background(), "", "a", false, 200, 0)
	if err != nil || len(messages) != 106 {
		t.Fatal("selected folders", len(messages), err)
	}
	if messages[0].FromName != "测试" {
		t.Fatal("address decoder", messages[0].FromName)
	}
	f.append(t, "INBOX", 100)
	syncAll(t, p)
	messages, _ = db.Messages(context.Background(), FolderID("a", "INBOX"), "a", false, 200, 0)
	if len(messages) != 94 {
		t.Fatal("incremental lost message", len(messages))
	}
	sess := imapmemserver.NewUserSession(f.user)
	defer sess.Close()
	_, _ = sess.Select("INBOX", nil)
	_ = sess.Store(nil, protocol.UIDSetNum(1), &protocol.StoreFlags{Op: protocol.StoreFlagsAdd, Flags: []protocol.Flag{protocol.FlagDeleted}, Silent: true}, nil)
	set := protocol.UIDSetNum(1)
	if err = sess.Expunge(nil, &set); err != nil {
		t.Fatal(err)
	}
	syncAll(t, p)
	messages, _ = db.Messages(context.Background(), FolderID("a", "INBOX"), "a", false, 200, 0)
	if len(messages) != 93 {
		t.Fatal("confirmed deletion missing", len(messages))
	}
}

func TestFailurePreservesPageAndEpochRejectsOldWrite(t *testing.T) {
	f := newFixture(t, false)
	f.append(t, "INBOX", 1)
	p, _ := newProvider(t, f, "INBOX")
	syncAll(t, p)
	old, _ := p.DB.Messages(context.Background(), "", "a", false, 10, 0)
	before, _ := p.DB.ProviderCheckpoint(context.Background(), "a", "imap", "folder:INBOX")
	f.append(t, "INBOX", 2)
	f.failFetch.Store(true)
	if _, err := p.Sync(context.Background()); err == nil {
		t.Fatal("fetch failure accepted")
	}
	after, _ := p.DB.ProviderCheckpoint(context.Background(), "a", "imap", "folder:INBOX")
	if !bytes.Equal(before, after) {
		t.Fatal("checkpoint advanced on failed fetch")
	}
	if err := f.user.Delete("INBOX"); err != nil {
		t.Fatal(err)
	}
	if err := f.user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	f.append(t, "INBOX", 3)
	v := true
	unknown, err := p.Replay(context.Background(), "update_flags", encode(mutation{RemoteID: old[0].RemoteID, IsRead: &v}))
	if unknown || !errors.Is(err, ErrStaleUID) {
		t.Fatal("stale identity accepted", unknown, err)
	}
	syncAll(t, p)
	messages, _ := p.DB.Messages(context.Background(), "", "a", false, 20, 0)
	if len(messages) != 1 || messages[0].ID == old[0].ID {
		t.Fatal("epoch alias", messages)
	}
}

func TestDurableFlagsMovesAndUnknown(t *testing.T) {
	f := newFixture(t, false)
	f.append(t, "INBOX", 1)
	p, _ := newProvider(t, f, "INBOX", "Archive")
	syncAll(t, p)
	messages, _ := p.DB.Messages(context.Background(), FolderID("a", "INBOX"), "a", false, 10, 0)
	id := messages[0].ID
	v := true
	if err := p.Flags(context.Background(), id, &v, &v); err != nil {
		t.Fatal(err)
	}
	local, _ := p.DB.Message(context.Background(), id)
	if !local.IsRead || !local.IsStarred {
		t.Fatal("no local intent")
	}
	ops, payloads, err := p.DB.ClaimPendingOps(context.Background(), 1, time.Now().Unix())
	if err != nil || len(ops) != 1 {
		t.Fatal(ops, err)
	}
	if unknown, err := p.Replay(context.Background(), ops[0].OpType, payloads[ops[0].ID]); unknown || err != nil {
		t.Fatal(unknown, err)
	}
	if err = p.DB.CompletePendingOp(context.Background(), ops[0].ID); err != nil {
		t.Fatal(err)
	}
	syncAll(t, p)
	local, _ = p.DB.Message(context.Background(), id)
	if !local.IsRead || !local.IsStarred {
		t.Fatal("flags did not survive authoritative refresh")
	}
	payload := mutation{RemoteID: local.RemoteID, TargetRemoteID: "Archive"}
	f.dropMove.Store(true)
	unknown, err := p.Replay(context.Background(), "move_to_folder", encode(payload))
	if !unknown || err == nil {
		t.Fatal("unknown not preserved", unknown, err)
	}
	if f.writes.Load() != 1 {
		t.Fatal("move call count", f.writes.Load())
	}
}

func TestFolderBarrierSTARTTLSAndCancel(t *testing.T) {
	f := newFixture(t, true)
	p, _ := newProvider(t, f, "INBOX")
	if err := p.Test(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := p.RefreshFolders(context.Background()); err != nil {
		t.Fatal(err)
	}
	blocked := errors.New("disk unavailable")
	if unknown, err := p.ChangeFolder(context.Background(), "create", "", "Created", func() error { return blocked }); unknown || !errors.Is(err, blocked) {
		t.Fatal(unknown, err)
	}
	if f.writes.Load() != 0 {
		t.Fatal("write bypassed barrier")
	}
	for _, request := range [][3]string{{"create", "", "Created"}, {"rename", "Created", "Renamed"}, {"delete", "Renamed", ""}} {
		if unknown, err := p.ChangeFolder(context.Background(), request[0], request[1], request[2], func() error { return nil }); unknown || err != nil {
			t.Fatal(request, unknown, err)
		}
		if err := p.RefreshFolders(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.ChangeFolder(context.Background(), "delete", "INBOX", "", func() error { return nil }); err == nil {
		t.Fatal("system mailbox not protected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Test(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

func TestWatchNotificationAndCancellation(t *testing.T) {
	f := newFixture(t, false)
	p, _ := newProvider(t, f, "INBOX")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notified := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() { done <- p.Watch(ctx, func() { notified <- struct{}{} }) }()
	// Poll fixture append to avoid relying on the watcher reaching IDLE at one instant.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case <-notified:
			goto received
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			goto received
		case <-ticker.C:
			f.append(t, "INBOX", 1)
		case <-timeout.C:
			t.Fatal("IDLE did not signal")
		}
	}
received:
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("IDLE cancellation did not join")
	}
}

func TestRemoteIdentityAndConfiguration(t *testing.T) {
	for _, name := range []string{"INBOX", "Projects/中文:one"} {
		id := RemoteID(name, 13, 42)
		mailbox, v, u, err := ParseRemoteID(id)
		if err != nil || mailbox != name || v != 13 || u != 42 {
			t.Fatal(id, mailbox, v, u, err)
		}
	}
	for _, id := range []string{"imap:SU5CT1g:0:1", "imap:SU5CT1g:1:0", "gmail:1", "imap:AA:1:2"} {
		if _, _, _, err := ParseRemoteID(id); err == nil {
			t.Fatal("invalid identity", id)
		}
	}
	f := newFixture(t, false)
	p, _ := newProvider(t, f, "INBOX")
	p.Config.IMAP.RootCAPEM = ""
	if err := p.Test(context.Background()); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
	p.Config.IMAP = f.endpoint
	p.Config.IMAP.Password = "wrong"
	if err := p.Test(context.Background()); err == nil {
		t.Fatal("bad credentials accepted")
	}
	var value map[string]any
	if err := json.Unmarshal(encode(checkpoint{Validity: 1, Next: 3}), &value); err != nil || value["uid_validity"] != float64(1) {
		t.Fatal(value, err)
	}
	if strings.Contains(RemoteID("INBOX", 1, 1), "fixture-password") {
		t.Fatal("secret identity")
	}
}

func TestCheckpointTransactionFailureRollsBackCacheAndReopens(t *testing.T) {
	f := newFixture(t, false)
	f.append(t, "INBOX", 1)
	p, path := newProvider(t, f, "INBOX")
	fault, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer fault.Close()
	if _, err = fault.Exec(`CREATE TRIGGER fail_imap_checkpoint BEFORE INSERT ON provider_checkpoints BEGIN SELECT RAISE(ABORT,'synthetic checkpoint fault'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Sync(context.Background()); err == nil {
		t.Fatal("checkpoint fault ignored")
	}
	messages, err := p.DB.Messages(context.Background(), "", "a", false, 10, 0)
	if err != nil || len(messages) != 0 {
		t.Fatal("cache escaped checkpoint rollback", messages, err)
	}
	if _, err = fault.Exec("DROP TRIGGER fail_imap_checkpoint"); err != nil {
		t.Fatal(err)
	}
	if err = p.DB.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p.DB = db
	syncAll(t, p)
	messages, err = db.Messages(context.Background(), "", "a", false, 10, 0)
	if err != nil || len(messages) != 1 {
		t.Fatal("reopen lost page", messages, err)
	}
}

func TestOversizedMessageDefersWithoutBodyAndRecovers(t *testing.T) {
	f := newFixture(t, false)
	f.append(t, "INBOX", 1)
	f.oversize.Store(true)
	p, _ := newProvider(t, f, "INBOX")
	result := syncAll(t, p)
	if result.DeferredCount != 1 || f.fetches.Load() != 1 {
		t.Fatal("oversized body fetched or not deferred", result, f.fetches.Load())
	}
	f.oversize.Store(false)
	syncAll(t, p)
	messages, err := p.DB.Messages(context.Background(), "", "a", false, 10, 0)
	if err != nil || len(messages) != 1 {
		t.Fatal("deferred recovery lost mail", messages, err)
	}
}

func TestSelectedRenamePreservesFolderIDAndPersistsBeforeJournalRetirement(t *testing.T) {
	f := newFixture(t, false)
	f.append(t, "Projects/One", 1)
	p, _ := newProvider(t, f, "Projects/One")
	syncAll(t, p)
	oldID := FolderID("a", "Projects/One")
	ctx := context.Background()
	_, err := p.ChangeFolder(ctx, "rename", "Projects/One", "Renamed", func() error {
		_, err := p.DB.BeginGmailLabelChange(ctx, "a", store.GmailLabelChange{Action: "rename", RemoteID: "Projects/One", Name: "Renamed"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	failed := errors.New("synthetic encrypted-config persistence failure")
	p.UpdateSelectedFolders = func(context.Context, string, string) error { return failed }
	if err = p.RefreshFolders(ctx); !errors.Is(err, failed) {
		t.Fatal("configuration failure ignored", err)
	}
	if pending, err := p.DB.GmailLabelChange(ctx, "a"); err != nil || pending == nil {
		t.Fatal("journal retired before configuration save", pending, err)
	}
	called := false
	p.UpdateSelectedFolders = func(_ context.Context, old, next string) error {
		called = true
		if old != "Projects/One" || next != "Renamed" {
			t.Fatal(old, next)
		}
		return nil
	}
	if err = p.RefreshFolders(ctx); err != nil {
		t.Fatal(err)
	}
	if !called || p.Config.SelectedFolders[0] != "Renamed" {
		t.Fatal("selection not updated")
	}
	folders, err := p.Folders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, folder := range folders {
		if folder.RemoteID == "Renamed" {
			found = folder.ID == oldID
		}
	}
	if !found {
		t.Fatal("rename replaced stable local folder ID", folders)
	}
	if pending, err := p.DB.GmailLabelChange(ctx, "a"); err != nil || pending != nil {
		t.Fatal("known rename not reconciled", pending, err)
	}
	syncAll(t, p)
	messages, err := p.DB.Messages(ctx, oldID, "a", false, 20, 0)
	if err != nil || len(messages) != 1 {
		t.Fatal("renamed selected folder unavailable", messages, err)
	}
}

func TestMoveSuccessAndNoUnsafeFallback(t *testing.T) {
	for _, mode := range []string{"native", "uidplus", "unsupported"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, false, mode != "native", mode == "unsupported")
			f.append(t, "INBOX", 1)
			p, _ := newProvider(t, f, "INBOX", "Archive")
			syncAll(t, p)
			messages, _ := p.DB.Messages(context.Background(), FolderID("a", "INBOX"), "a", false, 10, 0)
			unknown, err := p.Replay(context.Background(), "move_to_folder", encode(mutation{RemoteID: messages[0].RemoteID, TargetRemoteID: "Archive"}))
			if mode == "unsupported" {
				if err == nil || unknown || f.writes.Load() != 0 {
					t.Fatal("unsafe MOVE fallback executed", unknown, err, f.writes.Load())
				}
				return
			}
			if err != nil || unknown {
				t.Fatal(unknown, err)
			}
			syncAll(t, p)
			inbox, _ := p.DB.Messages(context.Background(), FolderID("a", "INBOX"), "a", false, 10, 0)
			archive, _ := p.DB.Messages(context.Background(), FolderID("a", "Archive"), "a", false, 10, 0)
			if len(inbox) != 0 || len(archive) != 1 {
				t.Fatal("remote MOVE not reconciled", len(inbox), len(archive))
			}
		})
	}
}

func TestNotificationsOnlyAfterBackfillAndCommittedNewMail(t *testing.T) {
	f := newFixture(t, false)
	f.append(t, "INBOX", 1)
	p, _ := newProvider(t, f, "INBOX")
	result := syncAll(t, p)
	if len(result.NewMessages) != 0 {
		t.Fatal("historical mail announced", result)
	}
	f.append(t, "INBOX", 2)
	result = syncAll(t, p)
	if len(result.NewMessages) != 1 {
		t.Fatal("new mail missing", result)
	}
	result = syncAll(t, p)
	if len(result.NewMessages) != 0 {
		t.Fatal("new mail replayed", result)
	}
}
