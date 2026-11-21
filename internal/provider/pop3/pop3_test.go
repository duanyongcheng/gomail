package pop3

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/mailnet"
	"github.com/duanyongcheng/pebble-go/internal/store"
)

type popMessage struct{ uid, raw string }
type popFixture struct {
	mu          sync.Mutex
	messages    []popMessage
	commands    []string
	retrieved   []string
	startTLS    bool
	stallUIDL   bool
	uidlEntered chan struct{}
	tlsConfig   *tls.Config
}

func popCertificate(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "synthetic pop3"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func popServer(t *testing.T, f *popFixture) Config {
	t.Helper()
	cert, roots := popCertificate(t)
	f.tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	stop := make(chan struct{})
	go func() {
		defer close(stop)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() { defer workers.Done(); f.serve(conn) }()
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-stop; workers.Wait() })
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	n, _ := strconv.Atoi(port)
	security := "tls"
	if f.startTLS {
		security = "starttls"
	}
	return Config{Endpoint: mailnet.Endpoint{Host: "127.0.0.1", Port: n, Security: security, RootCAPEM: roots}, Dialer: mailnet.Dialer{Timeout: time.Second}, Username: "pop@example.invalid", Password: "synthetic-password", MaxMessageBytes: 4096, MaxMessages: 10}
}

func (f *popFixture) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
	if !f.startTLS {
		conn = tls.Server(conn, f.tlsConfig)
	}
	r := bufio.NewReader(conn)
	write := func(s string) { _, _ = fmt.Fprint(conn, s) }
	write("+OK synthetic POP3\r\n")
	secure := !f.startTLS
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		f.mu.Lock()
		f.commands = append(f.commands, strings.Fields(line)[0])
		messages := append([]popMessage(nil), f.messages...)
		f.mu.Unlock()
		switch {
		case line == "CAPA":
			write("+OK capabilities\r\nUIDL\r\nUSER\r\nSTLS\r\n.\r\n")
		case line == "STLS":
			write("+OK begin TLS\r\n")
			conn = tls.Server(conn, f.tlsConfig)
			r = bufio.NewReader(conn)
			secure = true
		case strings.HasPrefix(line, "USER ") || strings.HasPrefix(line, "PASS "):
			if secure {
				write("+OK authenticated\r\n")
			} else {
				write("-ERR TLS required\r\n")
			}
		case line == "STAT":
			write(fmt.Sprintf("+OK %d 1000\r\n", len(messages)))
		case line == "UIDL":
			if f.stallUIDL {
				close(f.uidlEntered)
				_, _ = r.ReadByte()
				return
			}
			write("+OK UIDLs\r\n")
			for i, m := range messages {
				write(fmt.Sprintf("%d %s\r\n", i+1, m.uid))
			}
			write(".\r\n")
		case line == "LIST":
			write("+OK sizes\r\n")
			for i, m := range messages {
				write(fmt.Sprintf("%d %d\r\n", i+1, len(m.raw)))
			}
			write(".\r\n")
		case strings.HasPrefix(line, "LIST ") || strings.HasPrefix(line, "RETR "):
			fields := strings.Fields(line)
			index, err := strconv.Atoi(fields[1])
			if err != nil || index < 1 || index > len(messages) {
				write("-ERR no message\r\n")
				continue
			}
			m := messages[index-1]
			if fields[0] == "LIST" {
				write(fmt.Sprintf("+OK %d %d\r\n", index, len(m.raw)))
				continue
			}
			f.mu.Lock()
			f.retrieved = append(f.retrieved, m.uid)
			f.mu.Unlock()
			write("+OK message follows\r\n")
			for _, text := range strings.Split(strings.TrimSuffix(m.raw, "\r\n"), "\r\n") {
				if strings.HasPrefix(text, ".") {
					text = "." + text
				}
				write(text + "\r\n")
			}
			write(".\r\n")
		case line == "QUIT":
			write("+OK bye\r\n")
			return
		default:
			write("-ERR unsupported\r\n")
		}
	}
}

