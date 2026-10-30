package app

import (
	"bytes"
	"testing"
)

func TestSignatureOverwriteAuthenticatesExistingCiphertext(t *testing.T) {
	for _, fault := range []string{"wrong_key", "tamper", "wrong_account"} {
		for _, command := range []string{"set", "update", "migrate"} {
			t.Run(fault+"/"+command, func(t *testing.T) {
				compose, ring := composeFixture(t)
				ctx := t.Context()
				svc := &AccountService{Runtime: compose.Runtime}
				if _, err := svc.SetSignature(ctx, SignatureRequest{AccountID: "a", Signature: "Original private signature"}); err != nil {
					t.Fatal(err)
				}
				original, err := compose.Runtime.DB.EmailSignature(ctx, "a")
				if err != nil {
					t.Fatal(err)
				}
				switch fault {
				case "wrong_key":
					ring.key = bytes.Repeat([]byte{8}, 32)
				case "tamper":
					original[len(original)-1] ^= 1
				case "wrong_account":
					if _, err := svc.SetSignature(ctx, SignatureRequest{AccountID: "b", Signature: "Other account"}); err != nil {
						t.Fatal(err)
					}
					original, err = compose.Runtime.DB.EmailSignature(ctx, "b")
					if err != nil {
						t.Fatal(err)
					}
				}
				if _, err := compose.Runtime.DB.SaveEmailSignature(ctx, "a", original, false); err != nil {
					t.Fatal(err)
				}
				r := New(ctx, compose.Runtime.DB, ring, nil)
				defer r.Scheduler.Close()
				events := 0
				r.OnAccountChanged = func(AccountRequest) { events++ }
				svc = &AccountService{Runtime: r}
				if _, err := svc.Signature(ctx, AccountRequest{AccountID: "a"}); err == nil {
					t.Fatal("fixture must be unreadable")
				}
				var saveErr error
				sig := "Replacement signature"
				switch command {
				case "set":
					_, saveErr = svc.SetSignature(ctx, SignatureRequest{AccountID: "a", Signature: sig})
				case "update":
					_, saveErr = svc.Update(ctx, UpdateAccountRequest{AccountID: "a", Email: "a@example.invalid", DisplayName: "Must not save", Signature: &sig})
				case "migrate":
					_, saveErr = svc.MigrateSignature(ctx, SignatureRequest{AccountID: "a", Signature: sig})
				}
				if saveErr == nil {
					t.Fatal("unreadable signature accepted for replacement")
				}
				current, err := r.DB.EmailSignature(ctx, "a")
				if err != nil || !bytes.Equal(original, current) {
					t.Fatal("protected ciphertext changed", err)
				}
				a, err := r.DB.Account(ctx, "a")
				if err != nil || a.DisplayName != "" || events != 0 {
					t.Fatal("metadata changed or success event emitted", err, events)
				}
				// Local metadata-only updates still work without a usable key.
				if _, err := svc.Update(ctx, UpdateAccountRequest{AccountID: "a", Email: "a@example.invalid", DisplayName: "Local name"}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
