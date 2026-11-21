package main

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/app"
	"github.com/duanyongcheng/pebble-go/internal/store"
	"github.com/egoist/mygo"
)

var offlineSelftestChecks = []string{
	"offline browser state applied in real WebView",
	"offline list star persists locally while synthetic Gmail stalls",
	"offline uncached Starred view renders local message",
	"offline uncached basic Search submits and renders local hit",
	"offline Outbox renders existing unknown send",
	"offline Outbox local refresh completes while synthetic Gmail stalls",
	"offline unknown send remains unchanged without remote submission",
	"offline browser descriptor restored before native quit",
}

const offlineAccount = "selftest-gmail"
const offlineMessageID = offlineAccount + ":m1"
const offlineSubject = "Pebble Go selftest"
const offlineSearchTerm = "中文搜索"
const offlineUnknownSubject = "Unknown send remains unknown"

// Override only this disposable WebView's browser signal, not OS connectivity.
// Save the own descriptor (usually absent) rather than modifying Navigator's
// prototype. Install the saved state BEFORE a potentially throwing override.
const offlineBrowserSetup = `(() => {
	if(window.__pebbleOfflineSelftest)return false;
	window.__pebbleOfflineSelftest={descriptor:Object.getOwnPropertyDescriptor(navigator,'onLine'),value:navigator.onLine};
	Object.defineProperty(navigator,'onLine',{configurable:true,get:()=>false});
	window.dispatchEvent(new Event('offline'));
	return navigator.onLine===false;
})()`

const offlineBrowserRestore = `(() => {
	const saved=window.__pebbleOfflineSelftest;if(!saved)return false;
	let restored=false;
	try {
		if(saved.descriptor)Object.defineProperty(navigator,'onLine',saved.descriptor);
		else delete navigator.onLine;
		const current=Object.getOwnPropertyDescriptor(navigator,'onLine'), original=saved.descriptor;
		restored=navigator.onLine===saved.value && (original
			? Boolean(current)&&['get','set','value','writable','enumerable','configurable'].every(k=>current[k]===original[k])
			: current===undefined);
	} finally {
		delete window.__pebbleOfflineSelftest;
		window.dispatchEvent(new Event('online'));
	}
	return restored;
})()`

// Eval cancellation can race a delivered page action. Never retry a click or
// input assignment in a polling predicate; only retry read-only readiness.
func offlineEvalOnce(ctx context.Context, window *mygo.Window, script string) bool {
	evalCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	value, err := window.Page().EvalContext(evalCtx, script)
	return err == nil && value == true
}

func offlineRowScript(scope, body string) string {
	return fmt.Sprintf(`(() => {
		const row=Array.from(document.querySelectorAll(%q)).find(r=>r.querySelector('.message-row-subject')?.textContent.trim()===%q);
		if(!row||row.getClientRects().length===0)return false;
		%s
	})()`, scope+" .message-list-row", offlineSubject, body)
}

// Actions appear when the row is focused. Focus is not a message opening or
// flag action; the only toggle below is a single separate EvalContext call.
func offlineListStar(ctx context.Context, runtime *app.Runtime, window *mygo.Window) bool {
	before, err := runtime.DB.Message(ctx, offlineMessageID)
	if err != nil {
		return false
	}
	const starButton = `const button=Array.from(row.querySelectorAll('button')).find(b=>/^(标星|加星标|取消标星|取消星标|Star|Unstar)$/.test(b.getAttribute('aria-label')||''));`
	if !waitSelftestPage(ctx, window, offlineRowScript("", `return true;`), 5*time.Second) ||
		!offlineEvalOnce(ctx, window, offlineRowScript("", `row.focus();return true;`)) ||
		!waitSelftestPage(ctx, window, offlineRowScript("", starButton+`return Boolean(button)&&!button.disabled&&button.getAttribute('aria-pressed')==='`+fmt.Sprint(before.IsStarred)+`';`), 5*time.Second) ||
		!offlineEvalOnce(ctx, window, offlineRowScript("", starButton+`if(!button||button.disabled)return false;button.click();return true;`)) {
		return false
	}
	// Always make an actual UI change, even if an earlier fixture happened to
	// leave m1 starred. SQLite, not just optimistic pixels, is the oracle.
	if !selftestEventually(ctx, func() bool {
		current, err := runtime.DB.Message(ctx, offlineMessageID)
		return err == nil && current.IsStarred != before.IsStarred
	}) {
		return false
	}
	if before.IsStarred {
		// Restore star for first-time Starred navigation, with a second distinct
		// one-shot toggle only after the first local acknowledgement.
		if !waitSelftestPage(ctx, window, offlineRowScript("", starButton+`return Boolean(button)&&!button.disabled&&button.getAttribute('aria-pressed')==='false';`), 5*time.Second) ||
			!offlineEvalOnce(ctx, window, offlineRowScript("", starButton+`if(!button||button.disabled)return false;button.click();return true;`)) {
			return false
		}
	}
	return selftestEventually(ctx, func() bool {
		current, err := runtime.DB.Message(ctx, offlineMessageID)
		return err == nil && current.IsStarred
	})
}

