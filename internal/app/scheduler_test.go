package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bary/pebble-go/internal/domain"
)

type scheduledProvider struct {
	mu                       sync.Mutex
	calls, active, maxActive int
	err                      error
	block                    chan struct{}
	limited                  bool
}

func (p *scheduledProvider) Capabilities() domain.Capabilities {
	return domain.Capabilities{Read: true}
}
func (p *scheduledProvider) Flags(context.Context, string, *bool, *bool) error { return nil }
func (p *scheduledProvider) Sync(ctx context.Context) (domain.SyncResult, error) {
	p.mu.Lock()
	p.calls++
	p.active++
	p.maxActive = max(p.maxActive, p.active)
	block, err, limited := p.block, p.err, p.limited
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.active--; p.mu.Unlock() }()
	if block != nil {
		select {
		case <-ctx.Done():
			return domain.SyncResult{}, ctx.Err()
		case <-block:
		}
	}
	return domain.SyncResult{Count: 1, Limited: limited}, err
}
func (p *scheduledProvider) count() int { p.mu.Lock(); defer p.mu.Unlock(); return p.calls }
func schedulerFixture(t *testing.T) (*Runtime, *scheduledProvider) {
	t.Helper()
	r := New(t.Context(), nil, nil, nil)
	p := &scheduledProvider{}
	r.InstallProvider("a", p)
	t.Cleanup(r.Scheduler.Close)
	return r, p
}

func TestPollPolicyMatchesReference(t *testing.T) {
	for _, test := range []struct {
		foreground, recent bool
		failures           int
		interval, want     time.Duration
	}{
		{true, true, 0, 10 * time.Second, 10 * time.Second},
		{true, false, 0, 10 * time.Second, 30 * time.Second},
		{false, false, 0, 10 * time.Second, 120 * time.Second},
		{true, true, 1, 10 * time.Second, 30 * time.Second},
		{true, true, 2, 10 * time.Second, 60 * time.Second},
		{true, true, 3, 10 * time.Second, 120 * time.Second},
		{true, true, 4, 10 * time.Second, 240 * time.Second},
		{true, true, 1000, 10 * time.Second, 300 * time.Second},
		{true, false, 0, time.Minute, time.Minute},
	} {
		if got := pollDelay(test.interval, test.foreground, test.recent, test.failures); got != test.want {
			t.Errorf("policy %+v: %s", test, got)
		}
	}
}

func TestSchedulerReportsActualProviderForManualFailureAndStop(t *testing.T) {
	for _, provider := range []string{"gmail", "imap", "pop3", "outlook"} {
		t.Run(provider, func(t *testing.T) {
			r, _, db := pendingAppFixture(t)
			executeProvider := "UPDATE accounts SET provider='" + provider + "' WHERE id='a'"
			injectPendingSQL(t, db, executeProvider)
			r.InstallProvider("a", &scheduledProvider{err: errors.New("synthetic failure")})
			defer r.Scheduler.Close()
			statuses := make(chan RealtimeStatus, 8)
			r.OnRealtime = func(s RealtimeStatus) { statuses <- s }
			if err := r.Scheduler.Preference("manual"); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Scheduler.Trigger(t.Context(), "a", "manual"); err == nil {
				t.Fatal("failure not exercised")
			}
			r.Scheduler.Stop("a")
			close(statuses)
			sawFailure := false
			for status := range statuses {
				if status.AccountID != "a" {
					continue
				}
				if status.Provider != provider || strings.Contains(status.Message, "Gmail") {
					t.Fatalf("provider mislabeled: %+v", status)
				}
				if status.Mode == "error" {
					sawFailure = true
				}
			}
			if !sawFailure {
				t.Fatal("no failure status")
			}
		})
	}
}

func TestSchedulerWaitsForPreferenceAndManualDisablesAutomaticRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, p := schedulerFixture(t)
		if err := r.Scheduler.Start("a", 0); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if p.count() != 0 {
			t.Fatal("started before profile preference")
		}
		if err := r.Scheduler.Preference("manual"); err != nil {
			t.Fatal(err)
		}
		for _, reason := range []string{"startup", "window_focus", "network_online", "timer"} {
			if _, err := r.Scheduler.Trigger(t.Context(), "a", reason); err != nil {
				t.Fatal(err)
			}
		}
		time.Sleep(10 * time.Minute)
		if p.count() != 0 {
			t.Fatal("manual mode issued automatic request")
		}
		if _, err := (&SyncService{Runtime: r}).Sync(t.Context(), AccountRequest{AccountID: "a"}); err != nil {
			t.Fatal(err)
		}
		if p.count() != 1 {
			t.Fatal("manual request did not sync")
		}
	})
}

