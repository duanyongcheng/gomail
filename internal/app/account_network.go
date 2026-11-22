package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/mailnet"
)

type AccountNetworkRequest struct {
	AccountID string               `json:"accountId"`
	Setting   mailnet.ProxySetting `json:"setting"`
}

func (s *AccountService) AccountNetwork(ctx context.Context, request AccountRequest) (mailnet.ProxySetting, error) {
	a, err := s.Runtime.DB.Account(ctx, request.AccountID)
	if err != nil {
		return mailnet.ProxySetting{}, err
	}
	if a.Provider == "gmail" {
		return s.Runtime.DB.AccountNetwork(ctx, request.AccountID)
	}
	settings, err := s.ProtocolSettings(ctx, request)
	return settings.Proxy, err
}

func (r *Runtime) accountNetworkClientLocked(ctx context.Context, account string) (*http.Client, error) {
	setting, err := r.DB.AccountNetwork(ctx, account)
	if err != nil {
		return nil, err
	}
	route, err := mailnet.ResolveProxy(r.globalProxy, setting)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: (mailnet.Dialer{Proxy: route}).HTTPTransport(), Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}
func (r *Runtime) accountNetworkClient(ctx context.Context, account string) (*http.Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.accountNetworkClientLocked(ctx, account)
}

func (s *AccountService) SetAccountNetwork(ctx context.Context, request AccountNetworkRequest) (Empty, error) {
	r := s.Runtime
	// An authorization exchange pins its route until mailbox verification finishes.
	if !r.authFlowMu.TryLock() {
		return Empty{}, errors.New("Finish or cancel sign-in before changing network settings")
	}
	defer r.authFlowMu.Unlock()
	if _, err := mailnet.ResolveProxy(nil, request.Setting); err != nil {
		return Empty{}, err
	}
	a, err := r.DB.Account(ctx, request.AccountID)
	if err != nil {
		return Empty{}, err
	}
	if a.Provider != "gmail" {
		r.mu.RLock()
		c, encrypted, err := r.loadProtocol(ctx, a.ID)
		r.mu.RUnlock()
		if err != nil {
			return Empty{}, err
		}
		c.Proxy = request.Setting
		_, err = s.SaveProtocol(ctx, SaveProtocolRequest{AccountID: a.ID, Revision: protocolRevision(encrypted), DisplayName: a.DisplayName, Config: c})
		return Empty{}, err
	}
	release := r.quiesceAccount(request.AccountID)
	defer func() { release(); _ = r.Scheduler.Start(request.AccountID, 0) }()
	r.mu.Lock()
	defer r.mu.Unlock()
	a, err = r.DB.Account(ctx, request.AccountID)
	if err != nil || a.Provider != "gmail" {
		return Empty{}, errors.New("Existing Gmail account required")
	}
	if err := r.DB.SetAccountNetwork(ctx, a.ID, request.Setting); err != nil {
		return Empty{}, err
	}
	old := r.connections[a.ID]
	g := &accountConnection{state: domain.ConnectionAvailable}
	if old != nil {
		g.state = old.state
	}
	r.connections[a.ID] = g
	if err := r.installGmailLocked(a.ID, r.googleOAuthConfigLocked(), g); err != nil {
		g.state = domain.ConnectionCredentialsUnavailable
		return Empty{}, errors.New("Network setting saved; account credentials require attention")
	}
	return Empty{}, nil
}
