// Package pop3 provides bounded, non-destructive UIDL-based retrieval.
package pop3

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/mailmime"
	"github.com/bary/pebble-go/internal/mailnet"
	"github.com/bary/pebble-go/internal/store"
)

type Config struct {
	Endpoint                     mailnet.Endpoint
	Dialer                       mailnet.Dialer
	Username, Password           string
	MaxMessageBytes, MaxMessages int
	Sender                       domain.SendTransport
	BeforeRequest                func(context.Context) error
}
type Provider struct {
	DB        *store.DB
	AccountID string
	config    Config
	gate      sync.Mutex
}

func New(db *store.DB, account string, c Config) *Provider {
	if c.Dialer.Proxy != nil {
		proxy := *c.Dialer.Proxy
		c.Dialer.Proxy = &proxy
	}
	return &Provider{DB: db, AccountID: account, config: c}
}
func (p *Provider) Capabilities() domain.Capabilities {
	return domain.Capabilities{Read: true, HTML: true, Send: p.config.Sender != nil}
}
func (p *Provider) Flags(context.Context, string, *bool, *bool) error {
	return errors.New("POP3 does not support remote flags")
}
func (p *Provider) Folders(ctx context.Context) ([]domain.Folder, error) {
	return p.DB.Folders(ctx, p.AccountID)
}
func (p *Provider) Send(ctx context.Context, m domain.OutgoingMail, barrier func() error) domain.SendResult {
	if p.config.Sender == nil {
		return domain.SendResult{Message: "SMTP is not configured for this POP3 account."}
	}
	return p.config.Sender.Send(ctx, m, barrier)
}
func (p *Provider) CheckSent(ctx context.Context, email, id string) (string, error) {
	if p.config.Sender == nil {
		return "", errors.New("SMTP is not configured")
	}
	return p.config.Sender.CheckSent(ctx, email, id)
}

type session struct {
	conn   net.Conn
	reader *bufio.Reader
	stop   func() bool
}

func (s *session) close() { s.stop(); _ = s.conn.Close() }
func (s *session) line() ([]byte, error) {
	line, err := s.reader.ReadSlice('\n')
	if err != nil || len(line) < 2 || !bytes.HasSuffix(line, []byte("\r\n")) {
		return nil, errors.New("invalid or oversized POP3 line")
	}
	return bytes.Clone(line[:len(line)-2]), nil
}
func (s *session) status() error {
	line, err := s.line()
	if err != nil || !(bytes.Equal(line, []byte("+OK")) || bytes.HasPrefix(line, []byte("+OK "))) {
		return errors.New("POP3 command failed")
	}
	return nil
}
func (s *session) command(command string) error {
	if _, err := io.WriteString(s.conn, command+"\r\n"); err != nil {
		return err
	}
	return s.status()
}
func (p *Provider) connect(ctx context.Context) (*session, error) {
	c := p.config
	if c.BeforeRequest != nil {
		if err := c.BeforeRequest(ctx); err != nil {
			return nil, err
		}
	}
	if err := c.Endpoint.Validate(); err != nil {
		return nil, err
	}
	if c.Username == "" || len(c.Username) > 512 || len(c.Password) > 4096 || strings.ContainsAny(c.Username+c.Password, "\r\n\x00") {
		return nil, errors.New("invalid POP3 credentials")
	}
	var conn net.Conn
	var err error
	if c.Endpoint.Security == "tls" {
		conn, err = c.Dialer.DialTLS(ctx, c.Endpoint)
	} else {
		conn, err = c.Dialer.DialContext(ctx, "tcp", net.JoinHostPort(c.Endpoint.Host, strconv.Itoa(c.Endpoint.Port)))
	}
	if err != nil {
		return nil, err
	}
	s := &session{conn: conn, reader: bufio.NewReaderSize(conn, 65536)}
	s.stop = context.AfterFunc(ctx, func() { _ = conn.Close() })
	setDeadline(conn, ctx, 60*time.Second)
	fail := func() (*session, error) {
		s.close()
		return nil, errors.New("POP3 connection or authentication failed")
	}
	if s.status() != nil {
		return fail()
	}
	if c.Endpoint.Security == "starttls" {
		if s.command("STLS") != nil {
			return fail()
		}
		secure, err := c.Dialer.UpgradeTLS(ctx, conn, c.Endpoint)
		if err != nil {
			return fail()
		}
		s.conn = secure
		setDeadline(secure, ctx, 60*time.Second)
		s.reader = bufio.NewReaderSize(secure, 65536)
	}
	if s.command("USER "+c.Username) != nil || s.command("PASS "+c.Password) != nil {
		return fail()
	}
	setDeadline(s.conn, ctx, 60*time.Second)
	return s, nil
}
func (p *Provider) Test(ctx context.Context) error {
	s, err := p.connect(ctx)
	if err == nil {
		s.close()
	}
	return err
}

