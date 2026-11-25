package store

import (
	"bytes"
	"strings"
	"testing"

	"github.com/duanyongcheng/gomail/internal/domain"
)

func TestAISettingsPersistenceAndEncryptedDataFlag(t *testing.T) {
	db := openStore(t)
	ctx := t.Context()

	got, err := db.AISettings(ctx)
	if err != nil || got != nil {
		t.Fatalf("empty read = (%v, %v), want (nil, nil)", got, err)
	}
	if has, err := db.HasEncryptedData(ctx); err != nil || has {
		t.Fatalf("empty HasEncryptedData = (%v, %v), want (false, nil)", has, err)
	}
	if err := db.SaveAISettings(ctx, nil, 100); err == nil {
		t.Fatal("empty payload accepted")
	}
	if err := db.SaveAISettings(ctx, bytes.Repeat([]byte("x"), maxAISettingsBytes+1), 100); err == nil {
		t.Fatal("oversized payload accepted")
	}

	first := []byte("encrypted-ai-v1")
	if err := db.SaveAISettings(ctx, first, 100); err != nil {
		t.Fatal(err)
	}
	if has, err := db.HasEncryptedData(ctx); err != nil || !has {
		t.Fatalf("HasEncryptedData with AI settings = (%v, %v), want (true, nil)", has, err)
	}
	got, err = db.AISettings(ctx)
	if err != nil || !bytes.Equal(got, first) {
		t.Fatalf("read = (%q, %v), want %q", got, err, first)
	}

	second := []byte("encrypted-ai-v2")
	if err := db.SaveAISettings(ctx, second, 200); err != nil {
		t.Fatal(err)
	}
	got, err = db.AISettings(ctx)
	if err != nil || !bytes.Equal(got, second) {
		t.Fatalf("updated read = (%q, %v), want %q", got, err, second)
	}

	if err := db.DeleteAISettings(ctx); err != nil {
		t.Fatal(err)
	}
	got, err = db.AISettings(ctx)
	if err != nil || got != nil {
		t.Fatalf("deleted read = (%v, %v), want (nil, nil)", got, err)
	}
	if has, err := db.HasEncryptedData(ctx); err != nil || has {
		t.Fatalf("deleted HasEncryptedData = (%v, %v), want (false, nil)", has, err)
	}
}

func TestAICacheCRUDStatsAndAccountCleanup(t *testing.T) {
	db := openStore(t)
	ctx := t.Context()

	if err := db.SaveAccount(ctx, domain.Account{
		ID: "acc-1", Email: "a@example.test", DisplayName: "A", Provider: "gmail", CreatedAt: 1, UpdatedAt: 1,
	}, nil); err != nil {
		t.Fatal(err)
	}

	if got, err := db.GetAICache(ctx, "msg-1", "summary", "zh"); err != nil || got != nil {
		t.Fatalf("empty cache = (%v, %v), want (nil, nil)", got, err)
	}
	if err := db.SaveAICache(ctx, AICacheEntry{
		TargetID: "msg-1", AccountID: "acc-1", Kind: "summary", TargetLang: "zh",
		ContentHash: "hash1", Model: "gpt-4o-mini", Payload: `{"tldr":"ok"}`, CreatedAt: 10, UpdatedAt: 10,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAICache(ctx, AICacheEntry{
		TargetID: "thread-1", AccountID: "acc-1", Kind: "thread_summary", TargetLang: "zh",
		ContentHash: "hash2", Model: "gpt-4o-mini", Payload: `{"tldr":"thread"}`, CreatedAt: 11, UpdatedAt: 11,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveAICache(ctx, AICacheEntry{
		TargetID: "msg-1", AccountID: "acc-1", Kind: "translation", TargetLang: "zh",
		ContentHash: "hash1", Model: "gpt-4o-mini", Payload: `{"subject":"标题"}`, CreatedAt: 12, UpdatedAt: 12,
	}); err != nil {
		t.Fatal(err)
	}

	if err := db.SaveAICache(ctx, AICacheEntry{
		TargetID: "msg-big", AccountID: "acc-1", Kind: "translation", TargetLang: "zh",
		ContentHash: "h", Model: "m", Payload: strings.Repeat("a", maxAICacheBytes+1),
	}); err == nil {
		t.Fatal("oversized cache payload accepted")
	}

	got, err := db.GetAICache(ctx, "msg-1", "summary", "zh")
	if err != nil || got == nil || got.Payload != `{"tldr":"ok"}` || got.Model != "gpt-4o-mini" {
		t.Fatalf("got = (%+v, %v)", got, err)
	}

	stats, err := db.AICacheStats(ctx)
	if err != nil || stats.SummaryCount != 2 || stats.TranslationCount != 1 {
		t.Fatalf("stats = (%+v, %v), want summary=2 translation=1", stats, err)
	}

	// Upsert updates payload & updated_at
	if err := db.SaveAICache(ctx, AICacheEntry{
		TargetID: "msg-1", AccountID: "acc-1", Kind: "summary", TargetLang: "zh",
		ContentHash: "hash1-v2", Model: "gpt-4.1", Payload: `{"tldr":"v2"}`, UpdatedAt: 20,
	}); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetAICache(ctx, "msg-1", "summary", "zh")
	if err != nil || got == nil || got.Payload != `{"tldr":"v2"}` || got.Model != "gpt-4.1" || got.CreatedAt != 10 || got.UpdatedAt != 20 {
		t.Fatalf("updated got = (%+v, %v)", got, err)
	}

	// Deleting account cleans up its AI cache entries
	if err := db.DeleteAccount(ctx, "acc-1"); err != nil {
		t.Fatal(err)
	}
	stats, err = db.AICacheStats(ctx)
	if err != nil || stats.SummaryCount != 0 || stats.TranslationCount != 0 {
		t.Fatalf("stats after delete account = (%+v, %v), want 0/0", stats, err)
	}

	// ClearAICache clears remaining entries
	if err := db.SaveAICache(ctx, AICacheEntry{
		TargetID: "msg-2", AccountID: "acc-2", Kind: "summary", TargetLang: "en",
		ContentHash: "h", Model: "m", Payload: `{"tldr":"x"}`, UpdatedAt: 30,
	}); err != nil {
		t.Fatal(err)
	}
	deleted, err := db.ClearAICache(ctx)
	if err != nil || deleted != 1 {
		t.Fatalf("ClearAICache = (%d, %v), want (1, nil)", deleted, err)
	}
}
