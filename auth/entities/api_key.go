package entities

import (
	"time"

	"github.com/netgarden/maf/database"
	uuid "github.com/satori/go.uuid"
)

// ApiKey is a long-lived, revocable, personal-access-token-style credential a
// user generates for themselves to call the API non-interactively (as
// `Authorization: Bearer <token>`), instead of a browser session. Shaped
// after PasswordResetToken: the plaintext is shown exactly once, at
// creation, and never stored — only TokenHash (its sha256 hex digest) is
// persisted, an unsalted digest being fine here since the input already has
// 256 bits of entropy (same reasoning as PasswordResetToken and iris's own
// webhook tokens).
type ApiKey struct {
	database.EntityBase
	UserID uuid.UUID
	User   *User

	Name string
	// TokenHash is sha256(plaintext), hex-encoded, unique-indexed for lookup.
	TokenHash string `gorm:"uniqueIndex;not null"`
	// Prefix is the first few characters of the plaintext (e.g. "pat_a1b2c3d4"),
	// stored only for display so a user can tell their keys apart without ever
	// seeing the full value again.
	Prefix string

	// LastUsedAt is set only after a successful ValidateApiKey call (hash
	// match, not expired, not revoked, owning user active) — never on a
	// failed attempt. nil means the key has never been used.
	LastUsedAt *time.Time
	// ExpiresAt is nil for a key that never expires.
	ExpiresAt *time.Time
	// AllowedIPs are plain IPs or CIDR ranges; empty means "any source".
	// Same shape as iris's webhooks.Webhook.AllowedIPs.
	AllowedIPs []string `gorm:"serializer:json"`
}

func (ApiKey) TableName() string {
	return "auth_api_keys"
}
