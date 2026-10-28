package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bary/pebble-go/internal/crypto"
	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/googleapi"
	"github.com/bary/pebble-go/internal/oauth"
	"github.com/bary/pebble-go/internal/privacy"
	"github.com/bary/pebble-go/internal/provider/gmail"
	"github.com/bary/pebble-go/internal/store"
)

type Runtime struct {
	DB                   *store.DB
	Context              context.Context
	Keyring              crypto.Keyring
	OpenBrowser          func(string) error
	OnSync               func(SyncComplete)
	OnProgress           func(domain.SyncProgress)
	OnNewMail            func(domain.NewMail)
	OnRealtime           func(RealtimeStatus)
	OnPendingOpsChanged  func()
	OnAttachmentProgress func(AttachmentProgress)
	pendingWake          chan struct{}
	pendingMu            sync.Mutex
	pendingWorkerMu      sync.Mutex
	pendingSettlement    *pendingSettlement
	Scheduler            *Scheduler
	mu                   sync.RWMutex
	providers            map[string]domain.Transport
	crypto               *crypto.Service
}

type SyncComplete struct {
	AccountID string `json:"account_id"`
	Count     int    `json:"count"`
}

func New(ctx context.Context, db *store.DB, keyring crypto.Keyring, open func(string) error) *Runtime {
	r := &Runtime{DB: db, Context: ctx, Keyring: keyring, OpenBrowser: open, providers: map[string]domain.Transport{}, pendingWake: make(chan struct{}, 1)}
	r.Scheduler = newScheduler(r)
	return r
}
func httpClient() *http.Client {
	return &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// Restore only decrypts local records and constructs lazy token sources. It
// never invokes Token, Profile, Sync, or a browser during application startup.
func (r *Runtime) Restore() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	has, err := r.DB.HasAuthData(r.Context)
	if err != nil || !has {
		return err
	}
	r.crypto, err = crypto.Load(r.Keyring, true)
	if err != nil {
		return err
	}
	accounts, err := r.DB.Accounts(r.Context)
	if err != nil {
		return err
	}
	cfg := oauth.Google()
	for _, account := range accounts {
		if account.Provider != "gmail" {
			continue
		}
		vault := &oauth.DatabaseVault{DB: r.DB, Crypto: r.crypto, Context: r.Context, AccountID: account.ID, ClientID: cfg.ClientID}
		if _, err := vault.Load(); err != nil {
			return err
		}
		tokens := oauth.NewTokens(cfg, httpClient(), vault)
		r.providers[account.ID] = gmail.New(r.DB, &googleapi.API{Base: googleapi.APIBase, HTTP: httpClient(), Tokens: tokens}, account.ID)
	}
	return nil
}

// InstallProvider is used by the explicit local selftest and provider tests.
func (r *Runtime) InstallProvider(account string, p domain.Transport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[account] = p
}
func (r *Runtime) provider(account string) (domain.Transport, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p := r.providers[account]
	if p == nil {
		return nil, errors.New("Gmail sign-in required")
	}
	return p, nil
}

type Empty struct{}
type AccountRequest struct {
	AccountID string `json:"accountId"`
}
type LoginRequest struct {
	Provider     string  `json:"provider"`
	Email        string  `json:"email"`
	DisplayName  string  `json:"displayName"`
	AccountLabel *string `json:"accountLabel"`
}
type OAuthStatus struct {
	Configured bool   `json:"configured"`
	Message    string `json:"message"`
}
type AccountService struct{ Runtime *Runtime }