func offlineSearch(ctx context.Context, window *mygo.Window) bool {
	if !selftestClick(ctx, window, ".sidebar-nav--top", "^(搜索|Search)") ||
		!waitSelftestPage(ctx, window, `Boolean(document.querySelector('.search-toolbar .search-input'))`, 5*time.Second) ||
		!offlineEvalOnce(ctx, window, fmt.Sprintf(`(() => {
			const input=document.querySelector('.search-toolbar .search-input');if(!input||input.disabled)return false;
			Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,%q);
			input.dispatchEvent(new Event('input',{bubbles:true}));return true;
		})()`, offlineSearchTerm)) ||
		!waitSelftestPage(ctx, window, fmt.Sprintf(`document.querySelector('.search-toolbar .search-input')?.value===%q`, offlineSearchTerm), 5*time.Second) ||
		!offlineEvalOnce(ctx, window, `(() => {
			const button=document.querySelector('.search-toolbar button[type="submit"]');if(!button||button.disabled)return false;
			button.click();return true;
		})()`) {
		return false
	}
	return waitSelftestPage(ctx, window, fmt.Sprintf(`navigator.onLine===false && Array.from(document.querySelectorAll('.search-results-scroll [role="option"]')).some(r=>r.getClientRects().length>0&&r.textContent.includes(%q)&&r.textContent.includes(%q))`, offlineSubject, offlineSearchTerm), 5*time.Second)
}

// UI-only selector marker is removed by cleanup; no private React/QueryClient
// state, direct native RPC or mocked transport is used to manufacture a hit.
const offlineOutboxOpen = `(() => {
	const summary=Array.from(document.querySelectorAll('summary')).find(s=>/^(发件箱|Outbox)\s*\(/.test(s.textContent.trim()));
	const panel=summary?.closest('details');if(!panel)return false;
	panel.open=true;panel.setAttribute('data-offline-selftest-outbox','');return true;
})()`

const offlineOutboxVisible = `(() => {
	const panel=document.querySelector('[data-offline-selftest-outbox]');
	const row=Array.from(panel?.querySelectorAll('li')||[]).find(r=>r.querySelector('strong')?.textContent==='Unknown send remains unknown');
	return navigator.onLine===false&&Boolean(panel?.open)&&Boolean(row)&&row.getClientRects().length>0&&/结果未知|Outcome unknown/.test(row.innerText);
})()`

// Merely seeing the old cached row after clicking Refresh would falsely pass a
// paused refetch. Observe a real fetching cycle (disabled -> enabled) as well as
// the retained row; browser-offline paused queries never enter isFetching.
const offlineOutboxWatchRefresh = `(() => {
	const panel=document.querySelector('[data-offline-selftest-outbox]');
	const button=Array.from(panel?.querySelectorAll('button')||[]).find(b=>/^(刷新|Refresh)$/.test(b.textContent.trim()));
	if(!button||button.disabled)return false;
	const state={started:false,completed:false};window.__pebbleOfflineRefresh=state;
	const observer=new MutationObserver(records=>{
		if(records.some(r=>r.attributeName==='disabled'&&r.oldValue===null))state.started=true;
		if(state.started&&!button.disabled)state.completed=true;
	});
	state.observer=observer;observer.observe(button,{attributes:true,attributeFilter:['disabled'],attributeOldValue:true});
	button.click();return true;
})()`

const offlineOutboxCleanup = `(() => {
	window.__pebbleOfflineRefresh?.observer?.disconnect();delete window.__pebbleOfflineRefresh;
	document.querySelector('[data-offline-selftest-outbox]')?.removeAttribute('data-offline-selftest-outbox');return true;
})()`

func offlineUnknownUnchanged(before, after store.OutboxRecord) bool {
	return before.ID != "" && before.AccountID == offlineAccount && before.Status == "outcome_unknown" &&
		len(before.Data) > 0 && len(before.Summary) > 0 && reflect.DeepEqual(before, after)
}

