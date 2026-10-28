package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/mailmime"
	"github.com/bary/pebble-go/internal/provider/gmail"
	"github.com/bary/pebble-go/internal/store"
	"github.com/bary/pebble-go/internal/testgmail"
	"github.com/emersion/go-message/mail"
)

func sendRequest() SendRequest {
	return SendRequest{AccountID: "a", ClientKey: "send-key", To: []string{"to@example.invalid"}, CC: []string{"cc@example.invalid"}, BCC: []string{"hidden@example.invalid"}, Subject: "private sent subject", BodyText: "sent body", BodyHTML: "<p>sent body</p>", InReplyTo: "original@example.invalid"}
}
func TestOutboxSelfContainedEncryptedSendAndIdempotence(t *testing.T) {
	compose, _ := composeFixture(t)
	r := compose.Runtime
	ctx := t.Context()
	s := &OutboxService{Runtime: r}
	mock := testgmail.NewMock()
	defer mock.Close()
	mock.ProfileEmail = "a@example.invalid"
	r.InstallProvider("a", gmail.New(r.DB, mock.API(staticToken{"demo-access"}), "a"))
	attachment, err := compose.Stage(ctx, StageComposeRequest{Filename: "private-attachment.txt", Bytes: []int{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	request := sendRequest()
	request.AttachmentPaths = []string{attachment}
	if _, err := s.Queue(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, err := compose.Cleanup(ctx, CleanupComposeRequest{Path: attachment}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Queue(ctx, request); err != nil {
		t.Fatal("retry should resolve before missing attachment validation", err)
	}
	changed := request
	changed.Subject = "different content"
	if _, err := s.Queue(ctx, changed); err == nil {
		t.Fatal("same key accepted different message")
	}
	rows, _ := r.DB.OutboxList(ctx, "a")
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	row, _ := r.DB.Outbox(ctx, "a", rows[0].ID)
	if bytes.Contains(row.Data, []byte(request.Subject)) || bytes.Contains(row.Summary, []byte(request.Subject)) {
		t.Fatal("plaintext outbox")
	}
	if _, err := r.processOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	delivered := append([][]byte{}, mock.SentRaw...)
	mock.Unlock()
	if len(delivered) != 1 {
		t.Fatal(len(delivered))
	}
	parsed, err := mailmime.ParseWithLimit(delivered[0], mailmime.MaxOutgoingBytes)
	if err != nil || len(parsed.Attachments) != 1 || parsed.Subject != request.Subject {
		t.Fatal(parsed, err)
	}
	reader, err := mail.CreateReader(bytes.NewReader(delivered[0]))
	if err != nil {
		t.Fatal(err)
	}
	hidden, _ := reader.Header.AddressList("Bcc")
	reply, _ := reader.Header.MsgIDList("In-Reply-To")
	reader.Close()
	if len(hidden) != 1 || hidden[0].Address != "hidden@example.invalid" || len(reply) != 1 || reply[0] != "original@example.invalid" {
		t.Fatal("Bcc/reply header lost", hidden, reply)
	}
	if _, err := s.Queue(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, err := r.processOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	count := len(mock.SentRaw)
	mock.Unlock()
	if count != 1 {
		t.Fatal("duplicate send", count)
	}
	items, err := s.List(ctx, AccountRequest{AccountID: "a"})
	if err != nil || len(items) != 1 || items[0].Status != "sent" {
		t.Fatal(items, err)
	}
}
func TestOutboxUnknownSurvivesRestartAndReadOnlyReconciliation(t *testing.T) {
	compose, ring := composeFixture(t)
	r := compose.Runtime
	ctx := t.Context()
	s := &OutboxService{Runtime: r}
	mock := testgmail.NewMock()
	defer mock.Close()
	mock.ProfileEmail = "a@example.invalid"
	mock.LoseSendResponse = true
	r.InstallProvider("a", gmail.New(r.DB, mock.API(staticToken{"demo-access"}), "a"))
	if _, err := s.Queue(ctx, sendRequest()); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := r.processOutbox(ctx); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := r.DB.OutboxList(ctx, "a")
	if len(rows) != 1 || rows[0].Status != "outcome_unknown" {
		t.Fatal(rows)
	}
	db, err := store.Open(ctx, r.DB.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r2 := New(ctx, db, ring, nil)
	defer r2.Scheduler.Close()
	r2.InstallProvider("a", gmail.New(db, mock.API(staticToken{"demo-access"}), "a"))
	if err := db.RecoverOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r2.processOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	service := &OutboxService{Runtime: r2}
	if _, err := service.Action(ctx, OutboxActionRequest{AccountID: "a", ID: rows[0].ID, Action: "retry"}); err == nil {
		t.Fatal("unknown retry accepted")
	}
	if _, err := service.Action(ctx, OutboxActionRequest{AccountID: "a", ID: rows[0].ID, Action: "check"}); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	count := len(mock.SentRaw)
	mock.Unlock()
	if count != 1 {
		t.Fatal("unknown was resent", count)
	}
	row, _ := db.Outbox(ctx, "a", rows[0].ID)
	if row.Status != "sent" {
		t.Fatal(row.Status)
	}
}
func TestOutboxSettlementFailureDoesNotResend(t *testing.T) {
	compose, _ := composeFixture(t)
	r := compose.Runtime
	ctx := t.Context()
	mock := testgmail.NewMock()
	defer mock.Close()
	mock.ProfileEmail = "a@example.invalid"
	r.InstallProvider("a", gmail.New(r.DB, mock.API(staticToken{"demo-access"}), "a"))
	if _, err := (&OutboxService{Runtime: r}).Queue(ctx, sendRequest()); err != nil {
		t.Fatal(err)
	}
	injectPendingSQL(t, r.DB, `CREATE TRIGGER fail_outbox_settle BEFORE UPDATE OF status ON outbox WHEN NEW.status='sent' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	for range 2 {
		if _, err := r.processOutbox(ctx); err == nil {
			t.Fatal("settlement error ignored")
		}
	}
	mock.Lock()
	count := len(mock.SentRaw)
	mock.Unlock()
	if count != 1 || r.outboxSettlement == nil {
		t.Fatal(count)
	}
	injectPendingSQL(t, r.DB, "DROP TRIGGER fail_outbox_settle")
	if _, err := r.processOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	count = len(mock.SentRaw)
	mock.Unlock()
	if count != 1 {
		t.Fatal(count)
	}
}
func TestUnknownSettlementFailureStaysNonRetryable(t *testing.T) {
	compose, _ := composeFixture(t)
	r := compose.Runtime
	ctx := t.Context()
	mock := testgmail.NewMock()
	defer mock.Close()
	mock.ProfileEmail = "a@example.invalid"
	mock.LoseSendResponse = true
	r.InstallProvider("a", gmail.New(r.DB, mock.API(staticToken{"demo-access"}), "a"))
	if _, err := (&OutboxService{Runtime: r}).Queue(ctx, sendRequest()); err != nil {
		t.Fatal(err)
	}
	injectPendingSQL(t, r.DB, `CREATE TRIGGER block_unknown BEFORE UPDATE OF status ON outbox WHEN NEW.status='outcome_unknown' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	for range 2 {
		if _, err := r.processOutbox(ctx); err == nil {
			t.Fatal("DB failure hidden")
		}
	}
	injectPendingSQL(t, r.DB, "DROP TRIGGER block_unknown")
	for range 2 {
		if _, err := r.processOutbox(ctx); err != nil {
			t.Fatal(err)
		}
	}
	mock.Lock()
	count := len(mock.SentRaw)
	mock.Unlock()
	if count != 1 {
		t.Fatal("unknown resent", count)
	}
	rows, _ := r.DB.OutboxList(ctx, "a")
	if len(rows) != 1 || rows[0].Status != "outcome_unknown" {
		t.Fatal(rows)
	}
}

func TestOutboxBeginPersistenceFailureNeverPosts(t *testing.T) {
	compose, _ := composeFixture(t)
	r := compose.Runtime
	mock := testgmail.NewMock()
	defer mock.Close()
	mock.ProfileEmail = "a@example.invalid"
	r.InstallProvider("a", gmail.New(r.DB, mock.API(staticToken{"demo-access"}), "a"))
	if _, err := (&OutboxService{Runtime: r}).Queue(t.Context(), sendRequest()); err != nil {
		t.Fatal(err)
	}
	injectPendingSQL(t, r.DB, `CREATE TRIGGER block_send BEFORE UPDATE OF status ON outbox WHEN NEW.status='in_progress' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := r.processOutbox(t.Context()); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	count := len(mock.SentRaw)
	mock.Unlock()
	if count != 0 {
		t.Fatal("sent without durable start")
	}
	rows, _ := r.DB.OutboxList(t.Context(), "a")
	if len(rows) != 1 || rows[0].Status != "failed" {
		t.Fatal(rows)
	}
}

func TestOutboxQueuedRestartAndCancellation(t *testing.T) {
	compose, ring := composeFixture(t)
	r := compose.Runtime
	ctx := t.Context()
	s := &OutboxService{Runtime: r}
	if _, err := s.Queue(ctx, sendRequest()); err != nil {
		t.Fatal(err)
	}
	rows, _ := r.DB.OutboxList(ctx, "a")
	if _, err := s.Action(ctx, OutboxActionRequest{AccountID: "b", ID: rows[0].ID, Action: "cancel"}); err == nil {
		t.Fatal("cross-account cancel")
	}
	if _, err := s.Action(ctx, OutboxActionRequest{AccountID: "a", ID: rows[0].ID, Action: "cancel"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.DB.ClaimOutbox(ctx, time.Now().Unix()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	request := sendRequest()
	request.ClientKey = "second"
	if _, err := s.Queue(ctx, request); err != nil {
		t.Fatal(err)
	}
	path := r.DB.Path()
	if err := r.DB.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r2 := New(ctx, db, ring, nil)
	defer r2.Scheduler.Close()
	mock := testgmail.NewMock()
	defer mock.Close()
	mock.ProfileEmail = "a@example.invalid"
	r2.InstallProvider("a", gmail.New(db, mock.API(staticToken{"demo-access"}), "a"))
	if err := db.RecoverOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r2.processOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	count := len(mock.SentRaw)
	mock.Unlock()
	if count != 1 {
		t.Fatal(count)
	}
}

type blockedSender struct{ started chan struct{} }

func (b *blockedSender) Capabilities() domain.Capabilities { return domain.Capabilities{Send: true} }
func (b *blockedSender) Sync(context.Context) (domain.SyncResult, error) {
	return domain.SyncResult{}, nil
}
func (b *blockedSender) Flags(context.Context, string, *bool, *bool) error { return nil }
func (b *blockedSender) CheckSent(context.Context, string, string) (string, error) {
	return "", errors.New("unknown")
}
func (b *blockedSender) Send(ctx context.Context, _ string, _ []byte, start func() error) domain.SendResult {
	if err := start(); err != nil {
		return domain.SendResult{Message: "no send"}
	}
	close(b.started)
	<-ctx.Done()
	return domain.SendResult{Unknown: true, Message: "unknown"}
}
func TestOutboxWorkerCancellationRetainsUnknownOnRecovery(t *testing.T) {
	compose, _ := composeFixture(t)
	r := compose.Runtime
	sender := &blockedSender{started: make(chan struct{})}
	r.InstallProvider("a", sender)
	if _, err := (&OutboxService{Runtime: r}).Queue(t.Context(), sendRequest()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { r.RunOutboxWorker(ctx); close(done) }()
	select {
	case <-sender.started:
	case <-time.After(3 * time.Second):
		cancel()
		<-done
		t.Fatal("not started")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not stop")
	}
	if err := r.DB.RecoverOutbox(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, _ := r.DB.OutboxList(t.Context(), "a")
	if len(rows) != 1 || rows[0].Status != "outcome_unknown" {
		t.Fatal(rows)
	}
}
