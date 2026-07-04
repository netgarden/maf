package rrpc_auth

import (
	"time"

	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/auth/services"
	"github.com/netgarden/maf/locks"
	"github.com/netgarden/maf/security/passwords"

	"github.com/netgarden/maf"
	"github.com/netgarden/maf/security"
	"gorm.io/gorm"
)

func NewModule() *Module {
	return &Module{}
}

type Module struct {
	manager *maf.Manager
	config  *maf.Config
	db      *gorm.DB

	passwordsManager *passwords.Manager

	servicesManager *services.Manager
}

func (m *Module) GetID() string {
	return "auth"
}

func (m *Module) GetName() string {
	return "Auth"
}

func (m *Module) SetManager(manager *maf.Manager) {
	m.manager = manager
}

func (m *Module) GetDependencies() []string {
	return []string{"security"}
}

func (m *Module) GetConfigSchema() []maf.ConfigItem {
	return []maf.ConfigItem{
		{Name: "auth.token.ttl", Type: maf.Duration, DefaultValue: 15 * time.Minute},
		{Name: "auth.session.ttl", Type: maf.Duration, DefaultValue: 7 * 24 * time.Hour},
		{Name: "auth.session.cookie.name", Type: maf.String, DefaultValue: "session"},
		{Name: "auth.session.cookie.path", Type: maf.String, DefaultValue: "/"},
		{Name: "auth.session.cookie.force_secure", Type: maf.Bool, DefaultValue: false},
		{Name: "auth.admin.defaultPassword", Type: maf.String, DefaultValue: "admin"},
	}
}

func (m *Module) SetConfig(config *maf.Config) {
	m.config = config
}

func (m *Module) GetConfig() *maf.Config {
	return m.config
}

func (m *Module) SetDB(db *gorm.DB) {
	m.db = db
}

func (m *Module) GetDBEntities() []interface{} {
	return []interface{}{
		&entities.User{},
		&entities.Session{},
		// &locks.Lock{}: auth uses locks.Service directly (see Initialize)
		// to serialize EnsureAdminExists across replicas, without requiring
		// the consuming application to separately register locks.NewModule
		// — so this table needs to exist regardless of whether it does.
		// Harmless if the app *also* registers locks.NewModule itself:
		// AutoMigrate-ing the same entity twice is a no-op the second time.
		&locks.Lock{},
	}
}

func (m *Module) Initialize() error {

	var err error

	// Cross-module config: read security.secret without importing the security config type.
	secret := m.config.GetString("security.secret")

	// Security module is still needed for the passwords service (not config).
	securityModule := m.manager.GetModule("security").(*security.Module)
	m.passwordsManager = securityModule.GetPasswordsManager()

	// Constructed directly rather than looked up via a registered
	// locks.Module — locks.Service is a thin, stateless wrapper around a
	// *gorm.DB, safe to construct standalone, so auth doesn't need "locks"
	// as a maf-level GetDependencies() entry or force every consuming
	// application to register locks.NewModule() just for this.
	locksService := locks.NewService(m.db)

	m.servicesManager = services.NewManager(
		m.config,
		m.db,
		secret,
		m.passwordsManager,
		locksService,
	)
	err = m.servicesManager.Init()
	if err != nil {
		return err
	}

	defaultAdminPassword := m.config.GetString("auth.admin.defaultPassword")
	if err := m.servicesManager.GetUsersService().EnsureAdminExists(defaultAdminPassword); err != nil {
		return err
	}

	return nil
}

func (m *Module) PreStart() error {
	return nil
}

func (m *Module) GetServicesManager() *services.Manager {
	return m.servicesManager
}
