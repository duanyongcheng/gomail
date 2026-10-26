package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/bary/pebble-go/internal/crypto"
	"github.com/bary/pebble-go/internal/store"
	"golang.org/x/oauth2"
)

// DatabaseVault is scoped to the account and issuing OAuth client. It retains
// the ciphertext observed on load so refresh persists with a compare-and-swap.
type DatabaseVault struct {
	DB        *store.DB
	Crypto    *crypto.Service
	Context   context.Context
	AccountID string
	ClientID  string
	mu        sync.Mutex
	previous  []byte
}
type tokenRecord struct {
	ClientID string        `json:"client_id"`
	Token    *oauth2.Token `json:"token"`
}

func EncodeToken(service *crypto.Service, account, client string, token *oauth2.Token) ([]byte, error) {
	plain, err := json.Marshal(tokenRecord{ClientID: client, Token: token})
	if err != nil {
		return nil, err
	}
	return service.Encrypt("accounts.auth_data", account, plain)
}
func (v *DatabaseVault) Load() (*oauth2.Token, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	encrypted, err := v.DB.AuthData(v.Context, v.AccountID)
	if err != nil {
		return nil, err
	}
	plain, err := v.Crypto.Decrypt("accounts.auth_data", v.AccountID, encrypted)
	if err != nil {
		return nil, err
	}
	var record tokenRecord
	if json.Unmarshal(plain, &record) != nil || record.Token == nil || record.ClientID != v.ClientID || record.Token.RefreshToken == "" {
		return nil, errors.New("saved Gmail credentials require reauthorization with the configured client")
	}
	v.previous = append([]byte{}, encrypted...)
	return record.Token, nil
}
func (v *DatabaseVault) Save(token *oauth2.Token) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.previous == nil {
		return errors.New("token refresh requires loading current ciphertext first")
	}
	encrypted, err := EncodeToken(v.Crypto, v.AccountID, v.ClientID, token)
	if err != nil {
		return err
	}
	updated, err := v.DB.CompareAndSwapAuthData(v.Context, v.AccountID, v.previous, encrypted, time.Now().Unix())
	if err != nil {
		return err
	}
	if !updated {
		return errors.New("credentials changed during refresh; reload account before retry")
	}
	v.previous = encrypted
	return nil
}
