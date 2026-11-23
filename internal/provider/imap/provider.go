// Package imap implements configured, verified IMAP accounts independently of the PoC.
package imap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/mailnet"
	"github.com/duanyongcheng/gomail/internal/store"
	protocol "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const MaxFolders = 256

var ErrStaleUID = errors.New("IMAP mailbox identity changed; synchronize before retry")

type Provider struct {
	DB            *store.DB
	AccountID     string
	Config        domain.ProtocolConfig
	Dialer        mailnet.Dialer
	Token         func(context.Context) (string, error)
	BeforeRequest func(context.Context) error
	// UpdateSelectedFolders atomically updates encrypted account configuration
	// before a reconciled rename/delete journal is retired.
	UpdateSelectedFolders func(context.Context, string, string) error
	gate                  sync.Mutex
	folderIDs             map[string]string
}

func New(db *store.DB, account string, config domain.ProtocolConfig, global *mailnet.Proxy) (*Provider, error) {
	if db == nil || account == "" || config.IMAP.Username == "" {
		return nil, errors.New("IMAP configuration required")
	}
	if strings.ContainsAny(config.IMAP.Username, "\x00\x01\r\n") {
		return nil, errors.New("invalid IMAP username")
	}
	if err := config.IMAP.Endpoint().Validate(); err != nil {
		return nil, err
	}
	if config.IMAP.AuthMode != "" && config.IMAP.AuthMode != "password" && config.IMAP.AuthMode != "xoauth2" {
		return nil, errors.New("unsupported IMAP authentication")
	}
	proxy, err := mailnet.ResolveProxy(global, config.Proxy)
	if err != nil {
		return nil, err
	}
	if len(config.SelectedFolders) == 0 {
		config.SelectedFolders = []string{"INBOX"}
	}
	if len(config.SelectedFolders) > MaxFolders {
		return nil, errors.New("too many selected folders")
	}
	config.SelectedFolders = append([]string(nil), config.SelectedFolders...)
	seen := map[string]bool{}
	for _, folder := range config.SelectedFolders {
		if !validMailbox(folder) || seen[folder] {
			return nil, errors.New("invalid selected folders")
		}
		seen[folder] = true
	}
	return &Provider{DB: db, AccountID: account, Config: config, Dialer: mailnet.Dialer{Proxy: proxy}}, nil
}

func validMailbox(name string) bool {
	return name != "" && len(name) <= 1024 && !strings.ContainsAny(name, "\x00\r\n")
}
func FolderID(account, mailbox string) string {
	return store.ScopedID(account, "imap-folder:"+base64.RawURLEncoding.EncodeToString([]byte(mailbox)))
}
func RemoteID(mailbox string, validity, uid uint32) string {
	return fmt.Sprintf("imap:%s:%d:%d", base64.RawURLEncoding.EncodeToString([]byte(mailbox)), validity, uid)
}
func ParseRemoteID(id string) (mailbox string, validity, uid uint32, err error) {
	parts := strings.Split(id, ":")
	if len(parts) != 4 || parts[0] != "imap" {
		err = ErrStaleUID
		return
	}
	b, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
	if decodeErr != nil {
		err = ErrStaleUID
		return
	}
	mailbox = string(b)
	v, e1 := strconv.ParseUint(parts[2], 10, 32)
	u, e2 := strconv.ParseUint(parts[3], 10, 32)
	if e1 != nil || e2 != nil || v == 0 || u == 0 || !validMailbox(mailbox) {
		err = ErrStaleUID
		return
	}
	validity = uint32(v)
	uid = uint32(u)
	return
}

type xoauth struct{ username, token string }

func (x xoauth) Start() (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + x.username + "\x01auth=Bearer " + x.token + "\x01\x01"), nil
}
func (x xoauth) Next([]byte) ([]byte, error) {
	return nil, errors.New("IMAP OAuth authentication rejected")
}

