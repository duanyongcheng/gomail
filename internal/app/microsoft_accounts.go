package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/mailnet"
	"github.com/duanyongcheng/pebble-go/internal/oauth"
	"golang.org/x/oauth2"
)

type MicrosoftAuthorizeRequest struct {
	AccountID   string               `json:"accountId,omitempty"`
	DisplayName string               `json:"displayName"`
	ClientID    string               `json:"clientId"`
	Tenant      string               `json:"tenant"`
	Audience    string               `json:"audience"`
	Resource    string               `json:"resource"`
	Graph       bool                 `json:"graph"`
	IMAP        bool                 `json:"imap"`
	SMTP        bool                 `json:"smtp"`
	Proxy       mailnet.ProxySetting `json:"proxy"`
}

type MicrosoftStatus struct {
	SendState       domain.ConnectionState `json:"sendState"`
	WriteState      domain.ConnectionState `json:"writeState"`
	AccountID       string                 `json:"accountId"`
	ClientID        string                 `json:"clientId"`
	Tenant          string                 `json:"tenant"`
	Audience        string                 `json:"audience"`
	Graph           bool                   `json:"graph"`
	IMAP            bool                   `json:"imap"`
	SMTP            bool                   `json:"smtp"`
	GraphAuthorized bool                   `json:"graphAuthorized"`
	IMAPAuthorized  bool                   `json:"imapAuthorized"`
	SMTPAuthorized  bool                   `json:"smtpAuthorized"`
}

type MicrosoftService struct {
	Runtime *Runtime
	// Synthetic fixtures inject only local transports and browser flows.
	HTTP          *http.Client
	AuthorizeFlow func(context.Context, *oauth2.Config, *http.Client, func(string) error) (*oauth2.Token, error)
}

func (s *MicrosoftService) Cancel(ctx context.Context, request AccountRequest) (Empty, error) {
	return (&AccountService{Runtime: s.Runtime}).CancelReconnect(ctx, request)
}

func (s *MicrosoftService) Status(ctx context.Context, request AccountRequest) (MicrosoftStatus, error) {
	r := s.Runtime
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, _, err := r.loadProtocol(ctx, request.AccountID)
	if err != nil {
		return MicrosoftStatus{}, err
	}
	m := c.Microsoft
	if m == nil {
		return MicrosoftStatus{}, errors.New("Account has no Microsoft authorization")
	}
	has := func(resource string, scope string) bool {
		g := m.Grants[resource]
		if g.RefreshToken == "" {
			return false
		}
		for _, value := range g.Scopes {
			if value == scope || strings.TrimPrefix(scope, "https://graph.microsoft.com/") == value {
				return true
			}
		}
		return false
	}
	sendState, err := r.DB.ProviderSendState(ctx, request.AccountID)
	if err != nil {
		return MicrosoftStatus{}, errors.New("Cannot read Microsoft send authorization state")
	}
	writeState, err := r.DB.ProviderWriteState(ctx, request.AccountID)
	if err != nil {
		return MicrosoftStatus{}, err
	}
	return MicrosoftStatus{WriteState: writeState, SendState: sendState, AccountID: request.AccountID, ClientID: m.ClientID, Tenant: m.Tenant, Audience: m.Audience, Graph: m.Graph, IMAP: m.IMAP, SMTP: m.SMTP, GraphAuthorized: has("graph", oauth.MicrosoftMailReadWrite) && has("graph", oauth.MicrosoftMailSend), IMAPAuthorized: has("exchange", oauth.MicrosoftIMAP), SMTPAuthorized: has("exchange", oauth.MicrosoftSMTP)}, nil
}

// Authorize validates the stable provider identity before creating an account or
// CAS-replacing its encrypted config. Browser/profile work holds no Runtime lock.
func (s *MicrosoftService) Authorize(ctx context.Context, request MicrosoftAuthorizeRequest) (domain.Account, error) {
	account, err := s.authorizeMicrosoft(ctx, request)
	if err == nil && s.Runtime.OnAccountChanged != nil {
		s.Runtime.OnAccountChanged(AccountRequest{AccountID: account.ID})
	}
	return account, err
}

