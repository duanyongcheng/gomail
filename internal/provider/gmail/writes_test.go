package gmail_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/duanyongcheng/gomail/internal/store"
)

func TestReplayQueuedWritesUsesPersistedPayloadAndIsolatesAccounts(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "profile.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, mock, _ := fixture(t, db, "a")
	payload, _ := json.Marshal(store.MailOpPayload{RemoteID: "m1", AddLabels: []string{"STARRED"}, RemoveLabels: []string{"UNREAD"}})
	if _, err := p.Replay(t.Context(), "update_flags", payload); err != nil {
		t.Fatal(err)
	}
	message, err := p.API.Message(t.Context(), "m1")
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]bool{}
	for _, label := range message.Labels {
		labels[label] = true
	}
	if labels["UNREAD"] || !labels["STARRED"] {
		t.Fatal("replayed flags not applied", message.Labels)
	}
	archivePayload, _ := json.Marshal(map[string]any{"remote_id": "m1", "remove_labels": []string{"INBOX"}})
	if _, err := p.Replay(t.Context(), "archive", archivePayload); err != nil {
		t.Fatal(err)
	}
	message, _ = p.API.Message(t.Context(), "m1")
	for _, label := range message.Labels {
		if label == "INBOX" {
			t.Fatal("archive did not remove INBOX")
		}
	}
	for _, test := range []struct {
		opType  string
		payload string
	}{
		{"delete_permanent", `{"remote_id":"m1"}`},
		{"unknown_op", `{"remote_id":"m1"}`},
		{"archive", `{not json`},
		{"archive", `{}`},
	} {
		if _, err := p.Replay(t.Context(), test.opType, json.RawMessage(test.payload)); err == nil {
			t.Fatalf("unsupported replay accepted: %+v", test)
		}
	}
	mock.Lock()
	mock.FailMessage = "m1"
	mock.Unlock()
	if outcome, err := p.Replay(t.Context(), "archive", archivePayload); err == nil || outcome {
		t.Fatal("server failure must be retryable, not outcome-unknown", outcome, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := p.Replay(ctx, "archive", archivePayload); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}
