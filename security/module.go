package security

import (
	"github.com/netgarden/maf"
	"github.com/netgarden/maf/security/encryption"
	"github.com/netgarden/maf/security/passwords"
)

func NewModule() *Module {
	return &Module{}
}

type Module struct {
	manager *maf.Manager
	config  *maf.Config

	passwordsManager  *passwords.Manager
	encryptionManager *encryption.Manager
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
		// Deliberately separate from security.secret (used for JWT signing
		// in auth) — reusing one secret for two unrelated cryptographic
		// purposes is bad practice even though nothing technically stops it.
		{Name: "security.encryption.key", Type: maf.String, Required: true},
	}
}

func (m *Module) SetConfig(config *maf.Config) {
	m.config = config
}

func (m *Module) Initialize() error {
	m.passwordsManager = passwords.NewManager()
	m.encryptionManager = encryption.NewManager(m.config.GetString("security.encryption.key"))
	return nil
}

func (m *Module) PreStart() error {
	return nil
}

func (m *Module) GetPasswordsManager() *passwords.Manager {
	return m.passwordsManager
}

func (m *Module) GetEncryptionManager() *encryption.Manager {
	return m.encryptionManager
}
