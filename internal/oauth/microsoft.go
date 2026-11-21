package oauth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/emersion/go-sasl"
	"golang.org/x/oauth2"
)

type MicrosoftResource string

const (
	MicrosoftGraph         MicrosoftResource = "graph"
	MicrosoftExchange      MicrosoftResource = "exchange"
	MicrosoftUserRead                        = "https://graph.microsoft.com/User.Read"
	MicrosoftMailReadWrite                   = "https://graph.microsoft.com/Mail.ReadWrite"
	MicrosoftMailSend                        = "https://graph.microsoft.com/Mail.Send"
	MicrosoftIMAP                            = "https://outlook.office.com/IMAP.AccessAsUser.All"
	MicrosoftSMTP                            = "https://outlook.office.com/SMTP.Send"
)

// Audience describes the native application's registered supported account types.
type MicrosoftPolicy struct {
	ClientID string `json:"client_id"`
	Tenant   string `json:"tenant"`
	Audience string `json:"audience"`
	IMAP     bool   `json:"imap"`
	SMTP     bool   `json:"smtp"`
	Graph    bool   `json:"graph"`
}

var microsoftGUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (p MicrosoftPolicy) Validate() error {
	if !microsoftGUID.MatchString(p.ClientID) || p.ClientID == "00000000-0000-0000-0000-000000000000" {
		return errors.New("Microsoft native client ID must be a nonzero application GUID")
	}
	tenantGUID := microsoftGUID.MatchString(p.Tenant) && p.Tenant != "00000000-0000-0000-0000-000000000000"
	if !tenantGUID && p.Tenant != "common" && p.Tenant != "organizations" && p.Tenant != "consumers" {
		return errors.New("Microsoft tenant must be common, organizations, consumers or a nonzero tenant GUID")
	}
	ok := false
	switch p.Audience {
	case "single_tenant":
		ok = tenantGUID
	case "organizations":
		ok = tenantGUID || p.Tenant == "organizations"
	case "personal":
		ok = p.Tenant == "consumers"
	case "all":
		ok = true
	}
	if !ok {
		return errors.New("Microsoft tenant does not match the configured application audience")
	}
	return nil
}

func NewMicrosoftConfig(p MicrosoftPolicy, resource MicrosoftResource) (*oauth2.Config, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	scopes := []string{"offline_access"}
	switch resource {
	case MicrosoftGraph:
		if !p.Graph {
			return nil, errors.New("Microsoft Graph authorization is disabled")
		}
		scopes = append(scopes, MicrosoftUserRead, MicrosoftMailReadWrite, MicrosoftMailSend)
	case MicrosoftExchange:
		if p.IMAP {
			scopes = append(scopes, MicrosoftIMAP)
		}
		if p.SMTP {
			scopes = append(scopes, MicrosoftSMTP)
		}
		if len(scopes) == 1 {
			return nil, errors.New("Microsoft protocol authorization is disabled")
		}
	default:
		return nil, errors.New("unsupported Microsoft resource")
	}
	base := "https://login.microsoftonline.com/" + p.Tenant + "/oauth2/v2.0/"
	return &oauth2.Config{ClientID: p.ClientID, Scopes: scopes, Endpoint: oauth2.Endpoint{AuthURL: base + "authorize", TokenURL: base + "token", AuthStyle: oauth2.AuthStyleInParams}}, nil
}

