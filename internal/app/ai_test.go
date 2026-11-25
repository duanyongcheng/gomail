package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/duanyongcheng/gomail/internal/ai"
	"github.com/duanyongcheng/gomail/internal/crypto"
	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/store"
)

func openTestAIRuntime(t *testing.T, keyring crypto.Keyring) (*Runtime, *AIService) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "gomail.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	r := New(t.Context(), db, keyring, nil)
	return r, &AIService{Runtime: r}
}

func seedTestMessage(t *testing.T, db *store.DB, id, threadID, subject, body string) {
	t.Helper()
	if err := db.SaveAccount(t.Context(), domain.Account{
		ID: "acc-1", Email: "me@example.test", DisplayName: "Me", Provider: "gmail", CreatedAt: 1, UpdatedAt: 1,
	}, nil); err != nil {
		t.Fatal(err)
	}
	var tid *string
	if threadID != "" {
		tid = &threadID
	}
	if err := db.CommitSnapshot(t.Context(), store.Snapshot{
		AccountID: "acc-1",
		Folders: []domain.Folder{{
			ID: "f-inbox", AccountID: "acc-1", RemoteID: "INBOX", Name: "Inbox", FolderType: "folder", Role: strPtr("inbox"),
		}},
		Messages: []store.CachedMessage{{
			Message: domain.Message{
				ID:          id,
				AccountID:   "acc-1",
				RemoteID:    "r-" + id,
				ThreadID:    tid,
				Subject:     subject,
				FromAddress: "sender@example.test",
				FromName:    "Alice",
				To:          []domain.Address{{Address: "me@example.test"}},
				CC:          []domain.Address{},
				BCC:         []domain.Address{},
				BodyText:    body,
				Date:        1700000000,
				CreatedAt:   1700000000,
				UpdatedAt:   1700000000,
			},
			FolderIDs: []string{"f-inbox"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func strPtr(s string) *string { return &s }

func TestAIServiceEncryptedConfigKeyringProtectionAndNoPlaintextLeak(t *testing.T) {
	kr := &testKeyring{key: bytes.Repeat([]byte{9}, 32)}
	r, svc := openTestAIRuntime(t, kr)

	view, err := svc.GetConfig(t.Context(), Empty{})
	if err != nil || view.Configured || view.HasAPIKey {
		t.Fatalf("initial view = (%+v, %v)", view, err)
	}

	saved, err := svc.SaveConfig(t.Context(), SaveAIConfigRequest{
		Enabled:              true,
		Provider:             ai.ProviderOpenAI,
		Preset:               "openai",
		Endpoint:             "https://api.openai.com/v1",
		APIKey:               "sk-super-secret-9876",
		Model:                "gpt-4o-mini",
		APIMode:              ai.APIModeCompletions,
		ProxyMode:            ai.ProxyInherit,
		SummaryLanguage:      "zh",
		SummaryStyle:         ai.SummaryStyleStandard,
		TranslateLanguage:    "zh",
		TranslateDisplayMode: ai.TranslateDisplayBilingual,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Configured || !saved.Enabled || !saved.HasAPIKey || saved.MaskedAPIKey != "••••9876" {
		t.Fatalf("saved view = %+v", saved)
	}
	// Verify stored DB row is encrypted (does not contain plaintext secret)
	raw, err := r.DB.AISettings(t.Context())
	if err != nil || strings.Contains(string(raw), "sk-super-secret-9876") {
		t.Fatalf("raw DB settings leaked secret or failed: %v", err)
	}

	// KeepExistingKey preserves saved secret when APIKey is omitted
	updated, err := svc.SaveConfig(t.Context(), SaveAIConfigRequest{
		Enabled:         true,
		Provider:        ai.ProviderOpenAI,
		Preset:          "openai",
		Endpoint:        "https://api.openai.com/v1",
		APIKey:          "",
		KeepExistingKey: true,
		Model:           "gpt-4.1-mini",
	})
	if err != nil || !updated.HasAPIKey || updated.MaskedAPIKey != "••••9876" || updated.Model != "gpt-4.1-mini" {
		t.Fatalf("updated view = (%+v, %v)", updated, err)
	}

	// Simulate fresh restart with locked keyring: must report KeyringError and preserve ciphertext
	rLocked := New(t.Context(), r.DB, nil, nil)
	if err := rLocked.Restore(); err != nil {
		t.Fatalf("Restore should not fail solely on AI settings: %v", err)
	}
	svcLocked := &AIService{Runtime: rLocked}
	lockedView, err := svcLocked.GetConfig(t.Context(), Empty{})
	if err != nil || !lockedView.KeyringError || lockedView.HasAPIKey {
		t.Fatalf("lockedView = (%+v, %v)", lockedView, err)
	}
	if _, err := svcLocked.TranslateText(t.Context(), TranslateTextRequest{Text: "hi", ToLang: "zh"}); err == nil || !strings.Contains(err.Error(), "AI_KEYRING_LOCKED") {
		t.Fatalf("expected AI_KEYRING_LOCKED, got %v", err)
	}

	// ClearConfig removes settings cleanly
	cleared, err := svc.ClearConfig(t.Context(), Empty{})
	if err != nil || cleared.Configured || cleared.HasAPIKey {
		t.Fatalf("cleared = (%+v, %v)", cleared, err)
	}
}

func TestAIServiceSummarizeStreamCacheThreadAndCancel(t *testing.T) {
	var hits atomic.Int32
	blockCh := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			msgs, _ := body["messages"].([]any)
			var promptText string
			if len(msgs) > 0 {
				if m, ok := msgs[len(msgs)-1].(map[string]any); ok {
					promptText, _ = m["content"].(string)
				}
			}
			if strings.Contains(promptText, "SLOW_CANCEL_TEST") {
				w.Header().Set("Content-Type", "text/event-stream")
				flusher, _ := w.(http.Flusher)
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Start \"}}]}\n\n"))
				if flusher != nil {
					flusher.Flush()
				}
				select {
				case <-r.Context().Done():
					return
				case <-blockCh:
					return
				}
			}
			hits.Add(1)
			if strings.Contains(promptText, `"items"`) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"subject\",\"text\":\"季度更新\"},{\"id\":\"0\",\"text\":\"请查收附件。\"}]}"}}]}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"核心结论：周五发布。\\n\\n- 已完成测试\\n\\n待办事项：\\n- 确认发布清单（周五前）\"}}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		}
	}))
	defer srv.Close()
	defer close(blockCh)

	r, svc := openTestAIRuntime(t, &testKeyring{key: bytes.Repeat([]byte{5}, 32)})
	var eventsMu sync.Mutex
	var events []AIStreamEvent
	r.OnAIStream = func(e AIStreamEvent) {
		eventsMu.Lock()
		events = append(events, e)
		eventsMu.Unlock()
	}

	seedTestMessage(t, r.DB, "m-1", "native-thread-1", "Release Plan", "We are shipping Friday. Please confirm checklist.")
	seedTestMessage(t, r.DB, "m-2", "native-thread-1", "Re: Release Plan", "Looks good, I will confirm by Thursday.")
	seedTestMessage(t, r.DB, "m-slow", "", "Slow", "SLOW_CANCEL_TEST")

	_, err := svc.SaveConfig(t.Context(), SaveAIConfigRequest{
		Enabled:  true,
		Provider: ai.ProviderOpenAI,
		Endpoint: srv.URL + "/v1",
		APIKey:   "sk-local",
		Model:    "gpt-4o-mini",
	})
	if err != nil {
		t.Fatal(err)
	}

	// First summary hits LLM and streams
	res1, err := svc.SummarizeMessage(t.Context(), SummarizeMessageRequest{
		RequestID: "req-1", MessageID: "m-1", TargetLang: "zh",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res1.Cached || res1.TLDR != "核心结论：周五发布。" || len(res1.ActionItems) != 1 || res1.ActionItems[0].Due != "周五前" {
		t.Fatalf("res1 = %+v", res1)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}

	// Second summary hits SQLite cache without network call
	res2, err := svc.SummarizeMessage(t.Context(), SummarizeMessageRequest{
		RequestID: "req-2", MessageID: "m-1", TargetLang: "zh",
	})
	if err != nil || !res2.Cached || hits.Load() != 1 {
		t.Fatalf("res2 = (%+v, %v), hits=%d", res2, err, hits.Load())
	}

	// Thread summary works with public thread handle
	publicThreadID, err := r.DB.PublicThreadID(t.Context(), "m-1")
	if err != nil {
		t.Fatal(err)
	}
	threadRes, err := svc.SummarizeThread(t.Context(), SummarizeThreadRequest{
		RequestID: "req-thread", ThreadID: publicThreadID, TargetLang: "zh",
	})
	if err != nil || threadRes.TLDR == "" {
		t.Fatalf("threadRes = (%+v, %v)", threadRes, err)
	}

	// Batch translation + cache round-trip
	batchRes, err := svc.TranslateBatch(t.Context(), TranslateBatchRequest{
		RequestID: "req-batch",
		Items:     []ai.TranslateItem{{ID: "subject", Text: "Q3 Update"}, {ID: "0", Text: "Please see attached."}},
		FromLang:  "auto",
		ToLang:    "zh",
	})
	if err != nil || batchRes.Translated != 2 || batchRes.Items[0].Text != "季度更新" {
		t.Fatalf("batchRes = (%+v, %v)", batchRes, err)
	}
	if _, err := svc.SaveTranslationCache(t.Context(), SaveTranslationCacheRequest{
		MessageID: "m-1", AccountID: "acc-1", TargetLang: "zh", ContentHash: "h1", Model: "gpt-4o-mini",
		Subject: "季度更新", Segments: map[string]string{"0": "请查收附件。"},
	}); err != nil {
		t.Fatal(err)
	}
	cachedTr, err := svc.GetTranslationCache(t.Context(), CachedTranslationRequest{
		MessageID: "m-1", TargetLang: "zh", ContentHash: "h1",
	})
	if err != nil || !cachedTr.Found || cachedTr.Subject != "季度更新" || cachedTr.Segments["0"] != "请查收附件。" {
		t.Fatalf("cachedTr = (%+v, %v)", cachedTr, err)
	}

	// Cancel in-flight summary request
	cancelErrCh := make(chan error, 1)
	go func() {
		_, err := svc.SummarizeMessage(context.Background(), SummarizeMessageRequest{
			RequestID: "req-cancel", MessageID: "m-slow", TargetLang: "zh",
		})
		cancelErrCh <- err
	}()
	time.Sleep(60 * time.Millisecond)
	_, _ = svc.Cancel(t.Context(), CancelAIRequest{RequestID: "req-cancel"})
	select {
	case err := <-cancelErrCh:
		if err == nil || !strings.Contains(err.Error(), "AI_CANCELLED") {
			t.Fatalf("expected AI_CANCELLED, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Cancel did not abort in-flight AI request in time")
	}

	// ClearCache resets stats
	cleared, err := svc.ClearCache(t.Context(), Empty{})
	if err != nil || cleared.Deleted < 3 {
		t.Fatalf("ClearCache = (%+v, %v)", cleared, err)
	}
	eventsMu.Lock()
	eventCount := len(events)
	eventsMu.Unlock()
	if eventCount < 2 {
		t.Fatalf("expected at least 2 stream events, got %d", eventCount)
	}
}
