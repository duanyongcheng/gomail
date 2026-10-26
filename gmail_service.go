package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bary/pebble-go/internal/gmail"
	"github.com/bary/pebble-go/internal/mailcore"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sqlite"
	"golang.org/x/oauth2"
)

type GmailStatus struct {
	Mode      string `json:"mode"`
	Connected bool   `json:"connected"`
	Modify    bool   `json:"modify"`
	Message   string `json:"message"`
}
type GmailService struct {
	mu        sync.Mutex
	core      *gmail.Core
	cfg       *oauth2.Config
	tokens    *gmail.Tokens
	vault     *gmail.Vault
	client    *http.Client
	db        *sqlite.DB
	mock      *gmail.Mock
	root      string
	live      bool
	modify    bool
	connected bool
}

var gmailSynced = mygo.NewEvent[SyncComplete]("mail:sync-complete")

func newGmailService(root string) *GmailService {
	return &GmailService{root: root, live: os.Getenv("PEBBLE_GMAIL_LIVE") == "1", modify: os.Getenv("PEBBLE_GMAIL_ALLOW_MODIFY") == "1"}
}
func (s *GmailService) initialize(ctx context.Context, db *sqlite.DB) error {
	s.db = db
	s.client = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if s.live {
		return nil
	}
	if _, err := db.Execute(ctx, "DELETE FROM folders WHERE id LIKE 'poc-%'"); err != nil {
		return err
	}
	s.mock = gmail.NewMock()
	s.client = s.mock.Server.Client()
	s.cfg = s.mock.OAuthConfig()
	s.modify = true
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	s.vault = &gmail.Vault{Path: filepath.Join(s.root, "artifacts", "demo-token.enc"), Key: key, AAD: "demo-client:modify"}
	s.tokens = gmail.NewTokens(s.cfg, s.client, s.vault)
	if err := s.tokens.Install(&oauth2.Token{AccessToken: "demo-access", RefreshToken: "demo-refresh", Expiry: time.Now().Add(time.Hour)}); err != nil {
		return err
	}
	for i, subject := range []string{"Gmail API 验证：中文邮件", "Gmail second API message"} {
		raw, err := wireFixture(subject, "gmail-demo@example.invalid")
		if err != nil {
			return err
		}
		s.mock.Set([]string{"m1", "m2"}[i], raw, []string{"INBOX", "UNREAD", "Label_work"})
	}
	core, err := gmail.New(ctx, db, s.mock.API(s.tokens), true)
	if err != nil {
		return err
	}
	s.core = core
	s.connected = true
	return nil
}
func (s *GmailService) Status() GmailStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	mode := "demo"
	message := "Local Gmail API fixture; not Google"
	if s.live {
		mode = "live"
		message = "Ready for your explicit Google login; default readonly"
	}
	return GmailStatus{mode, s.connected, s.modify, message}
}
func (s *GmailService) Login(ctx context.Context) (GmailStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.live {
		return GmailStatus{"demo", true, true, "Local fixture only"}, nil
	}
	cfg, err := gmail.LoadDesktop(os.Getenv("PEBBLE_GMAIL_OAUTH_CONFIG"), s.modify)
	if err != nil {
		return GmailStatus{}, err
	}
	// One isolated credential namespace per client+scope. Never touches Pebble's DEK.
	digest := sha256.Sum256([]byte(cfg.ClientID + ":" + strings.Join(cfg.Scopes, ",")))
	namespace := hex.EncodeToString(digest[:8])
	store := mygo.Secrets.Service("dev.pebble.go.gmail.poc." + namespace)
	key, err := store.Get("token-dek")
	if errors.Is(err, mygo.ErrSecretNotFound) {
		path := filepath.Join(s.root, "artifacts", "live-token-"+namespace+".enc")
		if _, e := os.Stat(path); e == nil {
			return GmailStatus{}, errors.New("encrypted token exists but DEK is missing; refusing replacement")
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return GmailStatus{}, err
		}
		err = store.Set("token-dek", key)
	}
	if err != nil || len(key) != 32 {
		return GmailStatus{}, errors.New("isolated keyring unavailable/invalid; no plaintext fallback")
	}
	vault := &gmail.Vault{Path: filepath.Join(s.root, "artifacts", "live-token-"+namespace+".enc"), Key: key, AAD: cfg.ClientID + ":" + strings.Join(cfg.Scopes, ",")}
	token, err := gmail.Authorize(ctx, cfg, s.client, mygo.Shell.OpenExternal)
	if err != nil {
		return GmailStatus{}, err
	}
	tokens := gmail.NewTokens(cfg, s.client, vault)
	if err = tokens.Install(token); err != nil {
		return GmailStatus{}, err
	}
	api := &gmail.API{Base: gmail.APIBase, HTTP: s.client, Tokens: tokens}
	core, err := gmail.New(ctx, s.db, api, s.modify)
	if err != nil {
		return GmailStatus{}, err
	}
	s.cfg = cfg
	s.tokens = tokens
	s.vault = vault
	s.core = core
	s.connected = true
	return GmailStatus{"live", true, s.modify, "Google authorized; click Sync to load latest mail"}, nil
}
func (s *GmailService) Sync(ctx context.Context) (gmail.SyncResult, error) {
	s.mu.Lock()
	core := s.core
	s.mu.Unlock()
	if core == nil {
		return gmail.SyncResult{}, errors.New("sign in to Gmail first")
	}
	result, err := core.Sync(ctx)
	if err == nil {
		_ = gmailSynced.Broadcast(SyncComplete{gmail.AccountID, result.Count})
	}
	return result, err
}
func (s *GmailService) Flags(ctx context.Context, id string, read, starred *bool) error {
	s.mu.Lock()
	core := s.core
	s.mu.Unlock()
	if core == nil {
		return errors.New("Gmail not signed in")
	}
	return core.Flags(ctx, id, read, starred)
}
func (s *GmailService) Search(ctx context.Context, q, account string, limit int) ([]mailcore.SearchHit, error) {
	s.mu.Lock()
	core := s.core
	s.mu.Unlock()
	if core == nil {
		return []mailcore.SearchHit{}, nil
	}
	return core.Search(ctx, q, account, limit)
}
