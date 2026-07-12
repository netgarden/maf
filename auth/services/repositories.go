package services

import (
	"time"

	"github.com/netgarden/maf/auth/entities"
	uuid "github.com/satori/go.uuid"
)

type usersRepository interface {
	GetUser(id string) (*entities.User, error)
	GetUserByUsername(username string) (*entities.User, error)
	UpdatePassword(id, passwordHash string) error

	// GetUserByEmail, CreateExternalUser and SetAdmin exist for
	// AuthService.CompleteExternalLogin's JIT-provisioning/account-linking
	// flow (see identity.go) — nothing in the plain username/password path
	// uses them.
	GetUserByEmail(email string) (*entities.User, error)
	CreateExternalUser(email, firstName, lastName string) (*entities.User, error)
	SetAdmin(id string, admin bool) error
}

// identitiesRepository backs AuthService.CompleteExternalLogin's
// (ProviderType, ProviderID, Subject) lookup and linking — see identity.go.
type identitiesRepository interface {
	FindByProviderSubject(providerType string, providerID uuid.UUID, subject string) (*entities.UserIdentity, error)
	Create(userID uuid.UUID, providerType string, providerID uuid.UUID, subject, email string) (*entities.UserIdentity, error)
}

type sessionsRepository interface {
	CreateSession(userID, clientIP, userAgent string) (*entities.Session, error)
	DeleteSession(id string) error
	DeleteSessionsByUserID(userID string) error
	GetSession(id string) (*entities.Session, error)
	UpdateSession(id string) error
}

type passwordResetTokensRepository interface {
	Create(userID uuid.UUID, tokenHash string, expiresAt time.Time) error
	Consume(tokenHash string, now time.Time) (uuid.UUID, bool, error)
	DeleteUnusedForUser(userID uuid.UUID) error
}
