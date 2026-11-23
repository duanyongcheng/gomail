package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
)

const KeyringService = "dev.bary.gomail"
const KeyringAccount = "master-dek"

var ErrKeyNotFound = errors.New("DEK not found")
var header = []byte("GOMAIL\x00\x01")

type Keyring interface {
	Get(string) ([]byte, error)
	Set(string, []byte) error
}
type Service struct{ gcm cipher.AEAD }

// Load refuses to replace a missing key whenever encrypted records exist.
func Load(keyring Keyring, encryptedRecords bool) (*Service, error) {
	key, err := keyring.Get(KeyringAccount)
	if errors.Is(err, ErrKeyNotFound) && !encryptedRecords {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		err = keyring.Set(KeyringAccount, key)
	}
	if err != nil {
		return nil, errors.New("keyring unavailable or DEK missing; credentials were preserved")
	}
	return FromKey(key)
}

func FromKey(key []byte) (*Service, error) {
	if len(key) != 32 {
		return nil, errors.New("DEK must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	return &Service{gcm: gcm}, err
}

func aad(purpose, record string) []byte {
	out := append([]byte{}, header...)
	out = append(out, []byte("gomail.crypto.context.v1\x00")...)
	out = binary.BigEndian.AppendUint64(out, uint64(len(purpose)))
	out = append(out, purpose...)
	out = binary.BigEndian.AppendUint64(out, uint64(len(record)))
	return append(out, record...)
}

func (s *Service) Encrypt(purpose, record string, plain []byte) ([]byte, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append(append([]byte{}, header...), nonce...)
	return s.gcm.Seal(out, nonce, plain, aad(purpose, record)), nil
}

func (s *Service) Decrypt(purpose, record string, encrypted []byte) ([]byte, error) {
	var associated []byte
	if bytes.HasPrefix(encrypted, []byte("GOMAIL\x00")) {
		if !bytes.HasPrefix(encrypted, header) {
			return nil, errors.New("unsupported or truncated ciphertext envelope")
		}
		encrypted = encrypted[len(header):]
		associated = aad(purpose, record)
	}
	if len(encrypted) < s.gcm.NonceSize()+s.gcm.Overhead() {
		return nil, errors.New("truncated ciphertext")
	}
	plain, err := s.gcm.Open(nil, encrypted[:s.gcm.NonceSize()], encrypted[s.gcm.NonceSize():], associated)
	if err != nil {
		return nil, errors.New("credential authentication failed; ciphertext preserved")
	}
	return plain, nil
}