func microsoftClient(client *http.Client) (*http.Client, error) {
	if client == nil {
		return nil, errors.New("Microsoft requires an explicitly configured HTTP client")
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copy.Timeout == 0 {
		copy.Timeout = 30 * time.Second
	}
	return &copy, nil
}

// AuthorizeMicrosoft is a public-client PKCE flow; no client secret or Google parameters are sent.
func AuthorizeMicrosoft(ctx context.Context, cfg *oauth2.Config, client *http.Client, open func(string) error) (*oauth2.Token, error) {
	if cfg == nil || cfg.ClientID == "" || open == nil {
		return nil, errors.New("Microsoft authorization is not configured")
	}
	endpoint, parseErr := url.Parse(cfg.Endpoint.AuthURL)
	if parseErr != nil || endpoint.Scheme != "https" || endpoint.Host != "login.microsoftonline.com" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("invalid Microsoft authority")
	}
	parts := strings.Split(strings.TrimPrefix(endpoint.Path, "/"), "/")
	if len(parts) != 4 || parts[1] != "oauth2" || parts[2] != "v2.0" || parts[3] != "authorize" || endpoint.RawPath != "" || cfg.Endpoint.TokenURL != "https://login.microsoftonline.com/"+parts[0]+"/oauth2/v2.0/token" {
		return nil, errors.New("invalid Microsoft authority")
	}
	check := MicrosoftPolicy{ClientID: cfg.ClientID, Tenant: parts[0], Audience: "all"}
	if err := check.Validate(); err != nil {
		return nil, err
	}
	client, err := microsoftClient(client)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("Microsoft callback listener unavailable")
	}
	defer ln.Close()
	copy := *cfg
	copy.ClientSecret = ""
	copy.RedirectURL = "http://" + ln.Addr().String() + "/oauth/callback"
	state, verifier := oauth2.GenerateVerifier(), oauth2.GenerateVerifier()
	type callback struct {
		code string
		err  error
	}
	result := make(chan callback, 1)
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		q, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil || r.Method != http.MethodGet || r.Host != ln.Addr().String() || len(r.URL.RawQuery) > 8192 || len(q["state"]) != 1 || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "Invalid callback", 400)
			return
		}
		if len(q["error"]) > 0 {
			once.Do(func() { result <- callback{err: errors.New("Microsoft authorization denied")} })
			http.Error(w, "Authorization denied", 400)
			return
		}
		if len(q["code"]) != 1 || q.Get("code") == "" {
			http.Error(w, "Missing authorization code", 400)
			return
		}
		used := false
		once.Do(func() { used = true; result <- callback{code: q.Get("code")} })
		if !used {
			http.Error(w, "Callback already consumed", 409)
			return
		}
		_, _ = io.WriteString(w, "Authorization received. Return to Pebble.")
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 8192}
	defer server.Close()
	go func() { _ = server.Serve(ln) }()
	if err := open(copy.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))); err != nil {
		return nil, errors.New("could not open Microsoft authorization browser")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case reply := <-result:
		if reply.err != nil {
			return nil, reply.err
		}
		return microsoftExchange(ctx, client, &copy, url.Values{"grant_type": {"authorization_code"}, "code": {reply.code}, "redirect_uri": {copy.RedirectURL}, "code_verifier": {verifier}}, nil)
	}
}

func microsoftExchange(ctx context.Context, client *http.Client, cfg *oauth2.Config, form url.Values, old *MicrosoftGrant) (*oauth2.Token, error) {
	form.Set("client_id", cfg.ClientID)
	form.Set("scope", strings.Join(cfg.Scopes, " "))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Endpoint.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, errors.New("invalid Microsoft token endpoint")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &domain.AccessError{State: domain.ConnectionTemporaryError}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		return nil, errors.New("invalid Microsoft token response")
	}
	var body struct {
		Access  string  `json:"access_token"`
		Refresh string  `json:"refresh_token"`
		Type    string  `json:"token_type"`
		Expires int64   `json:"expires_in"`
		Scope   *string `json:"scope"`
		Error   string  `json:"error"`
	}
	if json.Unmarshal(data, &body) != nil {
		return nil, errors.New("invalid Microsoft token response")
	}
	if response.StatusCode != http.StatusOK {
		state := domain.ConnectionTemporaryError
		switch body.Error {
		case "invalid_grant", "invalid_token":
			state = domain.ConnectionReauthRequired
		case "invalid_client", "unauthorized_client":
			state = domain.ConnectionConfigurationRequired
		case "invalid_scope", "consent_required":
			state = domain.ConnectionPermissionRequired
		}
		return nil, &domain.AccessError{State: state}
	}
	if body.Access == "" || len(body.Access) > 32768 || strings.ContainsAny(body.Access, "\r\n\x00\x01") || !strings.EqualFold(body.Type, "Bearer") || body.Expires <= 0 || body.Expires > 86400*366 {
		return nil, errors.New("invalid Microsoft token response")
	}
	if old != nil && body.Refresh == "" {
		body.Refresh = old.RefreshToken
	}
	if body.Refresh == "" {
		return nil, &domain.AccessError{State: domain.ConnectionReauthRequired}
	}
	var scopes []string
	if body.Scope != nil {
		scopes = strings.Fields(*body.Scope)
	} else if old != nil {
		scopes = old.Scopes
	} else {
		return nil, &domain.AccessError{State: domain.ConnectionPermissionRequired}
	}
	if old == nil && !microsoftScopesContain(scopes, cfg.Scopes) {
		return nil, &domain.AccessError{State: domain.ConnectionPermissionRequired}
	}
	return (&oauth2.Token{AccessToken: body.Access, RefreshToken: body.Refresh, TokenType: "Bearer", Expiry: time.Now().Add(time.Duration(body.Expires) * time.Second)}).WithExtra(map[string]any{"scope": strings.Join(scopes, " ")}), nil
}

