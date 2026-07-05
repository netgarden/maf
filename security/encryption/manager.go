// Package encryption provides symmetric encryption for data at rest (e.g.
// a queued email's body — see github.com/netgarden/maf/mailer), separate
// from the JWT-signing secret in github.com/netgarden/maf/security's own
// config, since reusing one secret for two unrelated cryptographic
// purposes is bad practice even when nothing technically stops it.
package encryption

import (
	"encoding/binary"
	"fmt"
)

// prefixSize is the width, in bytes, of a Cryptor's serialized Prefix at
// the start of every ciphertext Manager produces.
const prefixSize = 8

// Cryptor is a single encryption/decryption algorithm, identified by a
// stable numeric Prefix serialized as a fixed-width big-endian uint64 at
// the start of every ciphertext it produces. This is the mechanism that
// lets Manager move to a new, more secure Cryptor in the future while
// still being able to decrypt data encrypted under an older one: keep the
// old Cryptor registered (just no longer the default used for new Encrypt
// calls) — its Prefix is what routes old ciphertext back to it in
// Manager.Decrypt. Mirrors
// github.com/netgarden/maf/security/passwords's Encoder/Manager pattern.
type Cryptor interface {
	// Prefix returns this Cryptor's stable identifier. Must be unique
	// among every Cryptor a given Manager knows about, and — once
	// anything has been encrypted under it — never reused for a
	// different algorithm.
	Prefix() uint64

	// Encrypt and Decrypt operate on ciphertext with Prefix already
	// stripped/added by Manager; implementations don't need to handle it
	// themselves.
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

// NewManager returns a Manager whose default Cryptor is AES-256-GCM (see
// NewAESGCMCryptor), deriving its key from secret the same simple way
// security.secret already works elsewhere (a plain string, not a
// properly-sized/encoded key the operator has to get exactly right).
func NewManager(secret string) *Manager {
	m := &Manager{}
	m.AddDefaultCryptor(NewAESGCMCryptor(secret))
	return m
}

type Manager struct {
	defaultCryptor Cryptor
	cryptors       map[uint64]Cryptor
}

// AddDefaultCryptor sets c as what Encrypt uses for new data, and also
// registers it (via AddCryptor) so Decrypt continues to recognize its
// ciphertext.
func (m *Manager) AddDefaultCryptor(c Cryptor) {
	m.defaultCryptor = c
	m.AddCryptor(c)
}

// AddCryptor registers c so Decrypt recognizes its ciphertext (by
// Prefix), without making it the default used for new Encrypt calls —
// this is how an older algorithm stays readable after a newer one takes
// over as default.
func (m *Manager) AddCryptor(c Cryptor) {
	if m.cryptors == nil {
		m.cryptors = make(map[uint64]Cryptor)
	}
	m.cryptors[c.Prefix()] = c
}

// Encrypt returns plaintext encrypted under the Manager's current default
// Cryptor, prefixed with that Cryptor's serialized identifier so a later
// Decrypt call (potentially after the default has moved on to something
// else) knows which algorithm to use.
func (m *Manager) Encrypt(plaintext []byte) ([]byte, error) {
	ciphertext, err := m.defaultCryptor.Encrypt(plaintext)
	if err != nil {
		return nil, err
	}

	out := make([]byte, prefixSize+len(ciphertext))
	binary.BigEndian.PutUint64(out[:prefixSize], m.defaultCryptor.Prefix())
	copy(out[prefixSize:], ciphertext)

	return out, nil
}

// Decrypt dispatches to whichever registered Cryptor's Prefix matches
// ciphertext's leading prefixSize bytes, so data encrypted under a
// previous default Cryptor still decrypts correctly after Manager moves
// on to a newer one. Returns an error if ciphertext is too short to
// contain a prefix, or no registered Cryptor's Prefix matches it
// (unrecognized algorithm, or corrupted input).
func (m *Manager) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < prefixSize {
		return nil, fmt.Errorf("encryption: ciphertext too short to contain an algorithm prefix")
	}

	prefix := binary.BigEndian.Uint64(ciphertext[:prefixSize])

	c, ok := m.cryptors[prefix]
	if !ok {
		return nil, fmt.Errorf("encryption: unrecognized algorithm prefix %d", prefix)
	}

	return c.Decrypt(ciphertext[prefixSize:])
}
