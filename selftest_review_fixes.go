package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/mail"
	"reflect"
	"strings"
	"time"

	"github.com/bary/pebble-go/internal/app"
	"github.com/bary/pebble-go/internal/crypto"
	"github.com/bary/pebble-go/internal/mailmime"
	"github.com/bary/pebble-go/internal/store"
	"github.com/egoist/mygo"
)

var reviewSelftestChecks = []string{
	"review linked send retains latest draft after UI cleanup",
	"review Outbox cancel restores latest source and quote through UI",
	"review cancelled draft retains encrypted attachments without sending",
	"review HTML rich conversion blocked visibly through UI",
	"review HTML Markdown conversion blocked visibly through UI",
	"review HTML save preserves source and attachment refs",
	"review synthetic drafts discarded safely through UI",
}

const reviewAccount = "selftest-gmail"
const reviewCancelRemoteID = "runtime-review-cancel"
const reviewHTMLRemoteID = "runtime-review-html"
const reviewCancelSubject = "Review cancel remote original"
const reviewLatestSubject = "Review cancel latest local only"
const reviewOriginalHTML = "<p>Review remote original body</p>"
const reviewLatestHTML = "<p><strong>Review latest local body only</strong></p>"
const reviewQuote = `<blockquote class="compose-original-quote"><p>Review retained quote</p></blockquote>`
const reviewHTMLSubject = "Review HTML conversion source"
const reviewUnsupportedHTML = `<p>Review HTML body</p><img src="cid:review-logo" alt="Review logo"><table><tbody><tr><td>Review table cell</td></tr></tbody></table>`
const reviewAttachmentName = "review-attachment.txt"
const reviewAttachmentBytes = "Review retained attachment bytes\n"

func reviewMIME(subject, html string, inline bool) ([]byte, error) {
	attachments := []mailmime.Attachment{{Filename: reviewAttachmentName, MIMEType: "text/plain", Data: []byte(reviewAttachmentBytes)}}
	if inline {
		// Generate a valid 1x1 PNG; never read a remote resource or profile file.
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
			return nil, err
		}
		attachments = append(attachments, mailmime.Attachment{Filename: "review-logo.png", MIMEType: "image/png", ContentID: "review-logo", Inline: true, Data: encoded.Bytes()})
	}
	return mailmime.BuildDraft(mailmime.Outgoing{
		From: mail.Address{Address: "demo@example.invalid"}, To: []mail.Address{{Address: "recipient@example.invalid"}},
		Subject: subject, Text: "Review remote original text", HTML: html,
		MessageID: "review-draft@example.invalid", Date: time.Unix(1700000000, 0),
		InReplyTo: "review-parent@example.invalid", References: []string{"review-parent@example.invalid"}, Attachments: attachments,
	}, mailmime.MaxOutgoingBytes)
}

// Read only the explicitly synthetic in-memory key. Never call a system keyring.
func reviewCipher(runtime *app.Runtime) (*crypto.Service, error) {
	keyring, ok := runtime.Keyring.(*selftestKeyring)
	if !ok {
		return nil, errors.New("review selftest requires its synthetic keyring")
	}
	return crypto.FromKey(keyring.key)
}

func reviewSameDraft(a, b app.LocalDraft) bool {
	// Autosave/explicit Send/save-close may update time, but not the frozen
	// semantic snapshot. In particular source and the sanitized quote stay separate.
	a.UpdatedAt, b.UpdatedAt = 0, 0
	return reflect.DeepEqual(a, b)
}

func reviewStoredDraft(ctx context.Context, runtime *app.Runtime, id string) (app.LocalDraft, bool) {
	stored, err := runtime.DB.ComposeDraft(ctx, reviewAccount, id)
	if err != nil || stored.Deleted || !bytes.HasPrefix(stored.Data, []byte("PEBBLE\x00\x01")) {
		return app.LocalDraft{}, false
	}
	cipher, err := reviewCipher(runtime)
	if err != nil {
		return app.LocalDraft{}, false
	}
	plain, err := cipher.Decrypt("compose_drafts.data", reviewAccount+"\x00"+id, stored.Data)
	var draft app.LocalDraft
	if err != nil || json.Unmarshal(plain, &draft) != nil {
		return app.LocalDraft{}, false
	}
	return draft, true
}

