package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/app"
	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/store"
	"github.com/egoist/mygo"
	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

var m2SelftestChecks = []string{
	"M2 TLS account test and encrypted creation",
	"M2 selected-folder IMAP and UIDL POP3 sync",
	"M2 encrypted reopen restores without network",
	"M2 incremental sync preserves identities",
	"M2 SMTP outbox accepted without remote ID",
	"M2 frozen Bcc envelope excludes MIME Bcc",
	"M2 POP3 capabilities disable remote mutations",
}

// All listeners and data belong to this disposable fixture, never the UI profile.
func selftestM2(ctx context.Context, runtime *app.Runtime, window *mygo.Window, checks map[string]bool) {
	for _, name := range m2SelftestChecks {
		checks[name] = false
	}
	for _, name := range m2UISelftestChecks {
		checks[name] = false
	}
	selftestM2UI(ctx, runtime, window, checks)
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	_ = runM2Selftest(ctx, checks)
}

func runM2Selftest(ctx context.Context, checks map[string]bool) error {
	dir, err := os.MkdirTemp("", "pebble-m2-synthetic-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	f, err := newM2SelftestFixture()
	if err != nil {
		return err
	}
	defer f.close()
	path := filepath.Join(dir, "profile.sqlite")
	db, err := store.Open(ctx, path)
	if err != nil {
		return err
	}
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()
	keyring := &selftestKeyring{}
	r := app.New(ctx, db, keyring, nil)
	r.RegisterStandardProviders()
	defer func() { r.Scheduler.Close() }()
	accounts := &app.AccountService{Runtime: r}
	requests := []app.AddAccountRequest{
		{Email: "fixture@example.invalid", DisplayName: "Synthetic IMAP", Provider: "imap", Config: &domain.ProtocolConfig{Version: 1, IMAP: f.endpoint(f.imapPort), SMTP: f.endpoint(f.smtpPort), SelectedFolders: []string{"INBOX", "Projects"}}},
		{Email: "pop@example.invalid", DisplayName: "Synthetic POP3", Provider: "pop3", Config: &domain.ProtocolConfig{Version: 1, POP3: f.endpoint(f.popPort), SMTP: domain.ProtocolEndpoint{AuthMode: "disabled"}}},
	}
	ids := []string{}
	for _, request := range requests {
		result, err := accounts.Test(ctx, request)
		if err != nil || !result.Success {
			return fmt.Errorf("synthetic account test: %w", err)
		}
		a, err := accounts.Add(ctx, request)
		if err != nil {
			return err
		}
		ids = append(ids, a.ID)
		ciphertext, err := db.AuthData(ctx, a.ID)
		if err != nil || !bytes.HasPrefix(ciphertext, []byte("PEBBLE\x00\x01")) || bytes.Contains(ciphertext, []byte("synthetic-password")) {
			return fmt.Errorf("synthetic credentials are not encrypted")
		}
	}
	checks[m2SelftestChecks[0]] = true
	initial := make([][]domain.Message, 2)
	for i, id := range ids {
		if _, err := (&app.SyncService{Runtime: r}).Sync(ctx, app.AccountRequest{AccountID: id}); err != nil {
			return err
		}
		initial[i], err = db.Messages(ctx, "", id, false, 20, 0)
		if err != nil || len(initial[i]) != 2-i {
			return fmt.Errorf("synthetic initial cache count: %d", len(initial[i]))
		}
	}
	checks[m2SelftestChecks[1]] = true
	list, err := accounts.List(ctx, app.Empty{})
	if err != nil {
		return err
	}
	for _, a := range list {
		if a.ID == ids[1] {
			c := a.Capabilities
			checks[m2SelftestChecks[6]] = c.Read && !c.Send && !c.ModifyFlags && !c.FolderManagement
		}
	}
	r.Scheduler.Close()
	if err := db.Close(); err != nil {
		return err
	}
	db, err = store.Open(ctx, path)
	if err != nil {
		return err
	}
	r = app.New(ctx, db, keyring, nil)
	r.RegisterStandardProviders()
	before := f.connections.Load()
	if err := r.Restore(); err != nil {
		return err
	}
	accounts = &app.AccountService{Runtime: r}
	list, err = accounts.List(ctx, app.Empty{})
	checks[m2SelftestChecks[2]] = err == nil && len(list) == 2 && before == f.connections.Load()
	for i, id := range ids {
		if _, err := (&app.SyncService{Runtime: r}).Sync(ctx, app.AccountRequest{AccountID: id}); err != nil {
			return err
		}
		current, err := db.Messages(ctx, "", id, false, 20, 0)
		if err != nil || len(current) != len(initial[i]) {
			return fmt.Errorf("synthetic incremental cache changed")
		}
		for j := range current {
			if current[j].ID != initial[i][j].ID {
				return fmt.Errorf("synthetic incremental identity changed")
			}
		}
	}
	if _, err := f.user.Append("INBOX", m2SelftestLiteral{bytes.NewReader([]byte(m2SelftestMail(3)))}, &protocol.AppendOptions{}); err != nil {
		return err
	}
	if _, err := (&app.SyncService{Runtime: r}).Sync(ctx, app.AccountRequest{AccountID: ids[0]}); err != nil {
		return err
	}
	incremental, err := db.Messages(ctx, "", ids[0], false, 20, 0)
	if err != nil {
		return err
	}
	checks[m2SelftestChecks[3]] = f.retrievals.Load() == 1 && len(incremental) == 3
	outbox := &app.OutboxService{Runtime: r}
	if _, err := outbox.Queue(ctx, app.SendRequest{AccountID: ids[0], ClientKey: "m2-packaged-send", To: []string{"recipient@example.invalid"}, BCC: []string{"hidden@example.invalid"}, Subject: "Synthetic immutable SMTP", BodyText: "Disposable package fixture"}); err != nil {
		return err
	}
	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); r.RunOutboxWorker(workerCtx) }()
	defer func() { stop(); <-done }()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		rows, err := db.OutboxList(ctx, ids[0])
		if err != nil {
			return err
		}
		if len(rows) == 1 && rows[0].Status == "sent" {
			checks[m2SelftestChecks[4]] = rows[0].RemoteID == "" && rows[0].Attempts == 1
			f.mu.Lock()
			checks[m2SelftestChecks[5]] = f.submissions == 1 && len(f.recipients) == 2 && strings.Contains(strings.Join(f.recipients, " "), "hidden@example.invalid") && !strings.Contains(strings.ToLower(f.raw), "bcc:") && !strings.Contains(f.raw, "hidden@example.invalid")
			f.mu.Unlock()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

type m2SelftestFixture struct {
	tlsConfig                   *tls.Config
	root                        string
	imapPort, smtpPort, popPort int
	listeners                   []net.Listener
	imap                        *imapserver.Server
	user                        *imapmemserver.User
	workers                     sync.WaitGroup
	connections, retrievals     atomic.Int32
	mu                          sync.Mutex
	recipients                  []string
	raw                         string
	submissions                 int
}

func (f *m2SelftestFixture) endpoint(port int) domain.ProtocolEndpoint {
	return domain.ProtocolEndpoint{Host: "127.0.0.1", Port: port, Security: "tls", RootCAPEM: f.root, Username: "fixture@example.invalid", Password: "synthetic-password", AuthMode: "password"}
}

type m2SelftestLiteral struct{ *bytes.Reader }

func (l m2SelftestLiteral) Size() int64 { return l.Reader.Size() }

func newM2SelftestFixture() (*m2SelftestFixture, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	f := &m2SelftestFixture{root: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), tlsConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}}
	user := imapmemserver.NewUser("fixture@example.invalid", "synthetic-password")
	f.user = user
	for i, folder := range []string{"INBOX", "Projects", "Excluded"} {
		if err := user.Create(folder, nil); err != nil {
			return nil, err
		}
		if _, err := user.Append(folder, m2SelftestLiteral{bytes.NewReader([]byte(m2SelftestMail(i)))}, &protocol.AppendOptions{}); err != nil {
			return nil, err
		}
	}
	f.imap = imapserver.New(&imapserver.Options{TLSConfig: f.tlsConfig, Logger: log.New(io.Discard, "", 0), Caps: protocol.CapSet{protocol.CapIMAP4rev1: {}, protocol.CapUIDPlus: {}, protocol.CapMove: {}}, NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		f.connections.Add(1)
		return imapmemserver.NewUserSession(user), nil, nil
	}})
	for i := 0; i < 3; i++ {
		ln, err := tls.Listen("tcp", "127.0.0.1:0", f.tlsConfig)
		if err != nil {
			f.close()
			return nil, err
		}
		f.listeners = append(f.listeners, ln)
		_, port, _ := net.SplitHostPort(ln.Addr().String())
		n, _ := strconv.Atoi(port)
		if i == 0 {
			f.imapPort = n
			f.workers.Add(1)
			go func() { defer f.workers.Done(); _ = f.imap.Serve(ln) }()
		} else {
			if i == 1 {
				f.smtpPort = n
			} else {
				f.popPort = n
			}
			f.workers.Add(1)
			go f.accept(ln, i == 1)
		}
	}
	return f, nil
}

