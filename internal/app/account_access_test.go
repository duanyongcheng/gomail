package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bary/pebble-go/internal/crypto"
	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/googleapi"
	"github.com/bary/pebble-go/internal/oauth"
	"github.com/bary/pebble-go/internal/provider/gmail"
	"github.com/bary/pebble-go/internal/store"
	"github.com/bary/pebble-go/internal/testgmail"
	"golang.org/x/oauth2"
)

func accessFixture(t *testing.T) (*Runtime, *testgmail.Mock, *testKeyring) {
	t.Helper()
	t.Setenv("GOOGLE_CLIENT_ID", "test-client")
	compose, ring := composeFixture(t)
	r := compose.Runtime
	cipher, _ := crypto.FromKey(ring.key)
	for _, id := range []string{"a", "b"} {
		encrypted, err := oauth.EncodeToken(cipher, id, "test-client", accessGrant())
		if err != nil {
			t.Fatal(err)
		}
		if err := r.DB.SaveAccount(t.Context(), domain.Account{ID: id, Email: id + "@example.invalid", DisplayName: "Sender " + id, Provider: "gmail", CreatedAt: 1}, encrypted); err != nil {
			t.Fatal(err)
		}
	}
	mock := testgmail.NewMock()
	t.Cleanup(mock.Close)
	mock.ProfileEmail = "a@example.invalid"
	r.GmailAPIBase = mock.Server.URL + "/gmail/v1/users/me"
	if err := r.Restore(); err != nil {
		t.Fatal(err)
	}
	r.Authorize = func(context.Context, *oauth2.Config, *http.Client, func(string) error) (*oauth2.Token, error) {
		return accessGrant(), nil
	}
	return r, mock, ring
}
func accessGrant() *oauth2.Token {
	return (&oauth2.Token{AccessToken: "demo-access", RefreshToken: "demo-refresh", Expiry: time.Now().Add(time.Hour)}).WithExtra(map[string]any{"scope": oauth.ModifyScope})
}
func connectionState(t *testing.T, r *Runtime, account string) domain.ConnectionState {
	t.Helper()
	states, err := (&AccountService{Runtime: r}).Connections(t.Context(), Empty{})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range states {
		if state.AccountID == account {
			return state.State
		}
	}
	t.Fatal("missing connection", account)
	return ""
}
func awaitAccessResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("reconnection did not finish")
		return nil
	}
}

func TestRestoreIsolatesUnreadableCredentialsAndPreservesCiphertext(t *testing.T) {
	r, mock, ring := accessFixture(t)
	old, _ := r.DB.AuthData(t.Context(), "a")
	bad := bytes.Clone(old)
	bad[len(bad)-1] ^= 1
	if ok, err := r.DB.CompareAndSwapAuthData(t.Context(), "a", old, bad, 2); err != nil || !ok {
		t.Fatal(ok, err)
	}
	r2 := New(t.Context(), r.DB, ring, func(string) error { t.Fatal("restore opened browser"); return nil })
	t.Cleanup(r2.Scheduler.Close)
	if err := r2.Restore(); err == nil {
		t.Fatal("bad credentials were hidden")
	}
	if connectionState(t, r2, "a") != domain.ConnectionCredentialsUnavailable {
		t.Fatal("bad account not identified")
	}
	if _, err := r2.provider("b"); err != nil {
		t.Fatal("healthy later account was skipped", err)
	}
	stored, _ := r.DB.AuthData(t.Context(), "a")
	if !bytes.Equal(stored, bad) || ring.writes != 0 {
		t.Fatal("restore overwrote encrypted credentials")
	}
	mock.Lock()
	requests := len(mock.Requests)
	mock.Unlock()
	if requests != 0 {
		t.Fatal("partial restore contacted Gmail")
	}
}

func TestLockedKeyringKeepsAllAccountsVisible(t *testing.T) {
	r, _, ring := accessFixture(t)
	old, _ := r.DB.AuthData(t.Context(), "a")
	locked := &testKeyring{err: errors.New("locked")}
	r2 := New(t.Context(), r.DB, locked, nil)
	t.Cleanup(r2.Scheduler.Close)
	if err := r2.Restore(); err == nil {
		t.Fatal("locked keyring ignored")
	}
	for _, id := range []string{"a", "b"} {
		if connectionState(t, r2, id) != domain.ConnectionCredentialsUnavailable {
			t.Fatal(id)
		}
	}
	stored, _ := r.DB.AuthData(t.Context(), "a")
	if !bytes.Equal(old, stored) || locked.writes != 0 || ring.writes != 0 {
		t.Fatal("keyring failure changed data")
	}
}

