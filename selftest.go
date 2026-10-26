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
	account := domain.Account{ID: "selftest-gmail", Email: "demo@example.invalid", DisplayName: "Selftest Gmail", Provider: "gmail", CreatedAt: 1, UpdatedAt: 1}
	if err := runtime.DB.SaveAccount(runtime.Context, account, nil); err != nil {
		return err
	}
	runtime.InstallProvider(account.ID, gmail.New(runtime.DB, fixture.API(selftestToken{}), account.ID))
	_, err := (&app.SyncService{Runtime: runtime}).Sync(runtime.Context, app.AccountRequest{AccountID: account.ID})
	return err
}
func runSelftest(ctx context.Context, runtime *app.Runtime, window *mygo.Window) {
	checks := map[string]bool{"react inbox rendered": false, "account restored from store": false, "typed provider and search": false, "opaque cursor preserved": false}
	defer func() {
		directory := os.Getenv("PEBBLE_SELFTEST_DIR")
		if directory == "" {
			directory = filepath.Join(filepath.Dir(runtime.DB.Path()), "selftest")
		}
		_ = os.MkdirAll(directory, 0700)
		data, _ := json.MarshalIndent(checks, "", "  ")
		_ = os.WriteFile(filepath.Join(directory, "runtime.json"), data, 0600)
		if screenshot, err := window.CapturePage(); err == nil {
			_ = os.WriteFile(filepath.Join(directory, "inbox.png"), screenshot, 0600)
		}
		mygo.App.Quit()
	}()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-ticker.C:
			evalCtx, cancel := context.WithTimeout(ctx, time.Second)
			value, err := window.Page().EvalContext(evalCtx, `document.body.innerText.includes('Pebble Go selftest')`)
			cancel()
			if err == nil && value == true {
				checks["react inbox rendered"] = true
				goto ready
			}
		}
	}
ready:
	accounts, err := runtime.DB.Accounts(ctx)
	checks["account restored from store"] = err == nil && len(accounts) > 0
	hits, err := runtime.DB.Search(ctx, "中文搜索", "selftest-gmail", 10)
	checks["typed provider and search"] = err == nil && len(hits) == 1
	cursor, err := runtime.DB.Cursor(ctx, "selftest-gmail")
	checks["opaque cursor preserved"] = err == nil && cursor == "900719925474099312345"
}
