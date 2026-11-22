package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/mailnet"
	"github.com/duanyongcheng/pebble-go/internal/provider/gmail"
	"github.com/duanyongcheng/pebble-go/internal/store"
)

type ProviderContext struct {
	Account       domain.Account
	Config        domain.ProtocolConfig
	DB            *store.DB
	Dialer        mailnet.Dialer
	BeforeRequest func(context.Context) error
	SaveConfig    func(context.Context, domain.ProtocolConfig) error
	UpdateConfig  func(context.Context, func(*domain.ProtocolConfig) error) error
}

// Factories must construct lazy transports: no login, HTTP or background jobs.
type ProviderFactory func(ProviderContext) (domain.Transport, error)

func (r *Runtime) RegisterProviderFactory(provider string, f ProviderFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.providerFactories == nil {
		r.providerFactories = map[string]ProviderFactory{}
	}
	r.providerFactories[provider] = f
}

func (r *Runtime) networkClientLocked() *http.Client {
	return &http.Client{Transport: (mailnet.Dialer{Proxy: r.globalProxy}).HTTPTransport(), Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func (r *Runtime) networkClient() *http.Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.networkClientLocked()
}

type AddAccountRequest struct {
	AccountLabel       *string                `json:"account_label,omitempty"`
	Email              string                 `json:"email"`
	DisplayName        string                 `json:"display_name"`
	Provider           string                 `json:"provider"`
	ImapHost           string                 `json:"imap_host"`
	ImapPort           int                    `json:"imap_port"`
	SmtpHost           string                 `json:"smtp_host"`
	SmtpPort           int                    `json:"smtp_port"`
	Username           string                 `json:"username"`
	Password           string                 `json:"password"`
	ImapSecurity       string                 `json:"imap_security"`
	SmtpSecurity       string                 `json:"smtp_security"`
	AcceptInvalidCerts bool                   `json:"accept_invalid_certs"`
	AllowPlaintext     bool                   `json:"allow_plaintext"`
	RootCAPEM          string                 `json:"root_ca_pem,omitempty"`
	ProxyHost          string                 `json:"proxy_host,omitempty"`
	ProxyPort          int                    `json:"proxy_port,omitempty"`
	ProxyMode          string                 `json:"proxy_mode,omitempty"`
	ProxyScheme        string                 `json:"proxy_scheme,omitempty"`
	ImapAuthMode       string                 `json:"imap_auth_mode,omitempty"`
	SmtpAuthMode       string                 `json:"smtp_auth_mode,omitempty"`
	SelectedFolders    []string               `json:"selected_folders,omitempty"`
	Config             *domain.ProtocolConfig `json:"config,omitempty"`
}
type SaveProtocolRequest struct {
	AccountID   string                `json:"accountId"`
	Revision    string                `json:"revision"`
	DisplayName string                `json:"displayName"`
	Config      domain.ProtocolConfig `json:"config"`
}
type ProtocolTestResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
type SafeEndpoint struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Security    string `json:"security"`
	RootCAPEM   string `json:"root_ca_pem"`
	Username    string `json:"username"`
	AuthMode    string `json:"auth_mode"`
	HasPassword bool   `json:"has_password"`
}
type ProtocolSettings struct {
	AccountID         string               `json:"accountId"`
	Revision          string               `json:"revision"`
	IMAP              SafeEndpoint         `json:"imap"`
	SMTP              SafeEndpoint         `json:"smtp"`
	POP3              SafeEndpoint         `json:"pop3"`
	SelectedFolders   []string             `json:"selected_folders"`
	Proxy             mailnet.ProxySetting `json:"proxy"`
	MicrosoftClientID string               `json:"microsoft_client_id,omitempty"`
	MicrosoftTenant   string               `json:"microsoft_tenant,omitempty"`
}
type NetworkSettings struct {
	Proxy *mailnet.Proxy `json:"proxy"`
}