func reviewQueuedContent(runtime *app.Runtime, row store.OutboxRecord, latest app.LocalDraft) bool {
	cipher, err := reviewCipher(runtime)
	if err != nil || !bytes.HasPrefix(row.Data, []byte("PEBBLE\x00\x01")) {
		return false
	}
	plain, err := cipher.Decrypt("outbox.data", row.AccountID+"\x00"+row.ID, row.Data)
	var payload struct{ Raw []byte }
	if err != nil || json.Unmarshal(plain, &payload) != nil {
		return false
	}
	parsed, err := mailmime.ParseWithLimit(payload.Raw, mailmime.MaxOutgoingBytes)
	return err == nil && parsed.Subject == latest.Subject && parsed.From.Address == "demo@example.invalid" &&
		strings.Contains(parsed.BodyHTML, latest.RawSource) && strings.Contains(parsed.BodyHTML, latest.QuotedReplyHTML) &&
		parsed.InReplyTo == latest.InReplyTo && parsed.References == latest.References &&
		len(parsed.Attachments) == 1 && parsed.Attachments[0].Filename == reviewAttachmentName &&
		bytes.Equal(parsed.Attachments[0].Data, []byte(reviewAttachmentBytes))
}

func reviewAttachmentsIntact(ctx context.Context, runtime *app.Runtime, draft app.LocalDraft, raw []byte) bool {
	parsed, err := mailmime.ParseWithLimit(raw, mailmime.MaxOutgoingBytes)
	if err != nil || len(draft.Attachments) != len(parsed.Attachments) {
		return false
	}
	cipher, err := reviewCipher(runtime)
	if err != nil {
		return false
	}
	for i, ref := range draft.Attachments {
		id, valid := strings.CutPrefix(ref.Path, "compose-attachment:")
		if !valid {
			return false
		}
		stored, err := runtime.DB.ComposeAttachment(ctx, id)
		if err != nil || !bytes.HasPrefix(stored.Data, []byte("PEBBLE\x00\x01")) {
			return false
		}
		plain, err := cipher.Decrypt("compose_attachments.data", id, stored.Data)
		var data struct {
			Name, MIMEType, ContentID string
			Bytes                     []byte
			Inline                    bool
		}
		want := parsed.Attachments[i]
		if err != nil || json.Unmarshal(plain, &data) != nil || data.Name != ref.Name || data.Name != want.Filename ||
			data.MIMEType != want.MIMEType || data.ContentID != want.ContentID || data.Inline != want.Inline ||
			!bytes.Equal(data.Bytes, want.Data) || ref.Size != int64(len(data.Bytes)) || stored.Size != ref.Size {
			return false
		}
	}
	return true
}

// Snapshot only write/send counters: background mailbox GETs are allowed.
// Requests also catches a forbidden fallback messages.send even if it failed.
func reviewRemoteUnchanged(id string, raw []byte) func() bool {
	selftestMailbox.Lock()
	original := selftestMailbox.Drafts[id]
	writes, deletes, sends, sent := selftestMailbox.DraftWrites, selftestMailbox.DraftDeletes, selftestMailbox.DraftSends, len(selftestMailbox.SentRaw)
	requestOffset := len(selftestMailbox.Requests)
	selftestMailbox.Unlock()
	return func() bool {
		selftestMailbox.Lock()
		defer selftestMailbox.Unlock()
		current, exists := selftestMailbox.Drafts[id]
		decoded, err := base64.RawURLEncoding.DecodeString(current.Message.Raw)
		if !exists || err != nil || !bytes.Equal(decoded, raw) || !reflect.DeepEqual(original, current) ||
			selftestMailbox.DraftWrites != writes || selftestMailbox.DraftDeletes != deletes ||
			selftestMailbox.DraftSends != sends || len(selftestMailbox.SentRaw) != sent {
			return false
		}
		for _, request := range selftestMailbox.Requests[requestOffset:] {
			if request == "POST /gmail/v1/users/me/messages/send" || request == "POST /gmail/v1/users/me/drafts/send" {
				return false
			}
		}
		return true
	}
}

