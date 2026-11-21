package app

import (
	"bytes"
	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/mailnet"
	"github.com/duanyongcheng/pebble-go/internal/oauth"
	"golang.org/x/oauth2"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGmailAccountNetworkRebuildPreservesCredentialsAndRestores(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "test-client")
	r, s, _ := protocolAccountFixture(t)
	cipher, err := r.composeCrypto(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := oauth.EncodeToken(cipher, "a", "test-client", (&oauth2.Token{AccessToken: "token", RefreshToken: "refresh"}).WithExtra(map[string]any{"scope": "https://www.googleapis.com/auth/gmail.modify"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.DB.SaveAccount(t.Context(), domain.Account{ID: "a", Email: "a@example.invalid", Provider: "gmail"}, encrypted); err != nil {
		t.Fatal(err)
	}
	r.globalProxy = &mailnet.Proxy{Scheme: "http", Host: "localhost", Port: 1}
	if err := r.Restore(); err != nil {
		t.Fatal(err)
	}
	old := r.connections["a"]
	if _, err := s.SetAccountNetwork(t.Context(), AccountNetworkRequest{AccountID: "a", Setting: mailnet.ProxySetting{Mode: "disabled"}}); err != nil {
		t.Fatal(err)
	}
	if r.connections["a"] == old {
		t.Fatal("network change retained old generation")
	}
	current, _ := r.DB.AuthData(t.Context(), "a")
	if !bytes.Equal(current, encrypted) {
		t.Fatal("network settings changed credentials")
	}
	if err := r.Restore(); err != nil {
		t.Fatal(err)
	}
	got, err := s.AccountNetwork(t.Context(), AccountRequest{AccountID: "a"})
	if err != nil || got.Mode != "disabled" {
		t.Fatal(got, err)
	}
	client, err := r.accountNetworkClient(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("environment proxy can override explicit route")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal("explicit direct was not direct", err)
	}
	response.Body.Close()
	if _, err := s.SetAccountNetwork(t.Context(), AccountNetworkRequest{AccountID: "a", Setting: mailnet.ProxySetting{Mode: "custom", Proxy: &mailnet.Proxy{Scheme: "http", Host: "127.0.0.1", Port: 1}}}); err != nil {
		t.Fatal(err)
	}
	proxied, err := r.accountNetworkClient(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if response, err := proxied.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("failed account proxy fell back to direct")
	}
	r.authFlowMu.Lock()
	_, err = s.SetAccountNetwork(t.Context(), AccountNetworkRequest{AccountID: "a", Setting: mailnet.ProxySetting{Mode: "inherit"}})
	r.authFlowMu.Unlock()
	if err == nil {
		t.Fatal("route changed during OAuth")
	}
}