func (p *Provider) client(ctx context.Context, handler *imapclient.UnilateralDataHandler) (*imapclient.Client, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if p.BeforeRequest != nil {
		if err := p.BeforeRequest(ctx); err != nil {
			return nil, nil, err
		}
	}
	ep := p.Config.IMAP.Endpoint()
	var conn net.Conn
	var err error
	if ep.Security == "tls" {
		conn, err = p.Dialer.DialTLS(ctx, ep)
	} else {
		conn, err = p.Dialer.DialContext(ctx, "tcp", net.JoinHostPort(ep.Host, strconv.Itoa(ep.Port)))
	}
	if err != nil {
		return nil, nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(time.Minute))
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	options := &imapclient.Options{UnilateralDataHandler: handler}
	var c *imapclient.Client
	if ep.Security == "starttls" {
		options.TLSConfig, err = ep.TLSConfig()
		if err == nil {
			c, err = imapclient.NewStartTLS(conn, options)
		}
	} else {
		c = imapclient.New(conn, options)
	}
	cleanup := func() {
		stop()
		_ = conn.Close()
		if c != nil {
			_ = c.Close()
		}
	}
	if err != nil {
		cleanup()
		return nil, nil, errors.New("IMAP TLS negotiation failed")
	}
	if err = c.WaitGreeting(); err == nil {
		if p.Config.IMAP.AuthMode == "xoauth2" {
			if p.Token == nil {
				err = errors.New("IMAP OAuth grant required")
			} else {
				var token string
				token, err = p.Token(ctx)
				if err == nil && (token == "" || strings.ContainsAny(token, "\x00\x01\r\n")) {
					err = errors.New("invalid IMAP OAuth credential")
				}
				if err == nil {
					err = c.Authenticate(xoauth{p.Config.IMAP.Username, token})
				}
			}
		} else {
			err = c.Login(p.Config.IMAP.Username, p.Config.IMAP.Password).Wait()
		}
	}
	if err != nil {
		cleanup()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		var access *domain.AccessError
		if errors.As(err, &access) {
			return nil, nil, err
		}
		return nil, nil, errors.New("IMAP authentication or connection failed")
	}
	_ = conn.SetDeadline(time.Time{})
	return c, cleanup, nil
}

func (p *Provider) Capabilities() domain.Capabilities {
	return domain.Capabilities{Read: true, ModifyFlags: true, HTML: true}
}

// Folders is a local-only read; RefreshFolders performs explicit discovery.
func (p *Provider) Folders(ctx context.Context) ([]domain.Folder, error) {
	return p.DB.Folders(ctx, p.AccountID)
}
func folder(account string, f *protocol.ListData) domain.Folder {
	role := ""
	system := false
	if strings.EqualFold(f.Mailbox, "INBOX") {
		role = "inbox"
		system = true
	}
	for _, attr := range f.Attrs {
		switch strings.ToLower(string(attr)) {
		case "\\trash":
			role = "trash"
		case "\\sent":
			role = "sent"
		case "\\drafts":
			role = "drafts"
		case "\\junk":
			role = "spam"
		case "\\archive":
			role = "archive"
		}
		if role != "" {
			system = true
		}
	}
	var rolePtr *string
	if role != "" {
		rolePtr = &role
	}
	return domain.Folder{ID: FolderID(account, f.Mailbox), AccountID: account, RemoteID: f.Mailbox, Name: f.Mailbox, FolderType: "folder", Role: rolePtr, IsSystem: system}
}
func (p *Provider) list(ctx context.Context, c *imapclient.Client) ([]domain.Folder, error) {
	cmd := c.List("", "*", nil)
	defer cmd.Close()
	out := []domain.Folder{}
	for f := cmd.Next(); f != nil; f = cmd.Next() {
		if len(out) >= MaxFolders {
			_ = c.Close()
			return nil, errors.New("IMAP folder limit exceeded")
		}
		if !validMailbox(f.Mailbox) {
			return nil, errors.New("invalid IMAP mailbox")
		}
		out = append(out, folder(p.AccountID, f))
	}
	if err := cmd.Close(); err != nil {
		return nil, err
	}
	cached, err := p.DB.Folders(ctx, p.AccountID)
	if err != nil {
		return nil, err
	}
	pending, err := p.DB.GmailLabelChange(ctx, p.AccountID)
	if err != nil {
		return nil, err
	}
	ids := map[string]string{}
	present := map[string]bool{}
	for _, f := range out {
		present[f.RemoteID] = true
	}
	for _, f := range cached {
		ids[f.RemoteID] = f.ID
		if pending != nil && pending.Action == "rename" && pending.RemoteID == f.RemoteID && !present[f.RemoteID] && present[pending.Name] {
			ids[pending.Name] = f.ID
			if err := p.updateSelection(ctx, f.RemoteID, pending.Name); err != nil {
				return nil, err
			}
			// Mailbox rename preserves the local folder, but its old UID identities
			// must retire before importing the new mailbox identity.
			state := checkpoint{Retire: true, RetireMailbox: f.RemoteID, Next: 1, Refresh: 1}
			if err := p.DB.CommitSnapshot(ctx, store.Snapshot{AccountID: p.AccountID, ProviderCheckpoints: []store.ProviderCheckpoint{cpRecord("folder:"+pending.Name, state)}}); err != nil {
				return nil, err
			}
		}
		if pending != nil && pending.Action == "delete" && pending.RemoteID == f.RemoteID && !present[f.RemoteID] {
			if err := p.updateSelection(ctx, f.RemoteID, ""); err != nil {
				return nil, err
			}
		}
	}
	p.folderIDs = map[string]string{}
	for i := range out {
		if id := ids[out[i].RemoteID]; id != "" {
			out[i].ID = id
		}
		p.folderIDs[out[i].RemoteID] = out[i].ID
	}
	return out, nil
}