func (f *m2SelftestFixture) close() {
	for _, ln := range f.listeners {
		_ = ln.Close()
	}
	if f.imap != nil {
		_ = f.imap.Close()
	}
	f.workers.Wait()
}

func (f *m2SelftestFixture) accept(ln net.Listener, smtp bool) {
	defer f.workers.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		f.connections.Add(1)
		f.workers.Add(1)
		go func() {
			defer f.workers.Done()
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			if smtp {
				f.serveSMTP(conn)
			} else {
				f.servePOP(conn)
			}
		}()
	}
}

func m2SelftestMail(i int) string {
	return fmt.Sprintf("From: sender@example.invalid\r\nTo: fixture@example.invalid\r\nSubject: Synthetic M2 %d\r\nDate: Mon, 02 Nov 2026 00:00:00 +0000\r\nMessage-ID: <m2-%d@example.invalid>\r\n\r\nSynthetic body\r\n", i, i)
}

func (f *m2SelftestFixture) servePOP(conn net.Conn) {
	r := bufio.NewReader(conn)
	write := func(s string) { _, _ = io.WriteString(conn, s) }
	write("+OK synthetic POP3\r\n")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		switch strings.TrimSpace(line) {
		case "USER fixture@example.invalid", "PASS synthetic-password":
			write("+OK\r\n")
		case "UIDL":
			write("+OK\r\n1 stable-uidl\r\n.\r\n")
		case "LIST 1":
			write(fmt.Sprintf("+OK 1 %d\r\n", len(m2SelftestMail(99))))
		case "RETR 1":
			f.retrievals.Add(1)
			write("+OK\r\n" + m2SelftestMail(99) + ".\r\n")
		case "QUIT":
			write("+OK\r\n")
			return
		default:
			write("-ERR unsupported\r\n")
		}
	}
}

func (f *m2SelftestFixture) serveSMTP(conn net.Conn) {
	r := bufio.NewReader(conn)
	write := func(s string) { _, _ = io.WriteString(conn, s) }
	write("220 synthetic SMTP\r\n")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "EHLO"):
			write("250-localhost\r\n250 AUTH PLAIN\r\n")
		case strings.HasPrefix(line, "AUTH PLAIN "):
			write("235 authenticated\r\n")
		case strings.HasPrefix(line, "MAIL FROM:"):
			write("250 accepted\r\n")
		case strings.HasPrefix(line, "RCPT TO:"):
			f.mu.Lock()
			f.recipients = append(f.recipients, line)
			f.mu.Unlock()
			write("250 accepted\r\n")
		case line == "DATA":
			write("354 continue\r\n")
			var raw strings.Builder
			for {
				text, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if text == ".\r\n" {
					break
				}
				raw.WriteString(text)
			}
			f.mu.Lock()
			f.submissions++
			f.raw = raw.String()
			f.mu.Unlock()
			write("250 accepted\r\n")
		case line == "QUIT":
			write("221 bye\r\n")
			return
		case line == "RSET", line == "NOOP":
			write("250 OK\r\n")
		default:
			write("500 unsupported\r\n")
		}
	}
}