func TestPermanentAccessStateSurvivesRestoreAndLateSuccess(t *testing.T) {
	r, mock, ring := accessFixture(t)
	c := r.connections["a"]
	r.observeConnection("a", c, &googleapi.HTTPError{Status: 403, Reasons: []string{"insufficientPermissions"}})
	r.observeConnection("a", c, nil)
	if connectionState(t, r, "a") != domain.ConnectionPermissionRequired {
		t.Fatal("late success undid pause")
	}
	r2 := New(t.Context(), r.DB, ring, nil)
	r2.GmailAPIBase = mock.Server.URL + "/gmail/v1/users/me"
	t.Cleanup(r2.Scheduler.Close)
	if err := r2.Restore(); err == nil {
		t.Fatal("persisted pause missing")
	}
	if connectionState(t, r2, "a") != domain.ConnectionPermissionRequired || !r2.accountBlocked("a") {
		t.Fatal("pause lost on restart")
	}
	if _, err := r2.provider("b"); err != nil {
		t.Fatal("healthy account blocked", err)
	}
	if err := r2.Scheduler.Preference("balanced"); err != nil {
		t.Fatal("paused account stopped preference activation", err)
	}
	// Join the automatically activated healthy account before fixture teardown.
	r2.Scheduler.Close()
}

func TestTemporaryFailureDoesNotPersistOrPause(t *testing.T) {
	r, _, _ := accessFixture(t)
	r.observeConnection("a", r.connections["a"], &googleapi.HTTPError{Status: 429})
	if r.accountBlocked("a") || connectionState(t, r, "a") != domain.ConnectionTemporaryError {
		t.Fatal("temporary failure paused account")
	}
	state, err := r.DB.AccountAuthState(t.Context(), "a")
	if err != nil || state != domain.ConnectionAvailable {
		t.Fatal("transient failure persisted", state, err)
	}
	r.observeConnection("a", r.connections["a"], nil)
	if connectionState(t, r, "a") != domain.ConnectionAvailable {
		t.Fatal("successful request did not recover transient status")
	}
}