func reviewSetInput(ctx context.Context, window *mygo.Window, selector, value string) bool {
	return waitSelftestPage(ctx, window, fmt.Sprintf(`(() => {
		const input=document.querySelector(%q);if(!input||input.disabled||input.closest('[inert]'))return false;
		const proto=input instanceof HTMLTextAreaElement?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;
		Object.getOwnPropertyDescriptor(proto,'value').set.call(input,%q);
		input.dispatchEvent(new Event('input',{bubbles:true}));return true;
	})()`, selector, value), 5*time.Second)
}

func reviewSourceVisible(ctx context.Context, window *mygo.Window, subject, source string) bool {
	return waitSelftestPage(ctx, window, fmt.Sprintf(`(() => {
		const input=document.querySelector('.compose-source-input'), active=document.querySelector('.compose-mode-button.is-active');
		return document.getElementById('compose-subject')?.value===%q && Boolean(input)&&!input.disabled&&input.value===%q&&active?.textContent.trim()==='HTML';
	})()`, subject, source), 5*time.Second)
}

func reviewImport(ctx context.Context, runtime *app.Runtime, window *mygo.Window, remoteID, subject, source string) (app.LocalDraft, bool) {
	if !selftestFreshCompose(ctx, window) || !selftestClick(ctx, window, "body", "^(Gmail 草稿|Gmail drafts)$") ||
		!selftestClick(ctx, window, fmt.Sprintf(`[data-remote-draft-id=%q]`, remoteID), "^(导入草稿|Import draft)$") ||
		!reviewSourceVisible(ctx, window, subject, source) {
		return app.LocalDraft{}, false
	}
	compose := &app.ComposeService{Runtime: runtime}
	rows, err := compose.List(ctx, app.AccountRequest{AccountID: reviewAccount})
	if err == nil {
		for _, row := range rows {
			if row.Subject == subject {
				draft, err := compose.Get(ctx, app.DraftRequest{AccountID: reviewAccount, DraftID: row.ID})
				return draft, err == nil
			}
		}
	}
	return app.LocalDraft{}, false
}

func reviewOpenLocal(ctx context.Context, window *mygo.Window, draft app.LocalDraft) bool {
	return selftestFreshCompose(ctx, window) && selftestClick(ctx, window, "body", "^(本地草稿|Local drafts)$") &&
		waitSelftestPage(ctx, window, fmt.Sprintf(`(() => {
			const row=document.querySelector(%q), button=Array.from(row?.querySelectorAll('button')||[]).find(b=>b.textContent.trim()===%q);
			if(!button||button.disabled)return false;button.click();return true;
		})()`, fmt.Sprintf(`[data-local-draft-id=%q]`, draft.ID), draft.Subject), 5*time.Second) &&
		reviewSourceVisible(ctx, window, draft.Subject, draft.RawSource)
}

func reviewCloseCompose(ctx context.Context, window *mygo.Window) bool {
	if !selftestClick(ctx, window, "body", "^(返回|Back)$") ||
		!waitSelftestPage(ctx, window, `!document.getElementById('compose-subject')||Boolean(document.querySelector('[role="dialog"]'))`, 5*time.Second) {
		return false
	}
	// A pristine compose closes directly; an edited synthetic draft requires its
	// real leave confirmation. No store/React state shortcut to force closure.
	if waitSelftestPage(ctx, window, `!document.getElementById('compose-subject')`, time.Second) {
		return true
	}
	return selftestClick(ctx, window, `[role="dialog"]`, "^(丢弃|Discard)$") &&
		waitSelftestPage(ctx, window, `!document.getElementById('compose-subject')`, 5*time.Second)
}