func (s *MicrosoftService) authorizeMicrosoft(ctx context.Context, request MicrosoftAuthorizeRequest) (domain.Account, error) {
	r := s.Runtime
	var zero domain.Account
	if !r.authFlowMu.TryLock() {
		return zero, errors.New("Another account sign-in is already in progress")
	}
	defer r.authFlowMu.Unlock()
	if !validAccountText(request.DisplayName, 256) {
		return zero, errors.New("Invalid display name")
	}
	policy := oauth.MicrosoftPolicy{ClientID: request.ClientID, Tenant: request.Tenant, Audience: request.Audience, Graph: request.Graph, IMAP: request.IMAP, SMTP: request.SMTP}
	if err := policy.Validate(); err != nil {
		return zero, err
	}
	resource := oauth.MicrosoftResource(request.Resource)
	if _, err := oauth.NewMicrosoftConfig(policy, resource); err != nil {
		return zero, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(r.Context, cancel)
	defer stop()
	r.mu.Lock()
	r.reconnectAccount, r.reconnectCancel = request.AccountID, cancel
	global := r.globalProxy
	r.mu.Unlock()
	defer func() { r.mu.Lock(); r.reconnectAccount = ""; r.reconnectCancel = nil; r.mu.Unlock() }()
	var account domain.Account
	var old []byte
	var c domain.ProtocolConfig
	if request.AccountID != "" {
		var err error
		account, err = r.DB.Account(ctx, request.AccountID)
		if err != nil {
			return zero, err
		}
		if account.Provider != "outlook" && account.Provider != "imap" {
			return zero, errors.New("Account does not support Microsoft authorization")
		}
		r.mu.RLock()
		c, old, err = r.loadProtocol(ctx, request.AccountID)
		r.mu.RUnlock()
		if err != nil {
			return zero, err
		}
		if c.Microsoft == nil {
			return zero, errors.New("Account has no Microsoft identity binding")
		}
		if c.Microsoft.ClientID != policy.ClientID || c.Microsoft.Tenant != policy.Tenant || c.Microsoft.Audience != policy.Audience {
			return zero, errors.New("Microsoft authority or application changed; add a separate account")
		}
	} else {
		c = domain.ProtocolConfig{Version: 1, Proxy: request.Proxy, SelectedFolders: []string{"INBOX"}, SMTP: domain.ProtocolEndpoint{AuthMode: "disabled"}}
	}
	proxy, err := mailnet.ResolveProxy(global, c.Proxy)
	if err != nil {
		return zero, err
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{Transport: (mailnet.Dialer{Proxy: proxy}).HTTPTransport(), Timeout: 30 * time.Second}
	}
	flow := s.AuthorizeFlow
	if flow == nil {
		flow = oauth.AuthorizeMicrosoft
	}
	// A Graph User.Read grant is necessary even for Exchange-only clients: mailbox
	// authority comes from /me, never a typed email or decoded access-token claims.
	identityPolicy := policy
	identityPolicy.Graph = true
	graphConfig, _ := oauth.NewMicrosoftConfig(identityPolicy, oauth.MicrosoftGraph)
	if !policy.Graph {
		graphConfig.Scopes = []string{"offline_access", oauth.MicrosoftUserRead}
	}
	graphToken, err := flow(ctx, graphConfig, client, r.OpenBrowser)
	if err != nil {
		return zero, err
	}
	identity, err := oauth.MicrosoftProfile(ctx, client, graphToken.AccessToken)
	if err != nil {
		return zero, err
	}
	if request.AccountID != "" && (identity.Subject != c.Microsoft.Subject || !strings.EqualFold(identity.Mailbox, account.Email)) {
		return zero, errors.New("Microsoft sign-in selected a different mailbox; saved credentials are unchanged")
	}
	if c.Microsoft == nil {
		c.Microsoft = &domain.MicrosoftConfig{Grants: map[string]domain.ResourceGrant{}}
	}
	if c.Microsoft.Grants == nil {
		c.Microsoft.Grants = map[string]domain.ResourceGrant{}
	}
	m := c.Microsoft
	m.ClientID = policy.ClientID
	m.Tenant = policy.Tenant
	m.Audience = policy.Audience
	m.Graph = policy.Graph
	m.IMAP = policy.IMAP
	m.SMTP = policy.SMTP
	m.Subject = identity.Subject
	m.Generation++
	grant := func(token *oauth2.Token) domain.ResourceGrant {
		scope, _ := token.Extra("scope").(string)
		return domain.ResourceGrant{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, Expiry: token.Expiry.Unix(), Scopes: strings.Fields(scope)}
	}
	m.Grants["graph"] = grant(graphToken)
	if resource == oauth.MicrosoftExchange {
		exchangeConfig, _ := oauth.NewMicrosoftConfig(policy, resource)
		token, e := flow(ctx, exchangeConfig, client, r.OpenBrowser)
		if e != nil {
			return zero, e
		}
		exchangeIdentity, verified, e := oauth.VerifyMicrosoftExchangeIdentity(ctx, policy, client, token)
		if e != nil {
			return zero, e
		}
		if exchangeIdentity.Subject != identity.Subject || !strings.EqualFold(exchangeIdentity.Mailbox, identity.Mailbox) {
			return zero, errors.New("Exchange authorization selected a different Microsoft mailbox")
		}
		m.Grants["exchange"] = grant(verified)
	}
	if policy.IMAP {
		c.IMAP = domain.ProtocolEndpoint{Host: "outlook.office365.com", Port: 993, Security: "tls", Username: identity.Mailbox, AuthMode: "xoauth2"}
	} else {
		c.IMAP = domain.ProtocolEndpoint{AuthMode: "disabled"}
	}
	if policy.SMTP {
		c.SMTP = domain.ProtocolEndpoint{Host: "smtp.office365.com", Port: 587, Security: "starttls", Username: identity.Mailbox, AuthMode: "xoauth2"}
	} else {
		c.SMTP = domain.ProtocolEndpoint{AuthMode: "disabled"}
	}
	if err = validateProtocolConfig("outlook", c); err != nil {
		return zero, err
	}
	if request.AccountID == "" {
		digest := sha256.Sum256([]byte(policy.ClientID + "\x00" + policy.Tenant + "\x00" + identity.Subject))
		now := time.Now().Unix()
		account = domain.Account{ID: "outlook:" + hex.EncodeToString(digest[:16]), Provider: "outlook", Email: identity.Mailbox, DisplayName: request.DisplayName, CreatedAt: now, UpdatedAt: now}
	}
	encrypted, err := r.encodeProtocol(ctx, account.ID, c)
	if err != nil {
		return zero, err
	}
	release := r.quiesceAccount(account.ID)
	defer release()
	r.mu.Lock()
	defer r.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	generation := &accountConnection{state: domain.ConnectionAvailable}
	provider, err := r.makeProtocolLocked(account, c, encrypted, generation)
	if err != nil {
		return zero, err
	}
	if request.AccountID == "" {
		err = r.DB.CreateProtocolAccount(ctx, account, encrypted)
	} else {
		var ok bool
		ok, err = r.DB.ReplaceProtocolAccount(ctx, account, old, encrypted)
		if err == nil && !ok {
			err = errors.New("Account changed during Microsoft authorization")
		}
	}
	if err != nil {
		closeProtocol(provider)
		return zero, errors.New("Microsoft account changed or could not be saved")
	}
	previous := r.providers[account.ID]
	r.providers[account.ID] = provider
	r.connections[account.ID] = generation
	closeProtocol(previous)
	account.Capabilities = provider.Capabilities()
	return account, nil
}