func microsoftScopesContain(granted, required []string) bool {
	normalize := func(s string) string {
		for _, scope := range []string{MicrosoftUserRead, MicrosoftMailReadWrite, MicrosoftMailSend} {
			if s == strings.TrimPrefix(scope, "https://graph.microsoft.com/") {
				return scope
			}
		}
		return s
	}
	for _, want := range required {
		if want == "offline_access" {
			continue
		}
		found := false
		for _, have := range granted {
			if normalize(have) == want {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

type MicrosoftGrant struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
	Scopes       []string  `json:"scopes"`
}
type MicrosoftCredentials struct {
	Generation uint64                               `json:"generation"`
	Grants     map[MicrosoftResource]MicrosoftGrant `json:"grants"`
}
type MicrosoftPersist func(context.Context, MicrosoftCredentials, MicrosoftCredentials) error

// MicrosoftTokens owns independent resource grants. Persist must CAS the encrypted
// observed credential record and reject deletion/reconnect conflicts.
type MicrosoftTokens struct {
	mu          sync.Mutex
	policy      MicrosoftPolicy
	client      *http.Client
	state       MicrosoftCredentials
	persist     MicrosoftPersist
	failed      bool
	invalidated map[MicrosoftResource]bool
}

func NewMicrosoftTokens(p MicrosoftPolicy, client *http.Client, initial MicrosoftCredentials, persist MicrosoftPersist) (*MicrosoftTokens, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	c, err := microsoftClient(client)
	if err != nil {
		return nil, err
	}
	if persist == nil {
		return nil, errors.New("Microsoft credential persistence is required")
	}
	return &MicrosoftTokens{policy: p, client: c, state: cloneMicrosoftCredentials(initial), persist: persist, invalidated: make(map[MicrosoftResource]bool)}, nil
}
func cloneMicrosoftCredentials(s MicrosoftCredentials) MicrosoftCredentials {
	n := MicrosoftCredentials{Generation: s.Generation, Grants: make(map[MicrosoftResource]MicrosoftGrant, len(s.Grants))}
	for k, v := range s.Grants {
		v.Scopes = append([]string(nil), v.Scopes...)
		n.Grants[k] = v
	}
	return n
}
func (t *MicrosoftTokens) Snapshot() MicrosoftCredentials {
	t.mu.Lock()
	defer t.mu.Unlock()
	return cloneMicrosoftCredentials(t.state)
}
func (t *MicrosoftTokens) save(ctx context.Context, r MicrosoftResource, g MicrosoftGrant) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	next := cloneMicrosoftCredentials(t.state)
	next.Generation++
	oldRefresh := t.state.Grants[r].RefreshToken
	if oldRefresh != "" && oldRefresh != g.RefreshToken {
		for resource, other := range next.Grants {
			if other.RefreshToken == oldRefresh {
				other.RefreshToken = g.RefreshToken
				next.Grants[resource] = other
			}
		}
	}
	next.Grants[r] = g
	if err := t.persist(ctx, cloneMicrosoftCredentials(t.state), cloneMicrosoftCredentials(next)); err != nil {
		t.failed = true
		return errors.New("cannot persist Microsoft credentials; reconnect required")
	}
	t.state = next
	delete(t.invalidated, r)
	return nil
}
func (t *MicrosoftTokens) Install(ctx context.Context, r MicrosoftResource, token *oauth2.Token) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	cfg, err := NewMicrosoftConfig(t.policy, r)
	if err != nil {
		return err
	}
	if token == nil {
		return errors.New("missing Microsoft grant")
	}
	scope, _ := token.Extra("scope").(string)
	if token.AccessToken == "" || token.RefreshToken == "" || !strings.EqualFold(token.TokenType, "Bearer") || token.Expiry.IsZero() || !token.Expiry.After(time.Now()) || !microsoftScopesContain(strings.Fields(scope), cfg.Scopes) {
		return &domain.AccessError{State: domain.ConnectionPermissionRequired}
	}
	return t.save(ctx, r, MicrosoftGrant{token.AccessToken, token.RefreshToken, token.TokenType, token.Expiry, strings.Fields(scope)})
}
func (t *MicrosoftTokens) Token(ctx context.Context, r MicrosoftResource, required ...string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if t.failed {
		return "", errors.New("Microsoft credential persistence conflicted; restore or reconnect required")
	}
	cfg, err := NewMicrosoftConfig(t.policy, r)
	if err != nil {
		return "", err
	}
	// Required scopes cannot cross resources or enable a disabled protocol.
	if !microsoftScopesContain(cfg.Scopes, required) {
		return "", &domain.AccessError{State: domain.ConnectionPermissionRequired}
	}
	g, ok := t.state.Grants[r]
	if !ok || g.RefreshToken == "" {
		return "", &domain.AccessError{State: domain.ConnectionReauthRequired}
	}
	if len(required) == 0 {
		required = cfg.Scopes
	}
	if !microsoftScopesContain(g.Scopes, required) {
		return "", &domain.AccessError{State: domain.ConnectionPermissionRequired}
	}
	if !t.invalidated[r] && g.AccessToken != "" && time.Until(g.Expiry) > time.Minute {
		return g.AccessToken, nil
	}
	fresh, err := microsoftExchange(ctx, t.client, cfg, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {g.RefreshToken}}, &g)
	if err != nil {
		return "", err
	}
	scope, _ := fresh.Extra("scope").(string)
	if err = t.save(ctx, r, MicrosoftGrant{fresh.AccessToken, fresh.RefreshToken, fresh.TokenType, fresh.Expiry, strings.Fields(scope)}); err != nil {
		return "", err
	}
	if !microsoftScopesContain(strings.Fields(scope), required) {
		return "", &domain.AccessError{State: domain.ConnectionPermissionRequired}
	}
	return fresh.AccessToken, nil
}
func (t *MicrosoftTokens) Invalidate(r MicrosoftResource, access string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	g := t.state.Grants[r]
	if g.AccessToken == access {
		t.invalidated[r] = true
	}
}

