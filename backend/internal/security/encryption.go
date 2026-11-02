// Credential encryption uses AES-256-GCM with a key derived from the configured master secret.
// Stored values contain the random nonce followed by authenticated ciphertext,
// encoded as base64. Empty credentials remain empty to represent no configured key.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// Encryptor holds the authenticated cipher used for provider credential storage and retrieval.
type Encryptor struct {
	gcm cipher.AEAD
}

// NewEncryptor rejects an empty master secret and derives a 32-byte key with SHA-256.
// The derived key initializes AES-GCM; changing the configured master secret
// makes previously stored ciphertext unreadable until credentials are replaced.
func NewEncryptor(masterKey string) (*Encryptor, error) {
	if masterKey == "" {
		return nil, fmt.Errorf("secret_key is empty")
	}
	key := sha256.Sum256([]byte(masterKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return &Encryptor{gcm: gcm}, nil
}

// Encrypt generates a fresh nonce and returns base64(nonce || ciphertext || tag).
// The nonce is included for later decryption; the master key is not stored in
// the result. Randomness/cipher errors are returned without embedding plaintext.
func (e *Encryptor) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	ct := e.gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt validates base64 and nonce length, then authenticates and decrypts with GCM.
// Wrong keys, truncated data, and modified ciphertext fail authentication instead
// of returning partial plaintext. Empty input is a valid empty credential.
func (e *Encryptor) Decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	if len(raw) < e.gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	plain, err := e.gcm.Open(nil, raw[:e.gcm.NonceSize()], raw[e.gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("gcm open: %w", err)
	}
	return string(plain), nil
}