func reviewDraftDiscarded(ctx context.Context, runtime *app.Runtime, draft app.LocalDraft) bool {
	return selftestEventually(ctx, func() bool {
		stored, err := runtime.DB.ComposeDraft(ctx, reviewAccount, draft.ID)
		if err != nil || !stored.Deleted || len(stored.Data) != 0 {
			return false
		}
		for _, ref := range draft.Attachments {
			_, err := runtime.DB.ComposeAttachment(ctx, strings.TrimPrefix(ref.Path, "compose-attachment:"))
			if !errors.Is(err, sql.ErrNoRows) {
				return false
			}
		}
		return true
	})
}

func selftestReviewCancel(ctx context.Context, runtime *app.Runtime, window *mygo.Window, checks map[string]bool, capture func(string)) bool {
	raw, err := reviewMIME(reviewCancelSubject, reviewOriginalHTML, false)
	if err != nil {
		return false
	}
	selftestMailbox.SetDraft(reviewCancelRemoteID, raw, "review-cancel-thread")
	draft, ok := reviewImport(ctx, runtime, window, reviewCancelRemoteID, reviewCancelSubject, reviewOriginalHTML)
	if !ok || !selftestSaveClose(ctx, window) {
		return false
	}
	compose := &app.ComposeService{Runtime: runtime}
	request := app.DraftRequest{AccountID: reviewAccount, DraftID: draft.ID}
	// Gmail import keeps quotes in the body rather than extracting them. Seed
	// separate quote metadata through the public service ONLY after save-close
	// has drained autosave. Latest subject/body edits below still come from UI.
	draft, err = compose.Get(ctx, request)
	if err != nil {
		return false
	}
	paths := make([]string, len(draft.Attachments))
	for i, ref := range draft.Attachments {
		paths[i] = ref.Path
	}
	_, err = compose.Save(ctx, app.SaveDraftRequest{
		AccountID: reviewAccount, ExistingDraftID: draft.ID, To: draft.To, CC: draft.CC, BCC: draft.BCC,
		Subject: draft.Subject, BodyHTML: draft.BodyHTML, BodyText: draft.BodyText, RawSource: draft.RawSource,
		EditorMode: draft.EditorMode, InReplyTo: draft.InReplyTo, QuotedReplyHTML: reviewQuote, AttachmentPaths: paths,
	})
	if err != nil || !reviewOpenLocal(ctx, window, draft) ||
		!reviewSetInput(ctx, window, "#compose-subject", reviewLatestSubject) ||
		!reviewSetInput(ctx, window, ".compose-source-input", reviewLatestHTML) ||
		!reviewSourceVisible(ctx, window, reviewLatestSubject, reviewLatestHTML) {
		return false
	}
	// Wait for a real latest autosave, capturing the browser-sanitized quote
	// rather than assuming its HTML serialization matches the seeded spelling.
	var latest app.LocalDraft
	if !selftestEventually(ctx, func() bool {
		var err error
		latest, err = compose.Get(ctx, request)
		return err == nil && latest.Subject == reviewLatestSubject && latest.RawSource == reviewLatestHTML &&
			latest.BodyHTML == reviewLatestHTML && latest.EditorMode == "html" &&
			strings.Contains(latest.QuotedReplyHTML, "Review retained quote") && len(latest.Attachments) == 1
	}) {
		return false
	}
	// Same old MIME, new remote revision: deterministic preflight rejection even
	// if the worker claims immediately. Never race ClaimOutbox/settlement in UI tests.
	selftestMailbox.SetDraft(reviewCancelRemoteID, raw, "review-cancel-thread")
	remoteUnchanged := reviewRemoteUnchanged(reviewCancelRemoteID, raw)
	before, err := runtime.DB.OutboxList(ctx, reviewAccount)
	if err != nil || !selftestClick(ctx, window, "body", "^(发送|Send)$") ||
		!waitSelftestPage(ctx, window, `!document.getElementById('compose-subject')`, 10*time.Second) {
		return false
	}
	var failed store.OutboxRecord
	checks[reviewSelftestChecks[0]] = selftestEventually(ctx, func() bool {
		items, err := (&app.OutboxService{Runtime: runtime}).List(ctx, app.AccountRequest{AccountID: reviewAccount})
		if err != nil || len(items) != len(before)+1 {
			return false
		}
		for _, item := range items {
			if item.Subject != reviewLatestSubject || item.Status != "failed" || !item.CloudDraft {
				continue
			}
			failed, err = runtime.DB.Outbox(ctx, reviewAccount, item.ID)
			copy, copyErr := runtime.DB.GmailDraftCopy(ctx, reviewAccount, draft.ID)
			retained, intact := reviewStoredDraft(ctx, runtime, draft.ID)
			_, getErr := compose.Get(ctx, request)
			return err == nil && !failed.Retryable && failed.Attempts == 1 && len(failed.Data) > 0 &&
				strings.Contains(failed.LastError, "Gmail draft changed") && copyErr == nil && copy.State == "sending" && copy.OutboxID == failed.ID &&
				errors.Is(getErr, store.ErrDraftSendProtected) && intact && reviewSameDraft(latest, retained) &&
				reviewQueuedContent(runtime, failed, latest) && remoteUnchanged()
		}
		return false
	})
	capture("review-linked-send-failed.png")
	if !checks[reviewSelftestChecks[0]] || !selftestClick(ctx, window, "body", "^(设置|Settings)$") ||
		!waitSelftestPage(ctx, window, `(() => {
			const tab=Array.from(document.querySelectorAll('[role="tab"]')).find(b=>/^(远端写回|Remote Writes)$/.test(b.textContent.trim()));
			if(!tab||tab.disabled)return false;tab.click();return true;
		})()`, 5*time.Second) || !waitSelftestPage(ctx, window, fmt.Sprintf(`(() => {
			const summary=Array.from(document.querySelectorAll('summary')).find(s=>/发件箱|Outbox/.test(s.textContent));
			if(!summary)return false;const panel=summary.closest('details');panel.open=true;
			const row=Array.from(panel.querySelectorAll('li')).find(r=>r.querySelector('strong')?.textContent===%q);
			const cancel=Array.from(row?.querySelectorAll('button')||[]).find(b=>/^(取消待发送邮件|Cancel queued send)$/.test(b.textContent.trim()));
			if(!cancel||cancel.disabled)return false;cancel.click();return true;
		})()`, reviewLatestSubject), 10*time.Second) ||
		!selftestClick(ctx, window, `[role="dialog"]`, "^(取消待发送邮件|Cancel queued send)$") {
		return false
	}
	if !selftestEventually(ctx, func() bool {
		row, err := runtime.DB.Outbox(ctx, reviewAccount, failed.ID)
		copy, copyErr := runtime.DB.GmailDraftCopy(ctx, reviewAccount, draft.ID)
		return err == nil && row.Status == "cancelled" && len(row.Data) == 0 && len(row.Summary) == 0 &&
			copyErr == nil && copy.State == "synced" && copy.OutboxID == ""
	}) || !waitSelftestPage(ctx, window, `!document.querySelector('[role="dialog"]')`, 5*time.Second) || !reviewOpenLocal(ctx, window, latest) {
		return false
	}
	quoteVisible := selftestClick(ctx, window, ".compose-quoted-reply", "^(显示引用原文|Show quoted message)$") &&
		waitSelftestPage(ctx, window, `document.querySelector('.compose-quoted-reply-content')?.innerText.includes('Review retained quote')`, 5*time.Second)
	restored, err := compose.Get(ctx, request)
	checks[reviewSelftestChecks[1]] = quoteVisible && err == nil && reviewSameDraft(latest, restored) &&
		waitSelftestPage(ctx, window, fmt.Sprintf(`document.querySelector('.compose-inline-panel')?.innerText.includes(%q)`, reviewAttachmentName), 5*time.Second)
	checks[reviewSelftestChecks[2]] = checks[reviewSelftestChecks[1]] && reviewAttachmentsIntact(ctx, runtime, restored, raw) && remoteUnchanged()
	capture("review-cancel-restored.png")
	// Establish a real dirty edit so Back takes the explicit Discard path even
	// when the restored snapshot itself is already saved.
	if !checks[reviewSelftestChecks[2]] || !reviewSetInput(ctx, window, "#compose-subject", "Review cancel discard") ||
		!reviewCloseCompose(ctx, window) || !reviewDraftDiscarded(ctx, runtime, restored) {
		return false
	}
	return remoteUnchanged()
}

