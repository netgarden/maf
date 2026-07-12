package services

import (
	"github.com/netgarden/maf"
	"github.com/netgarden/maf/locks"
	"github.com/netgarden/maf/security/encryption"
	"github.com/netgarden/maf/security/passwords"
	"gorm.io/gorm"
)

func NewManager(
	config *maf.Config,
	db *gorm.DB,
	secret string,
	passwordsManager *passwords.Manager,
	locksService *locks.Service,
	encryptionManager *encryption.Manager,
) *Manager {
	return &Manager{
		config:            config,
		db:                db,
		secret:            secret,
		passwordsManager:  passwordsManager,
		locksService:      locksService,
		encryptionManager: encryptionManager,
	}
}

type Manager struct {
	config            *maf.Config
	db                *gorm.DB
	secret            string
	passwordsManager  *passwords.Manager
	locksService      *locks.Service
	encryptionManager *encryption.Manager

	usersService         *UsersService
	sessionsService      *SessionsService
	resetTokensService   *PasswordResetTokensService
	identitiesService    *UserIdentitiesService
	authService          *AuthService
	providersService     *ProvidersService
	oidcProvidersService *OIDCProvidersService
	oidcAuthService      *OIDCAuthService
}

func (m *Manager) Init() error {

	m.usersService = NewUsersService(m.db, m.passwordsManager, m.locksService)
	m.sessionsService = NewSessionsService(m.db)
	m.resetTokensService = NewPasswordResetTokensService(m.db)
	m.identitiesService = NewUserIdentitiesService(m.db)
	m.authService = NewAuthService(
		m.config,
		m.secret,
		m.usersService,
		m.sessionsService,
		m.resetTokensService,
		m.passwordsManager,
		m.identitiesService,
	)
	m.providersService = NewProvidersService(m.db)
	m.oidcProvidersService = NewOIDCProvidersService(m.db, m.encryptionManager)
	m.oidcAuthService = NewOIDCAuthService(m.config, m.secret, m.oidcProvidersService, m.authService)

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

func (m *Manager) GetUserIdentitiesService() *UserIdentitiesService {
	return m.identitiesService
}

func (m *Manager) GetProvidersService() *ProvidersService {
	return m.providersService
}

func (m *Manager) GetOIDCProvidersService() *OIDCProvidersService {
	return m.oidcProvidersService
}

func (m *Manager) GetOIDCAuthService() *OIDCAuthService {
	return m.oidcAuthService
}
