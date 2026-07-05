package entities

import (
	"time"

	"github.com/netgarden/maf/database"
	uuid "github.com/satori/go.uuid"
)

// PasswordResetToken is a single-use, expiring credential for the
// logged-out "forgot password" flow. TokenHash is a sha256 hex digest of
// the raw token emailed to the user — the raw value is never stored,
// since it sits in a URL (browser history, mail logs, referrers) rather
// than behind an httpOnly cookie like a session.
type PasswordResetToken struct {
	database.EntityBase
	UserID    uuid.UUID
	TokenHash string `gorm:"uniqueIndex;not null"`
	ExpiresAt time.Time
	UsedAt    *time.Time
}

func (PasswordResetToken) TableName() string {
	return "auth_password_reset_tokens"
}
