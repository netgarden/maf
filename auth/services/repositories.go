package services

import "github.com/netgarden/maf/auth/entities"

type usersRepository interface {
	GetUser(id string) (*entities.User, error)
	GetUserByUsername(username string) (*entities.User, error)
	UpdatePassword(id, passwordHash string) error
}

type sessionsRepository interface {
	CreateSession(userID, clientIP, userAgent string) (*entities.Session, error)
	DeleteSession(id string) error
	GetSession(id string) (*entities.Session, error)
	UpdateSession(id string) error
}
