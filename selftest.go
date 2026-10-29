package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bary/pebble-go/internal/app"
	"github.com/bary/pebble-go/internal/crypto"
	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/mailmime"
	"github.com/bary/pebble-go/internal/provider/gmail"
	"github.com/bary/pebble-go/internal/store"
	"github.com/bary/pebble-go/internal/testgmail"
	"github.com/egoist/mygo"
	"github.com/emersion/go-message/mail"
)

var selftestMailbox *testgmail.Mock

type selftestToken struct{}

func (selftestToken) Token(context.Context) (string, error) { return "demo-access", nil }

type selftestKeyring struct{ key []byte }

func (k *selftestKeyring) Get(string) ([]byte, error) {
	if k.key == nil {
		return nil, crypto.ErrKeyNotFound
	}
	return k.key, nil
}
func (k *selftestKeyring) Set(_ string, key []byte) error { k.key = key; return nil }

func prepareSelftest(runtime *app.Runtime) error {
	runtime.Keyring = &selftestKeyring{}
	fixture := testgmail.NewMock()
	fixture.ProfileEmail = "demo@example.invalid"
	selftestMailbox = fixture
	context.AfterFunc(runtime.Context, fixture.Close)
	png := []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 31, 21, 196, 137, 0, 0, 0, 13, 73, 68, 65, 84, 120, 156, 99, 248, 207, 192, 240, 31, 0, 5, 0, 1, 255, 137, 153, 61, 29, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}
	raw, err := mailmime.Build("sender@example.invalid", "Pebble Go selftest", "中文搜索 selftest message", `<p>中文搜索 selftest message</p><img src="cid:logo">`, "selftest@example.invalid", []*mail.Address{{Address: "demo@example.invalid"}}, nil, []mailmime.Attachment{{ContentID: "logo", Inline: true, MIMEType: "image/png", Data: png}, {Filename: "selftest-export.txt", MIMEType: "text/plain", Data: []byte("Pebble Go attachment export selftest\n")}})
	if err != nil {
		return err
	}
	fixture.Set("m1", raw, []string{"INBOX", "UNREAD"})
	fixture.Set("m2", []byte("malformed local selftest message"), []string{"INBOX", "UNREAD"})
	// A second inbox message is the durable-write subject: archiving it must not
	// remove the message the UI assertions below inspect.
	queueRaw, err := mailmime.Build("sender@example.invalid", "Queued remote write", "queued write body", "", "queued@example.invalid", []*mail.Address{{Address: "demo@example.invalid"}}, nil, nil)
	if err != nil {
		return err
	}
	fixture.Set("m3", queueRaw, []string{"INBOX", "UNREAD"})
	fixture.Set("m4", []byte("From: second@example.invalid\r\nTo: demo@example.invalid\r\nMessage-ID: <thread-child@example.invalid>\r\nIn-Reply-To: <selftest@example.invalid>\r\nReferences: <selftest@example.invalid>\r\nSubject: Re: Pebble Go selftest\r\n\r\nThread selftest reply"), []string{"INBOX", "UNREAD"})
	fixture.Lock()
	threadChild := fixture.Messages["m4"]
	threadChild.ThreadID = fixture.Messages["m1"].ThreadID
	fixture.Messages["m4"] = threadChild
	fixture.Unlock()
	account := domain.Account{ID: "selftest-gmail", Email: "demo@example.invalid", DisplayName: "Selftest Gmail", Provider: "gmail", CreatedAt: 1, UpdatedAt: 1}
	if err := runtime.DB.SaveAccount(runtime.Context, account, nil); err != nil {
		return err
	}
	runtime.InstallProvider(account.ID, gmail.New(runtime.DB, fixture.API(selftestToken{}), account.ID))
	if _, err := (&app.SyncService{Runtime: runtime}).Sync(runtime.Context, app.AccountRequest{AccountID: account.ID}); err != nil {
		return err
	}
	// Queue a real offline-style write. The packaged run replays it through the
	// durable worker instead of relying on a synchronous provider call.
	if _, err := (&app.MessageService{Runtime: runtime}).Archive(runtime.Context, app.ArchiveRequest{MessageID: account.ID + ":m3"}); err != nil {
		return err
	}
	inInbox, err := runtime.DB.MessageInFolderRemote(runtime.Context, account.ID+":m3", "INBOX")
	if err != nil || inInbox {
		return errors.New("selftest archive did not take effect locally")
	}
	// Leave a durable failure visible in settings, but repair its remote content
	// so the explicit UI retry can exercise recovery through generated bindings.
	fixture.Set("m2", []byte("From: sender@example.invalid\r\nSubject: Recovered download selftest\r\n\r\nRecovered message"), []string{"INBOX", "UNREAD"})
	compose := &app.ComposeService{Runtime: runtime}
	attachment, err := compose.Stage(runtime.Context, app.StageComposeRequest{Filename: "draft-selftest.txt", Bytes: []int{100, 114, 97, 102, 116}})
	if err != nil {
		return err
	}
	if _, err := compose.Save(runtime.Context, app.SaveDraftRequest{AccountID: account.ID, ClientKey: "selftest-draft", Subject: "Local draft selftest", BodyText: "Restored local draft", BodyHTML: "<p>Restored local draft</p>", EditorMode: "rich", AttachmentPaths: []string{attachment}}); err != nil {
		return err
	}
	_, err = compose.Cleanup(runtime.Context, app.CleanupComposeRequest{Path: attachment})
	return err
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

