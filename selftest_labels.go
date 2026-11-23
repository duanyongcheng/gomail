package main

import (
	"context"
	"time"

	"github.com/duanyongcheng/gomail/internal/app"
	"github.com/egoist/mygo"
)

// Only invoked by the explicit disposable-profile/local-server selftest.
func selftestLabels(ctx context.Context, runtime *app.Runtime, window *mygo.Window) bool {
	if !waitSelftestPage(ctx, window, `(() => { const section=document.getElementById('gmail-labels-title')?.closest('section');if(!section)return false;section.scrollIntoView({block:'start'});return true;})()`, 5*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, window, `(() => { const input=document.querySelector('#gmail-labels-title')?.closest('section')?.querySelector('input'); if(!input)return false; Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,'Selftest label'); input.dispatchEvent(new Event('input',{bubbles:true})); return true; })()`, 5*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelector('#gmail-labels-title')?.closest('section')?.querySelectorAll('button')||[]).find(b=>/^(创建|Create)$/.test(b.textContent.trim())); if(!b||b.disabled)return false;b.click();return true; })()`, 5*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelector('#gmail-labels-title')?.closest('section')?.querySelectorAll('button')||[]).find(b=>/^(重命名|Rename): Selftest label$/.test(b.getAttribute('aria-label')||'')); if(!b||b.disabled)return false;b.click();return true; })()`, 10*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, window, `(() => { const input=Array.from(document.querySelector('#gmail-labels-title')?.closest('section')?.querySelectorAll('input')||[]).find(i=>/^(重命名标签|Rename label)$/.test(i.getAttribute('aria-label')||'')); if(!input)return false;Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,'Renamed selftest label');input.dispatchEvent(new Event('input',{bubbles:true}));return true; })()`, 5*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelector('#gmail-labels-title')?.closest('section')?.querySelectorAll('button')||[]).find(b=>/^(保存|Save)$/.test(b.textContent.trim()));if(!b||b.disabled)return false;b.click();return true; })()`, 5*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelector('#gmail-labels-title')?.closest('section')?.querySelectorAll('button')||[]).find(b=>/^(删除|Delete): Renamed selftest label$/.test(b.getAttribute('aria-label')||''));if(!b||b.disabled)return false;b.click();return true; })()`, 10*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, window, `(() => { const b=Array.from(document.querySelector('[role="dialog"]')?.querySelectorAll('button')||[]).find(b=>/^(删除|Delete)$/.test(b.textContent.trim()));if(!b||b.disabled)return false;b.click();return true; })()`, 5*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, window, `(() => {const section=document.querySelector('#gmail-labels-title')?.closest('section');return Boolean(section)&&!section.textContent.includes('Renamed selftest label')&&!document.querySelector('[role="dialog"]');})()`, 10*time.Second) {
		return false
	}
	folders, err := runtime.DB.Folders(ctx, "selftest-gmail")
	if err != nil {
		return false
	}
	for _, folder := range folders {
		if folder.Name == "Selftest label" || folder.Name == "Renamed selftest label" {
			return false
		}
	}
	selftestMailbox.Lock()
	writes := selftestMailbox.LabelWrites
	remoteDeleted := true
	for _, label := range selftestMailbox.LabelItems {
		if label.Name == "Selftest label" || label.Name == "Renamed selftest label" {
			remoteDeleted = false
		}
	}
	selftestMailbox.Unlock()
	pending, err := runtime.DB.GmailLabelChange(ctx, "selftest-gmail")
	return err == nil && pending == nil && writes == 3 && remoteDeleted
}
