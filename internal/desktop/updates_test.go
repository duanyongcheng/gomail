package desktop

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/duanyongcheng/gomail/internal/app"
)

type fakeNativeUpdate struct {
	version string
	notes   string
	date    time.Time
	install func(ctx context.Context, progress func(downloaded, total int64)) error
}

func (f fakeNativeUpdate) Version() string { return f.version }
func (f fakeNativeUpdate) Notes() string   { return f.notes }
func (f fakeNativeUpdate) Date() time.Time { return f.date }
func (f fakeNativeUpdate) Install(ctx context.Context, progress func(downloaded, total int64)) error {
	if f.install != nil {
		return f.install(ctx, progress)
	}
	return nil
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.2.0", "0.1.0", 1},
		{"v0.2.10", "v0.2.2", 1},
		{"0.2.0", "v0.2.0", 0},
		{"0.2.0-rc.1", "0.2.0", -1},
		{"0.2.0", "0.2.0-rc.1", 1},
		{"0.1.9", "0.2.0", -1},
		{"1.0", "1.0.0", 0},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestUpdateServiceNativeSignedUpdateAndProgressFlow(t *testing.T) {
	var (
		mu         sync.Mutex
		statuses   []UpdateStatus
		progresses []UpdateProgress
		relaunched = make(chan struct{}, 1)
	)
	svc := &UpdateService{
		ctx:            context.Background(),
		repo:           "duanyongcheng/gomail",
		currentVersion: func() string { return "0.2.0" },
		canAutoUpdate:  func() bool { return true },
		checkNative: func(context.Context) (NativeUpdate, error) {
			return fakeNativeUpdate{
				version: "0.2.1",
				notes:   "Delta update test release",
				date:    time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC),
				install: func(_ context.Context, progress func(downloaded, total int64)) error {
					progress(250, 1000)
					progress(1000, 1000)
					return nil
				},
			}, nil
		},
		relaunchApp: func() {
			relaunched <- struct{}{}
		},
	}
	svc.OnStatus = func(st UpdateStatus) {
		mu.Lock()
		statuses = append(statuses, st)
		mu.Unlock()
	}
	svc.OnProgress = func(p UpdateProgress) {
		mu.Lock()
		progresses = append(progresses, p)
		mu.Unlock()
	}

	checked, err := svc.Check(app.Empty{})
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if checked.State != "available" || !checked.CanAutoUpdate || checked.AvailableVersion != "0.2.1" {
		t.Fatalf("unexpected check status: %+v", checked)
	}
	if checked.ReleaseURL != "https://github.com/duanyongcheng/gomail/releases/tag/v0.2.1" {
		t.Fatalf("unexpected release url: %q", checked.ReleaseURL)
	}

	// Relaunch before install must fail cleanly.
	if _, err := svc.Relaunch(app.Empty{}); err == nil {
		t.Fatal("expected Relaunch before Install to fail")
	}

	installed, err := svc.Install(app.Empty{})
	if err != nil {
		t.Fatalf("Install failed: %v", err)
	}
	if installed.State != "installed" || installed.InstalledVersion != "0.2.1" || installed.Percent != 100 {
		t.Fatalf("unexpected installed status: %+v", installed)
	}

	if _, err := svc.Relaunch(app.Empty{}); err != nil {
		t.Fatalf("Relaunch failed: %v", err)
	}
	select {
	case <-relaunched:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for relaunch callback")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(progresses) < 3 {
		t.Fatalf("expected progress events, got %+v", progresses)
	}
	if len(statuses) < 4 {
		t.Fatalf("expected status transitions, got %+v", statuses)
	}
}

