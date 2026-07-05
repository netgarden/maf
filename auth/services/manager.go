package services

import (
	"github.com/netgarden/maf"
	"github.com/netgarden/maf/locks"
	"github.com/netgarden/maf/security/passwords"
	"gorm.io/gorm"
)

func NewManager(
	config *maf.Config,
	db *gorm.DB,
	secret string,
	passwordsManager *passwords.Manager,
	locksService *locks.Service,
) *Manager {
	return &Manager{
		config:           config,
		db:               db,
		secret:           secret,
		passwordsManager: passwordsManager,
		locksService:     locksService,
	}
}

type Manager struct {
	config           *maf.Config
	db               *gorm.DB
	secret           string
	passwordsManager *passwords.Manager
	locksService     *locks.Service

	usersService       *UsersService
	sessionsService    *SessionsService
	resetTokensService *PasswordResetTokensService
	authService        *AuthService
}

func (m *Manager) Init() error {

	m.usersService = NewUsersService(m.db, m.passwordsManager, m.locksService)
	m.sessionsService = NewSessionsService(m.db)
	m.resetTokensService = NewPasswordResetTokensService(m.db)
	m.authService = NewAuthService(
		m.config,
		m.secret,
		m.usersService,
		m.sessionsService,
		m.resetTokensService,
		m.passwordsManager,
	)

	return nil
}

func (m *Manager) GetAuthService() *AuthService {
	return m.authService
}

func (m *Manager) GetUsersService() *UsersService {
	return m.usersService
}

func (m *Manager) GetSessionsService() *SessionsService {
	return m.sessionsService
}

func (m *Manager) GetPasswordResetTokensService() *PasswordResetTokensService {
	return m.resetTokensService
}
