package app

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"time"

	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/oauth"
	"github.com/duanyongcheng/gomail/internal/provider/graph"
	"github.com/duanyongcheng/gomail/internal/provider/imap"
	"github.com/duanyongcheng/gomail/internal/provider/smtp"
)

// RegisterMicrosoftProviders is called after standard providers. Restoring an
// account constructs only local token sources; no Token call occurs here.
func (r *Runtime) RegisterMicrosoftProviders() {
	r.mu.RLock()
	standardIMAP := r.providerFactories["imap"]
	r.mu.RUnlock()
	factory := func(pc ProviderContext) (domain.Transport, error) {
		if pc.Config.Microsoft == nil {
			if standardIMAP != nil {
				return standardIMAP(pc)
			}
			return nil, errors.New("Microsoft configuration required")
		}
		m := pc.Config.Microsoft
		policy := oauth.MicrosoftPolicy{ClientID: m.ClientID, Tenant: m.Tenant, Audience: m.Audience, Graph: m.Graph, IMAP: m.IMAP, SMTP: m.SMTP}
		client := &http.Client{Transport: pc.Dialer.HTTPTransport(), Timeout: 30 * time.Second}
		initial := oauth.MicrosoftCredentials{Generation: m.Generation, Grants: map[oauth.MicrosoftResource]oauth.MicrosoftGrant{}}
		for resource, g := range m.Grants {
			initial.Grants[oauth.MicrosoftResource(resource)] = oauth.MicrosoftGrant{AccessToken: g.AccessToken, RefreshToken: g.RefreshToken, TokenType: "Bearer", Expiry: time.Unix(g.Expiry, 0), Scopes: append([]string{}, g.Scopes...)}
		}
		tokens, err := oauth.NewMicrosoftTokens(policy, client, initial, func(ctx context.Context, old, next oauth.MicrosoftCredentials) error {
			if pc.UpdateConfig == nil {
				return errors.New("Microsoft credential persistence unavailable")
			}
			if pc.BeforeRequest != nil {
				if err := pc.BeforeRequest(ctx); err != nil {
					return err
				}
			}
			return pc.UpdateConfig(ctx, func(current *domain.ProtocolConfig) error {
				if current.Microsoft == nil || current.Microsoft.Subject != m.Subject || current.Microsoft.ClientID != m.ClientID {
					return errors.New("Microsoft account changed")
				}
				for resource, g := range next.Grants {
					if reflect.DeepEqual(old.Grants[resource], g) {
						continue
					}
					prior := old.Grants[resource]
					stored := current.Microsoft.Grants[string(resource)]
					if stored.AccessToken != prior.AccessToken || stored.RefreshToken != prior.RefreshToken {
						return errors.New("Microsoft resource grant changed")
					}
					current.Microsoft.Grants[string(resource)] = domain.ResourceGrant{AccessToken: g.AccessToken, RefreshToken: g.RefreshToken, Expiry: g.Expiry.Unix(), Scopes: append([]string{}, g.Scopes...)}
				}
				current.Microsoft.Generation++
				return nil
			})
		})
		if err != nil {
			return nil, err
		}
		token := func(resource oauth.MicrosoftResource, scopes ...string) func(context.Context) (string, error) {
			return func(ctx context.Context) (string, error) {
				if pc.BeforeRequest != nil {
					if err := pc.BeforeRequest(ctx); err != nil {
						return "", err
					}
				}
				return tokens.Token(ctx, resource, scopes...)
			}
		}
		if m.Graph {
			p := graph.New(pc.DB, graph.NewAPI(client, token(oauth.MicrosoftGraph, oauth.MicrosoftUserRead, oauth.MicrosoftMailReadWrite)), pc.Account.ID, pc.Account.Email)
			p.SendingAPI = graph.NewAPI(client, token(oauth.MicrosoftGraph, oauth.MicrosoftUserRead, oauth.MicrosoftMailSend))
			p.Subject = m.Subject
			p.BeforeRequest = pc.BeforeRequest
			return p, nil
		}
		if !m.IMAP {
			return nil, errors.New("Enable Graph or IMAP to read this Microsoft mailbox")
		}
		p, err := imap.New(pc.DB, pc.Account.ID, pc.Config, pc.Dialer.Proxy)
		if err != nil {
			return nil, err
		}
		p.BeforeRequest = pc.BeforeRequest
		p.UpdateSelectedFolders = selectedFoldersUpdater(pc)
		p.Token = token(oauth.MicrosoftExchange, oauth.MicrosoftIMAP)
		var sender *smtp.Transport
		if m.SMTP && pc.Config.SMTP.AuthMode != "disabled" {
			e := pc.Config.SMTP
			sender = smtp.New(smtp.Config{Endpoint: e.Endpoint(), Dialer: pc.Dialer, Username: e.Username, Email: pc.Account.Email, AuthMode: "xoauth2", Token: token(oauth.MicrosoftExchange, oauth.MicrosoftSMTP), BeforeRequest: pc.BeforeRequest})
		}
		return &imapSubmission{Provider: p, sender: sender, before: pc.BeforeRequest}, nil
	}
	r.RegisterProviderFactory("outlook", factory)
	r.RegisterProviderFactory("imap", factory)
}