func TestUpdateServiceFallsBackToGitHubWhenSelfUpdateDisabledOrManifestMissing(t *testing.T) {
	svc := &UpdateService{
		ctx:            context.Background(),
		repo:           "duanyongcheng/gomail",
		currentVersion: func() string { return "0.2.0" },
		canAutoUpdate:  func() bool { return true },
		checkNative: func(context.Context) (NativeUpdate, error) {
			return nil, errors.New("GET https://github.com/.../update-darwin-arm64.json: 404 Not Found")
		},
		checkGitHub: func(_ context.Context, _ string, _ string) (*GitHubReleaseInfo, error) {
			return &GitHubReleaseInfo{
				Version: "0.2.1",
				Notes:   "Manual release fallback",
				URL:     "https://evil.example/phish",
			}, nil
		},
	}

	st, err := svc.Check(app.Empty{})
	if err != nil {
		t.Fatalf("Check fallback failed: %v", err)
	}
	if st.State != "available" || st.CanAutoUpdate || st.AvailableVersion != "0.2.1" {
		t.Fatalf("unexpected fallback status: %+v", st)
	}
	if st.ReleaseURL != "https://github.com/duanyongcheng/gomail/releases/tag/v0.2.1" {
		t.Fatalf("expected untrusted URL to be sanitized, got %q", st.ReleaseURL)
	}
	if _, err := svc.Install(app.Empty{}); err == nil {
		t.Fatal("Install must fail when only manual fallback update is available")
	}
}

func TestUpdateServiceInstallErrorPreservesPendingForRetry(t *testing.T) {
	attempts := 0
	svc := &UpdateService{
		ctx:            context.Background(),
		repo:           "duanyongcheng/gomail",
		currentVersion: func() string { return "0.2.0" },
		canAutoUpdate:  func() bool { return true },
		checkNative: func(context.Context) (NativeUpdate, error) {
			return fakeNativeUpdate{
				version: "0.2.1",
				install: func(context.Context, func(int64, int64)) error {
					attempts++
					if attempts == 1 {
						return errors.New("transient secret token error")
					}
					return nil
				},
			}, nil
		},
	}

	if _, err := svc.Check(app.Empty{}); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	failed, err := svc.Install(app.Empty{})
	if err == nil {
		t.Fatal("expected first install attempt to fail")
	}
	if failed.State != "error" || failed.Error == "" || failed.Error == "transient secret token error" {
		t.Fatalf("expected sanitized error state, got %+v", failed)
	}
	// Retry must succeed using the preserved pending update.
	retried, err := svc.Install(app.Empty{})
	if err != nil {
		t.Fatalf("retry Install failed: %v", err)
	}
	if retried.State != "installed" || retried.InstalledVersion != "0.2.1" {
		t.Fatalf("unexpected retry status: %+v", retried)
	}
}

func TestUpdateServiceDisabledInSelftestNeverTouchesNetwork(t *testing.T) {
	called := false
	svc := &UpdateService{
		ctx:            context.Background(),
		repo:           "duanyongcheng/gomail",
		disabled:       true,
		currentVersion: func() string { return "0.2.0" },
		canAutoUpdate:  func() bool { return true },
		checkNative: func(context.Context) (NativeUpdate, error) {
			called = true
			return nil, nil
		},
		checkGitHub: func(context.Context, string, string) (*GitHubReleaseInfo, error) {
			called = true
			return nil, nil
		},
	}

	st, err := svc.Check(app.Empty{})
	if err != nil {
		t.Fatalf("disabled Check returned error: %v", err)
	}
	if called {
		t.Fatal("disabled UpdateService must not perform network calls")
	}
	if st.CanAutoUpdate || st.State != "up_to_date" {
		t.Fatalf("unexpected disabled status: %+v", st)
	}
}

func TestFetchLatestGitHubReleaseFromURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.1","body":"Release notes","html_url":"https://github.com/duanyongcheng/gomail/releases/tag/v0.2.1","published_at":"2026-11-22T08:00:00Z","draft":false,"prerelease":false}`))
	}))
	defer srv.Close()

	info, err := fetchLatestGitHubReleaseFromURL(context.Background(), srv.URL, "duanyongcheng/gomail", "0.2.0", srv.Client())
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if info == nil || info.Version != "0.2.1" || info.Notes != "Release notes" {
		t.Fatalf("unexpected release info: %+v", info)
	}

	same, err := fetchLatestGitHubReleaseFromURL(context.Background(), srv.URL, "duanyongcheng/gomail", "0.2.1", srv.Client())
	if err != nil {
		t.Fatalf("same-version fetch failed: %v", err)
	}
	if same != nil {
		t.Fatalf("expected nil when already up to date, got %+v", same)
	}
}
