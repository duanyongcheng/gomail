package app

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
)

type idleFixtureProvider struct {
	scheduledProvider
	watchMu         sync.Mutex
	started, active int
	notify          func()
}

func (p *idleFixtureProvider) Watch(ctx context.Context, notify func()) error {
	p.watchMu.Lock()
	p.started++
	p.active++
	p.notify = notify
	p.watchMu.Unlock()
	defer func() { p.watchMu.Lock(); p.active--; p.watchMu.Unlock() }()
	<-ctx.Done()
	return ctx.Err()
}
func TestSchedulerOwnsIDLEManualLifecycleAndCoalescing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := New(t.Context(), nil, nil, nil)
		defer r.Scheduler.Close()
		p := &idleFixtureProvider{}
		r.InstallProvider("a", p)
		if err := r.Scheduler.Preference("manual"); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Scheduler.Trigger(t.Context(), "a", "manual"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		p.watchMu.Lock()
		started := p.started
		p.watchMu.Unlock()
		if started != 0 {
			t.Fatal("manual mode opened IDLE")
		}
		if err := r.Scheduler.Preference("realtime"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		p.watchMu.Lock()
		started = p.started
		notify := p.notify
		p.watchMu.Unlock()
		if started != 1 || notify == nil {
			t.Fatal("IDLE did not start once", started)
		}
		before := p.count()
		// Keep the first notified sync in flight. Otherwise this immediate fake
		// can finish between callbacks, making subsequent notifications new work
		// rather than concurrent signals that the scheduler should coalesce.
		block := make(chan struct{})
		p.mu.Lock()
		p.block = block
		p.mu.Unlock()
		notify()
		synctest.Wait()
		if p.count() != before+1 {
			t.Fatal("first IDLE notification did not start sync", before, p.count())
		}
		for range 19 {
			notify()
		}
		synctest.Wait()
		p.mu.Lock()
		active := p.scheduledProvider.active
		p.mu.Unlock()
		if p.count() != before+1 || active != 1 {
			t.Fatal("IDLE notifications started concurrent work", before, p.count(), active)
		}
		close(block)
		synctest.Wait()
		if p.count() != before+1 {
			t.Fatal("IDLE notifications did not coalesce", before, p.count())
		}
		r.Scheduler.Stop("a")
		p.watchMu.Lock()
		active = p.active
		p.watchMu.Unlock()
		if active != 0 {
			t.Fatal("Stop returned before watcher joined")
		}
		if err := r.Scheduler.Preference("manual"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		p.watchMu.Lock()
		started = p.started
		p.watchMu.Unlock()
		if started != 1 {
			t.Fatal("manual transition restarted watcher")
		}
	})
}
