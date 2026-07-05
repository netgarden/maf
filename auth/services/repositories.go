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
