package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// SecureStorage provides encrypted storage for sensitive credentials
type SecureStorage interface {
	Store(key, value string) error
	Retrieve(key string) (string, error)
	Delete(key string) error
}

// MemorySecureStorage implements in-memory encrypted storage
// WARNING: This is for development/testing only. Production should use a proper secrets manager.
type MemorySecureStorage struct {
	storage map[string]string
	key     []byte
}

// NewMemorySecureStorage creates a new in-memory secure storage
func NewMemorySecureStorage(encryptionKey []byte) (*MemorySecureStorage, error) {
	if len(encryptionKey) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes for AES-256")
	}

	return &MemorySecureStorage{
		storage: make(map[string]string),
		key:     encryptionKey,
	}, nil
}

// Store encrypts and stores a value
func (m *MemorySecureStorage) Store(key, value string) error {
	encrypted, err := m.encrypt(value)
	if err != nil {
		return fmt.Errorf("failed to encrypt value: %w", err)
	}

	m.storage[key] = encrypted
	return nil
}

// Retrieve decrypts and returns a stored value
func (m *MemorySecureStorage) Retrieve(key string) (string, error) {
	encrypted, exists := m.storage[key]
	if !exists {
		return "", fmt.Errorf("key not found: %s", key)
	}

	decrypted, err := m.decrypt(encrypted)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt value: %w", err)
	}

	return decrypted, nil
}

// Delete removes a stored value
func (m *MemorySecureStorage) Delete(key string) error {
	delete(m.storage, key)
	return nil
}

// encrypt encrypts plaintext using AES-256-GCM
func (m *MemorySecureStorage) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(m.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts ciphertext using AES-256-GCM
func (m *MemorySecureStorage) decrypt(ciphertext string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(m.key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
