package security

import (
	"github.com/netgarden/maf"
	"github.com/netgarden/maf/security/passwords"
)

func NewModule() *Module {
	return &Module{}
}

type Module struct {
	manager *maf.Manager
	config  *maf.Config

	passwordsManager *passwords.Manager
}

func (m *Module) GetID() string {
	return "security"
}

func (m *Module) GetName() string {
	return "Security"
}

func (m *Module) SetManager(manager *maf.Manager) {
	m.manager = manager
}

func (m *Module) GetConfigSchema() []maf.ConfigItem {
	return []maf.ConfigItem{
		{Name: "security.secret", Type: maf.String, Required: true},
	}
}

func (m *Module) SetConfig(config *maf.Config) {
	m.config = config
}

func (m *Module) Initialize() error {
	m.passwordsManager = passwords.NewManager()
	return nil
}

func (m *Module) PreStart() error {
	return nil
}

func (m *Module) GetPasswordsManager() *passwords.Manager {
	return m.passwordsManager
}
