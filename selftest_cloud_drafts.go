package main

import (
	"context"
	"github.com/duanyongcheng/pebble-go/internal/app"
	"github.com/egoist/mygo"
	"time"
)

func selftestCloudDraft(ctx context.Context, r *app.Runtime, w *mygo.Window) bool {
	if !waitSelftestPage(ctx, w, `(() => { const b=Array.from(document.querySelectorAll('button')).find(b=>/^(上传到 Gmail|Upload to Gmail)$/.test(b.textContent.trim()));if(!b||b.disabled)return false;b.click();return true;})()`, 5*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, w, `/快照已上传到 Gmail|Snapshot uploaded to Gmail/.test(document.body.innerText)`, 10*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, w, `(() => { const b=Array.from(document.querySelectorAll('button')).find(b=>/^(检查 Gmail 副本|Check Gmail copy)$/.test(b.textContent.trim()));if(!b||b.disabled)return false;b.click();return true;})()`, 5*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, w, `(() => { const b=Array.from(document.querySelectorAll('button')).find(b=>/^(更新 Gmail 副本|Update Gmail copy)$/.test(b.textContent.trim()));if(!b||b.disabled)return false;b.click();return true;})()`, 10*time.Second) {
		return false
	}
	if !waitSelftestPage(ctx, w, `(() => { const b=Array.from(document.querySelectorAll('button')).find(b=>/^(检查 Gmail 副本|Check Gmail copy)$/.test(b.textContent.trim()));return Boolean(b)&&!b.disabled&&/快照已上传到 Gmail|Snapshot uploaded to Gmail/.test(document.body.innerText);})()`, 10*time.Second) {
		return false
	}
	items, err := (&app.ComposeService{Runtime: r}).List(ctx, app.AccountRequest{AccountID: "selftest-gmail"})
	selftestMailbox.Lock()
	writes, count := selftestMailbox.DraftWrites, len(selftestMailbox.Drafts)
	selftestMailbox.Unlock()
	return err == nil && len(items) == 1 && items[0].CloudState == "synced" && writes == 2 && count == 1
}
