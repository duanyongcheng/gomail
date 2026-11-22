package store

import (
	"bytes"
	"testing"
)

func TestGoogleOAuthSettingsPersistenceAndEncryptedDataFlag(t *testing.T) {
	db := openStore(t)
	ctx := t.Context()

	got, err := db.GoogleOAuthSettings(ctx)
	if err != nil || got != nil {
		t.Fatalf("empty read = (%v, %v), want (nil, nil)", got, err)
	}
	if has, err := db.HasEncryptedData(ctx); err != nil || has {
		t.Fatalf("empty HasEncryptedData = (%v, %v), want (false, nil)", has, err)
	}
	if err := db.SaveGoogleOAuthSettings(ctx, nil); err == nil {
		t.Fatal("empty payload accepted")
	}
	if err := db.SaveGoogleOAuthSettings(ctx, bytes.Repeat([]byte("x"), 16<<10+1)); err == nil {
		t.Fatal("oversized payload accepted")
	}

	first := []byte("ciphertext-v1")
	if err := db.SaveGoogleOAuthSettings(ctx, first); err != nil {
		t.Fatal(err)
	}
	if has, err := db.HasEncryptedData(ctx); err != nil || !has {
		t.Fatalf("HasEncryptedData with settings = (%v, %v), want (true, nil)", has, err)
	}
	got, err = db.GoogleOAuthSettings(ctx)
	if err != nil || !bytes.Equal(got, first) {
		t.Fatalf("read = (%q, %v), want %q", got, err, first)
	}

	second := []byte("ciphertext-v2")
	if err := db.SaveGoogleOAuthSettings(ctx, second); err != nil {
		t.Fatal(err)
	}
	got, err = db.GoogleOAuthSettings(ctx)
	if err != nil || !bytes.Equal(got, second) {
		t.Fatalf("updated read = (%q, %v), want %q", got, err, second)
	}

	if err := db.DeleteGoogleOAuthSettings(ctx); err != nil {
		t.Fatal(err)
	}
	got, err = db.GoogleOAuthSettings(ctx)
	if err != nil || got != nil {
		t.Fatalf("deleted read = (%v, %v), want (nil, nil)", got, err)
	}
	if has, err := db.HasEncryptedData(ctx); err != nil || has {
		t.Fatalf("deleted HasEncryptedData = (%v, %v), want (false, nil)", has, err)
	}
}
