package security

import (
	"bytes"
	"testing"

	"github.com/netgarden/maf"
	"github.com/netgarden/maf/security/encryption"
)

func newInitializedModule(t *testing.T, secret string) *Module {
	t.Helper()
	m := NewModule()
	m.SetConfig(maf.NewConfig(map[string]any{"security.secret": secret}))
	if err := m.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return m
}

func TestGetConfigSchema_OnlyRequiresSecret(t *testing.T) {
	schema := NewModule().GetConfigSchema()

	if len(schema) != 1 {
		t.Fatalf("GetConfigSchema() = %+v, want exactly one item", schema)
	}
	if schema[0].Name != "security.secret" {
		t.Errorf("schema item name = %q, want %q", schema[0].Name, "security.secret")
	}
	if !schema[0].Required {
		t.Error("expected security.secret to be required")
	}
}

func TestInitialize_SameSecretProducesInteroperableEncryptionManagers(t *testing.T) {
	a := newInitializedModule(t, "shared-secret")
	b := newInitializedModule(t, "shared-secret")

	plaintext := []byte("hello world")
	ciphertext, err := a.GetEncryptionManager().Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	got, err := b.GetEncryptionManager().Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("got %q, want %q", got, plaintext)
	}
}

func TestInitialize_DifferentSecretsProduceNonInteroperableEncryptionManagers(t *testing.T) {
	a := newInitializedModule(t, "secret-a")
	b := newInitializedModule(t, "secret-b")

	ciphertext, err := a.GetEncryptionManager().Encrypt([]byte("hello world"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if _, err := b.GetEncryptionManager().Decrypt(ciphertext); err == nil {
		t.Error("expected different security.secret values to produce non-interoperable encryption managers")
	}
}

// TestInitialize_DoesNotPassBareSecretToEncryption proves the suffix is
// actually taking effect: an encryption.Manager built directly from the
// bare secret (no suffix) must not be able to decrypt what the module's
// (suffixed) manager produced, and vice versa.
func TestInitialize_DoesNotPassBareSecretToEncryption(t *testing.T) {
	mod := newInitializedModule(t, "shared-secret")
	bare := encryption.NewManager("shared-secret")

	ciphertext, err := mod.GetEncryptionManager().Encrypt([]byte("hello world"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := bare.Decrypt(ciphertext); err == nil {
		t.Error("expected the bare-secret manager not to decrypt the module's (suffixed) ciphertext")
	}

	ciphertext, err = bare.Encrypt([]byte("hello world"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := mod.GetEncryptionManager().Decrypt(ciphertext); err == nil {
		t.Error("expected the module's (suffixed) manager not to decrypt the bare-secret ciphertext")
	}
}
