package services

import (
	"gorm.io/gorm"
	"github.com/netgarden/maf"
	"github.com/netgarden/maf/security/passwords"
)

func NewManager(
	config *maf.Config,
	db *gorm.DB,
	secret string,
	passwordsManager *passwords.Manager,
) *Manager {
	return &Manager{
		config:           config,
		db:               db,
		secret:           secret,
		passwordsManager: passwordsManager,
	}
}

type Manager struct {
	config           *maf.Config
	db               *gorm.DB
	secret           string
	passwordsManager *passwords.Manager

	usersService    *UsersService
	sessionsService *SessionsService
	authService     *AuthService
}

func (m *Manager) Init() error {

	m.usersService = NewUsersService(m.db)
	m.sessionsService = NewSessionsService(m.db)
	m.authService = NewAuthService(
		m.config,
		m.secret,
		m.usersService,
		m.sessionsService,
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