type entry struct {
	number int
	uid    string
}

func (s *session) uidls() ([]entry, error) {
	if err := s.command("UIDL"); err != nil {
		return nil, err
	}
	out := []entry{}
	uids := map[string]bool{}
	numbers := map[int]bool{}
	size := 0
	for {
		line, err := s.line()
		if err != nil {
			return nil, err
		}
		if bytes.Equal(line, []byte(".")) {
			return out, nil
		}
		size += len(line)
		if len(out) >= 100000 || size > 10<<20 {
			return nil, errors.New("POP3 UIDL listing exceeds limit")
		}
		fields := strings.Fields(string(line))
		if len(fields) != 2 || len(fields[1]) > 70 {
			return nil, errors.New("invalid POP3 UIDL")
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil || n <= 0 || n > 1000000 || numbers[n] || uids[fields[1]] {
			return nil, errors.New("duplicate or invalid POP3 UIDL")
		}
		for _, c := range []byte(fields[1]) {
			if c < 33 || c > 126 {
				return nil, errors.New("invalid POP3 UIDL")
			}
		}
		uids[fields[1]] = true
		numbers[n] = true
		out = append(out, entry{n, fields[1]})
	}
}
func (s *session) retrieve(number, limit int) ([]byte, error) {
	if err := s.command("RETR " + strconv.Itoa(number)); err != nil {
		return nil, err
	}
	var raw bytes.Buffer
	for {
		line, err := s.line()
		if err != nil {
			return nil, err
		}
		if bytes.Equal(line, []byte(".")) {
			return raw.Bytes(), nil
		}
		if len(line) > 0 && line[0] == '.' {
			line = line[1:]
		}
		if raw.Len()+len(line)+2 > limit {
			return nil, errors.New("POP3 message exceeds size limit")
		}
		raw.Write(line)
		raw.WriteString("\r\n")
	}
}
func (p *Provider) Sync(ctx context.Context) (result domain.SyncResult, syncErr error) {
	defer func() {
		if ctx.Err() != nil {
			syncErr = ctx.Err()
		}
	}()
	p.gate.Lock()
	defer p.gate.Unlock()
	s, err := p.connect(ctx)
	if err != nil {
		return result, err
	}
	defer s.close()
	entries, err := s.uidls()
	if err != nil {
		return result, err
	}
	// The live UIDL listing is bounded above. Historical mappings can grow for
	// years; page them without retaining entries absent from the current mailbox.
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		seen[e.uid] = false
	}
	after := ""
	for {
		mappings, err := p.DB.ProviderMappingsPage(ctx, p.AccountID, "pop3", "inbox", after, 1000)
		if err != nil {
			return result, err
		}
		for _, m := range mappings {
			if _, current := seen[m.RemoteID]; current {
				seen[m.RemoteID] = true
			}
			after = m.RemoteID
		}
		if len(mappings) < 1000 {
			break
		}
	}
	maxBytes := p.config.MaxMessageBytes
	if maxBytes <= 0 || maxBytes > 50<<20 {
		maxBytes = 50 << 20
	}
	maxMessages := p.config.MaxMessages
	if maxMessages <= 0 || maxMessages > 100 {
		maxMessages = 100
	}
	folderID := store.ScopedID(p.AccountID, "pop3:inbox")
	role := "inbox"
	folder := domain.Folder{ID: folderID, AccountID: p.AccountID, RemoteID: "INBOX", Name: "Inbox", FolderType: "folder", Role: &role, IsSystem: true}
	if p.config.BeforeRequest != nil {
		if err := p.config.BeforeRequest(ctx); err != nil {
			return result, err
		}
	}
	if err := p.DB.CommitSnapshot(ctx, store.Snapshot{AccountID: p.AccountID, Folders: []domain.Folder{folder}}); err != nil {
		return result, err
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if seen[e.uid] {
			continue
		}
		if result.Count >= maxMessages {
			result.Limited = true
			break
		}
		raw, err := s.retrieve(e.number, maxBytes)
		if err != nil {
			return result, err
		}
		parsed, err := mailmime.ParseWithLimit(raw, maxBytes)
		if err != nil {
			return result, errors.New("invalid POP3 MIME; cached messages preserved")
		}
		remote := "pop3:" + e.uid
		id := store.ScopedID(p.AccountID, remote)
		now := time.Now().Unix()
		m := domain.Message{ID: id, AccountID: p.AccountID, RemoteID: remote, MessageIDHeader: &parsed.MessageID, InReplyTo: &parsed.InReplyTo, ReferencesHeader: &parsed.References, Subject: parsed.Subject, Snippet: parsed.Snippet, FromAddress: parsed.From.Address, BodyText: parsed.BodyText, BodyHTML: parsed.BodyHTML, Date: parsed.Date, CreatedAt: now, UpdatedAt: now, To: []domain.Address{}, CC: []domain.Address{}, BCC: []domain.Address{}}
		if parsed.From.Name != nil {
			m.FromName = *parsed.From.Name
		}
		for _, a := range parsed.To {
			m.To = append(m.To, domain.Address{Name: a.Name, Address: a.Address})
		}
		for _, a := range parsed.CC {
			m.CC = append(m.CC, domain.Address{Name: a.Name, Address: a.Address})
		}
		for _, a := range parsed.BCC {
			m.BCC = append(m.BCC, domain.Address{Name: a.Name, Address: a.Address})
		}
		cached := store.CachedMessage{Message: m, FolderIDs: []string{folderID}, Attachments: []domain.Attachment{}}
		for i, a := range parsed.Attachments {
			cid := a.ContentID
			cached.Attachments = append(cached.Attachments, domain.Attachment{ID: fmt.Sprintf("%s:attachment:%d", id, i), MessageID: id, Filename: a.Filename, MIMEType: a.MIMEType, ContentID: &cid, Inline: a.Inline, Data: a.Data})
		}
		cached.Message.HasAttachments = len(cached.Attachments) > 0
		checkpoint, _ := json.Marshal(struct {
			LastUIDL string `json:"last_uidl"`
		}{e.uid})
		if p.config.BeforeRequest != nil {
			if err := p.config.BeforeRequest(ctx); err != nil {
				return result, err
			}
		}
		err = p.DB.CommitSnapshot(ctx, store.Snapshot{AccountID: p.AccountID, Folders: []domain.Folder{folder}, Messages: []store.CachedMessage{cached}, ProviderMappings: []store.ProviderMapping{{Provider: "pop3", Scope: "inbox", RemoteID: e.uid, MessageID: id}}, ProviderCheckpoints: []store.ProviderCheckpoint{{Provider: "pop3", Scope: "inbox", Data: checkpoint}}})
		if err != nil {
			return result, err
		}
		seen[e.uid] = true
		result.Count++
	}
	// No DELE is ever sent, including on successful QUIT or partial traversal.
	return result, nil
}

func setDeadline(conn net.Conn, ctx context.Context, duration time.Duration) {
	deadline := time.Now().Add(duration)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	_ = conn.SetDeadline(deadline)
}