func (request AddAccountRequest) configuration() (domain.ProtocolConfig, error) {
	if request.AcceptInvalidCerts || request.AllowPlaintext {
		return domain.ProtocolConfig{}, errors.New("Explicit root CA trust and verified TLS are required")
	}
	if request.Config != nil {
		return *request.Config, nil
	}
	mode := request.ImapAuthMode
	if mode == "" {
		mode = "password"
	}
	smtpMode := request.SmtpAuthMode
	if smtpMode == "" {
		smtpMode = "password"
	}
	if request.SmtpHost == "" {
		smtpMode = "disabled"
	}
	username := request.Username
	if username == "" {
		username = request.Email
	}
	c := domain.ProtocolConfig{Version: 1, IMAP: domain.ProtocolEndpoint{Host: request.ImapHost, Port: request.ImapPort, Security: request.ImapSecurity, RootCAPEM: request.RootCAPEM, Username: username, Password: request.Password, AuthMode: mode}, SMTP: domain.ProtocolEndpoint{Host: request.SmtpHost, Port: request.SmtpPort, Security: request.SmtpSecurity, RootCAPEM: request.RootCAPEM, Username: username, Password: request.Password, AuthMode: smtpMode}, SelectedFolders: request.SelectedFolders, Proxy: mailnet.ProxySetting{Mode: request.ProxyMode}}
	if request.Provider == "pop3" {
		c.POP3 = c.IMAP
		c.IMAP = domain.ProtocolEndpoint{}
	}
	if request.ProxyHost != "" {
		scheme := request.ProxyScheme
		if scheme == "" {
			scheme = "http"
		}
		c.Proxy = mailnet.ProxySetting{Mode: "custom", Proxy: &mailnet.Proxy{Scheme: scheme, Host: request.ProxyHost, Port: request.ProxyPort}}
	}
	return c, nil
}

func validateProtocolConfig(provider string, c domain.ProtocolConfig) error {
	if c.Version != 1 {
		return errors.New("Unsupported protocol configuration version")
	}
	if _, err := mailnet.ResolveProxy(nil, c.Proxy); err != nil {
		return err
	}
	if len(c.SelectedFolders) > 256 {
		return errors.New("Too many selected folders")
	}
	for _, folder := range c.SelectedFolders {
		if !validAccountText(folder, 1024) || folder == "" {
			return errors.New("Invalid selected folder")
		}
	}
	var endpoints []domain.ProtocolEndpoint
	switch provider {
	case "imap":
		endpoints = append(endpoints, c.IMAP)
	case "pop3":
		endpoints = append(endpoints, c.POP3)
	case "outlook":
		if c.Microsoft == nil {
			return errors.New("Microsoft configuration required")
		}
	default:
		return errors.New("Unsupported protocol provider")
	}
	if c.SMTP.AuthMode != "disabled" && c.SMTP.Host != "" {
		endpoints = append(endpoints, c.SMTP)
	}
	for _, e := range endpoints {
		if err := e.Endpoint().Validate(); err != nil {
			return err
		}
		if !validAccountText(e.Username, 320) || e.Username == "" || len(e.Password) > 16384 {
			return errors.New("Invalid protocol credentials")
		}
		if e.AuthMode != "password" && e.AuthMode != "xoauth2" {
			return errors.New("Unsupported protocol authentication")
		}
		if e.AuthMode == "password" && e.Password == "" {
			return errors.New("Protocol password required")
		}
		if e.AuthMode == "xoauth2" && c.Microsoft == nil {
			return errors.New("OAuth grant required")
		}
	}
	return nil
}
func protocolRevision(encrypted []byte) string {
	s := sha256.Sum256(encrypted)
	return hex.EncodeToString(s[:])
}
func (r *Runtime) loadProtocol(ctx context.Context, account string) (domain.ProtocolConfig, []byte, error) {
	var c domain.ProtocolConfig
	encrypted, err := r.DB.AuthData(ctx, account)
	if err != nil {
		return c, nil, err
	}
	if r.crypto == nil {
		return c, nil, errors.New("Account credentials unavailable")
	}
	plain, err := r.crypto.Decrypt("accounts.auth_data", account, encrypted)
	if err != nil {
		return c, nil, errors.New("Account credentials unavailable")
	}
	if json.Unmarshal(plain, &c) != nil || c.Version != 1 {
		return c, nil, errors.New("Invalid encrypted protocol configuration")
	}
	return c, encrypted, nil
}
func (r *Runtime) encodeProtocol(ctx context.Context, id string, c domain.ProtocolConfig) ([]byte, error) {
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	return cipher.Encrypt("accounts.auth_data", id, plain)
}