func TestSchedulerDeduplicatesWorkersAndUsesBackgroundPolicy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, p := schedulerFixture(t)
		if err := r.Scheduler.Preference("realtime"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		for i := 0; i < 5; i++ {
			if err := r.Scheduler.Start("a", 10*time.Second); err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		if p.count() != 1 {
			t.Fatal("duplicate worker on start", p.count())
		}
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if p.count() != 2 {
			t.Fatal("foreground timer did not fire", p.count())
		}
		r.Scheduler.Foreground(false)
		time.Sleep(119 * time.Second)
		synctest.Wait()
		if p.count() != 2 {
			t.Fatal("background interval too short", p.count())
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if p.count() != 3 {
			t.Fatal("background timer did not fire", p.count())
		}
		r.Scheduler.Stop("a")
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		if p.count() != 3 {
			t.Fatal("stopped worker kept polling")
		}
	})
}

func TestSchedulerBackoffFocusAndExplicitRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, p := schedulerFixture(t)
		p.err = errors.New("synthetic failure")
		if err := r.Scheduler.Preference("realtime"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if _, err := r.Scheduler.Trigger(t.Context(), "a", "window_focus"); err != nil {
			t.Fatal(err)
		}
		if p.count() != 1 {
			t.Fatal("focus bypassed failure backoff")
		}
		time.Sleep(29 * time.Second)
		synctest.Wait()
		if p.count() != 1 {
			t.Fatal("retry happened before backoff")
		}
		if _, err := r.Scheduler.Trigger(t.Context(), "a", "manual"); err == nil {
			t.Fatal("manual error was hidden")
		}
		if p.count() != 2 {
			t.Fatal("manual did not bypass backoff")
		}
		p.mu.Lock()
		p.err = nil
		p.mu.Unlock()
		if _, err := r.Scheduler.Trigger(t.Context(), "a", "network_online"); err != nil {
			t.Fatal(err)
		}
		if p.count() != 3 {
			t.Fatal("reconnect did not bypass backoff")
		}
	})
}

func TestSchedulerCoalescesConcurrentTriggersAndCancelsOnStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, p := schedulerFixture(t)
		p.block = make(chan struct{})
		if err := r.Scheduler.Preference("realtime"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		results := make(chan error, 4)
		for i := 0; i < 4; i++ {
			go func() { _, err := r.Scheduler.Trigger(t.Context(), "a", "manual"); results <- err }()
		}
		synctest.Wait()
		if p.count() != 1 {
			t.Fatal("concurrent triggers duplicated pass", p.count())
		}
		r.Scheduler.Stop("a")
		for i := 0; i < 4; i++ {
			if err := <-results; !errors.Is(err, context.Canceled) {
				t.Fatal("stop did not cancel I/O", err)
			}
		}
		p.mu.Lock()
		active, maxActive := p.active, p.maxActive
		p.mu.Unlock()
		if active != 0 || maxActive != 1 {
			t.Fatal("overlapping/leaked passes", active, maxActive)
		}
	})
}

func TestExplicitManualBackfillContinuesWithoutEnablingPolling(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, p := schedulerFixture(t)
		p.limited = true
		if err := r.Scheduler.Preference("manual"); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Scheduler.Trigger(t.Context(), "a", "manual"); err != nil {
			t.Fatal(err)
		}
		p.mu.Lock()
		p.limited = false
		p.mu.Unlock()
		time.Sleep(time.Second)
		synctest.Wait()
		if p.count() != 2 {
			t.Fatal("manual backfill did not continue", p.count())
		}
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		if p.count() != 2 {
			t.Fatal("completed manual backfill enabled polling")
		}
	})
}

func TestPreferenceChangeCancelsPassAndContinuationRunsPromptly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, p := schedulerFixture(t)
		p.block = make(chan struct{})
		if err := r.Scheduler.Preference("realtime"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if err := r.Scheduler.Preference("manual"); err != nil {
			t.Fatal(err)
		}
		p.mu.Lock()
		p.block = nil
		p.limited = true
		p.mu.Unlock()
		time.Sleep(time.Minute)
		synctest.Wait()
		if p.count() != 1 {
			t.Fatal("preference left automatic worker running")
		}
		if err := r.Scheduler.Preference("battery"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if p.count() != 2 {
			t.Fatal("battery mode did not start")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if p.count() != 3 {
			t.Fatal("backfill continuation waited for normal poll interval", p.count())
		}
	})
}

func TestSchedulerPausesOnlyTheAccountWithAccessFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, p := schedulerFixture(t)
		other := &scheduledProvider{}
		r.InstallProvider("b", other)
		r.connections["a"] = &accountConnection{state: domain.ConnectionAvailable}
		if err := r.Scheduler.Preference("realtime"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if p.count() != 1 || other.count() != 1 {
			t.Fatal("initial sync missing")
		}
		r.mu.Lock()
		r.connections["a"].state = domain.ConnectionReauthRequired
		r.mu.Unlock()
		r.Scheduler.accessChanged("a")
		synctest.Wait()
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		if p.count() != 1 || other.count() < 2 {
			t.Fatal("pause lost or crossed accounts", p.count(), other.count())
		}
		r.mu.Lock()
		r.connections["a"].state = domain.ConnectionAvailable
		r.mu.Unlock()
		r.Scheduler.accessChanged("a")
		synctest.Wait()
		if p.count() != 2 {
			t.Fatal("reconnected account did not resume")
		}
	})
}
