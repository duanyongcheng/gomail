package main

import (
	"bytes"
	"encoding/json"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/app"
	"github.com/duanyongcheng/pebble-go/internal/domain"
	"github.com/duanyongcheng/pebble-go/internal/mailmime"
	"github.com/duanyongcheng/pebble-go/internal/provider/gmail"
	"github.com/duanyongcheng/pebble-go/internal/store"
	"github.com/duanyongcheng/pebble-go/internal/testgmail"
)

func TestReviewSelftestImportedEncryptedOracles(t *testing.T) {
	for _, inline := range []bool{false, true} {
		subject, html := reviewCancelSubject, reviewOriginalHTML
		if inline {
			subject, html = reviewHTMLSubject, reviewUnsupportedHTML
		}
		t.Run(subject, func(t *testing.T) {
			raw, err := reviewMIME(subject, html, inline)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := mailmime.ParseWithLimit(raw, mailmime.MaxOutgoingBytes)
			if err != nil || parsed.BodyHTML != html || parsed.Subject != subject || strings.Contains(string(raw), reviewLatestSubject) {
				t.Fatalf("invalid remote fixture: %v", err)
			}
			if inline {
				if len(parsed.Attachments) != 2 || parsed.Attachments[1].ContentID != "review-logo" || !parsed.Attachments[1].Inline {
					t.Fatal("HTML fixture lost inline image metadata")
				}
				if _, err := png.Decode(bytes.NewReader(parsed.Attachments[1].Data)); err != nil {
					t.Fatalf("invalid inline PNG: %v", err)
				}
			}
			db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "review.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if err := db.SaveAccount(t.Context(), domain.Account{ID: reviewAccount, Email: "demo@example.invalid", Provider: "gmail"}, nil); err != nil {
				t.Fatal(err)
			}
			runtime := app.New(t.Context(), db, &selftestKeyring{key: bytes.Repeat([]byte{7}, 32)}, nil)
			t.Cleanup(runtime.Scheduler.Close)
			mock := testgmail.NewMock()
			t.Cleanup(mock.Close)
			mock.ProfileEmail = "demo@example.invalid"
			messageID := mock.SetDraft("review-fixture", raw, "review-thread")
			runtime.InstallProvider(reviewAccount, gmail.New(db, mock.API(selftestToken{}), reviewAccount))
			compose := &app.ComposeService{Runtime: runtime}
			imported, err := compose.CloudImport(t.Context(), app.CloudDraftImportRequest{AccountID: reviewAccount, RemoteDraftID: "review-fixture", ExpectedMessageID: messageID})
			if err != nil || imported.BodyHTML != html || imported.RawSource != html || imported.EditorMode != "html" {
				t.Fatalf("import fixture failed: %v", err)
			}
			got, err := compose.Get(t.Context(), app.DraftRequest{AccountID: reviewAccount, DraftID: imported.ID})
			stored, ok := reviewStoredDraft(t.Context(), runtime, imported.ID)
			if err != nil || !ok || !reviewSameDraft(got, stored) || !reviewAttachmentsIntact(t.Context(), runtime, got, raw) {
				t.Fatalf("valid encrypted snapshot rejected: %v", err)
			}
			bad := got
			bad.Attachments = append([]app.DraftAttachment(nil), got.Attachments...)
			bad.Attachments[0].Size++
			if reviewAttachmentsIntact(t.Context(), runtime, bad, raw) {
				t.Fatal("accepted incorrect attachment size")
			}
			bad.Attachments[0] = got.Attachments[0]
			bad.Attachments[0].Path += "missing"
			if reviewAttachmentsIntact(t.Context(), runtime, bad, raw) {
				t.Fatal("accepted missing attachment ref")
			}
			if _, err := compose.Delete(t.Context(), app.DraftRequest{AccountID: reviewAccount, DraftID: got.ID}); err != nil {
				t.Fatal(err)
			}
			if !reviewDraftDiscarded(t.Context(), runtime, got) || reviewAttachmentsIntact(t.Context(), runtime, got, raw) {
				t.Fatal("discard oracle did not observe deleted encrypted bytes")
			}
		})
	}
}