// Caller holds r.mu. SaveConfig serializes resource refresh and persists before publication.
func (r *Runtime) makeProtocolLocked(a domain.Account, c domain.ProtocolConfig, encrypted []byte, generation *accountConnection) (domain.Transport, error) {
	if err := validateProtocolConfig(a.Provider, c); err != nil {
		return nil, err
	}
	factory := r.providerFactories[a.Provider]
	if factory == nil {
		return nil, errors.New("Provider is not configured")
	}
	proxy, err := mailnet.ResolveProxy(r.globalProxy, c.Proxy)
	if err != nil {
		return nil, err
	}
	var vaultMu sync.Mutex
	observed := append([]byte(nil), encrypted...)
	pc := ProviderContext{Account: a, Config: c, DB: r.DB, Dialer: mailnet.Dialer{Proxy: proxy}}
	pc.BeforeRequest = func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.mu.RLock()
		defer r.mu.RUnlock()
		if r.connections[a.ID] != generation {
			return errors.New("Account credentials changed")
		}
		if generation.state.Blocked() {
			return &domain.AccessError{State: generation.state}
		}
		return nil
	}
	pc.UpdateConfig = func(ctx context.Context, mutate func(*domain.ProtocolConfig) error) error {
		vaultMu.Lock()
		defer vaultMu.Unlock()
		if err := pc.BeforeRequest(ctx); err != nil {
			return err
		}
		plain, err := r.crypto.Decrypt("accounts.auth_data", a.ID, observed)
		if err != nil {
			return errors.New("Account credentials unavailable")
		}
		var next domain.ProtocolConfig
		if json.Unmarshal(plain, &next) != nil {
			return errors.New("Invalid encrypted protocol configuration")
		}
		if err := mutate(&next); err != nil {
			return err
		}
		if err := validateProtocolConfig(a.Provider, next); err != nil {
			return err
		}
		plain, err = json.Marshal(next)
		if err != nil {
			return err
		}
		sealed, err := r.crypto.Encrypt("accounts.auth_data", a.ID, plain)
		if err != nil {
			return err
		}
		ok, err := r.DB.CompareAndSwapAuthData(ctx, a.ID, observed, sealed, time.Now().Unix())
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("Account credentials changed")
		}
		observed = sealed
		return nil
	}
	pc.SaveConfig = func(ctx context.Context, next domain.ProtocolConfig) error {
		return pc.UpdateConfig(ctx, func(current *domain.ProtocolConfig) error { *current = next; return nil })
	}
	return factory(pc)
}

