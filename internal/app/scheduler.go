package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/bary/pebble-go/internal/domain"
)

type RealtimeStatus struct {
	AccountID     string `json:"account_id"`
	Mode          string `json:"mode"`
	Provider      string `json:"provider"`
	LastSuccessAt *int64 `json:"last_success_at"`
	NextRetryAt   *int64 `json:"next_retry_at"`
	DeferredCount int    `json:"deferred_count"`
	Message       string `json:"message"`
}

type syncFlight struct {
	done   chan struct{}
	result domain.SyncResult
	err    error
}

type syncWorker struct {
	account      string
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	wake         chan struct{}
	flight       *syncFlight
	pending      bool
	failures     int
	lastAttempt  time.Time
	lastFinished time.Time
	continuing   bool
	lastSuccess  time.Time
	nextAt       time.Time
	interval     time.Duration
}

// Scheduler is independent of the desktop framework. Control operations wait
// for cancelled workers before allowing a replacement for the same account.
type Scheduler struct {
	runtime    *Runtime
	control    sync.Mutex
	mu         sync.Mutex
	jobs       map[string]*syncWorker
	mode       string
	configured bool
	closed     bool
	foreground bool
	activity   time.Time
}

func newScheduler(r *Runtime) *Scheduler {
	return &Scheduler{runtime: r, jobs: map[string]*syncWorker{}, mode: "manual", foreground: true, activity: time.Now()}
}

func pollDelay(interval time.Duration, foreground, recent bool, failures int) time.Duration {
	idle := max(30*time.Second, interval)
	if failures > 0 {
		delay := min(idle, 300*time.Second)
		for i := 1; i < failures && delay < 300*time.Second; i++ {
			delay = min(delay*2, 300*time.Second)
		}
		return delay
	}
	if !foreground {
		return max(120*time.Second, idle)
	}
	if !recent {
		return idle
	}
	return interval
}

func (s *Scheduler) delay(w *syncWorker, now time.Time) time.Duration {
	return pollDelay(w.interval, s.foreground, now.Sub(s.activity) <= time.Minute, w.failures)
}

func modeInterval(mode string) time.Duration {
	switch mode {
	case "balanced":
		return 30 * time.Second
	case "battery":
		return time.Minute
	default:
		return 10 * time.Second
	}
}

func (s *Scheduler) ensure(account string, interval time.Duration) (*syncWorker, error) {
	if _, err := s.runtime.provider(account); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.runtime.Context.Err() != nil {
		return nil, context.Canceled
	}
	if w := s.jobs[account]; w != nil {
		if interval > 0 {
			w.interval = interval
		}
		return w, nil
	}
	if interval <= 0 {
		interval = modeInterval(s.mode)
	}
	ctx, cancel := context.WithCancel(s.runtime.Context)
	w := &syncWorker{account: account, ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), interval: interval, nextAt: time.Now()}
	s.jobs[account] = w
	go s.run(w)
	return w, nil
}

func wake(w *syncWorker) {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (s *Scheduler) Start(account string, interval time.Duration) error {
	s.control.Lock()
	defer s.control.Unlock()
	s.mu.Lock()
	enabled := s.configured && s.mode != "manual"
	s.mu.Unlock()
	if !enabled {
		return nil
	}
	w, err := s.ensure(account, interval)
	if err == nil {
		wake(w)
	}
	return err
}

func (s *Scheduler) Stop(account string) {
	s.control.Lock()
	defer s.control.Unlock()
	s.stop(account)
}

func (s *Scheduler) stop(account string) {
	s.mu.Lock()
	w := s.jobs[account]
	delete(s.jobs, account)
	s.mu.Unlock()
	if w != nil {
		w.cancel()
		<-w.done
	}
	s.status(RealtimeStatus{AccountID: account, Mode: "manual", Provider: "gmail"})
}

func (s *Scheduler) Close() {
	s.control.Lock()
	defer s.control.Unlock()
	s.mu.Lock()
	s.closed = true
	jobs := s.jobs
	s.jobs = map[string]*syncWorker{}
	s.mu.Unlock()
	for _, w := range jobs {
		w.cancel()
	}
	for _, w := range jobs {
		<-w.done
	}
}

// Preference arrives after profile-local frontend settings have loaded. Restore
// alone never schedules requests, and an initial manual preference stays local.
func (s *Scheduler) Preference(mode string) error {
	if mode != "realtime" && mode != "balanced" && mode != "battery" && mode != "manual" {
		return errors.New("invalid realtime preference")
	}
	s.control.Lock()
	defer s.control.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return context.Canceled
	}
	if s.configured && s.mode == mode {
		s.mu.Unlock()
		return nil
	}
	s.configured, s.mode = true, mode
	jobs := s.jobs
	s.jobs = map[string]*syncWorker{}
	s.mu.Unlock()
	for _, w := range jobs {
		w.cancel()
	}
	for _, w := range jobs {
		<-w.done
	}
	s.runtime.mu.RLock()
	accounts := make([]string, 0, len(s.runtime.providers))
	for id := range s.runtime.providers {
		accounts = append(accounts, id)
	}
	s.runtime.mu.RUnlock()
	for _, id := range accounts {
		if mode == "manual" {
			s.status(RealtimeStatus{AccountID: id, Provider: "gmail", Mode: "manual"})
			continue
		}
		if _, err := s.ensure(id, modeInterval(mode)); err != nil && !s.runtime.accountBlocked(id) {
			return err
		}
	}
	return nil
}