func (p *Provider) updateSelection(ctx context.Context, old, next string) error {
	selected := []string{}
	changed := false
	for _, name := range p.Config.SelectedFolders {
		if name == old {
			changed = true
			if next != "" {
				selected = append(selected, next)
			}
		} else {
			selected = append(selected, name)
		}
	}
	if !changed {
		return nil
	}
	if p.UpdateSelectedFolders == nil {
		return errors.New("selected folder configuration persistence required")
	}
	if err := p.UpdateSelectedFolders(ctx, old, next); err != nil {
		return err
	}
	if len(selected) == 0 {
		selected = []string{"INBOX"}
	}
	p.Config.SelectedFolders = selected
	return nil
}
func (p *Provider) RefreshFolders(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	p.gate.Lock()
	defer p.gate.Unlock()
	c, close, err := p.client(ctx, nil)
	if err != nil {
		return err
	}
	defer close()
	folders, err := p.list(ctx, c)
	if err != nil {
		return err
	}
	return p.DB.ReconcileProtocolFolders(ctx, p.AccountID, folders)
}

func (p *Provider) Test(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	p.gate.Lock()
	defer p.gate.Unlock()
	c, close, err := p.client(ctx, nil)
	if err != nil {
		return err
	}
	defer close()
	for _, name := range p.Config.SelectedFolders {
		if _, err = c.Select(name, &protocol.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			return errors.New("selected IMAP mailbox unavailable")
		}
	}
	return nil
}

// Watch owns one cancellable connection; the scheduler owns retries and joins.
func (p *Provider) Watch(ctx context.Context, notify func()) error {
	p.gate.Lock()
	mailbox := p.Config.SelectedFolders[0]
	p.gate.Unlock()
	signal := make(chan struct{}, 1)
	changed := func() {
		select {
		case signal <- struct{}{}:
		default:
		}
	}
	c, close, err := p.client(ctx, &imapclient.UnilateralDataHandler{Mailbox: func(*imapclient.UnilateralDataMailbox) { changed() }, Expunge: func(uint32) { changed() }, Fetch: func(m *imapclient.FetchMessageData) {
		for m.Next() != nil {
		}
		changed()
	}})
	if err != nil {
		return err
	}
	defer close()
	if !c.Caps().Has(protocol.CapIdle) {
		return errors.New("IMAP IDLE unavailable; use polling")
	}
	if _, err = c.Select(mailbox, &protocol.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return err
	}
	for {
		select {
		case <-signal:
		default:
			goto drained
		}
	}
drained:
	idle, err := c.Idle()
	if err != nil {
		return err
	}
	timer := time.NewTimer(20 * time.Minute)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		close()
		_ = idle.Close()
		_ = idle.Wait()
		return ctx.Err()
	case <-c.Closed():
		_ = idle.Close()
		_ = idle.Wait()
		return errors.New("IMAP IDLE disconnected")
	case <-signal:
		if notify != nil {
			notify()
		}
	case <-timer.C:
	}
	if err = idle.Close(); err != nil {
		return err
	}
	return idle.Wait()
}

func encode(v any) []byte { b, _ := json.Marshal(v); return b }
