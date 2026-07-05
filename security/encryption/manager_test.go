package encryption

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	m := NewManager("test-secret")

	plaintext := []byte("Hello, your temporary password is: correct-horse-battery-staple")

	ciphertext, err := m.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext must not equal plaintext")
	}

	got, err := m.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("got %q, want %q", got, plaintext)
	}
}

func TestEncrypt_DifferentNoncePerCall(t *testing.T) {
	m := NewManager("test-secret")

	a, err := m.Encrypt([]byte("same plaintext"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	b, err := m.Encrypt([]byte("same plaintext"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if bytes.Equal(a, b) {
		t.Error("encrypting the same plaintext twice should not produce identical ciphertext (fresh nonce each time)")
	}
}

func TestDecrypt_TamperedCiphertextFails(t *testing.T) {
	m := NewManager("test-secret")

	ciphertext, err := m.Encrypt([]byte("hello world"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Flip a byte after the prefix, well into the actual ciphertext.
	tampered := append([]byte(nil), ciphertext...)
	tampered[len(tampered)-1] ^= 0xFF

	if _, err := m.Decrypt(tampered); err == nil {
		t.Error("expected tampered ciphertext to fail GCM authentication")
	}
}

func TestDecrypt_WrongKeyFails(t *testing.T) {
	enc := NewManager("secret-a")
	dec := NewManager("secret-b")

	ciphertext, err := enc.Encrypt([]byte("hello world"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if _, err := dec.Decrypt(ciphertext); err == nil {
		t.Error("expected decryption under a different key to fail")
	}
}

func TestDecrypt_MalformedInputReturnsError(t *testing.T) {
	m := NewManager("test-secret")

	cases := [][]byte{
		nil,
		{},
		[]byte("short"),          // shorter than the 8-byte prefix
		{0, 0, 0, 0, 0, 0, 0, 1}, // valid AES-GCM prefix, no ciphertext bytes after it
		append(binary.BigEndian.AppendUint64(nil, AESGCMPrefix), []byte("too short for a nonce")...),
	}

	for _, c := range cases {
		if _, err := m.Decrypt(c); err == nil {
			t.Errorf("Decrypt(%v): expected an error, got nil", c)
		}
	}
}

func TestNewManager_DifferentSecretsProduceDifferentKeys(t *testing.T) {
	a := NewManager("secret-a")
	b := NewManager("secret-b")

	ciphertext, err := a.Encrypt([]byte("hello"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	_, err = b.Decrypt(ciphertext)
	if err == nil {
		t.Fatal("expected different secrets to derive different keys")
	}
	if !strings.Contains(err.Error(), "decrypt") {
		t.Errorf("expected a decrypt-related error, got: %v", err)
	}
}

func TestEncrypt_OutputHasAlgorithmPrefix(t *testing.T) {
	m := NewManager("test-secret")

	ciphertext, err := m.Encrypt([]byte("hello world"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if len(ciphertext) < prefixSize {
		t.Fatalf("ciphertext too short to contain a prefix: %v", ciphertext)
	}
	prefix := binary.BigEndian.Uint64(ciphertext[:prefixSize])
	if prefix != AESGCMPrefix {
		t.Errorf("expected prefix %d, got %d", AESGCMPrefix, prefix)
	}
}

// fakeCryptor is a trivial, non-cryptographic Cryptor stand-in for testing
// Manager's multi-Cryptor dispatch — not for real use, just reversible
// enough to prove which Cryptor actually handled a given round trip.
type fakeCryptor struct{}

const fakeCryptorPrefix uint64 = 999

func (fakeCryptor) Prefix() uint64 { return fakeCryptorPrefix }
func (fakeCryptor) Encrypt(plaintext []byte) ([]byte, error) {
	return reverseBytes(plaintext), nil
}
func (fakeCryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	return reverseBytes(ciphertext), nil
}

func reverseBytes(b []byte) []byte {
	r := append([]byte(nil), b...)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return r
}

// TestManager_DecryptsUnderPreviousDefaultAfterCryptorChange is the actual
// proof of the feature this file exists for: data encrypted while one
// Cryptor was the default must still decrypt correctly after a Manager
// moves on to a different default — simulating "we switched to a more
// secure algorithm" without losing the ability to read old data.
func TestManager_DecryptsUnderPreviousDefaultAfterCryptorChange(t *testing.T) {
	// "Before": fakeCryptor is the default.
	before := &Manager{}
	before.AddDefaultCryptor(fakeCryptor{})

	oldCiphertext, err := before.Encrypt([]byte("hello world"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if got := binary.BigEndian.Uint64(oldCiphertext[:prefixSize]); got != fakeCryptorPrefix {
		t.Fatalf("expected ciphertext prefixed with %d, got %d", fakeCryptorPrefix, got)
	}

	// "After": AES-GCM is now the default, but fakeCryptor is kept
	// registered so its old ciphertext still decrypts.
	after := NewManager("test-secret")
	after.AddCryptor(fakeCryptor{})

	got, err := after.Decrypt(oldCiphertext)
	if err != nil {
		t.Fatalf("Decrypt old ciphertext after default changed: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}

	// New data still uses the new default, not the old Cryptor.
	newCiphertext, err := after.Encrypt([]byte("new data"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if newPrefix := binary.BigEndian.Uint64(newCiphertext[:prefixSize]); newPrefix != AESGCMPrefix {
		t.Errorf("expected new ciphertext to use the current default (%d), got: %d", AESGCMPrefix, newPrefix)
	}
}

func TestDecrypt_UnknownPrefixReturnsError(t *testing.T) {
	m := NewManager("test-secret")

	unknown := binary.BigEndian.AppendUint64(nil, 123456789)
	unknown = append(unknown, []byte("abc123")...)

	if _, err := m.Decrypt(unknown); err == nil {
		t.Error("expected an unrecognized algorithm prefix to return an error")
	}
}
