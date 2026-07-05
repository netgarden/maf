package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
)

var _ Cryptor = (*AESGCMCryptor)(nil)

// AESGCMPrefix identifies ciphertext produced by AESGCMCryptor. Keep this
// constant and AESGCMCryptor's Decrypt logic in place even after
// introducing a newer Cryptor as Manager's default — this prefix is
// already embedded in every value encrypted under it.
const AESGCMPrefix uint64 = 1

// NewAESGCMCryptor derives a 32-byte AES-256 key from secret via SHA-256.
func NewAESGCMCryptor(secret string) *AESGCMCryptor {
	key := sha256.Sum256([]byte(secret))
	return &AESGCMCryptor{key: key[:]}
}

// AESGCMCryptor implements Cryptor using AES-256-GCM: a random 12-byte
// nonce prepended to the raw ciphertext+tag bytes.
type AESGCMCryptor struct {
	key []byte
}

func (c *AESGCMCryptor) Prefix() uint64 { return AESGCMPrefix }

// Encrypt uses a fresh random nonce per call, so encrypting the same
// plaintext twice produces different output — this can't be used to
// detect equal values by comparing stored ciphertext.
func (c *AESGCMCryptor) Encrypt(plaintext []byte) ([]byte, error) {

	gcm, err := c.newGCM()
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("encryption: generate nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt returns an error if ciphertext is truncated or fails GCM
// authentication — which covers both tampering and decrypting under the
// wrong key (e.g. after a key rotation without a migration of
// previously-stored ciphertext).
func (c *AESGCMCryptor) Decrypt(ciphertext []byte) ([]byte, error) {

	gcm, err := c.newGCM()
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("encryption: ciphertext too short")
	}

	nonce, encrypted := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, fmt.Errorf("encryption: decrypt: %w", err)
	}

	return plaintext, nil
}

func (c *AESGCMCryptor) newGCM() (cipher.AEAD, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, fmt.Errorf("encryption: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("encryption: new GCM: %w", err)
	}
	return gcm, nil
}