func (s *AccountService) Status(Empty) OAuthStatus {
	configured := oauth.Google().ClientID != ""
	message := ""
	if !configured {
		message = "未配置 Google OAuth client ID"
	}
	return OAuthStatus{Configured: configured, Message: message}
}
func (s *AccountService) List(ctx context.Context, _ Empty) ([]domain.Account, error) {
	accounts, err := s.Runtime.DB.Accounts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		if p, err := s.Runtime.provider(accounts[i].ID); err == nil {
			accounts[i].Capabilities = p.Capabilities()
		}
	}
	return accounts, nil
}
func (s *AccountService) Login(ctx context.Context, request LoginRequest) (domain.Account, error) {
	r := s.Runtime
	if request.Provider != "gmail" && request.Provider != "google" {
		return domain.Account{}, errors.New("NotImplemented: complete_oauth_flow for this provider (M2)")
	}
	cfg := oauth.Google()
	if cfg.ClientID == "" {
		return domain.Account{}, errors.New("未配置 Google OAuth client ID")
	}
	// Serialize authorizations and key creation. A second login cannot generate
	// a replacement DEK while the first is storing its account.
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.crypto == nil {
		has, err := r.DB.HasAuthData(ctx)
		if err != nil {
			return domain.Account{}, err
		}
		r.crypto, err = crypto.Load(r.Keyring, has)
		if err != nil {
			return domain.Account{}, err
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(r.Context, cancel)
	defer stop()
	client := httpClient()
	token, err := oauth.Authorize(ctx, cfg, client, r.OpenBrowser)
	if err != nil {
		return domain.Account{}, err
	}
	api := &googleapi.API{Base: googleapi.APIBase, HTTP: client, Tokens: staticToken{token.AccessToken}}
	profile, err := api.Profile(ctx)
	if err != nil {
		return domain.Account{}, err
	}
	digest := sha256.Sum256([]byte(strings.ToLower(profile.Email)))
	id := "gmail:" + hex.EncodeToString(digest[:16])
	now := time.Now().Unix()
	color := "#0ea5e9"
	account := domain.Account{ID: id, Email: profile.Email, DisplayName: profile.Email, Provider: "gmail", Color: &color, AccountLabel: request.AccountLabel, CreatedAt: now, UpdatedAt: now}
	encrypted, err := oauth.EncodeToken(r.crypto, id, cfg.ClientID, token)
	if err != nil {
		return domain.Account{}, err
	}
	if err := r.DB.SaveAccount(ctx, account, encrypted); err != nil {
		return domain.Account{}, err
	}
	vault := &oauth.DatabaseVault{DB: r.DB, Crypto: r.crypto, Context: r.Context, AccountID: id, ClientID: cfg.ClientID}
	if _, err := vault.Load(); err != nil {
		return domain.Account{}, err
	}
	api.Tokens = oauth.NewTokens(cfg, client, vault)
	r.providers[id] = gmail.New(r.DB, api, id)
	account.Capabilities = r.providers[id].Capabilities()
	return account, nil
}

type staticToken struct{ value string }

func (t staticToken) Token(context.Context) (string, error) { return t.value, nil }
func (s *AccountService) Delete(ctx context.Context, request AccountRequest) (Empty, error) {
	r := s.Runtime
	r.Scheduler.control.Lock()
	defer r.Scheduler.control.Unlock()
	r.Scheduler.stop(request.AccountID)
	r.mu.Lock()
	defer r.mu.Unlock()
	err := r.DB.DeleteAccount(ctx, request.AccountID)
	if err == nil {
		delete(r.providers, request.AccountID)
	}
	return Empty{}, err
}

type FolderService struct{ Runtime *Runtime }

func (s *FolderService) List(ctx context.Context, a AccountRequest) ([]domain.Folder, error) {
	return s.Runtime.DB.Folders(ctx, a.AccountID)
}

type MessageRequest struct {
	MessageID   string          `json:"messageId"`
	PrivacyMode json.RawMessage `json:"privacyMode,omitempty"`
}
type MessagesRequest struct {
	FolderID  string `json:"folderId"`
	AccountID string `json:"accountId"`
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset"`
	Starred   bool   `json:"starred"`
}
type FlagsRequest struct {
	MessageID string `json:"messageId"`
	IsRead    *bool  `json:"isRead"`
	IsStarred *bool  `json:"isStarred"`
}
type MessageService struct{ Runtime *Runtime }
type Tracker struct {
	Domain      string `json:"domain"`
	TrackerType string `json:"tracker_type"`
}
type RenderedHTML struct {
	HTML            string    `json:"html"`
	TrackersBlocked []Tracker `json:"trackers_blocked"`
	ImagesBlocked   int       `json:"images_blocked"`
}
type MessageWithHTML struct {
	Message  domain.Message `json:"message"`
	Rendered RenderedHTML   `json:"rendered"`
}

func (s *MessageService) List(ctx context.Context, a MessagesRequest) ([]domain.Message, error) {
	return s.Runtime.DB.Messages(ctx, a.FolderID, a.AccountID, a.Starred, a.Limit, a.Offset)
}
func (s *MessageService) Get(ctx context.Context, a MessageRequest) (domain.Message, error) {
	m, err := s.Runtime.DB.Message(ctx, a.MessageID)
	if err == nil {
		if provider, providerErr := s.Runtime.provider(m.AccountID); providerErr == nil {
			m.Capabilities = provider.Capabilities()
		}
	}
	return m, err
}
func (s *MessageService) Rendered(ctx context.Context, a MessageRequest) (MessageWithHTML, error) {
	m, err := s.Get(ctx, a)
	if err != nil {
		return MessageWithHTML{Message: m}, err
	}
	attachments, err := s.Runtime.DB.Attachments(ctx, m.ID)
	if err != nil {
		return MessageWithHTML{Message: m}, err
	}
	refs := make([]privacy.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment.ContentID != nil {
			refs = append(refs, privacy.Attachment{ID: attachment.ID, ContentID: *attachment.ContentID})
		}
	}
	rendered := privacy.Render(m.BodyHTML, m.BodyText, m.AccountID, m.ID, refs, privacy.ParseMode(a.PrivacyMode))
	trackers := make([]Tracker, 0, len(rendered.TrackersBlocked))
	for _, tracker := range rendered.TrackersBlocked {
		trackers = append(trackers, Tracker{Domain: tracker.Domain, TrackerType: tracker.Type})
	}
	return MessageWithHTML{Message: m, Rendered: RenderedHTML{HTML: rendered.HTML, TrackersBlocked: trackers, ImagesBlocked: rendered.ImagesBlocked}}, nil
}
func (s *MessageService) Flags(ctx context.Context, a FlagsRequest) (Empty, error) {
	return s.QueueFlags(ctx, a)
}
func (s *MessageService) Attachments(ctx context.Context, a MessageRequest) ([]domain.Attachment, error) {
	return s.Runtime.DB.Attachments(ctx, a.MessageID)
}

type CountsRequest struct {
	AccountID string `json:"accountId"`
	ByFolder  bool   `json:"byFolder"`
}

func (s *MessageService) UnreadCounts(ctx context.Context, a CountsRequest) (map[string]int, error) {
	return s.Runtime.DB.UnreadCounts(ctx, a.AccountID, a.ByFolder)
}

type SearchRequest struct {
	Query     string `json:"query"`
	AccountID string `json:"accountId"`
	Limit     int    `json:"limit"`
}
type SearchService struct{ Runtime *Runtime }

func (s *SearchService) Search(ctx context.Context, a SearchRequest) ([]domain.SearchHit, error) {
	return s.Runtime.DB.Search(ctx, a.Query, a.AccountID, a.Limit)
}

type SyncService struct{ Runtime *Runtime }

func (s *SyncService) Sync(ctx context.Context, a AccountRequest) (domain.SyncResult, error) {
	return s.Runtime.Scheduler.Trigger(ctx, a.AccountID, "manual")
}

func (r *Runtime) performSync(ctx context.Context, account string) (domain.SyncResult, error) {
	p, err := r.provider(account)
	if err != nil {
		return domain.SyncResult{}, err
	}
	if r.OnProgress != nil {
		r.OnProgress(domain.SyncProgress{AccountID: account, Status: "started", Phase: "sync"})
	}
	var result domain.SyncResult
	if provider, ok := p.(domain.ProgressTransport); ok {
		result, err = provider.SyncWithProgress(ctx, r.OnProgress)
	} else {
		result, err = p.Sync(ctx)
	}
	// NewMessages only contains pages already committed, including when a
	// later page failed. Replaying a checkpoint will not announce them twice.
	if r.OnNewMail != nil {
		for _, message := range result.NewMessages {
			r.OnNewMail(message)
		}
	}
	if r.OnProgress != nil {
		status, message := "completed", ""
		if err != nil && ctx.Err() == nil {
			status, message = "error", "Gmail sync failed; cached mail is available"
		}
		r.OnProgress(domain.SyncProgress{AccountID: account, Status: status, Phase: "sync", Count: result.Count, Message: message})
	}
	if err == nil && r.OnSync != nil {
		r.OnSync(SyncComplete{AccountID: account, Count: result.Count})
	}
	return result, err
}
