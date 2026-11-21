package app

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/store"
)

type downloadTestProvider struct {
	db      *store.DB
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (p *downloadTestProvider) Capabilities() domain.Capabilities {
	return domain.Capabilities{Read: true}
}
func (p *downloadTestProvider) Flags(context.Context, string, *bool, *bool) error { return nil }
func (p *downloadTestProvider) Sync(ctx context.Context) (domain.SyncResult, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call == 1 {
		close(p.started)
		select {
		case <-p.release:
		case <-ctx.Done():
			return domain.SyncResult{}, ctx.Err()
		}
		// Models a pass which already finished its recovery work before a
		// concurrent retry request marked more records eligible.
		return domain.SyncResult{}, nil
	}
	ready, err := p.db.ReadyMessageFailures(ctx, "a", time.Now().Unix(), 10)
	if err != nil {
		return domain.SyncResult{}, err
	}
	snapshot := store.Snapshot{AccountID: "a"}
	for _, f := range ready {
		snapshot.RecoveredRemoteIDs = append(snapshot.RecoveredRemoteIDs, f.RemoteID)
	}
	return domain.SyncResult{Count: len(ready)}, p.db.CommitSnapshot(ctx, snapshot)
}

func TestRetryDownloadsHandlesAnAlreadyFinishingSharedPass(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SaveAccount(t.Context(), domain.Account{ID: "a", Email: "a", Provider: "gmail"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.CommitSnapshot(t.Context(), store.Snapshot{AccountID: "a", Failures: []store.MessageFailure{{RemoteID: "m", Kind: "invalid_content"}}}); err != nil {
		t.Fatal(err)
	}
	r := New(t.Context(), db, nil, nil)
	defer r.Scheduler.Close()
	p := &downloadTestProvider{db: db, started: make(chan struct{}), release: make(chan struct{})}
	r.InstallProvider("a", p)
	s := &SyncService{Runtime: r}
	first := make(chan error, 1)
	retry := make(chan error, 1)
	go func() { _, err := s.Sync(t.Context(), AccountRequest{AccountID: "a"}); first <- err }()
	select {
	case <-p.started:
	case <-time.After(3 * time.Second):
		t.Fatal("pass did not start")
	}
	go func() { _, err := s.RetryDownloads(t.Context(), AccountRequest{AccountID: "a"}); retry <- err }()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		f, err := db.MessageFailure(t.Context(), "a", "m")
		if err != nil {
			t.Fatal(err)
		}
		if f.NextRetryAt == 0 {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("retry eligibility did not persist")
		}
	}
	close(p.release)
	for _, result := range []chan error{first, retry} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("retry did not finish")
		}
	}
	page, err := s.DownloadFailures(t.Context(), DownloadFailuresRequest{AccountID: "a", Limit: 20})
	p.mu.Lock()
	calls := p.calls
	p.mu.Unlock()
	if err != nil || page.Total != 0 || calls != 2 {
		t.Fatal("manual retry lost behind shared pass", page, calls, err)
	}
}

func TestDownloadFailureServicePaginationAndDisconnectedAccount(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, id := range []string{"a", "b"} {
		if err := db.SaveAccount(t.Context(), domain.Account{ID: id, Email: id, Provider: "gmail"}, nil); err != nil {
			t.Fatal(err)
		}
		if err := db.CommitSnapshot(t.Context(), store.Snapshot{AccountID: id, Failures: []store.MessageFailure{{RemoteID: "m", Kind: "too_large"}}}); err != nil {
			t.Fatal(err)
		}
	}
	r := New(t.Context(), db, nil, nil)
	defer r.Scheduler.Close()
	s := &SyncService{Runtime: r}
	page, err := s.DownloadFailures(t.Context(), DownloadFailuresRequest{Limit: 1, Offset: 1})
	if err != nil || page.Total != 2 || len(page.Items) != 1 {
		t.Fatal("pagination", page, err)
	}
	page, err = s.DownloadFailures(t.Context(), DownloadFailuresRequest{AccountID: "a"})
	if err != nil || page.Total != 1 || page.Items[0].AccountID != "a" {
		t.Fatal("account filter", page, err)
	}
	for _, request := range []DownloadFailuresRequest{{Limit: 101}, {Limit: -1}, {Offset: -1}} {
		if _, err := s.DownloadFailures(t.Context(), request); err == nil {
			t.Fatal("invalid pagination accepted", request)
		}
	}
	if _, err := s.RetryDownloads(t.Context(), AccountRequest{AccountID: "b"}); err == nil {
		t.Fatal("disconnected account pretended to retry")
	}
	f, err := db.MessageFailure(t.Context(), "b", "m")
	if err != nil || f.Attempts != 1 || f.NextRetryAt == 0 {
		t.Fatal("disconnected account lost failure", f, err)
	}
}