type MicrosoftIdentity struct {
	Subject string `json:"subject"`
	Mailbox string `json:"mailbox"`
}

// VerifyMicrosoftExchangeIdentity uses an explicitly scoped Graph refresh of
// the Exchange grant to bind that authorization to /me. A second browser flow
// may otherwise select a different mailbox from the initial Graph consent.
// The rotated refresh token is retained on the Exchange grant before storage.
func VerifyMicrosoftExchangeIdentity(ctx context.Context, p MicrosoftPolicy, client *http.Client, exchange *oauth2.Token) (MicrosoftIdentity, *oauth2.Token, error) {
	if exchange == nil || exchange.RefreshToken == "" {
		return MicrosoftIdentity{}, nil, errors.New("missing Exchange grant")
	}
	c, err := microsoftClient(client)
	if err != nil {
		return MicrosoftIdentity{}, nil, err
	}
	p.Graph = true
	cfg, err := NewMicrosoftConfig(p, MicrosoftGraph)
	if err != nil {
		return MicrosoftIdentity{}, nil, err
	}
	cfg.Scopes = []string{"offline_access", MicrosoftUserRead}
	old := MicrosoftGrant{RefreshToken: exchange.RefreshToken}
	profileToken, err := microsoftExchange(ctx, c, cfg, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {exchange.RefreshToken}}, &old)
	if err != nil {
		return MicrosoftIdentity{}, nil, err
	}
	scope, _ := profileToken.Extra("scope").(string)
	if !microsoftScopesContain(strings.Fields(scope), []string{MicrosoftUserRead}) {
		return MicrosoftIdentity{}, nil, &domain.AccessError{State: domain.ConnectionPermissionRequired}
	}
	identity, err := MicrosoftProfile(ctx, c, profileToken.AccessToken)
	if err != nil {
		return MicrosoftIdentity{}, nil, err
	}
	copy := *exchange
	copy.RefreshToken = profileToken.RefreshToken
	return identity, &copy, nil
}

