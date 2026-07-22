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
	}
}

func (m *Module) SetConfig(config *maf.Config) {
	m.config = config
}

// encryptionKeySuffix namespaces security.secret before it's handed to
// the encryption subsystem - a fixed constant, not a cryptographic
// algorithm, so unlike a hash it's never a thing that could need
// replacing later. The actual key derivation (today: SHA-256, see
// encryption.NewAESGCMCryptor) stays inside encryption's own Cryptor
// implementations, already covered by their Prefix-based crypto-agility -
// the mechanism that lets a future Cryptor use a different derivation
// while old ciphertext, tagged with the old Cryptor's Prefix, still
// decrypts under it. Baking a hash in here instead would create a
// second, un-versioned point of permanence that mechanism doesn't cover:
// this way, only this constant is permanent, not a whole algorithm
// choice.
const encryptionKeySuffix = "::encryption_key"

func (m *Module) Initialize() error {
	m.passwordsManager = passwords.NewManager()

	secret := m.config.GetString("security.secret")
	m.encryptionManager = encryption.NewManager(secret + encryptionKeySuffix)

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