func pendingQueueDrained(ctx context.Context, runtime *app.Runtime) bool {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		summary, err := runtime.DB.PendingMailOpsSummary(ctx, "selftest-gmail")
		if err == nil && summary.TotalActiveCount == 0 {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
		}
	}
}

func runSelftest(ctx context.Context, runtime *app.Runtime, window *mygo.Window) {
	checks := map[string]bool{"react inbox rendered": false, "account restored from store": false, "typed provider and search": false, "opaque cursor preserved": false, "cid attachment rendered": false, "queued remote write replayed": false, "download failures rendered": false, "failed download retried through UI": false, "attachment exported through UI": false, "archive survives authoritative sync": false, "restore and archive preserve order": false, "local draft restored through UI": false, "local draft saved through UI": false, "Gmail send through durable outbox UI": false, "thread conversation rendered": false, "Gmail labels managed through UI": false, "sender trust persisted and revoked through UI": false}
	directory := os.Getenv("PEBBLE_SELFTEST_DIR")
	if directory == "" {
		directory = filepath.Join(filepath.Dir(runtime.DB.Path()), "selftest")
	}
	_ = os.MkdirAll(directory, 0700)
	exportDirectory := filepath.Join(directory, "exports")
	mygo.App.SetPath(mygo.PathDownloads, exportDirectory)
	defer mygo.App.SetPath(mygo.PathDownloads, "")
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
	if waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelectorAll('button')).find(b => /^(会话视图|会话模式|Thread view)$/.test(b.getAttribute('aria-label') || '')); if (!b) return false; b.click(); return true; })()`, 5*time.Second) {
		if waitSelftestPage(ctx, window, `(() => { const row=Array.from(document.querySelectorAll('[role="option"]')).find(r=>r.textContent.includes('Pebble Go selftest')); if(!row)return false;row.click();return true; })()`, 5*time.Second) {
			checks["thread conversation rendered"] = waitSelftestPage(ctx, window, `document.querySelectorAll('.thread-message-scroll button[aria-expanded]').length === 2 && document.querySelector('.thread-message-scroll')?.innerText.includes('second@example.invalid')`, 10*time.Second)
			capture("thread.png")
		}
		waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelectorAll('button')).find(b => /^(邮件视图|Message view)$/.test(b.getAttribute('aria-label') || '')); if (!b) return false; b.click(); return true; })()`, 5*time.Second)
	}
	accounts, err := runtime.DB.Accounts(ctx)
	checks["account restored from store"] = err == nil && len(accounts) > 0
	hits, err := runtime.DB.Search(ctx, "中文搜索", "selftest-gmail", 10)
	checks["typed provider and search"] = err == nil && len(hits) == 1
	cursor, err := runtime.DB.Cursor(ctx, "selftest-gmail")
	checks["opaque cursor preserved"] = err == nil && cursor == "900719925474099312345"
	privacyMode := json.RawMessage(`"Strict"`)
	rendered, renderErr := (&app.MessageService{Runtime: runtime}).Rendered(ctx, app.MessageRequest{MessageID: "selftest-gmail:m1", PrivacyMode: privacyMode})
	checks["cid attachment rendered"] = renderErr == nil && strings.Contains(rendered.Rendered.HTML, "pebble-attachment://localhost/")
	checks["queued remote write replayed"] = pendingQueueDrained(ctx, runtime)
	// Fetch m3 authoritatively so a drained-but-incorrect queue cannot pass.
	service := &app.MessageService{Runtime: runtime}
	request := app.ArchiveRequest{MessageID: "selftest-gmail:m3"}
	first, firstErr := service.Archive(ctx, request)
	second, secondErr := service.Archive(ctx, request)
	checks["restore and archive preserve order"] = firstErr == nil && secondErr == nil && first == "unarchived" && second == "archived" && pendingQueueDrained(ctx, runtime)
	// Full backfill includes m3; the default mock History only lists m1/m2.
	cursor, cursorErr := runtime.DB.Cursor(ctx, "selftest-gmail")
	checkpoint := &store.GmailCheckpoint{Phase: "messages", HistoryID: cursor, SuppressNotifications: true}
	checkpointErr := runtime.DB.CommitSnapshot(ctx, store.Snapshot{AccountID: "selftest-gmail", Checkpoint: checkpoint, ResetBackfill: true})
	_, syncErr := (&app.SyncService{Runtime: runtime}).Sync(ctx, app.AccountRequest{AccountID: "selftest-gmail"})
	inInbox, membershipErr := runtime.DB.MessageInFolderRemote(ctx, "selftest-gmail:m3", "INBOX")
	checks["archive survives authoritative sync"] = cursorErr == nil && checkpointErr == nil && syncErr == nil && membershipErr == nil && !inInbox
	selected := waitSelftestPage(ctx, window, `(() => { const row = Array.from(document.querySelectorAll('[role="option"]')).find(row => row.textContent.includes('Pebble Go selftest')); if (!row) return false; row.click(); return true; })()`, 5*time.Second)
	if selected && waitSelftestPage(ctx, window, `(() => { const button = Array.from(document.querySelectorAll('button')).find(button => /^(下载|Download): selftest-export.txt$/.test(button.getAttribute('aria-label') || '')); if (!button || button.disabled) return false; button.click(); return true; })()`, 10*time.Second) {
		saved := waitSelftestPage(ctx, window, `Array.from(document.querySelectorAll('.attachment-list-download')).some(button => button.title.includes('exports') && button.title.includes('selftest-export.txt'))`, 10*time.Second)
		content, err := os.ReadFile(filepath.Join(exportDirectory, "selftest-export.txt"))
		checks["attachment exported through UI"] = saved && err == nil && string(content) == "Pebble Go attachment export selftest\n"
		capture("attachment-export.png")
	}
	if !waitSelftestPage(ctx, window, `(() => { const button = Array.from(document.querySelectorAll('button')).find(button => /^(设置|Settings)$/.test(button.textContent.trim())); if (!button) return false; button.click(); return true; })()`, 5*time.Second) {
		return
	}
	checks["download failures rendered"] = waitSelftestPage(ctx, window, `(() => { const section = document.getElementById('mail-download-failures-title')?.closest('section'); return Boolean(section) && /1 封邮件等待下载|1 messages? waiting for download/.test(section.innerText); })()`, 10*time.Second)
	if !checks["download failures rendered"] {
		return
	}
	capture("download-failure.png")
	checks["Gmail labels managed through UI"] = selftestLabels(ctx, runtime, window)
	capture("gmail-labels.png")
	if !waitSelftestPage(ctx, window, `(() => { const button = document.getElementById('mail-download-failures-title')?.closest('section')?.querySelector('button'); if (!button || button.disabled) return false; button.click(); return true; })()`, 5*time.Second) {
		return
	}
	empty := waitSelftestPage(ctx, window, `/没有下载失败的邮件|No failed downloads/.test(document.body.innerText)`, 10*time.Second)
	page, err := runtime.DB.DownloadFailures(ctx, "selftest-gmail", 20, 0)
	checks["failed download retried through UI"] = empty && err == nil && page.Total == 0
	// Clear the selected message via the mailbox row so the compose FAB appears.
	if !waitSelftestPage(ctx, window, `(() => { const button = Array.from(document.querySelectorAll('button')).find(b => /^(收件箱|Inbox)/.test(b.textContent.trim())); if (!button) return false; button.click(); return true; })()`, 5*time.Second) {
		return
	}
	if !waitSelftestPage(ctx, window, `(() => { const button = Array.from(document.querySelectorAll('button')).find(b => /^(撰写|写邮件|写信|Compose)$/.test(b.getAttribute('aria-label') || '')); if (!button) return false; button.click(); return true; })()`, 5*time.Second) {
		return
	}
	if !waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelectorAll('button')).find(b => /^(本地草稿|Local drafts)$/.test(b.textContent.trim())); if (!b) return false; b.click(); return true; })()`, 10*time.Second) {
		return
	}
	if !waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelectorAll('button')).find(b => b.textContent.trim()==='Local draft selftest'); if (!b || b.disabled) return false; b.click(); return true; })()`, 10*time.Second) {
		return
	}
	checks["local draft restored through UI"] = waitSelftestPage(ctx, window, `document.getElementById('compose-subject')?.value === 'Local draft selftest' && document.body.innerText.includes('draft-selftest.txt')`, 10*time.Second)
	capture("local-draft.png")
	if !waitSelftestPage(ctx, window, `(() => { const input=document.getElementById('compose-subject'); if (!input) return false; Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,'Edited local draft selftest'); input.dispatchEvent(new Event('input',{bubbles:true})); return true; })()`, 5*time.Second) {
		return
	}
	if !waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelectorAll('button')).find(b => /^(保存并关闭|Save and close)$/.test(b.textContent.trim())); if (!b || b.disabled) return false; b.click(); return true; })()`, 5*time.Second) {
		return
	}
	closed := waitSelftestPage(ctx, window, `document.getElementById('compose-subject') === null`, 15*time.Second)
	drafts, draftErr := (&app.ComposeService{Runtime: runtime}).List(ctx, app.AccountRequest{AccountID: "selftest-gmail"})
	checks["local draft saved through UI"] = closed && draftErr == nil && len(drafts) == 1 && drafts[0].Subject == "Edited local draft selftest" && drafts[0].AttachmentCount == 1
	if !waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelectorAll('button')).find(b => /^(撰写|Compose)$/.test(b.getAttribute('aria-label') || '')); if (!b) return false; b.click(); return true; })()`, 5*time.Second) {
		return
	}
	if !waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelectorAll('button')).find(b => /^(本地草稿|Local drafts)$/.test(b.textContent.trim())); if (!b) return false; b.click(); return true; })()`, 10*time.Second) {
		return
	}
	if !waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelectorAll('button')).find(b => b.textContent.trim()==='Edited local draft selftest'); if (!b || b.disabled) return false; b.click(); return true; })()`, 10*time.Second) {
		return
	}
	if !waitSelftestPage(ctx, window, `(() => { const input=document.getElementById('compose-to-input'); if (!input) return false; Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,'recipient@example.invalid'); input.dispatchEvent(new Event('input',{bubbles:true})); return true; })()`, 5*time.Second) {
		return
	}
	if !waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelectorAll('button')).find(b => /^(发送|Send)$/.test(b.textContent.trim())); if (!b || b.disabled) return false; b.click(); return true; })()`, 5*time.Second) {
		return
	}
	sentClosed := waitSelftestPage(ctx, window, `document.getElementById('compose-subject') === null`, 10*time.Second)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		items, err := (&app.OutboxService{Runtime: runtime}).List(ctx, app.AccountRequest{AccountID: "selftest-gmail"})
		if err == nil && len(items) == 1 && items[0].Status == "sent" {
			selftestMailbox.Lock()
			count := len(selftestMailbox.SentRaw)
			var raw []byte
			if count == 1 {
				raw = append([]byte{}, selftestMailbox.SentRaw[0]...)
			}
			selftestMailbox.Unlock()
			parsed, parseErr := mailmime.ParseWithLimit(raw, mailmime.MaxOutgoingBytes)
			checks["Gmail send through durable outbox UI"] = sentClosed && count == 1 && parseErr == nil && parsed.Subject == "Edited local draft selftest" && len(parsed.Attachments) == 1
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	if waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelectorAll('button')).find(b => /^(设置|Settings)$/.test(b.textContent.trim())); if (!b) return false; b.click(); return true; })()`, 5*time.Second) &&
		waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelectorAll('[role="tab"]')).find(b => /^(远端写回|Remote Writes)$/.test(b.textContent.trim())); if (!b) return false; b.click(); return true; })()`, 5*time.Second) {
		waitSelftestPage(ctx, window, `(() => { const s=Array.from(document.querySelectorAll('summary')).find(s => /发件箱|Outbox/.test(s.textContent)); if (!s) return false; s.parentElement.open=true; return /Gmail 已接收|Accepted by Gmail/.test(s.parentElement.innerText); })()`, 10*time.Second)
		capture("outbox-sent.png")
	}
	checks["sender trust persisted and revoked through UI"] = selftestTrust(ctx, runtime, window)
	capture("trusted-senders.png")
}