func MicrosoftProfile(ctx context.Context, client *http.Client, access string) (MicrosoftIdentity, error) {
	if access == "" || len(access) > 32768 || strings.ContainsAny(access, "\r\n\x00\x01") {
		return MicrosoftIdentity{}, errors.New("invalid Microsoft access token")
	}
	c, err := microsoftClient(client)
	if err != nil {
		return MicrosoftIdentity{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://graph.microsoft.com/v1.0/me?$select=id,mail,userPrincipalName", nil)
	if err != nil {
		return MicrosoftIdentity{}, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := c.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return MicrosoftIdentity{}, ctx.Err()
		}
		return MicrosoftIdentity{}, errors.New("Microsoft identity request failed")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(data) > 65536 || resp.StatusCode != 200 {
		return MicrosoftIdentity{}, errors.New("Microsoft identity verification failed")
	}
	var p struct {
		ID   string `json:"id"`
		Mail string `json:"mail"`
		UPN  string `json:"userPrincipalName"`
	}
	if json.Unmarshal(data, &p) != nil || p.ID == "" || len(p.ID) > 256 || strings.ContainsAny(p.ID, "\r\n\x00") {
		return MicrosoftIdentity{}, errors.New("invalid Microsoft mailbox identity")
	}
	address := p.Mail
	if address == "" {
		address = p.UPN
	}
	parsed, e := mail.ParseAddress(address)
	if e != nil || parsed.Address != address || !strings.Contains(address, "@") {
		return MicrosoftIdentity{}, errors.New("Microsoft profile has no canonical mailbox")
	}
	return MicrosoftIdentity{p.ID, strings.ToLower(address)}, nil
}

type microsoftXOAUTH2 struct {
	mailbox, access     string
	started, challenged bool
}

func NewMicrosoftXOAUTH2(mailbox, access string) (sasl.Client, error) {
	parsed, err := mail.ParseAddress(mailbox)
	if err != nil || parsed.Address != mailbox || len(mailbox) > 320 || access == "" || len(access) > 32768 || strings.ContainsAny(mailbox+access, "\x00\x01\r\n") {
		return nil, errors.New("invalid XOAUTH2 credentials")
	}
	return &microsoftXOAUTH2{mailbox: mailbox, access: access}, nil
}
func (c *microsoftXOAUTH2) Start() (string, []byte, error) {
	if c.started {
		return "", nil, errors.New("XOAUTH2 already started")
	}
	c.started = true
	return "XOAUTH2", []byte("user=" + c.mailbox + "\x01auth=Bearer " + c.access + "\x01\x01"), nil
}
func (c *microsoftXOAUTH2) Next(challenge []byte) ([]byte, error) {
	if !c.started || c.challenged || len(challenge) > 8192 {
		return nil, errors.New("invalid XOAUTH2 challenge")
	}
	c.challenged = true
	return []byte{}, nil
}
