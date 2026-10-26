// Package testmail supplies loopback-only TLS mail servers. It never relays mail.
package testmail

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

const Username = "poc@example.invalid"
const Password = "local-test-password"

type Captured struct {
	From string   `json:"from"`
	To   []string `json:"to"`
	Raw  []byte   `json:"raw"`
}

type Servers struct {
	IMAPAddr      string
	SMTPAddr      string
	ClientTLS     *tls.Config
	User          *imapmemserver.User
	imap          *imapserver.Server
	smtp          *smtp.Server
	listeners     []net.Listener
	wg            sync.WaitGroup
	mu            sync.Mutex
	captured      []Captured
	commands      []string
	FailFetch     atomic.Bool
	DropFetch     atomic.Bool
	DropSMTPReply atomic.Bool
}

func Start() (*Servers, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	certTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "loopback PoC"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	s := &Servers{ClientTLS: &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}, User: imapmemserver.NewUser(Username, Password)}
	if err = s.User.Create("INBOX", nil); err != nil {
		return nil, err
	}
	serverTLS := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	logger := log.New(io.Discard, "", 0)
	s.imap = imapserver.New(&imapserver.Options{NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return &imapSession{UserSession: imapmemserver.NewUserSession(s.User), server: s, conn: conn}, nil, nil
	}, TLSConfig: serverTLS, Logger: logger, Caps: imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}}})
	s.smtp = smtp.NewServer(smtp.BackendFunc(func(c *smtp.Conn) (smtp.Session, error) { return &smtpSession{server: s, conn: c}, nil }))
	s.smtp.Domain = "localhost"
	s.smtp.ReadTimeout = 5 * time.Second
	s.smtp.WriteTimeout = 5 * time.Second
	s.smtp.MaxMessageBytes = 2 << 20
	s.smtp.MaxRecipients = 20
	s.smtp.ErrorLog = logger
	for _, serve := range []func(net.Listener) error{s.imap.Serve, s.smtp.Serve} {
		ln, e := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
		if e != nil {
			s.Close()
			return nil, e
		}
		s.listeners = append(s.listeners, ln)
		s.wg.Add(1)
		go func() { defer s.wg.Done(); _ = serve(ln) }()
	}
	s.IMAPAddr = s.listeners[0].Addr().String()
	s.SMTPAddr = s.listeners[1].Addr().String()
	return s, nil
}

func (s *Servers) Close() {
	// Close listener objects too: Serve might not have registered them yet.
	for _, ln := range s.listeners {
		_ = ln.Close()
	}
	if s.imap != nil {
		_ = s.imap.Close()
	}
	if s.smtp != nil {
		_ = s.smtp.Close()
	}
	s.wg.Wait()
}
func (s *Servers) record(command string) {
	s.mu.Lock()
	s.commands = append(s.commands, command)
	s.mu.Unlock()
}
func (s *Servers) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}
func (s *Servers) Captures() []Captured {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Captured, len(s.captured))
	for i, c := range s.captured {
		out[i] = Captured{c.From, append([]string(nil), c.To...), append([]byte(nil), c.Raw...)}
	}
	return out
}

type literal struct{ *bytes.Reader }

func (r literal) Size() int64 { return r.Reader.Size() }
func (s *Servers) Append(raw []byte) (imap.UID, error) {
	r, err := s.User.Append("INBOX", literal{bytes.NewReader(raw)}, &imap.AppendOptions{})
	if err != nil {
		return 0, err
	}
	return r.UID, nil
}
func (s *Servers) ResetMailbox() error {
	if err := s.User.Delete("INBOX"); err != nil {
		return err
	}
	return s.User.Create("INBOX", nil)
}
func (s *Servers) Expunge(uid imap.UID) error {
	sess := imapmemserver.NewUserSession(s.User)
	defer sess.Close()
	if _, err := sess.Select("INBOX", nil); err != nil {
		return err
	}
	if err := sess.Store(nil, imap.UIDSetNum(uid), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil); err != nil {
		return err
	}
	set := imap.UIDSetNum(uid)
	return sess.Expunge(nil, &set)
}

type imapSession struct {
	*imapmemserver.UserSession
	server *Servers
	conn   *imapserver.Conn
}

func (s *imapSession) Login(user, password string) error {
	s.server.record("LOGIN")
	return s.UserSession.Login(user, password)
}
func (s *imapSession) Select(name string, o *imap.SelectOptions) (*imap.SelectData, error) {
	s.server.record("SELECT")
	return s.UserSession.Select(name, o)
}
func (s *imapSession) Fetch(w *imapserver.FetchWriter, set imap.NumSet, o *imap.FetchOptions) error {
	s.server.record("UID FETCH")
	if s.server.DropFetch.Swap(false) {
		_ = s.conn.NetConn().Close()
		return errors.New("injected IMAP disconnect")
	}
	if s.server.FailFetch.Swap(false) {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "injected fetch failure"}
	}
	return s.UserSession.Fetch(w, set, o)
}
func (s *imapSession) Store(w *imapserver.FetchWriter, set imap.NumSet, flags *imap.StoreFlags, o *imap.StoreOptions) error {
	s.server.record("UID STORE")
	return s.UserSession.Store(w, set, flags, o)
}

type smtpSession struct {
	server *Servers
	conn   *smtp.Conn
	auth   bool
	from   string
	to     []string
}

func (s *smtpSession) AuthMechanisms() []string { return []string{sasl.Plain} }
func (s *smtpSession) Auth(mech string) (sasl.Server, error) {
	if mech != sasl.Plain {
		return nil, errors.New("unsupported auth")
	}
	return sasl.NewPlainServer(func(identity, user, password string) error {
		if (identity != "" && identity != user) || user != Username || password != Password {
			return errors.New("invalid local credentials")
		}
		s.auth = true
		s.server.record("SMTP AUTH")
		return nil
	}), nil
}
func (s *smtpSession) Mail(from string, _ *smtp.MailOptions) error {
	if !s.auth {
		return smtp.ErrAuthRequired
	}
	s.from = from
	s.server.record("SMTP MAIL")
	return nil
}
func (s *smtpSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	if !s.auth {
		return smtp.ErrAuthRequired
	}
	s.to = append(s.to, to)
	return nil
}
func (s *smtpSession) Data(r io.Reader) error {
	if !s.auth {
		return smtp.ErrAuthRequired
	}
	raw, err := io.ReadAll(io.LimitReader(r, (2<<20)+1))
	if err != nil {
		return err
	}
	if len(raw) > 2<<20 {
		return errors.New("message too large")
	}
	s.server.mu.Lock()
	s.server.captured = append(s.server.captured, Captured{s.from, append([]string(nil), s.to...), raw})
	s.server.commands = append(s.server.commands, "SMTP DATA")
	s.server.mu.Unlock()
	if s.server.DropSMTPReply.Swap(false) {
		_ = s.conn.Conn().Close()
	}
	return nil
}
func (s *smtpSession) Reset()        { s.from = ""; s.to = nil }
func (s *smtpSession) Logout() error { return nil }