func TestReviewSelftestSnapshotRejectsLostSourceQuoteAndAttachments(t *testing.T) {
	original := app.LocalDraft{ID: "review", AccountID: reviewAccount, Subject: reviewLatestSubject,
		EditorMode: "html", RawSource: reviewLatestHTML, BodyHTML: reviewLatestHTML, QuotedReplyHTML: reviewQuote,
		Attachments: []app.DraftAttachment{{Name: reviewAttachmentName, Path: "compose-attachment:retained", Size: 10}}}
	newer := original
	newer.UpdatedAt = 99
	if !reviewSameDraft(original, newer) {
		t.Fatal("semantic snapshot treated autosave timestamp as an edit")
	}
	for _, mutate := range []func(*app.LocalDraft){
		func(d *app.LocalDraft) { d.Subject = reviewCancelSubject },
		func(d *app.LocalDraft) { d.RawSource = reviewOriginalHTML },
		func(d *app.LocalDraft) { d.BodyHTML = reviewOriginalHTML },
		func(d *app.LocalDraft) { d.QuotedReplyHTML = "" },
		func(d *app.LocalDraft) { d.EditorMode = "rich" },
		func(d *app.LocalDraft) { d.Attachments = nil },
	} {
		changed := original
		mutate(&changed)
		if reviewSameDraft(original, changed) {
			t.Fatal("snapshot oracle accepted lost latest content")
		}
	}
}

func TestReviewSelftestQueuedContentOracle(t *testing.T) {
	runtime := &app.Runtime{Keyring: &selftestKeyring{key: bytes.Repeat([]byte{7}, 32)}}
	latest := app.LocalDraft{Subject: reviewLatestSubject, RawSource: reviewLatestHTML, QuotedReplyHTML: reviewQuote,
		InReplyTo: "review-parent@example.invalid", References: "review-parent@example.invalid"}
	raw, err := reviewMIME(latest.Subject, latest.RawSource+"<br/><br/>"+latest.QuotedReplyHTML, false)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := json.Marshal(struct{ Raw []byte }{Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := reviewCipher(runtime)
	if err != nil {
		t.Fatal(err)
	}
	row := store.OutboxRecord{AccountID: reviewAccount, ID: "review-outbox"}
	row.Data, err = cipher.Encrypt("outbox.data", row.AccountID+"\x00"+row.ID, plain)
	if err != nil || !reviewQueuedContent(runtime, row, latest) {
		t.Fatalf("valid immutable MIME rejected: %v", err)
	}
	wrong := latest
	wrong.QuotedReplyHTML = "lost quote"
	if reviewQueuedContent(runtime, row, wrong) {
		t.Fatal("queued content oracle accepted a missing quote")
	}
	row.Data[len(row.Data)-1] ^= 1
	if reviewQueuedContent(runtime, row, latest) {
		t.Fatal("queued content oracle accepted tampered ciphertext")
	}
}

func TestReviewSelftestRemoteRevisionAndNoSendOracle(t *testing.T) {
	mock := testgmail.NewMock()
	t.Cleanup(mock.Close)
	previous := selftestMailbox
	selftestMailbox = mock
	t.Cleanup(func() { selftestMailbox = previous })
	raw, err := reviewMIME(reviewCancelSubject, reviewOriginalHTML, false)
	if err != nil {
		t.Fatal(err)
	}
	first := mock.SetDraft(reviewCancelRemoteID, raw, "review-thread")
	second := mock.SetDraft(reviewCancelRemoteID, raw, "review-thread")
	if first == second {
		t.Fatal("synthetic revision knob cannot reject preflight")
	}
	unchanged := reviewRemoteUnchanged(reviewCancelRemoteID, raw)
	if !unchanged() {
		t.Fatal("unchanged remote rejected")
	}
	mock.Lock()
	mock.Requests = append(mock.Requests, "POST /gmail/v1/users/me/messages/send")
	mock.Unlock()
	if unchanged() {
		t.Fatal("failed fallback send attempt was accepted as no-send")
	}
	unchanged = reviewRemoteUnchanged(reviewCancelRemoteID, raw)
	mock.SetDraft(reviewCancelRemoteID, raw, "review-thread")
	if unchanged() {
		t.Fatal("unexpected remote revision change was accepted")
	}
	seen := map[string]bool{}
	for _, check := range reviewSelftestChecks {
		if check == "" || seen[check] {
			t.Fatal("review checks must have distinct explicit names")
		}
		seen[check] = true
	}
	if len(seen) != 7 {
		t.Fatal("expected seven new runtime checks")
	}
}
