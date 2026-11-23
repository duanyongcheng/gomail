package app

import (
	"context"
	"errors"
	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/oauth"
	"github.com/duanyongcheng/gomail/internal/provider/graph"
	"testing"
)

func TestMicrosoftActualFactoryLazyIdentityAndIndependentProtocolTokens(t *testing.T) {
	r, _, _ := protocolAccountFixture(t)
	r.RegisterStandardProviders()
	r.RegisterMicrosoftProviders()
	policy := &domain.MicrosoftConfig{ClientID: "11111111-1111-1111-1111-111111111111", Tenant: "common", Audience: "all", Subject: "verified-subject", Graph: true, Grants: map[string]domain.ResourceGrant{}}
	requests := 0
	pc := ProviderContext{Account: domain.Account{ID: "synthetic-ms", Email: "sender@example.test", Provider: "outlook"}, DB: r.DB, Config: domain.ProtocolConfig{Version: 1, Microsoft: policy}, BeforeRequest: func(context.Context) error { requests++; return errors.New("fixture stops before network") }, UpdateConfig: func(context.Context, func(*domain.ProtocolConfig) error) error {
		t.Fatal("lazy restore persisted credentials")
		return nil
	}}
	p, err := r.providerFactories["outlook"](pc)
	if err != nil {
		t.Fatal(err)
	}
	g, ok := p.(*graph.Provider)
	if !ok || g.Subject != "verified-subject" || g.BeforeRequest == nil || g.SendingAPI == nil || requests != 0 {
		t.Fatalf("factory lost identity or was eager: %T %+v %d", p, g, requests)
	}
	result := g.Send(t.Context(), domain.OutgoingMail{Email: pc.Account.Email}, func() error { t.Fatal("authorization failure reached barrier"); return nil })
	if result.Accepted || result.Unknown || result.Retryable || requests != 1 {
		t.Fatalf("terminal send auth must not retry or use network %+v requests=%d", result, requests)
	}
	copy := *policy
	copy.Graph = false
	copy.IMAP = true
	copy.SMTP = true
	pc.Config.Microsoft = &copy
	pc.Config.IMAP = domain.ProtocolEndpoint{Host: "imap.example.test", Port: 993, Security: "tls", Username: pc.Account.Email, AuthMode: "xoauth2"}
	pc.Config.SMTP = domain.ProtocolEndpoint{Host: "smtp.example.test", Port: 587, Security: "starttls", Username: pc.Account.Email, AuthMode: "xoauth2"}
	transport, err := r.providerFactories["outlook"](pc)
	if err != nil {
		t.Fatal(err)
	}
	i, ok := transport.(*imapSubmission)
	if !ok || i.Token == nil || i.sender == nil || requests != 1 {
		t.Fatalf("IMAP/SMTP lazy policies missing: %T %d", transport, requests)
	}
	if _, err := i.Token(t.Context()); err == nil {
		t.Fatal("missing exchange grant accepted")
	}
	if policy.Grants[string(oauth.MicrosoftExchange)].AccessToken != "" {
		t.Fatal("protocol exchanged Graph grant")
	}
}