func selftestReviewHTML(ctx context.Context, runtime *app.Runtime, window *mygo.Window, checks map[string]bool, capture func(string)) bool {
	raw, err := reviewMIME(reviewHTMLSubject, reviewUnsupportedHTML, true)
	if err != nil {
		return false
	}
	selftestMailbox.SetDraft(reviewHTMLRemoteID, raw, "review-html-thread")
	remoteUnchanged := reviewRemoteUnchanged(reviewHTMLRemoteID, raw)
	draft, ok := reviewImport(ctx, runtime, window, reviewHTMLRemoteID, reviewHTMLSubject, reviewUnsupportedHTML)
	if !ok {
		return false
	}
	for i, mode := range []string{"^(富文本|Rich Text)$", "^Markdown$"} {
		checks[reviewSelftestChecks[3+i]] = selftestClick(ctx, window, ".compose-mode-tabs", mode) &&
			reviewSourceVisible(ctx, window, reviewHTMLSubject, reviewUnsupportedHTML) &&
			waitSelftestPage(ctx, window, `Array.from(document.querySelectorAll('.compose-form-shell [role="alert"]')).some(a=>a.getClientRects().length>0 && /The editor mode was not changed because the chosen editor cannot preserve|编辑模式未更改，因为所选编辑器无法保留/.test(a.innerText))`, 5*time.Second)
		capture(fmt.Sprintf("review-html-conversion-%d.png", i+1))
		if !checks[reviewSelftestChecks[3+i]] {
			return false
		}
	}
	if !selftestSaveClose(ctx, window) {
		return false
	}
	saved, err := (&app.ComposeService{Runtime: runtime}).Get(ctx, app.DraftRequest{AccountID: reviewAccount, DraftID: draft.ID})
	checks[reviewSelftestChecks[5]] = err == nil && saved.EditorMode == "html" && saved.Subject == draft.Subject &&
		saved.BodyHTML == draft.BodyHTML && saved.RawSource == draft.RawSource && saved.QuotedReplyHTML == draft.QuotedReplyHTML &&
		reflect.DeepEqual(saved.Attachments, draft.Attachments) && saved.References == draft.References &&
		saved.InReplyTo == draft.InReplyTo && reviewAttachmentsIntact(ctx, runtime, saved, raw) && remoteUnchanged()
	capture("review-html-saved.png")
	if !checks[reviewSelftestChecks[5]] || !reviewOpenLocal(ctx, window, saved) ||
		!reviewSetInput(ctx, window, "#compose-subject", "Review HTML discard") || !reviewCloseCompose(ctx, window) {
		return false
	}
	return reviewDraftDiscarded(ctx, runtime, saved) && remoteUnchanged()
}

func selftestReviewFixes(ctx context.Context, runtime *app.Runtime, window *mygo.Window, checks map[string]bool, capture func(string)) {
	// Earlier lifecycle assertions depend on exact fixture counts. Run after
	// all 40 original checks (except quit), and only from its known clean state.
	if !checks["Gmail remote draft deleted through UI"] || selftestMailbox == nil || !reviewCloseCompose(ctx, window) {
		return
	}
	if !selftestReviewCancel(ctx, runtime, window, checks, capture) {
		return
	}
	checks[reviewSelftestChecks[6]] = selftestReviewHTML(ctx, runtime, window, checks, capture)
}
