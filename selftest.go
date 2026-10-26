package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/bary/pebble-go/internal/app"
	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/provider/gmail"
	"github.com/bary/pebble-go/internal/testgmail"
	"github.com/egoist/mygo"
)

type selftestToken struct{}

func (selftestToken) Token(context.Context) (string, error) { return "demo-access", nil }

func prepareSelftest(runtime *app.Runtime) error {
	fixture := testgmail.NewMock()
	context.AfterFunc(runtime.Context, fixture.Close)
	raw := []byte("From: sender@example.invalid\r\nTo: demo@example.invalid\r\nSubject: Pebble Go selftest\r\nMessage-ID: <selftest@example.invalid>\r\nDate: Tue, 14 Nov 2023 22:13:20 +0000\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n中文搜索 selftest message")
	fixture.Set("m1", raw, []string{"INBOX", "UNREAD"})
	fixture.Set("m2", []byte("malformed local selftest message"), []string{"INBOX", "UNREAD"})
	account := domain.Account{ID: "selftest-gmail", Email: "demo@example.invalid", DisplayName: "Selftest Gmail", Provider: "gmail", CreatedAt: 1, UpdatedAt: 1}
	if err := runtime.DB.SaveAccount(runtime.Context, account, nil); err != nil {
		return err
	}
	runtime.InstallProvider(account.ID, gmail.New(runtime.DB, fixture.API(selftestToken{}), account.ID))
	if _, err := (&app.SyncService{Runtime: runtime}).Sync(runtime.Context, app.AccountRequest{AccountID: account.ID}); err != nil {
		return err
	}
	// Leave a durable failure visible in settings, but repair its remote content
	// so the explicit UI retry can exercise recovery through generated bindings.
	fixture.Set("m2", []byte("From: sender@example.invalid\r\nSubject: Recovered download selftest\r\n\r\nRecovered message"), []string{"INBOX", "UNREAD"})
	return nil
}

func waitSelftestPage(ctx context.Context, window *mygo.Window, script string, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
			evalCtx, cancel := context.WithTimeout(ctx, time.Second)
			value, err := window.Page().EvalContext(evalCtx, script)
			cancel()
			if err == nil && value == true {
				return true
			}
		}
	}
}

func runSelftest(ctx context.Context, runtime *app.Runtime, window *mygo.Window) {
	checks := map[string]bool{"react inbox rendered": false, "account restored from store": false, "typed provider and search": false, "opaque cursor preserved": false, "download failures rendered": false, "failed download retried through UI": false}
	directory := os.Getenv("PEBBLE_SELFTEST_DIR")
	if directory == "" {
		directory = filepath.Join(filepath.Dir(runtime.DB.Path()), "selftest")
	}
	_ = os.MkdirAll(directory, 0700)
	capture := func(name string) {
		if screenshot, err := window.CapturePage(); err == nil {
			_ = os.WriteFile(filepath.Join(directory, name), screenshot, 0600)
		}
	}
	defer func() {
		data, _ := json.MarshalIndent(checks, "", "  ")
		_ = os.WriteFile(filepath.Join(directory, "runtime.json"), data, 0600)
		capture("downloads.png")
		mygo.App.Quit()
	}()
	checks["react inbox rendered"] = waitSelftestPage(ctx, window, `document.body.innerText.includes('Pebble Go selftest')`, 30*time.Second)
	if !checks["react inbox rendered"] {
		return
	}
	capture("inbox.png")
	accounts, err := runtime.DB.Accounts(ctx)
	checks["account restored from store"] = err == nil && len(accounts) > 0
	hits, err := runtime.DB.Search(ctx, "中文搜索", "selftest-gmail", 10)
	checks["typed provider and search"] = err == nil && len(hits) == 1
	cursor, err := runtime.DB.Cursor(ctx, "selftest-gmail")
	checks["opaque cursor preserved"] = err == nil && cursor == "900719925474099312345"
	if !waitSelftestPage(ctx, window, `(() => { const button = Array.from(document.querySelectorAll('button')).find(button => /^(设置|Settings)$/.test(button.textContent.trim())); if (!button) return false; button.click(); return true; })()`, 5*time.Second) {
		return
	}
	checks["download failures rendered"] = waitSelftestPage(ctx, window, `(() => { const section = document.getElementById('mail-download-failures-title')?.closest('section'); return Boolean(section) && /1 封邮件等待下载|1 messages? waiting for download/.test(section.innerText); })()`, 10*time.Second)
	if !checks["download failures rendered"] {
		return
	}
	capture("download-failure.png")
	if !waitSelftestPage(ctx, window, `(() => { const button = document.getElementById('mail-download-failures-title')?.closest('section')?.querySelector('button'); if (!button || button.disabled) return false; button.click(); return true; })()`, 5*time.Second) {
		return
	}
	empty := waitSelftestPage(ctx, window, `/没有下载失败的邮件|No failed downloads/.test(document.body.innerText)`, 10*time.Second)
	page, err := runtime.DB.DownloadFailures(ctx, "selftest-gmail", 20, 0)
	checks["failed download retried through UI"] = empty && err == nil && page.Total == 0
}
