package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/app"
	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/egoist/mygo"
	protocol "github.com/emersion/go-imap/v2"
)

var m2UISelftestChecks = []string{
	"M2 WebView entered TLS settings tested through binding",
	"M2 WebView protocol account added with encrypted credentials",
	"M2 WebView selected folders saved through settings",
	"M2 WebView manual sync honors selected folders",
	"M2 WebView opens IMAP message and persists read flag",
	"M2 WebView compose queues SMTP accepted without remote ID",
	"M2 WebView SMTP Bcc envelope stays absent from MIME",
}

const m2UIForm = `form#protocol-account-settings`

// Each edit dispatches one ordinary DOM event. Only readiness predicates poll;
// no services or bridge functions are invoked by injected JavaScript.
func m2UIEdit(ctx context.Context, window *mygo.Window, name, value string) bool {
	script := fmt.Sprintf(`(() => {
		const form=document.querySelector(%q);if(!form)return false;
		const input=form.elements.namedItem(%q);
		if(!input||input.disabled||input.closest('fieldset[disabled]'))return false;
		const proto=input instanceof HTMLSelectElement?HTMLSelectElement.prototype:input instanceof HTMLTextAreaElement?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;
		Object.getOwnPropertyDescriptor(proto,'value').set.call(input,%q);
		input.dispatchEvent(new Event(input instanceof HTMLSelectElement?'change':'input',{bubbles:true}));return true;
	})()`, m2UIForm, name, value)
	return offlineEvalOnce(ctx, window, script)
}

func m2UISelector(ctx context.Context, window *mygo.Window, selector, value string) bool {
	return offlineEvalOnce(ctx, window, fmt.Sprintf(`(() => {
		const input=document.querySelector(%q);if(!input||input.disabled)return false;
		Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype,'value').set.call(input,%q);
		input.dispatchEvent(new Event('change',{bubbles:true}));return true;
	})()`, selector, value))
}

func m2UIEndpoint(ctx context.Context, window *mygo.Window, name string, port int, root string) bool {
	if !m2UIEdit(ctx, window, name+"-authentication", "password") ||
		!waitSelftestPage(ctx, window, fmt.Sprintf(`Boolean(document.querySelector(%q)?.elements.namedItem(%q))`, m2UIForm, name+"-host"), 5*time.Second) {
		return false
	}
	for _, field := range [][2]string{{"host", "127.0.0.1"}, {"port", fmt.Sprint(port)}, {"username", "fixture@example.invalid"}, {"password", "synthetic-password"}} {
		if !m2UIEdit(ctx, window, name+"-"+field[0], field[1]) {
			return false
		}
	}
	if !offlineEvalOnce(ctx, window, fmt.Sprintf(`(() => {
		const field=Array.from(document.querySelectorAll(%q+' fieldset')).find(f=>f.querySelector(':scope > legend')?.textContent===%q);
		const box=field?.querySelector('input[type=checkbox]');if(!box||box.checked)return false;box.click();return true;
	})()`, m2UIForm, name)) || !waitSelftestPage(ctx, window, fmt.Sprintf(`Boolean(document.querySelector(%q)?.elements.namedItem(%q))`, m2UIForm, name+"-ca"), 5*time.Second) {
		return false
	}
	return m2UIEdit(ctx, window, name+"-ca", root)
}

