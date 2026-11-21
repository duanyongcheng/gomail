package gmail_test

import (
	"context"
	"errors"
	"net/mail"
	"path/filepath"
	"testing"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/mailmime"
	"github.com/duanyongcheng/pebble-go/internal/store"
)

func TestSenderVerificationAndUnknownReconciliation(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, mock, _ := fixture(t, db, "a")
	raw, err := mailmime.BuildOutgoing(mailmime.Outgoing{From: mail.Address{Name: "Configured Name", Address: "a@example.invalid"}, To: []mail.Address{{Address: "to@example.invalid"}}, BCC: []mail.Address{{Address: "hidden@example.invalid"}}, IncludeBcc: true, Text: "body", MessageID: "stable@example.invalid", Date: time.Now()}, mailmime.MaxOutgoingBytes)
	if err != nil {
		t.Fatal(err)
	}
	countSent := func() int { mock.Lock(); defer mock.Unlock(); return len(mock.SentRaw) }
	starts := 0
	outgoing := domain.OutgoingMail{Email: "a@example.invalid", Raw: raw}
	result := p.Send(t.Context(), outgoing, func() error { starts++; return nil })
	if result.RemoteID != "" || result.Unknown || result.Retryable || starts != 0 || countSent() != 0 {
		t.Fatal("identity mismatch submitted", result, starts)
	}
	mock.Lock()
	mock.ProfileEmail = "a@example.invalid"
	mock.Unlock()
	result = p.Send(t.Context(), outgoing, func() error { return errors.New("DB failure") })
	if result.Unknown || !result.Retryable || countSent() != 0 {
		t.Fatal(result)
	}
	mock.Lock()
	mock.LoseSendResponse = true
	mock.Unlock()
	result = p.Send(t.Context(), outgoing, func() error { starts++; return nil })
	if !result.Unknown || result.Retryable || starts != 1 || countSent() != 1 {
		t.Fatal(result, starts)
	}
	remote, err := p.CheckSent(t.Context(), "a@example.invalid", "stable@example.invalid")
	if err != nil || remote != "sent-1" || countSent() != 1 {
		t.Fatal(remote, err)
	}
	if _, err := p.CheckSent(t.Context(), "a@example.invalid", "absent@example.invalid"); err == nil {
		t.Fatal("absence confirmed send")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result = p.Send(ctx, outgoing, func() error { t.Error("cancelled send began"); return nil })
	if countSent() != 1 || result.RemoteID != "" {
		t.Fatal(result)
	}
}
