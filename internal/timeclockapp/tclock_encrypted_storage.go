package timeclockapp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

// EncryptedStorage encrypts every durable kiosk value with an injected key.
// The key is held by the host (for example WebCrypto/Keychain) and is never
// written through the wrapped Storage.
type EncryptedStorage struct {
	base Storage
	aead cipher.AEAD
}

const encryptedStorageVersion = "v1:"

const invalidEncryptedState = "!invalid-encrypted-state"

// NewEncryptedStorage creates encrypted custody around base. Keys must be
// supplied by the host and are never generated or persisted here.
func NewEncryptedStorage(base Storage, key []byte) (*EncryptedStorage, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.Join(ErrStorage, err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.Join(ErrStorage, err)
	}
	if base == nil {
		return nil, errors.Join(ErrStorage, errors.New("nil storage"))
	}
	return &EncryptedStorage{base: base, aead: aead}, nil
}

// Load decrypts one value. Tampered values are treated as unavailable.
func (s *EncryptedStorage) Load(key string) (string, bool) {
	raw, ok := s.base.Load(key)
	if !ok {
		return "", false
	}
	if len(raw) <= len(encryptedStorageVersion) || raw[:len(encryptedStorageVersion)] != encryptedStorageVersion {
		return invalidEncryptedState, true
	}
	data, err := base64.RawStdEncoding.DecodeString(raw[len(encryptedStorageVersion):])
	if err != nil {
		return invalidEncryptedState, true
	}
	nonceSize := s.aead.NonceSize()
	if len(data) < nonceSize {
		return invalidEncryptedState, true
	}
	plain, err := s.aead.Open(nil, data[:nonceSize], data[nonceSize:], []byte(key))
	if err != nil {
		return invalidEncryptedState, true
	}
	return string(plain), true
}

// Save encrypts one value with a fresh nonce before writing it.
func (s *EncryptedStorage) Save(key, value string) error {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return errors.Join(ErrStorage, err)
	}
	sealed := s.aead.Seal(nonce, nonce, []byte(value), []byte(key))
	encoded := encryptedStorageVersion + base64.RawStdEncoding.EncodeToString(sealed)
	if err := s.base.Save(key, encoded); err != nil {
		return errors.Join(ErrStorage, err)
	}
	return nil
}
