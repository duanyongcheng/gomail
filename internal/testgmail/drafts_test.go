package testgmail

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"

	"github.com/bary/pebble-go/internal/googleapi"
)

type draftToken string

func (t draftToken) Token(context.Context) (string, error) { return string(t), nil }

func TestDraftFixtureListMetadataSendAndDelete(t *testing.T) {
	mock := NewMock()
	defer mock.Close()
	api := mock.API(draftToken("demo-access"))
	for i := 0; i < 23; i++ {
		raw := []byte(fmt.Sprintf("From: sender@example.invalid\r\nSubject: Draft %d\r\nMessage-ID: <draft-%d@pebble.invalid>\r\n\r\nbody", i, i))
		mock.SetDraft(fmt.Sprintf("seed-%02d", i), raw, "reply-thread")
	}
	first, err := api.ListDrafts(t.Context(), "")
	if err != nil || len(first.Drafts) != 20 || first.Next == "" {
		t.Fatal(first, err)
	}
	second, err := api.ListDrafts(t.Context(), first.Next)
	if err != nil || len(second.Drafts) != 3 || second.Next != "" {
		t.Fatal(second, err)
	}
	found, err := api.FindDraft(t.Context(), "draft-5@pebble.invalid")
	if err != nil || len(found.Drafts) != 1 || found.Drafts[0].ID != "seed-05" {
		t.Fatal(found, err)
	}
	metadata, err := api.DraftMetadata(t.Context(), "seed-05")
	if err != nil || metadata.Message.ThreadID != "reply-thread" || len(metadata.Message.Payload.Headers) != 3 {
		t.Fatal(metadata, err)
	}
	raw := []byte("From: sender@example.invalid\r\nSubject: frozen snapshot\r\n\r\nnew contents")
	sent := api.SendDraftRaw(t.Context(), "seed-05", raw, "reply-thread")
	if sent.RemoteID == "" || sent.Unknown {
		t.Fatal(sent)
	}
	mock.Lock()
	_, present := mock.Drafts["seed-05"]
	unchanged := len(mock.Drafts) == 22 && mock.DraftSends == 1 && mock.DraftDeletes == 0
	payload := string(mock.SentRaw[0])
	mock.Unlock()
	if present || !unchanged || payload != string(raw) {
		t.Fatal(present, unchanged, payload)
	}
	again := api.SendDraftRaw(t.Context(), "seed-05", raw, "")
	if again.RemoteID != "" || again.Unknown || again.Retryable {
		t.Fatal("missing draft fallback", again)
	}
	if _, err := api.DeleteDraft(t.Context(), "seed-06"); err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	deleted := len(mock.Drafts) == 21 && mock.DraftDeletes == 1 && len(mock.SentRaw) == 1
	mock.Unlock()
	if !deleted {
		t.Fatal("wrong delete/send effect")
	}
}

func TestDraftFixtureRejectedAndLostSend(t *testing.T) {
	mock := NewMock()
	defer mock.Close()
	api := mock.API(draftToken("demo-access"))
	raw := []byte("From: sender@example.invalid\r\n\r\nmessage")
	mock.SetDraft("draft", raw, "")
	mock.Lock()
	mock.SendStatus = 400
	mock.Unlock()
	if result := api.SendDraftRaw(t.Context(), "draft", raw, ""); result.RemoteID != "" || result.Unknown {
		t.Fatal(result)
	}
	mock.Lock()
	retained := len(mock.Drafts) == 1 && len(mock.SentRaw) == 0
	mock.SendStatus = 0
	mock.LoseSendResponse = true
	mock.Unlock()
	if !retained {
		t.Fatal("rejection consumed draft")
	}
	if result := api.SendDraftRaw(t.Context(), "draft", raw, ""); !result.Unknown || result.Retryable {
		t.Fatal(result)
	}
	mock.Lock()
	consumed := len(mock.Drafts) == 0 && len(mock.SentRaw) == 1 && mock.DraftSends == 2
	mock.Unlock()
	if !consumed {
		t.Fatal("lost response did not represent accepted send")
	}
}

func TestDraftFixtureLostDeleteAndCreateIdentity(t *testing.T) {
	mock := NewMock()
	defer mock.Close()
	api := mock.API(draftToken("demo-access"))
	raw := []byte("From: sender@example.invalid\r\n\r\nmessage")
	first, err := api.WriteDraft(t.Context(), "", raw, "")
	if err != nil {
		t.Fatal(err)
	}
	mock.Lock()
	mock.LoseDraftDeleteResponse = true
	mock.Unlock()
	if result, err := api.DeleteDraft(t.Context(), first.Draft.ID); err == nil || !result.Unknown {
		t.Fatal(result, err)
	}
	if _, err := api.Draft(t.Context(), first.Draft.ID); err == nil {
		t.Fatal("deleted draft still exists")
	} else {
		var httpErr *googleapi.HTTPError
		if !errors.As(err, &httpErr) || httpErr.Status != 404 {
			t.Fatal(err)
		}
	}
	next, err := api.WriteDraft(t.Context(), "", raw, "")
	if err != nil || next.Draft.ID == first.Draft.ID {
		t.Fatal("reused deleted identity", next, err)
	}
	remote, err := api.Draft(t.Context(), next.Draft.ID)
	if err != nil || remote.Message.Raw != base64.RawURLEncoding.EncodeToString(raw) {
		t.Fatal(remote, err)
	}
}
