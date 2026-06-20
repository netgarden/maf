package rrpc_auth

import (
	"time"

	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/auth/services"
	"github.com/netgarden/maf/security/passwords"

	"gorm.io/gorm"
	"github.com/netgarden/maf"
	"github.com/netgarden/maf/security"
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

func (m *Module) GetConfigSchema() []maf.ConfigItem {
	return []maf.ConfigItem{
		{Name: "auth.token.ttl", Type: maf.Duration, DefaultValue: 15 * time.Minute},
		{Name: "auth.session.ttl", Type: maf.Duration, DefaultValue: 7 * 24 * time.Hour},
		{Name: "auth.session.cookie.name", Type: maf.String, DefaultValue: "session"},
		{Name: "auth.session.cookie.path", Type: maf.String, DefaultValue: "/"},
		{Name: "auth.session.cookie.force_secure", Type: maf.Bool, DefaultValue: false},
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
	}
}

func (m *Module) Initialize() error {

	var err error

	// Cross-module config: read security.secret without importing the security config type.
	secret := m.config.GetString("security.secret")

	// Security module is still needed for the passwords service (not config).
	securityModule := m.manager.GetModule("security").(*security.Module)
	m.passwordsManager = securityModule.GetPasswordsManager()

	m.servicesManager = services.NewManager(
		m.config,
		m.db,
		secret,
		m.passwordsManager,
	)
	err = m.servicesManager.Init()
	if err != nil {
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