func TestReconnectKeepsIdentityDraftUnknownOutboxAndPreferences(t *testing.T) {
	r, mock, _ := accessFixture(t)
	ctx := t.Context()
	compose := &ComposeService{Runtime: r}
	draft, err := compose.Save(ctx, draftFixture())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&OutboxService{Runtime: r}).Queue(ctx, sendRequest()); err != nil {
		t.Fatal(err)
	}
	claimed, err := r.DB.ClaimOutbox(ctx, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.DB.BeginOutboxSend(ctx, claimed.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.DB.SettleOutbox(ctx, claimed.ID, "outcome_unknown", "unknown", "", false); err != nil {
		t.Fatal(err)
	}
	injectPendingSQL(t, r.DB, `UPDATE accounts SET color='#abcdef',account_label='Work',sort_order=7 WHERE id='a'`)
	cursor := "opaque-cursor"
	if err := r.DB.CommitSnapshot(ctx, store.Snapshot{AccountID: "a", Cursor: &cursor}); err != nil {
		t.Fatal(err)
	}
	before, _ := r.DB.Account(ctx, "a")
	old, _ := r.DB.AuthData(ctx, "a")
	oldProvider := r.providers["a"].(*gmail.Provider)
	stale := &oauth.DatabaseVault{DB: r.DB, Crypto: r.crypto, Context: ctx, AccountID: "a", ClientID: "test-client"}
	if _, err := stale.Load(); err != nil {
		t.Fatal(err)
	}
	r.observeConnection("a", r.connections["a"], &googleapi.HTTPError{Status: 401})
	if _, err := (&AccountService{Runtime: r}).Reconnect(ctx, AccountRequest{AccountID: "a"}); err != nil {
		t.Fatal(err)
	}
	after, _ := r.DB.Account(ctx, "a")
	after.UpdatedAt = before.UpdatedAt
	if !reflect.DeepEqual(before, after) {
		t.Fatal("account metadata changed", before, after)
	}
	next, _ := r.DB.AuthData(ctx, "a")
	if bytes.Equal(old, next) || bytes.Contains(next, []byte("demo-access")) {
		t.Fatal("credential replacement not encrypted")
	}
	if err := stale.Save(accessGrant()); err == nil {
		t.Fatal("old refresher overwrote new credentials")
	}
	if _, err := compose.Get(ctx, DraftRequest{AccountID: "a", DraftID: draft}); err != nil {
		t.Fatal("draft lost", err)
	}
	if got, _ := r.DB.Cursor(ctx, "a"); got != cursor {
		t.Fatal("checkpoint lost")
	}
	if _, err := r.processOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	row, _ := r.DB.Outbox(ctx, "a", claimed.ID)
	if row.Status != "outcome_unknown" {
		t.Fatal("unknown send changed", row.Status)
	}
	mock.Lock()
	calls, sends := len(mock.Requests), len(mock.SentRaw)
	mock.Unlock()
	if sends != 0 {
		t.Fatal("unknown send resent")
	}
	oldProvider.API.OnResult(&googleapi.HTTPError{Status: 401})
	if connectionState(t, r, "a") != domain.ConnectionAvailable {
		t.Fatal("old callback changed replacement state")
	}
	if _, err := oldProvider.API.Profile(ctx); err == nil {
		t.Fatal("old provider still usable")
	}
	mock.Lock()
	afterCalls := len(mock.Requests)
	mock.Unlock()
	if calls != afterCalls {
		t.Fatal("old provider reached network")
	}
}

func TestReconnectFailurePreservesOriginalCredentialsAndPause(t *testing.T) {
	for _, kind := range []string{"wrong mailbox", "missing scope", "cancel", "denied", "save failure", "credential conflict"} {
		t.Run(kind, func(t *testing.T) {
			r, mock, _ := accessFixture(t)
			old, _ := r.DB.AuthData(t.Context(), "a")
			r.observeConnection("a", r.connections["a"], &googleapi.HTTPError{Status: 401})
			switch kind {
			case "wrong mailbox":
				mock.ProfileEmail = "different@example.invalid"
			case "missing scope":
				r.Authorize = func(context.Context, *oauth2.Config, *http.Client, func(string) error) (*oauth2.Token, error) {
					return accessGrant().WithExtra(map[string]any{"scope": oauth.ReadScope}), nil
				}
			case "cancel":
				r.Authorize = func(context.Context, *oauth2.Config, *http.Client, func(string) error) (*oauth2.Token, error) {
					return nil, context.Canceled
				}
			case "denied":
				r.Authorize = func(context.Context, *oauth2.Config, *http.Client, func(string) error) (*oauth2.Token, error) {
					return nil, errors.New("Authorization denied")
				}
			case "save failure":
				injectPendingSQL(t, r.DB, `CREATE TRIGGER reject_reauth BEFORE UPDATE OF auth_data ON accounts BEGIN SELECT RAISE(ABORT,'private SQL diagnostic'); END`)
			case "credential conflict":
				r.Authorize = func(context.Context, *oauth2.Config, *http.Client, func(string) error) (*oauth2.Token, error) {
					changed, _ := oauth.EncodeToken(r.crypto, "a", "test-client", accessGrant())
					ok, err := r.DB.CompareAndSwapAuthData(t.Context(), "a", old, changed, 3)
					if err != nil || !ok {
						t.Fatal(ok, err)
					}
					old = changed
					return accessGrant(), nil
				}
			}
			_, err := (&AccountService{Runtime: r}).Reconnect(t.Context(), AccountRequest{AccountID: "a"})
			if err == nil || strings.Contains(err.Error(), "private SQL") {
				t.Fatal("unsafe or missing failure", err)
			}
			next, _ := r.DB.AuthData(t.Context(), "a")
			if !bytes.Equal(old, next) || connectionState(t, r, "a") != domain.ConnectionReauthRequired {
				t.Fatal("failed authorization changed original credentials/state")
			}
		})
	}
}

func TestReconnectCanCancelWithoutHoldingRuntimeAndCannotResurrectDeletedAccount(t *testing.T) {
	for _, action := range []string{"cancel", "delete"} {
		t.Run(action, func(t *testing.T) {
			r, _, _ := accessFixture(t)
			old, _ := r.DB.AuthData(t.Context(), "a")
			started := make(chan struct{})
			r.Authorize = func(ctx context.Context, _ *oauth2.Config, _ *http.Client, _ func(string) error) (*oauth2.Token, error) {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			s := &AccountService{Runtime: r}
			done := make(chan error, 1)
			go func() { _, err := s.Reconnect(t.Context(), AccountRequest{AccountID: "a"}); done <- err }()
			<-started
			if state := connectionState(t, r, "a"); state != domain.ConnectionReconnecting {
				t.Fatal(state)
			}
			if _, err := r.provider("b"); err != nil {
				t.Fatal("browser blocked another account")
			}
			if _, err := s.Reconnect(t.Context(), AccountRequest{AccountID: "b"}); err == nil {
				t.Fatal("second browser flow queued")
			}
			if action == "cancel" {
				_, _ = s.CancelReconnect(t.Context(), AccountRequest{AccountID: "a"})
			} else if _, err := s.Delete(t.Context(), AccountRequest{AccountID: "a"}); err != nil {
				t.Fatal(err)
			}
			if err := awaitAccessResult(t, done); err == nil {
				t.Fatal("cancelled flow succeeded")
			}
			if action == "cancel" {
				next, _ := r.DB.AuthData(t.Context(), "a")
				if !bytes.Equal(old, next) || r.accountBlocked("a") {
					t.Fatal("cancel did not restore existing account")
				}
			} else if _, err := r.DB.Account(t.Context(), "a"); err == nil {
				t.Fatal("deleted account resurrected")
			}
		})
	}
}

func TestOutboxAuthorizationFailureRetainsAttemptsAndOtherAccountsProgress(t *testing.T) {
	r, mock, _ := accessFixture(t)
	other := testgmail.NewMock()
	defer other.Close()
	other.ProfileEmail = "b@example.invalid"
	r.InstallProvider("b", gmail.New(r.DB, other.API(staticToken{"demo-access"}), "b"))
	s := &OutboxService{Runtime: r}
	for _, account := range []string{"a", "b"} {
		request := sendRequest()
		request.AccountID = account
		if _, err := s.Queue(t.Context(), request); err != nil {
			t.Fatal(err)
		}
	}
	mock.Lock()
	mock.FailStatus = 401
	mock.Unlock()
	for range 3 {
		if _, err := r.processOutbox(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := r.DB.OutboxList(t.Context(), "a")
	if len(rows) != 1 || rows[0].Attempts != 0 || rows[0].Status != "queued" || !r.accountBlocked("a") {
		t.Fatal("paused claim consumed send budget", rows)
	}
	other.Lock()
	sent := len(other.SentRaw)
	other.Unlock()
	if sent != 1 {
		t.Fatal("other account stalled", sent)
	}
	if _, err := (&AccountService{Runtime: r}).Reconnect(t.Context(), AccountRequest{AccountID: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.processOutbox(t.Context()); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	sent = len(mock.SentRaw)
	mock.Unlock()
	if sent != 1 {
		t.Fatal("queued message did not resume exactly once", sent)
	}
}

func TestPendingAuthorizationFailureReleasesClaimAndPauses(t *testing.T) {
	r, mock, _ := accessFixture(t)
	ctx := t.Context()
	if err := r.DB.CommitSnapshot(ctx, store.Snapshot{AccountID: "a", Messages: []store.CachedMessage{{Message: domain.Message{ID: "a:m", AccountID: "a", RemoteID: "m", To: []domain.Address{}, CC: []domain.Address{}, BCC: []domain.Address{}}}}}); err != nil {
		t.Fatal(err)
	}
	read := true
	if _, err := (&MessageService{Runtime: r}).QueueFlags(ctx, FlagsRequest{MessageID: "a:m", IsRead: &read}); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	mock.FailStatus = 401
	mock.Unlock()
	for range 3 {
		if _, err := r.processPendingOps(ctx); err != nil {
			t.Fatal(err)
		}
	}
	ops, _ := r.DB.PendingMailOps(ctx, "a", 10)
	if len(ops) != 1 || ops[0].Attempts != 0 || ops[0].Status != "pending" || !r.accountBlocked("a") {
		t.Fatal("paused mutation consumed budget", ops)
	}
	mock.Lock()
	before := len(mock.Requests)
	mock.Unlock()
	if _, err := r.processPendingOps(ctx); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	after := len(mock.Requests)
	mock.Unlock()
	if after != before {
		t.Fatal("paused account still made requests")
	}
}

func TestSchedulerPermanentFailureStopsAutomaticNetworkWork(t *testing.T) {
	r, _, _ := accessFixture(t)
	var attempts atomic.Int32
	// Report the authorization problem from a real provider request boundary.
	p := r.providers["a"].(*gmail.Provider)
	p.API.Tokens = staticToken{"demo-access"}
	p.API.HTTP = &http.Client{Transport: accessRoundTrip(func(*http.Request) (*http.Response, error) {
		attempts.Add(1)
		return &http.Response{StatusCode: 401, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}
	if _, err := (&SyncService{Runtime: r}).Sync(t.Context(), AccountRequest{AccountID: "a"}); err == nil {
		t.Fatal("authorization failure hidden")
	}
	if _, err := (&SyncService{Runtime: r}).Sync(t.Context(), AccountRequest{AccountID: "a"}); err == nil {
		t.Fatal("paused manual sync accepted")
	}
	if attempts.Load() != 1 {
		t.Fatal("paused scheduler contacted provider")
	}
}

type accessRoundTrip func(*http.Request) (*http.Response, error)

func (f accessRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAccessStatePersistenceFailureRemainsPausedAndRetriesLocally(t *testing.T) {
	r, _, _ := accessFixture(t)
	injectPendingSQL(t, r.DB, `CREATE TRIGGER block_auth_status BEFORE UPDATE OF sync_state ON accounts BEGIN SELECT RAISE(ABORT,'injected'); END`)
	r.observeConnection("a", r.connections["a"], &googleapi.HTTPError{Status: 401})
	if !r.accountBlocked("a") || !r.connections["a"].persistPending {
		t.Fatal("failed persistence allowed more work")
	}
	injectPendingSQL(t, r.DB, "DROP TRIGGER block_auth_status")
	if len(r.blockedAccounts()) != 1 || r.connections["a"].persistPending {
		t.Fatal("local state persistence did not recover")
	}
	state, err := r.DB.AccountAuthState(t.Context(), "a")
	if err != nil || state != domain.ConnectionReauthRequired {
		t.Fatal(state, err)
	}
}

func TestAddingExistingGoogleMailboxPreservesAccountPreferences(t *testing.T) {
	r, _, _ := accessFixture(t)
	s := &AccountService{Runtime: r}
	account, err := s.Login(t.Context(), LoginRequest{Provider: "gmail"})
	if err != nil {
		t.Fatal(err)
	}
	injectPendingSQL(t, r.DB, `UPDATE accounts SET display_name='Custom sender',account_label='Work',color='#abcdef' WHERE id='`+account.ID+`'`)
	before, _ := r.DB.Account(t.Context(), account.ID)
	label := "New login label"
	if _, err := s.Login(t.Context(), LoginRequest{Provider: "gmail", AccountLabel: &label}); err != nil {
		t.Fatal(err)
	}
	after, _ := r.DB.Account(t.Context(), account.ID)
	before.UpdatedAt, after.UpdatedAt = 0, 0
	if !reflect.DeepEqual(before, after) {
		t.Fatal("repeated sign-in overwrote local preferences", before, after)
	}
}

func TestReconnectCancellationWhileWaitingForQueuedWork(t *testing.T) {
	r, mock, _ := accessFixture(t)
	service := &AccountService{Runtime: r}
	var authorizations atomic.Int32
	r.Authorize = func(context.Context, *oauth2.Config, *http.Client, func(string) error) (*oauth2.Token, error) {
		authorizations.Add(1)
		return accessGrant(), nil
	}
	r.pendingMu.Lock()
	done := make(chan error, 1)
	go func() { _, err := service.Reconnect(t.Context(), AccountRequest{AccountID: "a"}); done <- err }()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		r.mu.RLock()
		registered := r.reconnectCancel != nil
		r.mu.RUnlock()
		if registered {
			break
		}
		select {
		case <-deadline.C:
			r.pendingMu.Unlock()
			_ = awaitAccessResult(t, done)
			t.Fatal("reconnect cancellation was not registered before waiting")
		case <-ticker.C:
		}
	}
	_, _ = service.CancelReconnect(t.Context(), AccountRequest{AccountID: "a"})
	r.pendingMu.Unlock()
	if err := awaitAccessResult(t, done); err == nil {
		t.Fatal("cancelled preparation succeeded")
	}
	mock.Lock()
	requests := len(mock.Requests)
	mock.Unlock()
	if authorizations.Load() != 0 || requests != 0 {
		t.Fatal("cancelled preparation opened sign-in or contacted Gmail")
	}
}
