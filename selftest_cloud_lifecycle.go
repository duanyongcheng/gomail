package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/app"
	"github.com/duanyongcheng/pebble-go/internal/mailmime"
	"github.com/egoist/mygo"
)

const lifecycleDraftID = "runtime-a-lifecycle"
const deleteDraftID = "runtime-b-delete"
const lifecycleThreadID = "runtime-cloud-thread"
const lifecycleSubject = "Remote lifecycle selftest"
const editedLifecycleSubject = "Re: Remote lifecycle selftest"
const deleteDraftSubject = "Remote delete selftest"
const lifecycleAttachment = "Gmail imported attachment\n"

func selftestClick(ctx context.Context, window *mygo.Window, selector, pattern string) bool {
	return waitSelftestPage(ctx, window, fmt.Sprintf(`(() => {
		const root=document.querySelector(%q);if(!root)return false;
		const button=Array.from(root.querySelectorAll('button')).find(b=>new RegExp(%q).test(b.textContent.trim()));
		if(!button||button.disabled)return false;button.click();return true;
	})()`, selector, pattern), 5*time.Second)
}

func selftestFreshCompose(ctx context.Context, window *mygo.Window) bool {
	if !selftestClick(ctx, window, "body", "^(收件箱|Inbox)") {
		return false
	}
	return waitSelftestPage(ctx, window, `(() => {
		const button=Array.from(document.querySelectorAll('button')).find(b=>/^(撰写|写邮件|写信|Compose)$/.test(b.getAttribute('aria-label')||''));
		if(!button||button.disabled)return false;button.click();return true;
	})()`, 5*time.Second) && waitSelftestPage(ctx, window, `Boolean(document.getElementById('compose-subject'))`, 5*time.Second)
}

func selftestSaveClose(ctx context.Context, window *mygo.Window) bool {
	return selftestClick(ctx, window, "body", "^(保存并关闭|Save and close)$") &&
		waitSelftestPage(ctx, window, `document.getElementById('compose-subject')===null`, 10*time.Second)
}

