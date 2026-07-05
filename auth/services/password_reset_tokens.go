package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/netgarden/maf/auth/entities"
	uuid "github.com/satori/go.uuid"
	"gorm.io/gorm"
)

func NewPasswordResetTokensService(db *gorm.DB) *PasswordResetTokensService {
	return &PasswordResetTokensService{db: db}
}

type PasswordResetTokensService struct {
	db *gorm.DB
}

// Create stores tokenHash (never the raw token — see generateResetToken)
// for userID, valid until expiresAt.
func (s *PasswordResetTokensService) Create(userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	return s.db.Create(&entities.PasswordResetToken{
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	}).Error
}

// Consume atomically marks the token identified by tokenHash as used, but
// only if it exists, hasn't expired, and hasn't been used already — the
// same conditional-UPDATE-checks-RowsAffected pattern
// mailer.Service.RetryEmail/CancelEmail use, so a token can never be
// consumed twice even under concurrent confirm requests racing each
// other. Returns ok=false (with a nil error) for every invalid case
// (unknown, expired, already used, or lost the race) — deliberately
// indistinguishable to the caller.
func (s *PasswordResetTokensService) Consume(tokenHash string, now time.Time) (uuid.UUID, bool, error) {
	var token entities.PasswordResetToken
	err := s.db.Where("token_hash = ? AND used_at IS NULL AND expires_at > ?", tokenHash, now).
		First(&token).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, err
	}

	result := s.db.Model(&entities.PasswordResetToken{}).
		Where("id = ? AND used_at IS NULL", token.ID).
		Update("used_at", now)
	if result.Error != nil {
		return uuid.Nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		// Raced with another Consume call between the SELECT and UPDATE.
		return uuid.Nil, false, nil
	}

	return token.UserID, true, nil
}

// DeleteUnusedForUser removes every not-yet-used token for userID — called
// before issuing a new one so a user never accumulates multiple valid
// reset links across repeated requests.
func (s *PasswordResetTokensService) DeleteUnusedForUser(userID uuid.UUID) error {
	return s.db.Where("user_id = ? AND used_at IS NULL", userID).Delete(&entities.PasswordResetToken{}).Error
}

// generateResetToken returns a fresh random token (base64url-encoded, safe
// to embed in a URL query parameter) alongside the sha256 hex digest that
// should be stored in place of the raw value.
func generateResetToken() (raw string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, hashResetToken(raw), nil
}

func hashResetToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
