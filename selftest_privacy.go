package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/duanyongcheng/gomail/internal/app"
	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/store"
	"github.com/egoist/mygo"
)

func selftestTrust(ctx context.Context, r *app.Runtime, window *mygo.Window) bool {
	if !waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelectorAll('[role="tab"]')).find(b=>/^(隐私|Privacy)$/.test(b.textContent.trim()));if(!b)return false;b.click();return true;})()`, 5*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, window, `(() => {const input=document.querySelector('#trusted-senders-title')?.closest('section')?.querySelector('input');if(!input)return false;Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,'privacy@example.invalid');input.dispatchEvent(new Event('input',{bubbles:true}));return true;})()`, 5*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, window, `(() => {const b=Array.from(document.querySelector('#trusted-senders-title')?.closest('section')?.querySelectorAll('button')||[]).find(b=>/^(保存信任|Save trust)$/.test(b.textContent.trim()));if(!b||b.disabled)return false;b.click();return true;})()`, 5*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, window, `Boolean(Array.from(document.querySelectorAll('button')).find(b=>/^(撤销信任|Remove trust): privacy@example.invalid$/.test(b.getAttribute('aria-label')||'')))`, 10*time.Second) {
		return false
	}
	level, err := r.DB.SenderTrust(ctx, "selftest-gmail", "PRIVACY@example.invalid")
	if err != nil || level != "images" {
		return false
	}
	// These synthetic resource URLs are inspected in Go only, never loaded into
	// the WebView or fetched. The user-visible selftest mail remains resource-free.
	m := domain.Message{ID: "selftest-gmail:privacy-policy", AccountID: "selftest-gmail", RemoteID: "privacy-policy", FromAddress: "privacy@example.invalid", BodyHTML: `<img src="https://images.invalid/photo.png"><img src="https://tracker.invalid/pixel" width="1" height="1">`}
	if err := r.DB.CommitSnapshot(ctx, store.Snapshot{AccountID: m.AccountID, Messages: []store.CachedMessage{{Message: m}}}); err != nil {
		return false
	}
	defer r.DB.CommitSnapshot(ctx, store.Snapshot{AccountID: m.AccountID, Removed: []string{m.ID}})
	service := &app.MessageService{Runtime: r}
	request := app.MessageRequest{MessageID: m.ID, PrivacyMode: json.RawMessage(`"Strict"`)}
	trusted, err := service.Rendered(ctx, request)
	if err != nil || !strings.Contains(trusted.Rendered.HTML, "images.invalid") || strings.Contains(trusted.Rendered.HTML, "tracker.invalid") {
		return false
	}
	if !waitSelftestPage(ctx, window, `(() => {const b=Array.from(document.querySelectorAll('button')).find(b=>/^(撤销信任|Remove trust): privacy@example.invalid$/.test(b.getAttribute('aria-label')||''));if(!b||b.disabled)return false;b.click();return true;})()`, 5*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, window, `!Array.from(document.querySelectorAll('button')).some(b=>/^(撤销信任|Remove trust): privacy@example.invalid$/.test(b.getAttribute('aria-label')||''))`, 10*time.Second) {
		return false
	}
	level, err = r.DB.SenderTrust(ctx, m.AccountID, m.FromAddress)
	if err != nil || level != "" {
		return false
	}
	revoked, err := service.Rendered(ctx, request)
	return err == nil && !strings.Contains(revoked.Rendered.HTML, "images.invalid") && !strings.Contains(revoked.Rendered.HTML, "tracker.invalid")
}