func selftestEventually(ctx context.Context, predicate func() bool) bool {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if predicate() {
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

func lifecycleMIME(subject string) ([]byte, error) {
	return mailmime.BuildDraft(mailmime.Outgoing{
		From:       mail.Address{Name: "Gmail web sender", Address: "demo@example.invalid"},
		To:         []mail.Address{{Address: "recipient@example.invalid"}},
		CC:         []mail.Address{{Address: "copy@example.invalid"}},
		BCC:        []mail.Address{{Address: "hidden@example.invalid"}},
		IncludeBcc: true, Subject: subject,
		Text: "Remote lifecycle body", HTML: "<p>Remote lifecycle body</p>",
		MessageID: "remote-draft@example.invalid", Date: time.Unix(1700000000, 0),
		InReplyTo:   "parent@example.invalid",
		References:  []string{"ancestor@example.invalid", "parent@example.invalid"},
		Attachments: []mailmime.Attachment{{Filename: "remote-selftest.txt", MIMEType: "text/plain", Data: []byte(lifecycleAttachment)}},
	}, mailmime.MaxOutgoingBytes)
}

func lifecycleContent(raw []byte, subject string) bool {
	parsed, err := mailmime.ParseWithLimit(raw, mailmime.MaxOutgoingBytes)
	return err == nil && parsed.Subject == subject && parsed.From.Address == "demo@example.invalid" &&
		len(parsed.To) == 1 && parsed.To[0].Address == "recipient@example.invalid" &&
		len(parsed.CC) == 1 && parsed.CC[0].Address == "copy@example.invalid" &&
		len(parsed.BCC) == 1 && parsed.BCC[0].Address == "hidden@example.invalid" &&
		parsed.InReplyTo == "parent@example.invalid" &&
		parsed.References == "ancestor@example.invalid parent@example.invalid" &&
		strings.Contains(parsed.BodyHTML, "Remote lifecycle body") &&
		len(parsed.Attachments) == 1 && parsed.Attachments[0].Filename == "remote-selftest.txt" &&
		bytes.Equal(parsed.Attachments[0].Data, []byte(lifecycleAttachment))
}

func selftestCloudLifecycle(ctx context.Context, runtime *app.Runtime, window *mygo.Window, checks map[string]bool, capture func(string)) {
	original, err := lifecycleMIME(lifecycleSubject)
	if err != nil {
		return
	}
	toDelete, err := lifecycleMIME(deleteDraftSubject)
	if err != nil {
		return
	}
	selftestMailbox.SetDraft(lifecycleDraftID, original, lifecycleThreadID)
	selftestMailbox.SetDraft(deleteDraftID, toDelete, "runtime-delete-thread")
	selftestMailbox.Lock()
	selftestMailbox.DraftPageSize = 1
	beforeWrites, beforeSends, beforeDeletes := selftestMailbox.DraftWrites, selftestMailbox.DraftSends, selftestMailbox.DraftDeletes
	beforeSentCount := len(selftestMailbox.SentRaw)
	selftestMailbox.Unlock()
	defer func() {
		selftestMailbox.Lock()
		selftestMailbox.DraftPageSize = 0
		selftestMailbox.Unlock()
	}()

	if !selftestFreshCompose(ctx, window) || !selftestClick(ctx, window, "body", "^(Gmail 草稿|Gmail drafts)$") {
		return
	}
	if !waitSelftestPage(ctx, window, `document.querySelectorAll('[data-remote-draft-id]').length===1 && Boolean(document.querySelector('[data-remote-draft-id="runtime-a-lifecycle"]'))`, 10*time.Second) ||
		!selftestClick(ctx, window, "body", "^(加载更多草稿|Load more drafts)$") {
		return
	}
	checks["Gmail remote draft pages loaded through UI"] = waitSelftestPage(ctx, window, `document.querySelectorAll('[data-remote-draft-id]').length===2 && Boolean(document.querySelector('[data-remote-draft-id="runtime-b-delete"]'))`, 10*time.Second)
	capture("gmail-drafts-list.png")
	if !checks["Gmail remote draft pages loaded through UI"] ||
		!selftestClick(ctx, window, `[data-remote-draft-id="`+lifecycleDraftID+`"]`, "^(导入草稿|Import draft)$") {
		return
	}
	importedUI := waitSelftestPage(ctx, window, `document.getElementById('compose-subject')?.value==='Remote lifecycle selftest' && document.body.innerText.includes('remote-selftest.txt') && document.body.innerText.includes('hidden@example.invalid')`, 10*time.Second)
	compose := &app.ComposeService{Runtime: runtime}
	localID := ""
	drafts, err := compose.List(ctx, app.AccountRequest{AccountID: "selftest-gmail"})
	if err == nil {
		for _, draft := range drafts {
			if draft.Subject == lifecycleSubject {
				localID = draft.ID
				break
			}
		}
	}
	if localID == "" {
		return
	}
	local, err := compose.Get(ctx, app.DraftRequest{AccountID: "selftest-gmail", DraftID: localID})
	selftestMailbox.Lock()
	noImportWrites := selftestMailbox.DraftWrites == beforeWrites && selftestMailbox.DraftSends == beforeSends && selftestMailbox.DraftDeletes == beforeDeletes
	selftestMailbox.Unlock()
	checks["Gmail remote draft imported through UI"] = importedUI && err == nil && noImportWrites &&
		len(local.BCC) == 1 && strings.Contains(local.BCC[0], "hidden@example.invalid") &&
		len(local.Attachments) == 1 && local.Attachments[0].Name == "remote-selftest.txt"
	capture("gmail-draft-imported.png")
	if !checks["Gmail remote draft imported through UI"] {
		return
	}
	if !waitSelftestPage(ctx, window, `(() => {
		const input=document.getElementById('compose-subject');if(!input)return false;
		Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,'Re: Remote lifecycle selftest');
		input.dispatchEvent(new Event('input',{bubbles:true}));return true;
	})()`, 5*time.Second) || !selftestSaveClose(ctx, window) || !selftestFreshCompose(ctx, window) ||
		!selftestClick(ctx, window, "body", "^(本地草稿|Local drafts)$") {
		return
	}
	localSelector := fmt.Sprintf(`[data-local-draft-id=%q]`, localID)
	if !selftestClick(ctx, window, localSelector, "^(更新 Gmail 副本|Update Gmail copy)$") {
		return
	}
	checks["Gmail imported draft updated through UI"] = selftestEventually(ctx, func() bool {
		selftestMailbox.Lock()
		draft, exists := selftestMailbox.Drafts[lifecycleDraftID]
		writes := selftestMailbox.DraftWrites
		selftestMailbox.Unlock()
		raw, err := base64.RawURLEncoding.DecodeString(draft.Message.Raw)
		if !exists || err != nil || writes != beforeWrites+1 || draft.Message.ThreadID != lifecycleThreadID || !lifecycleContent(raw, editedLifecycleSubject) {
			return false
		}
		state, err := runtime.DB.GmailDraftCopy(ctx, "selftest-gmail", localID)
		return err == nil && state.State == "synced"
	})
	capture("gmail-draft-updated.png")
	if !checks["Gmail imported draft updated through UI"] ||
		!selftestClick(ctx, window, localSelector, "^Re: Remote lifecycle selftest$") ||
		!waitSelftestPage(ctx, window, `document.getElementById('compose-subject')?.value==='Re: Remote lifecycle selftest'`, 5*time.Second) ||
		!selftestClick(ctx, window, "body", "^(发送|Send)$") {
		return
	}
	sentClosed := waitSelftestPage(ctx, window, `document.getElementById('compose-subject')===null`, 10*time.Second)
	checks["Gmail linked draft send consumes matching remote copy"] = sentClosed && selftestEventually(ctx, func() bool {
		selftestMailbox.Lock()
		_, consumed := selftestMailbox.Drafts[lifecycleDraftID]
		_, otherPreserved := selftestMailbox.Drafts[deleteDraftID]
		count, sends := len(selftestMailbox.SentRaw), selftestMailbox.DraftSends
		var raw []byte
		if count == beforeSentCount+1 {
			raw = append([]byte(nil), selftestMailbox.SentRaw[count-1]...)
		}
		sent := selftestMailbox.Messages[fmt.Sprintf("sent-%d", count)]
		selftestMailbox.Unlock()
		if consumed || !otherPreserved || sends != beforeSends+1 || sent.ThreadID != lifecycleThreadID || !lifecycleContent(raw, editedLifecycleSubject) {
			return false
		}
		items, err := (&app.OutboxService{Runtime: runtime}).List(ctx, app.AccountRequest{AccountID: "selftest-gmail"})
		if err != nil {
			return false
		}
		for _, item := range items {
			if item.Subject == editedLifecycleSubject && item.Status == "sent" {
				return true
			}
		}
		return false
	})
	capture("gmail-linked-draft-sent.png")
	if !checks["Gmail linked draft send consumes matching remote copy"] ||
		!selftestFreshCompose(ctx, window) || !selftestClick(ctx, window, "body", "^(Gmail 草稿|Gmail drafts)$") ||
		!selftestClick(ctx, window, `[data-remote-draft-id="`+deleteDraftID+`"]`, "^(导入草稿|Import draft)$") ||
		!waitSelftestPage(ctx, window, `document.getElementById('compose-subject')?.value==='Remote delete selftest'`, 5*time.Second) ||
		!selftestSaveClose(ctx, window) || !selftestFreshCompose(ctx, window) ||
		!selftestClick(ctx, window, "body", "^(本地草稿|Local drafts)$") {
		return
	}
	drafts, err = compose.List(ctx, app.AccountRequest{AccountID: "selftest-gmail"})
	if err != nil {
		return
	}
	deleteLocalID := ""
	for _, draft := range drafts {
		if draft.Subject == deleteDraftSubject {
			deleteLocalID = draft.ID
		}
	}
	if deleteLocalID == "" {
		return
	}
	if !selftestClick(ctx, window, fmt.Sprintf(`[data-local-draft-id=%q]`, deleteLocalID), "^(删除 Gmail 副本|Delete Gmail copy)$") ||
		!selftestClick(ctx, window, `[role="dialog"]`, "^(删除 Gmail 副本|Delete Gmail copy)$") {
		return
	}
	checks["Gmail remote draft deleted through UI"] = selftestEventually(ctx, func() bool {
		selftestMailbox.Lock()
		_, remains := selftestMailbox.Drafts[deleteDraftID]
		deletes := selftestMailbox.DraftDeletes
		selftestMailbox.Unlock()
		if remains || deletes != beforeDeletes+1 {
			return false
		}
		drafts, err := compose.List(ctx, app.AccountRequest{AccountID: "selftest-gmail"})
		if err != nil {
			return false
		}
		for _, draft := range drafts {
			if draft.ID == deleteLocalID && draft.Subject == deleteDraftSubject {
				return draft.CloudState == "local" && draft.AttachmentCount == 1
			}
		}
		return false
	})
	capture("gmail-draft-deleted.png")
}
