package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bary/pebble-go/internal/domain"
)

type resourceDeniedTransport struct {
	acceptedTransport
	reads int
}

type writeDeniedTransport struct {
	resourceDeniedTransport
	writes int
}

func (p *writeDeniedTransport) Replay(context.Context, string, json.RawMessage) (bool, error) {
	p.writes++
	return false, &domain.AccessError{State: domain.ConnectionPermissionRequired}
}

func TestTerminalWritePermissionRetainsQueueAndLeavesSendResourceAvailable(t *testing.T) {
	r, _, db := pendingAppFixture(t)
	p := &writeDeniedTransport{}
	r.InstallProvider("a", p)
	read := true
	if _, err := (&MessageService{Runtime: r}).QueueFlags(t.Context(), FlagsRequest{MessageID: "a:m", IsRead: &read}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := r.processPendingOps(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.PendingMailOps(t.Context(), "a", 10)
	if err != nil || len(rows) != 1 || rows[0].Attempts != 0 || rows[0].Status != "pending" || p.writes != 1 {
		t.Fatal(rows, err, p.writes)
	}
	state, err := db.ProviderSendState(t.Context(), "a")
	if err != nil || state != domain.ConnectionAvailable {
		t.Fatal("write permission paused send", state, err)
	}
}

func (p *resourceDeniedTransport) Send(context.Context, domain.OutgoingMail, func() error) domain.SendResult {
	p.calls++
	return domain.SendResult{AccessState: domain.ConnectionPermissionRequired, Message: "Sending permission required"}
}
func (p *resourceDeniedTransport) Sync(context.Context) (domain.SyncResult, error) {
	p.reads++
	return domain.SyncResult{}, nil
}

func TestTerminalSendPermissionPausesOnlyResourceWithoutSpendingAttempts(t *testing.T) {
	compose, _ := composeFixture(t)
	r := compose.Runtime
	ctx := t.Context()
	injectPendingSQL(t, r.DB, "UPDATE accounts SET provider='imap' WHERE id='a'")
	p := &resourceDeniedTransport{}
	r.InstallProvider("a", p)
	if _, err := (&OutboxService{Runtime: r}).Queue(ctx, sendRequest()); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := r.processOutbox(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if p.calls != 1 {
		t.Fatal("blocked resource retried", p.calls)
	}
	rows, err := r.DB.OutboxList(ctx, "a")
	if err != nil || len(rows) != 1 || rows[0].Status != "queued" || rows[0].Attempts != 0 {
		t.Fatal(rows, err)
	}
	state, err := r.DB.ProviderSendState(ctx, "a")
	if err != nil || state != domain.ConnectionPermissionRequired {
		t.Fatal(state, err)
	}
	if _, err := (&SyncService{Runtime: r}).Sync(ctx, AccountRequest{AccountID: "a"}); err != nil || p.reads != 1 {
		t.Fatal("send resource blocked healthy reads", err, p.reads)
	}
	// Persisted resource state survives another runtime and does not reset unknown sends.
	next := New(ctx, r.DB, r.Keyring, nil)
	defer next.Scheduler.Close()
	next.InstallProvider("a", p)
	if _, err := next.processOutbox(ctx); err != nil || p.calls != 1 {
		t.Fatal(err, p.calls)
	}
}
