package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/duanyongcheng/gomail/internal/ai"
	"github.com/duanyongcheng/gomail/internal/crypto"
	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/googleapi"
	"github.com/duanyongcheng/gomail/internal/mailnet"
	"github.com/duanyongcheng/gomail/internal/oauth"
	"github.com/duanyongcheng/gomail/internal/privacy"
	"github.com/duanyongcheng/gomail/internal/store"
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
	OnOutboxChanged      func()
	OnFoldersChanged     func(AccountRequest)
	OnTrustChanged       func(SenderRequest)
	OnAccountChanged     func(AccountRequest)
	OnAIStream           func(AIStreamEvent)
	// Dependencies are overridden only by isolated local fixtures.
	Authorize         func(context.Context, *oauth2.Config, *http.Client, func(string) error) (*oauth2.Token, error)
	GmailAPIBase      string
	authFlowMu        sync.Mutex
	reconnectAccount  string
	reconnectCancel   context.CancelFunc
	connections       map[string]*accountConnection
	labelManagementMu sync.Mutex
	cloudDraftMu      sync.Mutex
	outboxWake        chan struct{}
	outboxMu          sync.Mutex
	outboxWorkerMu    sync.Mutex
	outboxSettlement  *outboxSettlement
	pendingWake       chan struct{}
	pendingMu         sync.Mutex
	pendingWorkerMu   sync.Mutex
	pendingSettlement *pendingSettlement
	Scheduler         *Scheduler
	mu                sync.RWMutex
	providers         map[string]domain.Transport
	crypto            *crypto.Service
	composeMu         sync.Mutex
	providerFactories map[string]ProviderFactory
	globalProxy       *mailnet.Proxy
	googleOAuth       *storedGoogleOAuth
	googleOAuthLocked bool
	aiConfig          *ai.Config
	aiConfigLocked    bool
	aiCancelMu        sync.Mutex
	aiCancels         map[string]context.CancelFunc
}

type SyncComplete struct {
	AccountID string `json:"account_id"`
	Count     int    `json:"count"`
}

func New(ctx context.Context, db *store.DB, keyring crypto.Keyring, open func(string) error) *Runtime {
	r := &Runtime{DB: db, Context: ctx, Keyring: keyring, OpenBrowser: open, Authorize: oauth.Authorize, GmailAPIBase: googleapi.APIBase, providers: map[string]domain.Transport{}, connections: map[string]*accountConnection{}, pendingWake: make(chan struct{}, 1), outboxWake: make(chan struct{}, 1), aiCancels: map[string]context.CancelFunc{}}
	r.Scheduler = newScheduler(r)
	return r
}
func httpClient() *http.Client {
	return &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// Restore only decrypts local records and constructs lazy token sources. It
// never invokes Token, Profile, Sync, or a browser during application startup.
func (r *Runtime) Restore() error {
	return r.restoreAccounts()
}

// InstallProvider is used by the explicit local selftest and provider tests.
func (r *Runtime) InstallProvider(account string, p domain.Transport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[account] = p
	delete(r.connections, account)
}
func (r *Runtime) provider(account string) (domain.Transport, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p := r.providers[account]
	if c := r.connections[account]; c != nil && c.state.Blocked() {
		return nil, &domain.AccessError{State: c.state}
	}
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
	Provider     string                `json:"provider"`
	Email        string                `json:"email"`
	DisplayName  string                `json:"displayName"`
	AccountLabel *string               `json:"accountLabel"`
	Proxy        *mailnet.ProxySetting `json:"proxy,omitempty"`
}
type OAuthStatus struct {
	Configured bool   `json:"configured"`
	Message    string `json:"message"`
}
type AccountService struct{ Runtime *Runtime }

func (s *AccountService) Status(Empty) OAuthStatus {
	cfg, locked := s.Runtime.googleOAuthStatus()
	configured := cfg.ClientID != ""
	message := ""
	if locked {
		message = "Google OAuth 配置已加密，请先解锁系统钥匙串"
	} else if !configured {
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
	return s.login(ctx, request)
}

type staticToken struct{ value string }

func (t staticToken) Token(context.Context) (string, error) { return t.value, nil }
func (s *AccountService) Delete(ctx context.Context, request AccountRequest) (Empty, error) {
	r := s.Runtime
	release := r.quiesceAccount(request.AccountID)
	defer release()
	r.mu.Lock()
	defer r.mu.Unlock()
	if c := r.connections[request.AccountID]; c != nil && c.cancel != nil {
		c.cancel()
	}
	err := r.DB.DeleteAccount(ctx, request.AccountID)
	if err == nil {
		closeProtocol(r.providers[request.AccountID])
		delete(r.providers, request.AccountID)
		delete(r.connections, request.AccountID)
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
	messages, err := s.Runtime.DB.Messages(ctx, a.FolderID, a.AccountID, a.Starred, a.Limit, a.Offset)
	if err != nil {
		return nil, err
	}
	for i := range messages {
		if provider, err := s.Runtime.provider(messages[i].AccountID); err == nil {
			messages[i].Capabilities = provider.Capabilities()
		}
		if thread, e := s.Runtime.DB.PublicThreadID(ctx, messages[i].ID); e == nil {
			messages[i].ThreadID = &thread
		}
	}
	return messages, nil
}
func (s *MessageService) Get(ctx context.Context, a MessageRequest) (domain.Message, error) {
	m, err := s.Runtime.DB.Message(ctx, a.MessageID)
	if err == nil {
		if thread, threadErr := s.Runtime.DB.PublicThreadID(ctx, m.ID); threadErr == nil {
			m.ThreadID = &thread
		}
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
	mode, err := s.effectivePrivacyMode(ctx, m, privacy.ParseMode(a.PrivacyMode))
	if err != nil {
		return MessageWithHTML{}, err
	}
	rendered := privacy.Render(m.BodyHTML, m.BodyText, m.AccountID, m.ID, refs, mode)
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
	// Label reconciliation commits independently of message pages. Refresh the
	// sidebar even if a later message fetch failed during this sync pass.
	if r.OnFoldersChanged != nil {
		r.OnFoldersChanged(AccountRequest{AccountID: account})
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
			status, message = "error", "Mail sync failed; cached mail is available"
		}
		r.OnProgress(domain.SyncProgress{AccountID: account, Status: status, Phase: "sync", Count: result.Count, Message: message})
	}
	if err == nil && r.OnSync != nil {
		r.OnSync(SyncComplete{AccountID: account, Count: result.Count})
	}
	return result, err
}