func (s *Scheduler) Foreground(foreground bool) {
	s.mu.Lock()
	s.foreground = foreground
	if foreground {
		s.activity = time.Now()
	}
	for _, w := range s.jobs {
		if w.failures == 0 && !w.continuing && !w.lastFinished.IsZero() {
			w.nextAt = w.lastFinished.Add(s.delay(w, time.Now()))
		}
		wake(w)
	}
	s.mu.Unlock()
}

// Concurrent triggers share one pass. A caller going away does not cancel a
// pass shared with another caller; Stop/Preference/application exit own that.
func (s *Scheduler) Trigger(ctx context.Context, account, reason string) (domain.SyncResult, error) {
	if reason == "" {
		reason = "manual"
	}
	switch reason {
	case "window_blur":
		s.Foreground(false)
		return domain.SyncResult{}, nil
	case "window_focus":
		s.Foreground(true)
	case "manual", "network_online", "startup", "timer", "provider_push":
	default:
		return domain.SyncResult{}, errors.New("invalid sync trigger")
	}
	if err := ctx.Err(); err != nil {
		return domain.SyncResult{}, err
	}
	s.control.Lock()
	s.mu.Lock()
	enabled := s.configured && s.mode != "manual"
	s.mu.Unlock()
	if reason != "manual" && !enabled {
		s.control.Unlock()
		return domain.SyncResult{}, nil
	}
	w, err := s.ensure(account, 0)
	if err != nil {
		s.control.Unlock()
		return domain.SyncResult{}, err
	}
	s.mu.Lock()
	now := time.Now()
	if reason != "manual" && reason != "network_online" && (w.failures > 0 && now.Before(w.nextAt) || now.Sub(w.lastAttempt) < time.Second) {
		s.mu.Unlock()
		s.control.Unlock()
		return domain.SyncResult{}, nil
	}
	if reason != "timer" {
		s.activity = now
	}
	flight := w.flight
	if flight == nil {
		flight = &syncFlight{done: make(chan struct{})}
		w.flight, w.pending = flight, true
	}
	s.mu.Unlock()
	s.control.Unlock()
	wake(w)
	select {
	case <-ctx.Done():
		return domain.SyncResult{}, ctx.Err()
	case <-flight.done:
		return flight.result, flight.err
	}
}

