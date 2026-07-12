package entities

import (
	"github.com/netgarden/maf/database"
	uuid "github.com/satori/go.uuid"
)

// OIDCProvider holds the fields unique to the OIDC protocol for one
// entities.Provider row (Type: "oidc") — everything protocol-agnostic
// (Slug, Name, Enabled, the admin-claim mapping) lives on Provider itself.
// It's a separate table rather than extra columns on Provider so a future
// SAML/LDAP provider type doesn't inherit OIDC-only columns it has no use
// for.
//
// ProviderID is a real foreign key with ON DELETE CASCADE (unlike
// UserIdentity's deliberately FK-less ProviderID — see that type's doc
// comment): this relationship is 1:1 with a single, fixed target table, not
// polymorphic across protocols, so there's no reason not to let Postgres
// enforce and cascade it. Deleting the Provider row is what
// ProvidersService.Delete does; this table's row disappears with it for
// free.
type OIDCProvider struct {
	database.EntityBase

	ProviderID uuid.UUID `gorm:"type:uuid;uniqueIndex;not null"`
	Provider   *Provider `gorm:"foreignKey:ProviderID;references:ID;constraint:OnDelete:CASCADE"`

	IssuerURL string
	ClientID  string

	// ClientSecretEncrypted is the OAuth client secret encrypted at rest via
	// security.Module.GetEncryptionManager() — see OIDCProvidersService.
	// Never stored or logged in plaintext.
	ClientSecretEncrypted []byte

	Scopes string `gorm:"not null;default:'openid profile email'"`
}

func (OIDCProvider) TableName() string {
	return "auth_oidc_providers"
}
