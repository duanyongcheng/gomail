package app

import (
	"bytes"
	"context"
	"testing"

	"github.com/duanyongcheng/gomail/internal/domain"
)

type acceptedTransport struct {
	calls   int
	message domain.OutgoingMail
}

func (p *acceptedTransport) Capabilities() domain.Capabilities {
	return domain.Capabilities{Read: true, Send: true}
}
func (p *acceptedTransport) Sync(context.Context) (domain.SyncResult, error) {
	return domain.SyncResult{}, nil
}
func (p *acceptedTransport) Flags(context.Context, string, *bool, *bool) error { return nil }
func (p *acceptedTransport) CheckSent(context.Context, string, string) (string, error) {
	return "", nil
}
func (p *acceptedTransport) Send(_ context.Context, m domain.OutgoingMail, barrier func() error) domain.SendResult {
	if barrier() != nil {
		return domain.SendResult{Retryable: true}
	}
	p.calls++
	p.message = m
	return domain.SendResult{Accepted: true}
}

func TestOutboxSMTPFrozenEnvelopeAndAcceptedWithoutRemoteID(t *testing.T) {
	compose, _ := composeFixture(t)
	r := compose.Runtime
	ctx := t.Context()
	injectPendingSQL(t, r.DB, `UPDATE accounts SET provider='imap' WHERE id='a'`)
	provider := &acceptedTransport{}
	r.InstallProvider("a", provider)
	request := sendRequest()
	service := &OutboxService{Runtime: r}
	if _, err := service.Queue(ctx, request); err != nil {
		t.Fatal(err)
	}
	rows, err := r.DB.OutboxList(ctx, "a")
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	record, err := r.DB.Outbox(ctx, "a", rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := r.decodeOutbox(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.EnvelopeRecipients) != 3 || payload.EnvelopeRecipients[2] != "hidden@example.invalid" || bytes.Contains(payload.Raw, []byte("hidden@example.invalid")) {
		t.Fatal("SMTP Bcc policy was not frozen")
	}
	injectPendingSQL(t, r.DB, `CREATE TRIGGER fail_accepted_settle BEFORE UPDATE OF status ON outbox WHEN NEW.status='sent' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	for range 2 {
		if _, err := r.processOutbox(ctx); err == nil {
			t.Fatal("settlement fault ignored")
		}
	}
	if provider.calls != 1 {
		t.Fatal("submission repeated", provider.calls)
	}
	injectPendingSQL(t, r.DB, `DROP TRIGGER fail_accepted_settle`)
	for range 2 {
		if _, err := r.processOutbox(ctx); err != nil {
			t.Fatal(err)
		}
	}
	row, err := r.DB.Outbox(ctx, "a", rows[0].ID)
	if err != nil || row.Status != "sent" || provider.calls != 1 {
		t.Fatal(row.Status, provider.calls, err)
	}
	if len(provider.message.EnvelopeRecipients) != 3 {
		t.Fatal("envelope lost after encryption")
	}
}