func (s *Scheduler) run(w *syncWorker) {
	defer close(w.done)
	defer func() {
		s.mu.Lock()
		if w.flight != nil {
			w.flight.err = context.Canceled
			close(w.flight.done)
			w.flight = nil
		}
		s.mu.Unlock()
	}()
	for {
		paused := s.runtime.accountBlocked(w.account)
		s.mu.Lock()
		now := time.Now()
		enabled := !paused && (s.configured && s.mode != "manual" || w.continuing)
		if w.failures == 0 && !w.continuing && !w.lastFinished.IsZero() {
			w.nextAt = w.lastFinished.Add(s.delay(w, now))
		}
		run := w.pending || enabled && !now.Before(w.nextAt)
		var timer *time.Timer
		var timerC <-chan time.Time
		if !run && enabled {
			timer = time.NewTimer(max(0, time.Until(w.nextAt)))
			timerC = timer.C
		}
		if run && w.flight == nil {
			w.flight = &syncFlight{done: make(chan struct{})}
		}
		flight := w.flight
		if run {
			w.pending = false
			w.lastAttempt = now
		}
		s.mu.Unlock()
		if !run {
			select {
			case <-w.ctx.Done():
				if timer != nil {
					timer.Stop()
				}
				return
			case <-w.wake:
			case <-timerC:
			}
			if timer != nil {
				timer.Stop()
			}
			continue
		}
		if w.ctx.Err() != nil {
			return
		}
		result, err := s.runtime.performSync(w.ctx, w.account)
		s.mu.Lock()
		if err != nil {
			w.failures++
		} else {
			w.failures = 0
			w.lastSuccess = time.Now()
		}
		w.lastFinished = time.Now()
		w.continuing = err == nil && result.Limited
		w.nextAt = w.lastFinished.Add(s.delay(w, w.lastFinished))
		if w.continuing {
			w.nextAt = w.lastFinished.Add(time.Second)
		}
		status := RealtimeStatus{AccountID: w.account, Provider: "gmail", Mode: "polling", DeferredCount: result.DeferredCount}
		if !w.lastSuccess.IsZero() {
			value := w.lastSuccess.Unix()
			status.LastSuccessAt = &value
		}
		if s.mode == "manual" {
			status.Mode = "manual"
		} else {
			value := w.nextAt.Unix()
			status.NextRetryAt = &value
		}
		if err != nil && w.ctx.Err() == nil {
			status.Message = "Gmail sync failed; retry scheduled"
			if s.mode == "manual" || !s.configured {
				status.Mode = "error"
				status.Message = "Gmail sync failed; synchronize to retry"
			} else {
				status.Mode = "backoff"
			}
		}
		flight.result, flight.err = result, err
		close(flight.done)
		w.flight = nil
		s.mu.Unlock()
		if w.ctx.Err() != nil {
			return
		}
		if s.runtime.accountBlocked(w.account) {
			status.Mode = "error"
			status.NextRetryAt = nil
			status.Message = "Account access requires attention; reconnect in Settings."
		}
		s.status(status)
	}
}

func (s *Scheduler) accessChanged(account string) {
	s.mu.Lock()
	if w := s.jobs[account]; w != nil {
		wake(w)
	}
	s.mu.Unlock()
}

func (s *Scheduler) status(status RealtimeStatus) {
	if s.runtime.OnRealtime != nil {
		s.runtime.OnRealtime(status)
	}
}

type StartSyncRequest struct {
	AccountID        string `json:"accountId"`
	PollIntervalSecs *int   `json:"pollIntervalSecs"`
}
type TriggerSyncRequest struct {
	AccountID string `json:"accountId"`
	Reason    string `json:"reason"`
}
type RealtimePreferenceRequest struct {
	Mode string `json:"mode"`
}
type SyncStarted struct {
	Value string `json:"value"`
}

func (s *SyncService) Start(_ context.Context, request StartSyncRequest) (SyncStarted, error) {
	var interval time.Duration
	if request.PollIntervalSecs != nil {
		if *request.PollIntervalSecs < 1 || *request.PollIntervalSecs > 86400 {
			return SyncStarted{}, errors.New("invalid poll interval")
		}
		interval = time.Duration(*request.PollIntervalSecs) * time.Second
	}
	return SyncStarted{Value: request.AccountID}, s.Runtime.Scheduler.Start(request.AccountID, interval)
}
func (s *SyncService) Stop(_ context.Context, request AccountRequest) (Empty, error) {
	s.Runtime.Scheduler.Stop(request.AccountID)
	return Empty{}, nil
}
func (s *SyncService) Preference(_ context.Context, request RealtimePreferenceRequest) (Empty, error) {
	return Empty{}, s.Runtime.Scheduler.Preference(request.Mode)
}
func (s *SyncService) Trigger(ctx context.Context, request TriggerSyncRequest) (domain.SyncResult, error) {
	return s.Runtime.Scheduler.Trigger(ctx, request.AccountID, request.Reason)
}