func popMail(uid string) popMessage {
	return popMessage{uid: uid, raw: "From: sender@example.invalid\r\nTo: pop@example.invalid\r\nSubject: " + uid + "\r\nMessage-ID: <" + uid + "@example.invalid>\r\nDate: Mon, 02 Nov 2026 00:00:00 +0000\r\n\r\nSynthetic body\r\n"}
}

func popDB(t *testing.T, path string) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAccount(t.Context(), domain.Account{ID: "pop", Email: "pop@example.invalid", Provider: "pop3", CreatedAt: 1, UpdatedAt: 1}, nil); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	return db
}

func popCached(t *testing.T, db *store.DB) []domain.Message {
	t.Helper()
	rows, err := db.Messages(t.Context(), "", "pop", false, 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestPOP3UIDLReorderAndDatabaseReopen(t *testing.T) {
	for _, startTLS := range []bool{false, true} {
		t.Run(fmt.Sprintf("stls=%v", startTLS), func(t *testing.T) {
			fixture := &popFixture{startTLS: startTLS, messages: []popMessage{popMail("uid-a"), popMail("uid-b")}}
			config := popServer(t, fixture)
			path := filepath.Join(t.TempDir(), "profile.sqlite")
			db := popDB(t, path)
			if _, err := New(db, "pop", config).Sync(t.Context()); err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			before := popCached(t, db)
			if len(before) != 2 {
				t.Fatalf("cached=%d", len(before))
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := store.Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			fixture.mu.Lock()
			fixture.messages = []popMessage{popMail("uid-b"), popMail("uid-c"), popMail("uid-a")}
			fixture.mu.Unlock()
			if _, err := New(db, "pop", config).Sync(t.Context()); err != nil {
				t.Fatal(err)
			}
			after := popCached(t, db)
			if len(after) != 3 {
				t.Fatalf("cached after reopen=%d", len(after))
			}
			for _, old := range before {
				found := false
				for _, current := range after {
					if old.ID == current.ID {
						found = true
					}
				}
				if !found {
					t.Fatal("UIDL changed local identity after renumbering")
				}
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if len(fixture.retrieved) != 3 {
				t.Fatalf("RETR count=%d want 3", len(fixture.retrieved))
			}
			for _, command := range fixture.commands {
				if command == "DELE" {
					t.Fatal("automatic remote deletion")
				}
			}
		})
	}
}

func TestPOP3DuplicateUIDLRejectsBeforeRetrieval(t *testing.T) {
	fixture := &popFixture{messages: []popMessage{popMail("duplicate"), popMail("duplicate")}}
	config := popServer(t, fixture)
	db := popDB(t, filepath.Join(t.TempDir(), "profile.sqlite"))
	defer db.Close()
	if _, err := New(db, "pop", config).Sync(t.Context()); err == nil {
		t.Fatal("duplicate UIDL accepted")
	}
	if len(popCached(t, db)) != 0 {
		t.Fatal("duplicate UIDL committed partial cache")
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.retrieved) != 0 {
		t.Fatal("retrieved ambiguous UIDL")
	}
}

func TestPOP3HistoricalUIDLPagesDoNotHideCurrentOrNewMessages(t *testing.T) {
	fixture := &popFixture{messages: []popMessage{popMail("zz-current"), popMail("new-current")}}
	config := popServer(t, fixture)
	db := popDB(t, filepath.Join(t.TempDir(), "profile.sqlite"))
	defer db.Close()
	id := store.ScopedID("pop", "pop3:zz-current")
	snapshot := store.Snapshot{AccountID: "pop", Messages: []store.CachedMessage{{Message: domain.Message{ID: id, AccountID: "pop", RemoteID: "pop3:zz-current"}}}}
	for n := range 2105 {
		snapshot.ProviderMappings = append(snapshot.ProviderMappings, store.ProviderMapping{Provider: "pop3", Scope: "inbox", RemoteID: fmt.Sprintf("historical-%05d", n), MessageID: id})
	}
	snapshot.ProviderMappings = append(snapshot.ProviderMappings, store.ProviderMapping{Provider: "pop3", Scope: "inbox", RemoteID: "zz-current", MessageID: id})
	if err := db.CommitSnapshot(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	provider := New(db, "pop", config)
	for pass := range 2 {
		result, err := provider.Sync(t.Context())
		if err != nil || result.Count != 1-pass {
			t.Fatalf("pass %d: %+v %v", pass, result, err)
		}
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.retrieved) != 1 || fixture.retrieved[0] != "new-current" {
		t.Fatal("historical cursor missed known UIDL or duplicated new mail", fixture.retrieved)
	}
}

func TestPOP3OversizePreservesCachedMail(t *testing.T) {
	fixture := &popFixture{messages: []popMessage{popMail("good")}}
	config := popServer(t, fixture)
	db := popDB(t, filepath.Join(t.TempDir(), "profile.sqlite"))
	defer db.Close()
	if _, err := New(db, "pop", config).Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	large := popMail("large")
	large.raw += strings.Repeat("x", 8192)
	fixture.mu.Lock()
	fixture.messages = append(fixture.messages, large)
	fixture.mu.Unlock()
	if _, err := New(db, "pop", config).Sync(t.Context()); err == nil {
		t.Fatal("oversize message accepted")
	}
	if len(popCached(t, db)) != 1 {
		t.Fatal("oversize changed cached mail")
	}
}

func TestPOP3CancellationInterruptsBlockedUIDL(t *testing.T) {
	fixture := &popFixture{stallUIDL: true, uidlEntered: make(chan struct{})}
	config := popServer(t, fixture)
	db := popDB(t, filepath.Join(t.TempDir(), "profile.sqlite"))
	defer db.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := New(db, "pop", config).Sync(ctx); done <- err }()
	select {
	case <-fixture.uidlEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("UIDL not reached")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled sync succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt read")
	}
	if len(popCached(t, db)) != 0 {
		t.Fatal("cancelled sync committed mail")
	}
}

func TestPOP3BoundedContinuationPersistsEachUIDL(t *testing.T) {
	fixture := &popFixture{messages: []popMessage{popMail("one"), popMail("two"), popMail("three")}}
	config := popServer(t, fixture)
	config.MaxMessages = 1
	path := filepath.Join(t.TempDir(), "profile.sqlite")
	db := popDB(t, path)
	for pass := 0; pass < 3; pass++ {
		result, err := New(db, "pop", config).Sync(t.Context())
		if err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		if result.Count != 1 || result.Limited != (pass < 2) {
			t.Fatalf("pass %d: %+v", pass, result)
		}
		mappings, err := db.ProviderMappings(t.Context(), "pop", "pop3", "inbox")
		if err != nil || len(mappings) != pass+1 {
			t.Fatalf("pass %d mappings=%d error=%v", pass, len(mappings), err)
		}
		if _, err := db.ProviderCheckpoint(t.Context(), "pop", "pop3", "inbox"); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		db, err = store.Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
	}
	defer db.Close()
	if len(popCached(t, db)) != 3 {
		t.Fatal("continuation lost cached messages")
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.retrieved) != 3 {
		t.Fatalf("retrieved=%d", len(fixture.retrieved))
	}
}

func TestPOP3UntrustedTLSNeverAuthenticates(t *testing.T) {
	for _, startTLS := range []bool{false, true} {
		t.Run(fmt.Sprintf("stls=%v", startTLS), func(t *testing.T) {
			fixture := &popFixture{startTLS: startTLS}
			config := popServer(t, fixture)
			config.Endpoint.RootCAPEM = ""
			if err := New(nil, "pop", config).Test(t.Context()); err == nil {
				t.Fatal("untrusted certificate accepted")
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			for _, command := range fixture.commands {
				if command == "USER" || command == "PASS" {
					t.Fatal("credentials sent before verified TLS")
				}
			}
		})
	}
}

func TestPOP3CapabilitiesRejectRemoteWrites(t *testing.T) {
	provider := New(nil, "pop", Config{})
	caps := provider.Capabilities()
	if !caps.Read || !caps.HTML || caps.Send || caps.ModifyFlags {
		t.Fatalf("capabilities=%+v", caps)
	}
	read := true
	if err := provider.Flags(t.Context(), "synthetic", &read, nil); err == nil {
		t.Fatal("POP3 remote flag operation accepted")
	}
	if result := provider.Send(t.Context(), domain.OutgoingMail{}, func() error { t.Fatal("send barrier called without SMTP"); return nil }); result.Accepted || result.Unknown {
		t.Fatalf("send without SMTP=%+v", result)
	}
}

func TestPOP3CheckpointFailureRollsBackMailAndMappingAcrossReopen(t *testing.T) {
	fixture := &popFixture{messages: []popMessage{popMail("committed")}}
	config := popServer(t, fixture)
	path := filepath.Join(t.TempDir(), "profile.sqlite")
	db := popDB(t, path)
	if _, err := New(db, "pop", config).Sync(t.Context()); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	checkpoint, err := db.ProviderCheckpoint(t.Context(), "pop", "pop3", "inbox")
	if err != nil {
		t.Fatal(err)
	}
	fault, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer fault.Close()
	if _, err := fault.ExecContext(t.Context(), `CREATE TRIGGER reject_pop_checkpoint BEFORE INSERT ON provider_checkpoints WHEN NEW.provider='pop3' BEGIN SELECT RAISE(ABORT,'synthetic checkpoint failure'); END`); err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	fixture.messages = append(fixture.messages, popMail("uncommitted"))
	fixture.mu.Unlock()
	if _, err := New(db, "pop", config).Sync(t.Context()); err == nil {
		t.Fatal("checkpoint fault did not fail sync")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if len(popCached(t, db)) != 1 {
		t.Fatal("message escaped failed checkpoint transaction")
	}
	mappings, err := db.ProviderMappings(t.Context(), "pop", "pop3", "inbox")
	if err != nil || len(mappings) != 1 || mappings[0].RemoteID != "committed" {
		t.Fatalf("mappings=%+v error=%v", mappings, err)
	}
	current, err := db.ProviderCheckpoint(t.Context(), "pop", "pop3", "inbox")
	if err != nil || string(current) != string(checkpoint) {
		t.Fatalf("checkpoint advanced on failure: %v", err)
	}
	if _, err := fault.ExecContext(t.Context(), `DROP TRIGGER reject_pop_checkpoint`); err != nil {
		t.Fatal(err)
	}
	if _, err := New(db, "pop", config).Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(popCached(t, db)) != 2 {
		t.Fatal("repaired sync lost mail")
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.retrieved) != 3 {
		t.Fatalf("retrieved=%d want committed once and uncommitted twice", len(fixture.retrieved))
	}
}

func TestPOP3StaleConfigurationFencePreventsMessageCommit(t *testing.T) {
	fixture := &popFixture{messages: []popMessage{popMail("fenced")}}
	config := popServer(t, fixture)
	fences := 0
	config.BeforeRequest = func(context.Context) error {
		fences++
		if fences == 3 {
			return errors.New("synthetic stale configuration")
		}
		return nil
	}
	path := filepath.Join(t.TempDir(), "profile.sqlite")
	db := popDB(t, path)
	if _, err := New(db, "pop", config).Sync(t.Context()); err == nil {
		t.Fatal("stale configuration sync succeeded")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if fences != 3 || len(popCached(t, db)) != 0 {
		t.Fatalf("fences=%d; stale configuration committed mail", fences)
	}
	mappings, err := db.ProviderMappings(t.Context(), "pop", "pop3", "inbox")
	if err != nil || len(mappings) != 0 {
		t.Fatalf("stale configuration mappings=%+v error=%v", mappings, err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.retrieved) != 1 {
		t.Fatalf("RETR count=%d want 1", len(fixture.retrieved))
	}
}