func selftestOffline(ctx context.Context, runtime *app.Runtime, window *mygo.Window, checks map[string]bool, capture func(string)) {
	// Do not perturb any original/review fixture counts or normal close/exit.
	// This smoke requires their known clean, closed-compose state.
	if selftestMailbox == nil || !checks[reviewSelftestChecks[6]] || ctx.Err() != nil {
		return
	}
	checkCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	items, err := (&app.OutboxService{Runtime: runtime}).List(checkCtx, app.AccountRequest{AccountID: offlineAccount})
	if err != nil {
		return
	}
	var unknown store.OutboxRecord
	for _, item := range items {
		if item.Subject == offlineUnknownSubject && item.Status == "outcome_unknown" {
			unknown, err = runtime.DB.Outbox(checkCtx, offlineAccount, item.ID)
			break
		}
	}
	if err != nil || !offlineUnknownUnchanged(unknown, unknown) {
		return
	}
	if !selftestClick(checkCtx, window, ".sidebar-mail-scroll", "^(收件箱|Inbox)") ||
		!waitSelftestPage(checkCtx, window, offlineRowScript("", `return true;`), 5*time.Second) {
		return
	}

	// ONLY the synthetic HTTP fixture mutex is held; never Runtime/DB/provider
	// gates. Native replay does not use navigator.onLine and may attempt HTTP,
	// but cannot complete it here. Hold through every local persistence check.
	selftestMailbox.Lock()
	sentBefore := len(selftestMailbox.SentRaw)
	requestsBefore := len(selftestMailbox.Requests)
	defer selftestMailbox.Unlock()
	// Independent bounded cleanup survives a smoke timeout/cancellation. MyGo
	// EvalContext aborts its wait; shutdown joins selftest after UI dispatch drains.
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cleanupCancel()
		checks[offlineSelftestChecks[7]] = offlineEvalOnce(cleanupCtx, window, fmt.Sprintf(
			`(() => { try { %s; } finally { return %s; } })()`, offlineOutboxCleanup, offlineBrowserRestore))
	}()
	checks[offlineSelftestChecks[0]] = offlineEvalOnce(checkCtx, window, offlineBrowserSetup) &&
		waitSelftestPage(checkCtx, window, `navigator.onLine===false`, time.Second)
	if !checks[offlineSelftestChecks[0]] {
		return
	}
	capture("offline-inbox.png")
	checks[offlineSelftestChecks[1]] = offlineListStar(checkCtx, runtime, window)
	capture("offline-list-star.png")
	if !checks[offlineSelftestChecks[1]] {
		return
	}
	// Earlier selftests never navigate to Starred or Search, so these are first
	// mounts/uncached query keys. No prefetch or cache injection is performed.
	checks[offlineSelftestChecks[2]] = selftestClick(checkCtx, window, ".sidebar-mail-scroll", "^(星标邮件|Starred)$") &&
		waitSelftestPage(checkCtx, window, offlineRowScript(".starred-list-scroll", `return navigator.onLine===false;`), 5*time.Second)
	capture("offline-starred.png")
	checks[offlineSelftestChecks[3]] = offlineSearch(checkCtx, window)
	capture("offline-search.png")
	if !selftestClick(checkCtx, window, ".sidebar-nav--bottom", "^(设置|Settings)$") ||
		!waitSelftestPage(checkCtx, window, `Boolean(document.getElementById('settings-tab-accounts'))`, 5*time.Second) ||
		!offlineEvalOnce(checkCtx, window, `(() => {
			const tab=Array.from(document.querySelectorAll('[role="tab"]')).find(b=>/^(远端写回|Remote Writes)$/.test(b.textContent.trim()));
			if(!tab||tab.disabled)return false;tab.click();return true;
		})()`) || !waitSelftestPage(checkCtx, window, `Array.from(document.querySelectorAll('summary')).some(s=>/^(发件箱|Outbox)\s*\(/.test(s.textContent.trim()))`, 5*time.Second) ||
		!offlineEvalOnce(checkCtx, window, offlineOutboxOpen) {
		return
	}
	checks[offlineSelftestChecks[4]] = waitSelftestPage(checkCtx, window, offlineOutboxVisible, 5*time.Second)
	if checks[offlineSelftestChecks[4]] {
		const refreshReady = `(() => {
			const panel=document.querySelector('[data-offline-selftest-outbox]');
			return Array.from(panel?.querySelectorAll('button')||[]).some(b=>/^(刷新|Refresh)$/.test(b.textContent.trim())&&!b.disabled);
		})()`
		checks[offlineSelftestChecks[5]] = waitSelftestPage(checkCtx, window, refreshReady, 5*time.Second) &&
			offlineEvalOnce(checkCtx, window, offlineOutboxWatchRefresh) &&
			waitSelftestPage(checkCtx, window, `window.__pebbleOfflineRefresh?.completed===true`, 5*time.Second) &&
			waitSelftestPage(checkCtx, window, offlineOutboxVisible, time.Second)
	}
	after, err := runtime.DB.Outbox(checkCtx, offlineAccount, unknown.ID)
	checks[offlineSelftestChecks[6]] = err == nil && offlineUnknownUnchanged(unknown, after) &&
		len(selftestMailbox.SentRaw) == sentBefore && len(selftestMailbox.Requests) == requestsBefore
	capture("offline-outbox.png")
}
