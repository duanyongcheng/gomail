package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/bary/pebble-go/internal/crypto"
	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/mailnet"
	"github.com/bary/pebble-go/internal/store"
)

type protocolFixture struct {
	context ProviderContext
	calls   int
	closed  bool
	fail    bool
}

func (p *protocolFixture) Capabilities() domain.Capabilities { return domain.Capabilities{Read: true} }
func (p *protocolFixture) Sync(ctx context.Context) (domain.SyncResult, error) {
	p.calls++
	return domain.SyncResult{}, p.context.BeforeRequest(ctx)
}
func (p *protocolFixture) Flags(context.Context, string, *bool, *bool) error { return nil }
func (p *protocolFixture) Close() error                                      { p.closed = true; return nil }
func (p *protocolFixture) Test(ctx context.Context) error {
	p.calls++
	if p.fail {
		return errors.New("secret credential details must be hidden")
	}
	return p.context.BeforeRequest(ctx)
}

func protocolAccountFixture(t *testing.T) (*Runtime, *AccountService, *[]*protocolFixture) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	r := New(t.Context(), db, &testKeyring{key: bytes.Repeat([]byte{3}, 32)}, nil)
	created := []*protocolFixture{}
	r.RegisterProviderFactory("imap", func(pc ProviderContext) (domain.Transport, error) {
		p := &protocolFixture{context: pc}
		created = append(created, p)
		return p, nil
	})
	return r, &AccountService{Runtime: r}, &created
}
func protocolRequest() AddAccountRequest {
	return AddAccountRequest{Email: "user@example.invalid", DisplayName: "User", Provider: "imap", ImapHost: "imap.example.invalid", ImapPort: 993, ImapSecurity: "tls", Username: "user@example.invalid", Password: "secret-value", SelectedFolders: []string{"INBOX"}}
}