// The supplied Runtime owns the window's real bindings and disposable profile.
// Reads below assert persisted effects; all account/sync/read/send actions use UI.
func selftestM2UI(ctx context.Context, runtime *app.Runtime, window *mygo.Window, checks map[string]bool) {
	defer func() {
		if directory := os.Getenv("PEBBLE_SELFTEST_DIR"); directory != "" {
			if image, err := window.CapturePage(); err == nil {
				_ = os.WriteFile(filepath.Join(directory, "m2-ui-stage.png"), image, 0600)
			}
			stages := make(map[string]bool, len(m2UISelftestChecks))
			for _, name := range m2UISelftestChecks {
				stages[name] = checks[name]
			}
			if data, err := json.MarshalIndent(stages, "", "  "); err == nil {
				_ = os.WriteFile(filepath.Join(directory, "m2-ui-stages.json"), data, 0600)
			}
		}
	}()
	fixture, err := newM2SelftestFixture()
	if err != nil {
		return
	}
	defer fixture.close()
	if !selftestClick(ctx, window, ".sidebar-nav--bottom", "^(设置|Settings)$") ||
		!selftestClick(ctx, window, "body", "^(账户|Accounts)$") ||
		!selftestClick(ctx, window, "body", "^(添加账户|Add Account)$") ||
		!m2UISelector(ctx, window, `select[name="account-provider"]`, "imap") ||
		!waitSelftestPage(ctx, window, fmt.Sprintf(`Boolean(document.querySelector(%q))`, m2UIForm), 5*time.Second) ||
		!m2UIEdit(ctx, window, "email", "fixture@example.invalid") ||
		!m2UIEdit(ctx, window, "display-name", "WebView synthetic IMAP") ||
		!m2UIEndpoint(ctx, window, "IMAP", fixture.imapPort, fixture.root) ||
		!m2UIEndpoint(ctx, window, "SMTP", fixture.smtpPort, fixture.root) ||
		!m2UIEdit(ctx, window, "selected-folders", "INBOX\nProjects") ||
		!m2UIEdit(ctx, window, "proxy-mode", "disabled") ||
		!selftestClick(ctx, window, m2UIForm, "^(测试当前设置|Test entered settings)$") {
		return
	}
	checks[m2UISelftestChecks[0]] = waitSelftestPage(ctx, window, fmt.Sprintf(`/^(连接验证通过|Connection verified)$/.test(document.querySelector(%q+' [role=status]')?.textContent||'')&&!document.querySelector(%q+' fieldset[disabled]')&&!document.querySelector(%q+' [role=alert]')`, m2UIForm, m2UIForm, m2UIForm), 10*time.Second) && fixture.connections.Load() >= 2
	if directory := os.Getenv("PEBBLE_SELFTEST_DIR"); directory != "" && checks[m2UISelftestChecks[0]] {
		if image, err := window.CapturePage(); err == nil {
			_ = os.WriteFile(filepath.Join(directory, "m2-ui-localized-settings.png"), image, 0600)
		}
	}
	if !checks[m2UISelftestChecks[0]] || !selftestClick(ctx, window, m2UIForm, "^(添加协议账户|Add protocol account)$") ||
		!waitSelftestPage(ctx, window, `!document.querySelector('[role=dialog]')`, 10*time.Second) {
		return
	}
	var account domain.Account
	accounts, err := runtime.DB.Accounts(ctx)
	if err != nil {
		return
	}
	for _, candidate := range accounts {
		if candidate.Email == "fixture@example.invalid" && candidate.Provider == "imap" {
			account = candidate
		}
	}
	if account.ID == "" {
		return
	}
	ciphertext, err := runtime.DB.AuthData(ctx, account.ID)
	checks[m2UISelftestChecks[1]] = err == nil && bytes.HasPrefix(ciphertext, []byte("PEBBLE\x00\x01")) && !bytes.Contains(ciphertext, []byte("synthetic-password"))
	// Locate the account's own settings card; never click another mailbox's edit.
	if !waitSelftestPage(ctx, window, fmt.Sprintf(`Boolean(document.querySelector('[data-account-settings-id="%s"]'))`, account.ID), 10*time.Second) || !offlineEvalOnce(ctx, window, fmt.Sprintf(`(() => {
		const card=document.querySelector('[data-account-settings-id="%s"]');
		const button=Array.from(card?.querySelectorAll('button')||[]).find(b=>/^(编辑账户|编辑账号|Edit account|Edit Account)$/.test(b.getAttribute('aria-label')||''));if(!button||button.disabled)return false;button.click();return true;
	})()`, account.ID)) || !waitSelftestPage(ctx, window, fmt.Sprintf(`Boolean(document.querySelector(%q+' input[type=email][disabled]'))&&!document.querySelector(%q+' fieldset[disabled]')`, m2UIForm, m2UIForm), 10*time.Second) ||
		!m2UIEdit(ctx, window, "selected-folders", "Projects") ||
		!selftestClick(ctx, window, m2UIForm, "^(保存协议设置|Save protocol settings)$") {
		return
	}
	checks[m2UISelftestChecks[2]] = waitSelftestPage(ctx, window, `!document.querySelector('[role=dialog]')`, 10*time.Second)
	if !checks[m2UISelftestChecks[2]] || !waitSelftestPage(ctx, window, fmt.Sprintf(`Boolean(document.querySelector('[data-testid="account-select-%s"]'))`, account.ID), 10*time.Second) ||
		!offlineEvalOnce(ctx, window, fmt.Sprintf(`(() => {document.querySelector('[data-testid="account-select-%s"]').click();return true;})()`, account.ID)) ||
		!waitSelftestPage(ctx, window, fmt.Sprintf(`document.querySelector('[data-testid="account-select-%s"]')?.getAttribute('aria-current')==='true'`, account.ID), 5*time.Second) ||
		!waitSelftestPage(ctx, window, `Array.from(document.querySelectorAll('.app-statusbar button')).some(b=>!b.disabled&&/^(立即同步|Sync now)$/.test(b.getAttribute('aria-label')||''))`, 10*time.Second) ||
		!offlineEvalOnce(ctx, window, `(() => {const b=Array.from(document.querySelectorAll('.app-statusbar button')).find(b=>/^(立即同步|Sync now)$/.test(b.getAttribute('aria-label')||''));if(!b||b.disabled)return false;b.click();return true;})()`) {
		return
	}
	var message domain.Message
	checks[m2UISelftestChecks[3]] = selftestEventually(ctx, func() bool {
		messages, err := runtime.DB.Messages(ctx, "", account.ID, false, 20, 0)
		if err == nil && len(messages) == 1 && messages[0].Subject == "Synthetic M2 1" {
			status, statusErr := fixture.user.Status("Projects", &protocol.StatusOptions{NumUnseen: true})
			if statusErr != nil || status.NumUnseen == nil || *status.NumUnseen != 1 || messages[0].IsRead {
				return false
			}
			message = messages[0]
			return true
		}
		return false
	})
	if !checks[m2UISelftestChecks[3]] {
		return
	}
	// The account's default Inbox is intentionally empty: use the selected folder.
	if !waitSelftestPage(ctx, window, fmt.Sprintf(`Array.from(document.querySelectorAll('[data-testid="account-folders-%s"] .sidebar-row-label')).some(e=>e.textContent.trim()==='Projects')`, account.ID), 10*time.Second) ||
		!offlineEvalOnce(ctx, window, fmt.Sprintf(`(() => {const root=document.querySelector('[data-testid="account-folders-%s"]');const b=Array.from(root?.querySelectorAll('button')||[]).find(b=>b.querySelector('.sidebar-row-label')?.textContent.trim()==='Projects');if(!b)return false;b.click();return true;})()`, account.ID)) ||
		!waitSelftestPage(ctx, window, `Array.from(document.querySelectorAll('.message-row-subject')).some(e=>e.textContent.trim()==='Synthetic M2 1')`, 10*time.Second) ||
		!offlineEvalOnce(ctx, window, `(() => {const row=Array.from(document.querySelectorAll('.message-list-row')).find(r=>r.querySelector('.message-row-subject')?.textContent.trim()==='Synthetic M2 1');if(!row)return false;row.click();return true;})()`) {
		return
	}
	checks[m2UISelftestChecks[4]] = waitSelftestPage(ctx, window, `document.body.innerText.includes('Synthetic body')`, 10*time.Second) && selftestEventually(ctx, func() bool {
		stored, err := runtime.DB.Message(ctx, message.ID)
		status, statusErr := fixture.user.Status("Projects", &protocol.StatusOptions{NumUnseen: true})
		pending, pendingErr := runtime.DB.PendingMailOpsSummary(ctx, account.ID)
		return err == nil && stored.IsRead && statusErr == nil && status.NumUnseen != nil && *status.NumUnseen == 0 && pendingErr == nil && pending.TotalActiveCount == 0
	})
	if !checks[m2UISelftestChecks[4]] || !selftestFreshCompose(ctx, window) ||
		!m2UISelector(ctx, window, "#compose-from-account", account.ID) ||
		!reviewSetInput(ctx, window, "#compose-to-input", "recipient@example.invalid") ||
		!selftestClick(ctx, window, "body", "^(密送|Bcc)$") ||
		!reviewSetInput(ctx, window, "#compose-bcc-input", "hidden@example.invalid") ||
		!reviewSetInput(ctx, window, "#compose-subject", "WebView synthetic SMTP") ||
		!selftestClick(ctx, window, ".compose-mode-tabs", "^HTML$") ||
		!reviewSetInput(ctx, window, ".compose-source-input", "<p>Disposable WebView body</p>") ||
		!selftestClick(ctx, window, "body", "^(发送|Send)$") {
		return
	}
	checks[m2UISelftestChecks[5]] = selftestEventually(ctx, func() bool {
		rows, err := runtime.DB.OutboxList(ctx, account.ID)
		return err == nil && len(rows) == 1 && rows[0].Status == "sent" && rows[0].RemoteID == "" && rows[0].Attempts == 1
	})
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	checks[m2UISelftestChecks[6]] = checks[m2UISelftestChecks[5]] && fixture.submissions == 1 && len(fixture.recipients) == 2 && strings.Contains(strings.Join(fixture.recipients, " "), "hidden@example.invalid") && !strings.Contains(strings.ToLower(fixture.raw), "bcc:") && !strings.Contains(fixture.raw, "hidden@example.invalid")
}
