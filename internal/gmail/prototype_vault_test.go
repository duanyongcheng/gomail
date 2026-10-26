package gmail

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
)

// Vault encrypts tokens with a DEK supplied by the desktop's isolated keyring.
// It never creates a plaintext fallback. AAD binds tokens to client and scope.
type Vault struct {
	Path string
	Key  []byte
	AAD  string
}

func (v *Vault) Save(token *oauth2.Token) error {
	if len(v.Key) != 32 {
		return errors.New("token vault requires 32-byte DEK")
	}
	plain, err := json.Marshal(token)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(v.Key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	blob := gcm.Seal(nonce, nonce, plain, []byte(v.AAD))
	if err = os.MkdirAll(filepath.Dir(v.Path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(v.Path), ".token-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(blob)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, v.Path)
}
func (v *Vault) Load() (*oauth2.Token, error) {
	blob, err := os.ReadFile(v.Path)
	if err != nil {
		return nil, err
	}
	if len(v.Key) != 32 {
		return nil, errors.New("invalid DEK")
	}
	block, err := aes.NewCipher(v.Key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("invalid encrypted token")
	}
	plain, err := gcm.Open(nil, blob[:gcm.NonceSize()], blob[gcm.NonceSize():], []byte(v.AAD))
	if err != nil {
		return nil, errors.New("token authentication failed; do not replace key")
	}
	var token oauth2.Token
	if err = json.Unmarshal(plain, &token); err != nil {
		return nil, err
	}
	return &token, nil
}
