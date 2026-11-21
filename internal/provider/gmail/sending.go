package gmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/googleapi"
	"github.com/emersion/go-message/mail"
)

func (p *Provider) sendSession(ctx context.Context, expectedEmail string) (*googleapi.API, error) {
	session, err := p.API.SendingSession(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := session.Profile(ctx)
	if err != nil {
		return nil, errors.New("Cannot verify Gmail sender; check connection and authorization.")
	}
	if !strings.EqualFold(strings.TrimSpace(profile.Email), expectedEmail) {
		return nil, errSenderMismatch
	}
	return session, nil
}

var errSenderMismatch = errors.New("Gmail mailbox does not match the queued sender; sign in to the correct account.")

func (p *Provider) Send(ctx context.Context, message domain.OutgoingMail, beforeSend func() error) domain.SendResult {
	email, raw := message.Email, message.Raw
	p.gate.Lock()
	defer p.gate.Unlock()
	session, err := p.sendSession(ctx, email)
	if err != nil {
		return domain.SendResult{Retryable: !errors.Is(err, errSenderMismatch), Message: err.Error()}
	}
	// Reject even a corrupted/internal caller whose MIME From differs from the
	// identity checked with the pinned token. Display name is a local snapshot.
	reader, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return domain.SendResult{Message: "Invalid outgoing MIME."}
	}
	from, err := reader.Header.AddressList("From")
	reader.Close()
	if err != nil || len(from) != 1 || !strings.EqualFold(from[0].Address, email) {
		return domain.SendResult{Message: "Outgoing sender does not match the account."}
	}
	if err := beforeSend(); err != nil {
		return domain.SendResult{Retryable: true, Message: "Could not persist send start; no message was submitted."}
	}
	receipt := session.SendMessage(ctx, raw, message.ThreadID)
	return domain.SendResult{RemoteID: receipt.RemoteID, Unknown: receipt.Unknown, Retryable: receipt.Retryable, Message: receipt.Message}
}

// CheckSent is read-only and requires a unique matching Message-ID and From.
// Search absence is never evidence that retrying an unknown send is safe.
func (p *Provider) CheckSent(ctx context.Context, email, messageID string) (string, error) {
	p.gate.Lock()
	defer p.gate.Unlock()
	session, err := p.sendSession(ctx, email)
	if err != nil {
		return "", err
	}
	page, err := session.FindSent(ctx, messageID)
	if err != nil {
		return "", err
	}
	if page.Next != "" || len(page.Messages) != 1 {
		return "", errors.New("Sent check is inconclusive; no resend was attempted.")
	}
	message, err := session.Message(ctx, page.Messages[0].ID)
	if err != nil {
		return "", err
	}
	sent := false
	for _, label := range message.Labels {
		if label == "SENT" {
			sent = true
		}
	}
	raw, err := base64.RawURLEncoding.DecodeString(message.Raw)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(message.Raw)
	}
	if err != nil || !sent {
		return "", errors.New("Sent check is inconclusive.")
	}
	reader, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return "", errors.New("Sent check is inconclusive.")
	}
	defer reader.Close()
	id, err := reader.Header.MessageID()
	if err != nil || id != messageID {
		return "", errors.New("Sent message identity did not match.")
	}
	from, err := reader.Header.AddressList("From")
	if err != nil || len(from) != 1 || !strings.EqualFold(from[0].Address, email) {
		return "", errors.New("Sent sender did not match.")
	}
	return message.ID, nil
}