func TestProtocolAccountEncryptedCreateEditLazyRestoreAndGenerationFence(t *testing.T) {
	r, s, created := protocolAccountFixture(t)
	ctx := t.Context()
	request := protocolRequest()
	a, err := s.Add(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := r.DB.AuthData(ctx, a.ID)
	if bytes.Contains(original, []byte(request.Password)) {
		t.Fatal("plaintext credentials persisted")
	}
	if _, err := s.Add(ctx, request); err == nil {
		t.Fatal("duplicate account replaced")
	}
	settings, err := s.ProtocolSettings(ctx, AccountRequest{AccountID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	safe, _ := json.Marshal(settings)
	if bytes.Contains(safe, []byte(request.Password)) || !settings.IMAP.HasPassword {
		t.Fatal("unsafe settings DTO", string(safe))
	}
	old := (*created)[0]
	config := old.context.Config
	config.SelectedFolders = []string{"INBOX", "Archive"}
	config.IMAP.Password = ""
	updated, err := s.SaveProtocol(ctx, SaveProtocolRequest{AccountID: a.ID, Revision: settings.Revision, DisplayName: "Renamed", Config: config})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName != "Renamed" || !old.closed {
		t.Fatal(updated, old.closed)
	}
	if err := old.context.BeforeRequest(ctx); err == nil {
		t.Fatal("old provider generation remained active")
	}
	if _, err := s.SaveProtocol(ctx, SaveProtocolRequest{AccountID: a.ID, Revision: settings.Revision, Config: config}); err == nil {
		t.Fatal("stale edit overwrote settings")
	}
	next := (*created)[len(*created)-1]
	if next.context.Config.IMAP.Password != request.Password {
		t.Fatal("blank edit erased saved password")
	}
	if err := old.context.SaveConfig(ctx, old.context.Config); err == nil {
		t.Fatal("stale refresh replaced new credentials")
	}
	// Reconstruct with the same key; factory receives config but startup never calls Test/Sync.
	restored := New(ctx, r.DB, &testKeyring{key: bytes.Repeat([]byte{3}, 32)}, nil)
	var restoredProvider *protocolFixture
	restored.RegisterProviderFactory("imap", func(pc ProviderContext) (domain.Transport, error) {
		restoredProvider = &protocolFixture{context: pc}
		return restoredProvider, nil
	})
	if err := restored.Restore(); err != nil {
		t.Fatal(err)
	}
	if restoredProvider == nil || restoredProvider.calls != 0 || restoredProvider.context.Config.IMAP.Password != request.Password {
		t.Fatal("restore did not stay lazy")
	}
	if err := restored.Scheduler.Preference("manual"); err != nil {
		t.Fatal(err)
	}
	restored.Scheduler.Close()
}

func TestProtocolRestoreWrongKeyPreservesCiphertextAndHealthyAccount(t *testing.T) {
	r, s, _ := protocolAccountFixture(t)
	a, err := s.Add(t.Context(), protocolRequest())
	if err != nil {
		t.Fatal(err)
	}
	encrypted, _ := r.DB.AuthData(t.Context(), a.ID)
	bad := New(t.Context(), r.DB, &testKeyring{key: bytes.Repeat([]byte{8}, 32)}, nil)
	bad.RegisterProviderFactory("imap", func(pc ProviderContext) (domain.Transport, error) {
		t.Fatal("wrong key reached provider factory")
		return nil, nil
	})
	if err := bad.Restore(); err == nil {
		t.Fatal("wrong key accepted")
	}
	after, _ := r.DB.AuthData(t.Context(), a.ID)
	if !bytes.Equal(encrypted, after) {
		t.Fatal("wrong-key restore mutated vault")
	}
	corrupt := domain.Account{ID: "broken", Email: "broken@example.invalid", Provider: "imap"}
	if err := r.DB.CreateProtocolAccount(t.Context(), corrupt, []byte("invalid")); err != nil {
		t.Fatal(err)
	}
	restored := New(t.Context(), r.DB, &testKeyring{key: bytes.Repeat([]byte{3}, 32)}, nil)
	count := 0
	restored.RegisterProviderFactory("imap", func(pc ProviderContext) (domain.Transport, error) { count++; return &protocolFixture{context: pc}, nil })
	if err := restored.Restore(); err == nil {
		t.Fatal("corrupt account not reported")
	}
	if count != 1 {
		t.Fatal("healthy account not restored", count)
	}
}

func TestProtocolExplicitTestCancellationTrustAndProxy(t *testing.T) {
	r, s, created := protocolAccountFixture(t)
	request := protocolRequest()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Test(ctx, request); err == nil {
		t.Fatal("cancelled test succeeded")
	}
	request.AcceptInvalidCerts = true
	if _, err := s.Test(t.Context(), request); err == nil {
		t.Fatal("certificate bypass accepted")
	}
	request.AcceptInvalidCerts = false
	r.globalProxy = &mailnet.Proxy{Scheme: "socks5", Host: "proxy.example.invalid", Port: 1080}
	c, _ := request.configuration()
	c.Proxy.Mode = "disabled"
	request.Config = &c
	if _, err := s.Test(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	p := (*created)[len(*created)-1]
	if p.context.Dialer.Proxy != nil || !p.closed {
		t.Fatal("direct policy or connection cleanup missing")
	}
	accounts, _ := r.DB.Accounts(t.Context())
	if len(accounts) != 0 {
		t.Fatal("test persisted account")
	}
	c.Proxy = mailnet.ProxySetting{Mode: "custom", Proxy: &mailnet.Proxy{Scheme: "http", Host: "localhost", Port: 8080}}
	if _, err := s.Test(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	p = (*created)[len(*created)-1]
	if p.context.Dialer.Proxy == nil || p.context.Dialer.Proxy.Host != "localhost" {
		t.Fatal("custom proxy missing")
	}
}

func TestProtocolRefreshCASMustPersistBeforePublication(t *testing.T) {
	r, s, created := protocolAccountFixture(t)
	a, err := s.Add(t.Context(), protocolRequest())
	if err != nil {
		t.Fatal(err)
	}
	p := (*created)[0]
	old, _ := r.DB.AuthData(t.Context(), a.ID)
	next := p.context.Config
	next.IMAP.Password = "rotated"
	if err := p.context.SaveConfig(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	data, _ := r.DB.AuthData(t.Context(), a.ID)
	if bytes.Equal(old, data) {
		t.Fatal("refresh not persisted")
	}
	cipher, _ := crypto.FromKey(bytes.Repeat([]byte{3}, 32))
	plain, err := cipher.Decrypt("accounts.auth_data", a.ID, data)
	if err != nil || !bytes.Contains(plain, []byte("rotated")) {
		t.Fatal(err)
	}
	if err := r.DB.DeleteAccount(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.context.SaveConfig(t.Context(), next); err == nil {
		t.Fatal("deleted account accepted refresh")
	}
}

func TestProtocolConfigMutatorsMergeConcurrentResourceChanges(t *testing.T) {
	_, s, created := protocolAccountFixture(t)
	a, err := s.Add(t.Context(), protocolRequest())
	if err != nil {
		t.Fatal(err)
	}
	p := (*created)[0]
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, folder := range []string{"Archive", "Sent"} {
		wg.Add(1)
		go func(folder string) {
			defer wg.Done()
			errs <- p.context.UpdateConfig(t.Context(), func(c *domain.ProtocolConfig) error {
				c.SelectedFolders = append(c.SelectedFolders, folder)
				return nil
			})
		}(folder)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	settings, err := s.ProtocolSettings(t.Context(), AccountRequest{AccountID: a.ID})
	if err != nil || len(settings.SelectedFolders) != 3 {
		t.Fatal(settings, err)
	}
}

func TestProtocolAddRejectsUnverifiedMicrosoftGrant(t *testing.T) {
	_, s, _ := protocolAccountFixture(t)
	request := protocolRequest()
	c, _ := request.configuration()
	c.Microsoft = &domain.MicrosoftConfig{Subject: "unverified"}
	request.Config = &c
	if _, err := s.Add(t.Context(), request); err == nil {
		t.Fatal("unverified grant accepted")
	}
}

func TestGlobalNetworkSettingsPersistAndRebuildLazyProvider(t *testing.T) {
	r, s, created := protocolAccountFixture(t)
	a, err := s.Add(t.Context(), protocolRequest())
	if err != nil {
		t.Fatal(err)
	}
	old := (*created)[0]
	setting := NetworkSettings{Proxy: &mailnet.Proxy{Scheme: "http", Host: "localhost", Port: 8081}}
	if _, err := s.SetNetwork(t.Context(), setting); err != nil {
		t.Fatal(err)
	}
	latest := (*created)[len(*created)-1]
	if latest.context.Dialer.Proxy == nil || latest.context.Dialer.Proxy.Port != 8081 || latest.calls != 0 || !old.closed {
		t.Fatal("proxy replacement did not rebuild lazily")
	}
	if err := old.context.BeforeRequest(t.Context()); err == nil {
		t.Fatal("old network generation remained active")
	}
	stored, err := s.Network(t.Context(), Empty{})
	if err != nil || stored.Proxy == nil || stored.Proxy.Host != "localhost" {
		t.Fatal(stored, err)
	}
	restored := New(t.Context(), r.DB, &testKeyring{key: bytes.Repeat([]byte{3}, 32)}, nil)
	var route *mailnet.Proxy
	restored.RegisterProviderFactory("imap", func(pc ProviderContext) (domain.Transport, error) {
		route = pc.Dialer.Proxy
		return &protocolFixture{context: pc}, nil
	})
	if err := restored.Restore(); err != nil {
		t.Fatal(err)
	}
	if route == nil || route.Port != 8081 {
		t.Fatal("network setting was not restored")
	}
	if _, err := restored.provider(a.ID); err != nil {
		t.Fatal(err)
	}
}
