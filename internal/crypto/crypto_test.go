package crypto

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestEnvelopeVectorsAndAAD(t *testing.T) {
	data, err := os.ReadFile("testdata/envelope_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector map[string]string
	if err := json.Unmarshal(data, &vector); err != nil {
		t.Fatal(err)
	}
	key, _ := hex.DecodeString(vector["key"])
	service, err := FromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"legacy", "v1"} {
		blob, _ := hex.DecodeString(vector[version])
		plain, err := service.Decrypt(vector["purpose"], vector["record"], blob)
		if err != nil || string(plain) != vector["plain"] {
			t.Fatalf("%s: %q, %v", version, plain, err)
		}
		if version == "v1" {
			if _, err := service.Decrypt(vector["purpose"], "wrong-account", blob); err == nil {
				t.Fatal("wrong AAD accepted")
			}
		}
		blob[len(blob)-1] ^= 1
		if _, err := service.Decrypt(vector["purpose"], vector["record"], blob); err == nil {
			t.Fatal("tampered token accepted")
		}
	}
	first, err := service.Encrypt("accounts.auth_data", "a", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	second, _ := service.Encrypt("accounts.auth_data", "a", []byte("secret"))
	if bytes.Equal(first, second) {
		t.Fatal("nonce reused")
	}
	if _, err := service.Decrypt("wrong-purpose", "a", first); err == nil {
		t.Fatal("wrong purpose accepted")
	}
	first[7] = 2
	if _, err := service.Decrypt("accounts.auth_data", "a", first); err == nil {
		t.Fatal("unknown version fell back to legacy")
	}
}

type fakeKeyring struct {
	value  []byte
	err    error
	writes int
}

func (f *fakeKeyring) Get(string) ([]byte, error)       { return f.value, f.err }
func (f *fakeKeyring) Set(_ string, value []byte) error { f.writes++; f.value = value; return nil }
func TestMissingKeyNeverReplacesExistingCiphertext(t *testing.T) {
	for _, err := range []error{ErrKeyNotFound, errors.New("keyring locked")} {
		ring := &fakeKeyring{err: err}
		if _, err := Load(ring, true); err == nil {
			t.Fatal("missing DEK accepted")
		}
		if ring.writes != 0 {
			t.Fatal("replaced key for existing ciphertext")
		}
	}
	ring := &fakeKeyring{err: ErrKeyNotFound}
	if _, err := Load(ring, false); err != nil || ring.writes != 1 || len(ring.value) != 32 {
		t.Fatal("fresh key creation failed", err)
	}
}