func (s *AccountService) Add(ctx context.Context, request AddAccountRequest) (domain.Account, error) {
	var zero domain.Account
	if request.Provider == "outlook" || request.Config != nil && request.Config.Microsoft != nil {
		return zero, errors.New("Use Microsoft authorization to verify the account identity")
	}
	addr, err := mail.ParseAddress(request.Email)
	if err != nil || addr.Address != strings.TrimSpace(request.Email) || !validAccountText(request.DisplayName, 256) || request.AccountLabel != nil && !validAccountText(*request.AccountLabel, 120) {
		return zero, errors.New("Invalid account identity")
	}
	c, err := request.configuration()
	if err != nil {
		return zero, err
	}
	if err = validateProtocolConfig(request.Provider, c); err != nil {
		return zero, err
	}
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(request.Email))))
	id := request.Provider + ":" + hex.EncodeToString(digest[:16])
	now := time.Now().Unix()
	a := domain.Account{ID: id, Email: strings.TrimSpace(request.Email), DisplayName: request.DisplayName, Provider: request.Provider, AccountLabel: request.AccountLabel, CreatedAt: now, UpdatedAt: now}
	r := s.Runtime
	encrypted, err := r.encodeProtocol(ctx, id, c)
	if err != nil {
		return zero, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	g := &accountConnection{state: domain.ConnectionAvailable}
	p, err := r.makeProtocolLocked(a, c, encrypted, g)
	if err != nil {
		return zero, err
	}
	if err = r.DB.CreateProtocolAccount(ctx, a, encrypted); err != nil {
		closeProtocol(p)
		return zero, errors.New("Account already exists or could not be saved")
	}
	r.connections[id] = g
	r.providers[id] = p
	a.Capabilities = p.Capabilities()
	return a, nil
}
func closeProtocol(p domain.Transport) {
	if closer, ok := p.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

func (s *AccountService) ProtocolSettings(ctx context.Context, request AccountRequest) (ProtocolSettings, error) {
	r := s.Runtime
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, encrypted, err := r.loadProtocol(ctx, request.AccountID)
	if err != nil {
		return ProtocolSettings{}, err
	}
	safe := func(e domain.ProtocolEndpoint) SafeEndpoint {
		return SafeEndpoint{Host: e.Host, Port: e.Port, Security: e.Security, RootCAPEM: e.RootCAPEM, Username: e.Username, AuthMode: e.AuthMode, HasPassword: e.Password != ""}
	}
	out := ProtocolSettings{AccountID: request.AccountID, Revision: protocolRevision(encrypted), IMAP: safe(c.IMAP), SMTP: safe(c.SMTP), POP3: safe(c.POP3), SelectedFolders: c.SelectedFolders, Proxy: c.Proxy}
	if len(out.SelectedFolders) == 0 && c.IMAP.Host != "" {
		out.SelectedFolders = []string{"INBOX"}
	}
	if c.Microsoft != nil {
		out.MicrosoftClientID = c.Microsoft.ClientID
		out.MicrosoftTenant = c.Microsoft.Tenant
	}
	return out, nil
}
func (s *AccountService) SaveProtocol(ctx context.Context, request SaveProtocolRequest) (domain.Account, error) {
	r := s.Runtime
	var zero domain.Account
	if !validAccountText(request.DisplayName, 256) {
		return zero, errors.New("Invalid display name")
	}
	r.mu.RLock()
	oldConfig, old, err := r.loadProtocol(ctx, request.AccountID)
	r.mu.RUnlock()
	if err != nil {
		return zero, err
	}
	if request.Revision == "" || protocolRevision(old) != request.Revision {
		return zero, errors.New("Account settings changed; reload before saving")
	}
	c := request.Config
	if c.IMAP.Password == "" {
		c.IMAP.Password = oldConfig.IMAP.Password
	}
	if c.SMTP.Password == "" {
		c.SMTP.Password = oldConfig.SMTP.Password
	}
	if c.POP3.Password == "" {
		c.POP3.Password = oldConfig.POP3.Password
	}
	// Resource grants can only be replaced by the validated OAuth path.
	c.Microsoft = oldConfig.Microsoft
	a, err := r.DB.Account(ctx, request.AccountID)
	if err != nil {
		return zero, err
	}
	if err = validateProtocolConfig(a.Provider, c); err != nil {
		return zero, err
	}
	encrypted, err := r.encodeProtocol(ctx, a.ID, c)
	if err != nil {
		return zero, err
	}
	release := r.quiesceAccount(a.ID)
	committed := false
	defer func() {
		release()
		if committed {
			r.connectionChanged(a.ID)
			_ = r.Scheduler.Start(a.ID, 0)
		}
	}()
	r.mu.Lock()
	defer r.mu.Unlock()
	g := &accountConnection{state: domain.ConnectionAvailable}
	a.DisplayName = request.DisplayName
	p, err := r.makeProtocolLocked(a, c, encrypted, g)
	if err != nil {
		return zero, err
	}
	ok, err := r.DB.ReplaceProtocolAccount(ctx, a, old, encrypted)
	if err != nil || !ok {
		closeProtocol(p)
		return zero, errors.New("Account settings changed or could not be saved")
	}
	previous := r.providers[a.ID]
	r.connections[a.ID] = g
	r.providers[a.ID] = p
	closeProtocol(previous)
	a.Capabilities = p.Capabilities()
	committed = true
	return a, nil
}

func (s *AccountService) Test(ctx context.Context, request AddAccountRequest) (ProtocolTestResult, error) {
	c, err := request.configuration()
	if err != nil {
		return ProtocolTestResult{}, err
	}
	if err = validateProtocolConfig(request.Provider, c); err != nil {
		return ProtocolTestResult{}, err
	}
	r := s.Runtime
	r.mu.RLock()
	factory := r.providerFactories[request.Provider]
	proxy, err := mailnet.ResolveProxy(r.globalProxy, c.Proxy)
	r.mu.RUnlock()
	if err != nil {
		return ProtocolTestResult{}, err
	}
	if factory == nil {
		return ProtocolTestResult{}, errors.New("Provider is not configured")
	}
	p, err := factory(ProviderContext{Account: domain.Account{ID: "protocol-connection-test", Email: request.Email, Provider: request.Provider}, Config: c, DB: r.DB, Dialer: mailnet.Dialer{Proxy: proxy}, BeforeRequest: func(ctx context.Context) error { return ctx.Err() }, SaveConfig: func(context.Context, domain.ProtocolConfig) error {
		return errors.New("Connection test cannot replace saved grants")
	}})
	if err != nil {
		return ProtocolTestResult{}, errors.New("Connection test configuration failed")
	}
	defer closeProtocol(p)
	tester, ok := p.(interface{ Test(context.Context) error })
	if !ok {
		return ProtocolTestResult{}, errors.New("Provider connection test unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := tester.Test(ctx); err != nil {
		return ProtocolTestResult{}, errors.New("Connection test failed; verify endpoint, trust and authentication settings")
	}
	return ProtocolTestResult{Success: true, Message: "Connection verified"}, nil
}

func (s *AccountService) TestStored(ctx context.Context, request AccountRequest) (ProtocolTestResult, error) {
	p, err := s.Runtime.provider(request.AccountID)
	if err != nil {
		return ProtocolTestResult{}, err
	}
	if g, ok := p.(*gmail.Provider); ok {
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		profile, err := g.API.Profile(ctx)
		if err != nil {
			return ProtocolTestResult{}, errors.New("Gmail connection test failed; verify network and authorization settings")
		}
		a, err := s.Runtime.DB.Account(ctx, request.AccountID)
		if err != nil || !strings.EqualFold(a.Email, profile.Email) {
			return ProtocolTestResult{}, errors.New("Gmail mailbox identity does not match the saved account")
		}
		return ProtocolTestResult{Success: true, Message: "Connection verified"}, nil
	}
	return testProtocolTransport(ctx, p)
}

func testProtocolTransport(ctx context.Context, p domain.Transport) (ProtocolTestResult, error) {
	tester, ok := p.(interface{ Test(context.Context) error })
	if !ok {
		return ProtocolTestResult{}, errors.New("Provider connection test unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := tester.Test(ctx); err != nil {
		return ProtocolTestResult{}, errors.New("Connection test failed; verify endpoint, trust and authentication settings")
	}
	return ProtocolTestResult{Success: true, Message: "Connection verified"}, nil
}

// TestProtocolSettings retains saved passwords without publishing them to the UI.
func (s *AccountService) TestProtocolSettings(ctx context.Context, request SaveProtocolRequest) (ProtocolTestResult, error) {
	r := s.Runtime
	r.mu.RLock()
	oldConfig, encrypted, err := r.loadProtocol(ctx, request.AccountID)
	r.mu.RUnlock()
	if err != nil {
		return ProtocolTestResult{}, err
	}
	if request.Revision == "" || request.Revision != protocolRevision(encrypted) {
		return ProtocolTestResult{}, errors.New("Account settings changed; reload before testing")
	}
	a, err := r.DB.Account(ctx, request.AccountID)
	if err != nil {
		return ProtocolTestResult{}, err
	}
	c := request.Config
	if c.IMAP.Password == "" {
		c.IMAP.Password = oldConfig.IMAP.Password
	}
	if c.SMTP.Password == "" {
		c.SMTP.Password = oldConfig.SMTP.Password
	}
	if c.POP3.Password == "" {
		c.POP3.Password = oldConfig.POP3.Password
	}
	// Microsoft tests must refresh through the saved CAS vault. Edited routes and
	// policies are applied only by SaveProtocol, then tested with TestStored.
	if oldConfig.Microsoft != nil {
		return ProtocolTestResult{}, errors.New("Save Microsoft settings, then use Test saved connection so refreshed grants persist safely")
	}
	return s.Test(ctx, AddAccountRequest{Email: a.Email, Provider: a.Provider, Config: &c})
}

func (s *AccountService) Network(ctx context.Context, _ Empty) (NetworkSettings, error) {
	var out NetworkSettings
	data, err := s.Runtime.DB.NetworkSettings(ctx)
	if err != nil {
		return out, err
	}
	if len(data) > 0 && json.Unmarshal(data, &out) != nil {
		return out, errors.New("Invalid network settings")
	}
	return out, nil
}
func (s *AccountService) SetNetwork(ctx context.Context, request NetworkSettings) (Empty, error) {
	if !s.Runtime.authFlowMu.TryLock() {
		return Empty{}, errors.New("Finish or cancel sign-in before changing network settings")
	}
	defer s.Runtime.authFlowMu.Unlock()
	if _, err := mailnet.ResolveProxy(request.Proxy, mailnet.ProxySetting{}); err != nil {
		return Empty{}, err
	}
	r := s.Runtime
	accounts, err := r.DB.Accounts(ctx)
	if err != nil {
		return Empty{}, err
	}
	// The settings transaction is local. Stop and join every account before replacing transports.
	r.Scheduler.control.Lock()
	for _, a := range accounts {
		r.Scheduler.stop(a.ID)
	}
	r.labelManagementMu.Lock()
	r.cloudDraftMu.Lock()
	r.pendingMu.Lock()
	r.outboxMu.Lock()
	defer func() {
		r.outboxMu.Unlock()
		r.pendingMu.Unlock()
		r.cloudDraftMu.Unlock()
		r.labelManagementMu.Unlock()
		r.Scheduler.control.Unlock()
		for _, a := range accounts {
			_ = r.Scheduler.Start(a.ID, 0)
		}
	}()
	data, _ := json.Marshal(request)
	if err := r.DB.SaveNetworkSettings(ctx, data); err != nil {
		return Empty{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.globalProxy = request.Proxy
	for _, a := range accounts {
		if a.Provider == "gmail" {
			g := &accountConnection{state: domain.ConnectionAvailable}
			if old := r.connections[a.ID]; old != nil {
				g.state = old.state
			}
			r.connections[a.ID] = g
			if err := r.installGmailLocked(a.ID, r.googleOAuthConfigLocked(), g); err != nil {
				g.state = domain.ConnectionCredentialsUnavailable
			}
			continue
		}
		c, encrypted, err := r.loadProtocol(ctx, a.ID)
		if err != nil {
			continue
		}
		g := &accountConnection{state: domain.ConnectionAvailable}
		if previous := r.connections[a.ID]; previous != nil {
			g.state = previous.state
		}
		p, err := r.makeProtocolLocked(a, c, encrypted, g)
		if err != nil {
			r.connections[a.ID] = &accountConnection{state: domain.ConnectionConfigurationRequired}
			continue
		}
		old := r.providers[a.ID]
		r.providers[a.ID] = p
		r.connections[a.ID] = g
		closeProtocol(old)
	}
	return Empty{}, nil
}
