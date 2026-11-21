package graph

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/mailmime"
	"github.com/emersion/go-message/mail"
)

func (p *Provider) Test(ctx context.Context) error {
	p.gate.Lock()
	defer p.gate.Unlock()
	session, err := p.API.Session(ctx)
	if err != nil {
		return err
	}
	return p.verifySender(ctx, session, p.Email)
}

func (p *Provider) verifySender(ctx context.Context, api *API, email string) error {
	profile, err := api.Profile(ctx)
	if err != nil {
		var access *domain.AccessError
		if errors.As(err, &access) {
			return access
		}
		var remote *HTTPError
		if errors.As(err, &remote) {
			return remote
		}
		return errors.New("cannot verify Microsoft sender")
	}
	if !strings.EqualFold(profile.Email, email) || !strings.EqualFold(p.Email, email) {
		return errors.New("Microsoft mailbox does not match queued sender")
	}
	if p.Subject != "" && p.Subject != profile.ID {
		return errors.New("Microsoft stable identity changed")
	}
	account, err := p.DB.Account(ctx, p.AccountID)
	if err != nil {
		return errors.New("Microsoft account is unavailable")
	}
	if account.OAuthSubject != nil && *account.OAuthSubject != "" && *account.OAuthSubject != profile.ID {
		return errors.New("Microsoft stable identity changed")
	}
	return nil
}

func (p *Provider) Send(ctx context.Context, message domain.OutgoingMail, beforeSend func() error) domain.SendResult {
	p.gate.Lock()
	defer p.gate.Unlock()
	api := p.SendingAPI
	if api == nil {
		api = p.API
	}
	session, err := api.Session(ctx)
	if err != nil {
		return sendAccessFailure(err)
	}
	if err := p.verifySender(ctx, session, message.Email); err != nil {
		return sendAccessFailure(err)
	}
	if len(message.Raw) > MaxSendRawBytes {
		return domain.SendResult{Message: "Graph MIME exceeds wire limit"}
	}
	r, err := mail.CreateReader(bytes.NewReader(message.Raw))
	if err != nil {
		return domain.SendResult{Message: "invalid outgoing MIME"}
	}
	from, err := r.Header.AddressList("From")
	r.Close()
	if err != nil || len(from) != 1 || !strings.EqualFold(from[0].Address, message.Email) {
		return domain.SendResult{Message: "outgoing sender does not match Microsoft account"}
	}
	if beforeSend == nil {
		return domain.SendResult{Message: "Graph send requires durable barrier"}
	}
	if p.BeforeRequest != nil {
		if err := p.BeforeRequest(ctx); err != nil {
			return domain.SendResult{Message: "Microsoft account changed before submission"}
		}
	}
	if err := beforeSend(); err != nil {
		return domain.SendResult{Retryable: true, Message: "could not persist send start; no message submitted"}
	}
	accepted, unknown, retryable, err := session.Send(ctx, message.Raw)
	result := domain.SendResult{Accepted: accepted, Unknown: unknown, Retryable: retryable}
	var remote *HTTPError
	if !unknown && errors.As(err, &remote) {
		if remote.Status == 401 {
			result.AccessState = domain.ConnectionReauthRequired
			result.Retryable = false
		}
		if remote.Status == 403 {
			result.AccessState = domain.ConnectionPermissionRequired
			result.Retryable = false
		}
	}
	if err != nil {
		result.Message = err.Error()
	}
	return result
}

func sendAccessFailure(err error) domain.SendResult {
	result := domain.SendResult{Message: "Microsoft send authorization unavailable"}
	var access *domain.AccessError
	if errors.As(err, &access) {
		result.AccessState = access.State
		result.Retryable = !access.State.Blocked()
	}
	var remote *HTTPError
	if errors.As(err, &remote) {
		if remote.Status == 401 {
			result.AccessState = domain.ConnectionReauthRequired
		}
		if remote.Status == 403 {
			result.AccessState = domain.ConnectionPermissionRequired
		}
	}
	return result
}

// CheckSent never resubmits mail. A single metadata candidate is hydrated and
// compared with the immutable outgoing Message-ID and verified mailbox.
func (p *Provider) CheckSent(ctx context.Context, email, messageID string) (string, error) {
	p.gate.Lock()
	defer p.gate.Unlock()
	session, err := p.API.Session(ctx)
	if err != nil {
		return "", err
	}
	if err := p.verifySender(ctx, session, email); err != nil {
		return "", err
	}
	if messageID == "" || len(messageID) > 998 || strings.ContainsAny(messageID, "\r\n\x00") {
		return "", errors.New("invalid sent-check identity")
	}
	messages, err := session.FindSent(ctx, messageID)
	if err != nil {
		return "", err
	}
	if len(messages) != 1 || messages[0].ID == "" {
		return "", errors.New("sent check is inconclusive; no resend attempted")
	}
	raw, err := session.Raw(ctx, messages[0].ID)
	if err != nil {
		return "", err
	}
	parsed, err := mailmime.ParseWithLimit(raw, wireLimit)
	if err != nil || parsed.MessageID != messageID || !strings.EqualFold(parsed.From.Address, email) {
		return "", errors.New("sent check identity did not match")
	}
	return messages[0].ID, nil
}
