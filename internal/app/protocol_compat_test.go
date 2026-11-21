package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/mailnet"
	"golang.org/x/oauth2"
)

func TestProtocolCompatibilityNetworkFoldersAndSafeStatus(t *testing.T) {
	r, s, created := protocolAccountFixture(t)
	a, err := s.Add(t.Context(), protocolRequest())
	if err != nil {
		t.Fatal(err)
	}
	old := (*created)[0]
	setting := mailnet.ProxySetting{Mode: "custom", Proxy: &mailnet.Proxy{Scheme: "socks5", Host: "localhost", Port: 1080}}
	if _, err := s.SetAccountNetwork(t.Context(), AccountNetworkRequest{AccountID: a.ID, Setting: setting}); err != nil {
		t.Fatal(err)
	}
	got, err := s.AccountNetwork(t.Context(), AccountRequest{AccountID: a.ID})
	if err != nil || got.Proxy == nil || *got.Proxy != *setting.Proxy {
		t.Fatal(got, err)
	}
	if !old.closed || old.context.BeforeRequest(t.Context()) == nil {
		t.Fatal("route replacement retained prior generation")
	}
	result, err := s.SetSyncFolders(t.Context(), IMAPSyncFoldersRequest{AccountID: a.ID, SelectedRemoteIDs: []string{"INBOX", "Archive"}})
	if err != nil || len(result.SelectedRemoteIDs) != 2 {
		t.Fatal(result, err)
	}
	status, err := s.XOAuth2Status(t.Context(), AccountRequest{AccountID: a.ID})
	if err != nil || status.IMAPUsesToken || status.HasRefreshToken || status.ClientID != nil {
		t.Fatal(status, err)
	}
	config, _, _ := r.loadProtocol(t.Context(), a.ID)
	if config.IMAP.Password != protocolRequest().Password {
		t.Fatal("compatibility edit lost password")
	}
	// The IMAP provider defaults an empty selection to INBOX; never advertise
	// an empty selection while it actually reads Inbox.
	result, err = s.SetSyncFolders(t.Context(), IMAPSyncFoldersRequest{AccountID: a.ID, SelectedRemoteIDs: nil})
	if err != nil || len(result.SelectedRemoteIDs) != 1 || result.SelectedRemoteIDs[0] != "INBOX" {
		t.Fatal(result, err)
	}
}

func TestProtocolSavedAndEditedConnectionTests(t *testing.T) {
	_, s, created := protocolAccountFixture(t)
	a, err := s.Add(t.Context(), protocolRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.TestStored(t.Context(), AccountRequest{AccountID: a.ID}); err != nil {
		t.Fatal(err)
	}
	if len(*created) != 1 || (*created)[0].calls != 1 || (*created)[0].context.UpdateConfig == nil {
		t.Fatal("saved test constructed a transient provider without refresh persistence")
	}
	settings, _ := s.ProtocolSettings(t.Context(), AccountRequest{AccountID: a.ID})
	config := (*created)[0].context.Config
	config.IMAP.Password = ""
	request := SaveProtocolRequest{AccountID: a.ID, Revision: settings.Revision, Config: config}
	if _, err := s.TestProtocolSettings(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	tested := (*created)[1]
	if tested.context.Config.IMAP.Password != protocolRequest().Password || !tested.closed {
		t.Fatal("edited test lost saved password or leaked connection")
	}
	request.Revision = "stale"
	if _, err := s.TestProtocolSettings(t.Context(), request); err == nil {
		t.Fatal("stale test accepted")
	}
}

func TestGoogleLoginPinsAndPersistsExplicitDirectRoute(t *testing.T) {
	r, _, _ := accessFixture(t)
	r.globalProxy = &mailnet.Proxy{Scheme: "http", Host: "127.0.0.1", Port: 1}
	s := &AccountService{Runtime: r}
	setting := mailnet.ProxySetting{Mode: "disabled"}
	a, err := s.Login(t.Context(), LoginRequest{Provider: "gmail", Proxy: &setting})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.AccountNetwork(t.Context(), AccountRequest{AccountID: a.ID})
	if err != nil || saved.Mode != "disabled" {
		t.Fatal(saved, err)
	}
	encrypted, _ := r.DB.AuthData(t.Context(), a.ID)
	if _, err := s.Login(t.Context(), LoginRequest{Provider: "gmail", Email: a.Email, Proxy: &mailnet.ProxySetting{Mode: "inherit"}}); err == nil {
		t.Fatal("changed explicit route ignored on existing mailbox")
	}
	after, _ := r.DB.AuthData(t.Context(), a.ID)
	if !bytes.Equal(after, encrypted) {
		t.Fatal("rejected route changed credentials")
	}
	client, err := r.accountNetworkClient(t.Context(), a.ID)
	if err != nil || client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal(err)
	}
}

func TestGoogleAuthorizationUsesExplicitProxyAndNeverFallsBack(t *testing.T) {
	r, mock, _ := accessFixture(t)
	called := false
	r.Authorize = func(ctx context.Context, _ *oauth2.Config, client *http.Client, _ func(string) error) (*oauth2.Token, error) {
		called = true
		response, err := client.Get(mock.Server.URL)
		if err == nil {
			response.Body.Close()
			t.Fatal("broken explicit proxy fell back to direct in authorization client")
		}
		return nil, errors.New("synthetic proxy refused")
	}
	_, err := (&AccountService{Runtime: r}).Login(t.Context(), LoginRequest{Provider: "gmail", Proxy: &mailnet.ProxySetting{Mode: "custom", Proxy: &mailnet.Proxy{Scheme: "http", Host: "127.0.0.1", Port: 1}}})
	if err == nil || !called {
		t.Fatal("authorization did not use routed client", err)
	}
}

func TestGlobalProxyEditPreservesProtocolAuthenticationPause(t *testing.T) {
	r, s, _ := protocolAccountFixture(t)
	a, err := s.Add(t.Context(), protocolRequest())
	if err != nil {
		t.Fatal(err)
	}
	r.connections[a.ID].state = domain.ConnectionReauthRequired
	if _, err := s.SetNetwork(t.Context(), NetworkSettings{}); err != nil {
		t.Fatal(err)
	}
	if r.connections[a.ID].state != domain.ConnectionReauthRequired {
		t.Fatal("network edit cleared auth pause")
	}
}
