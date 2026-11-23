package app

import (
	"context"
	"errors"

	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/provider/imap"
	"github.com/duanyongcheng/gomail/internal/provider/pop3"
	"github.com/duanyongcheng/gomail/internal/provider/smtp"
)

type imapSubmission struct {
	*imap.Provider
	sender *smtp.Transport
	before func(context.Context) error
}

func (p *imapSubmission) Capabilities() domain.Capabilities {
	c := p.Provider.Capabilities()
	c.Send = p.sender != nil
	c.FolderManagement = true
	return c
}
func (p *imapSubmission) Test(ctx context.Context) error {
	if err := p.Provider.Test(ctx); err != nil {
		return err
	}
	if p.sender != nil {
		return p.sender.Test(ctx)
	}
	return nil
}
func (p *imapSubmission) Send(ctx context.Context, m domain.OutgoingMail, barrier func() error) domain.SendResult {
	if p.sender == nil {
		return domain.SendResult{Message: "SMTP is not configured"}
	}
	if err := p.before(ctx); err != nil {
		return domain.SendResult{Message: "Account credentials changed"}
	}
	return p.sender.Send(ctx, m, barrier)
}
func (p *imapSubmission) CheckSent(ctx context.Context, email, id string) (string, error) {
	// An SMTP receipt has no mailbox ID. Absence is never evidence for a resend.
	return "", errors.New("SMTP sent checking is inconclusive; inspect the sent mailbox")
}

// RegisterStandardProviders constructs transports only; startup performs no I/O.
func (r *Runtime) RegisterStandardProviders() {
	r.RegisterProviderFactory("imap", func(pc ProviderContext) (domain.Transport, error) {
		p, err := imap.New(pc.DB, pc.Account.ID, pc.Config, pc.Dialer.Proxy)
		if err != nil {
			return nil, err
		}
		p.BeforeRequest = pc.BeforeRequest
		p.UpdateSelectedFolders = selectedFoldersUpdater(pc)
		var sender *smtp.Transport
		if pc.Config.SMTP.Host != "" && pc.Config.SMTP.AuthMode != "disabled" {
			e := pc.Config.SMTP
			sender = smtp.New(smtp.Config{BeforeRequest: pc.BeforeRequest, Endpoint: e.Endpoint(), Dialer: pc.Dialer, Username: e.Username, Password: e.Password, Email: pc.Account.Email, AuthMode: e.AuthMode})
		}
		return &imapSubmission{Provider: p, sender: sender, before: pc.BeforeRequest}, nil
	})
	r.RegisterProviderFactory("pop3", func(pc ProviderContext) (domain.Transport, error) {
		e := pc.Config.POP3
		c := pop3.Config{BeforeRequest: pc.BeforeRequest, Endpoint: e.Endpoint(), Dialer: pc.Dialer, Username: e.Username, Password: e.Password}
		if s := pc.Config.SMTP; s.Host != "" && s.AuthMode != "disabled" {
			c.Sender = smtp.New(smtp.Config{BeforeRequest: pc.BeforeRequest, Endpoint: s.Endpoint(), Dialer: pc.Dialer, Username: s.Username, Password: s.Password, Email: pc.Account.Email, AuthMode: s.AuthMode})
		}
		return pop3.New(pc.DB, pc.Account.ID, c), nil
	})
}

func selectedFoldersUpdater(pc ProviderContext) func(context.Context, string, string) error {
	return func(ctx context.Context, old, next string) error {
		if pc.UpdateConfig == nil {
			return errors.New("Selected folder persistence unavailable")
		}
		return pc.UpdateConfig(ctx, func(c *domain.ProtocolConfig) error {
			folders := make([]string, 0, len(c.SelectedFolders))
			for _, folder := range c.SelectedFolders {
				if folder == old {
					if next != "" {
						folders = append(folders, next)
					}
				} else {
					folders = append(folders, folder)
				}
			}
			if len(folders) == 0 {
				folders = []string{"INBOX"}
			}
			c.SelectedFolders = folders
			return nil
		})
	}
}
