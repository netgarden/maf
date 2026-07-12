package entities

import (
	"github.com/netgarden/maf/database"
	uuid "github.com/satori/go.uuid"
)

// UserIdentity links a local User to an identity asserted by an external
// authentication protocol — OIDC today; SAML/LDAP are expected to reuse
// this same table later (see auth/services/identity.go's ExternalIdentity).
//
// ProviderID references the generic Provider row (auth_providers.id), not
// a protocol-specific one — a UserIdentity's provider is unambiguous and
// global regardless of protocol, so (ProviderID, Subject) alone is already
// unique without needing ProviderType in the key. ProviderType is kept as
// a plain, denormalized field purely so "list a user's linked identities"
// can render a label per row without an extra join.
//
// There's still no DB foreign key on ProviderID, unlike OIDCProvider's own
// link to Provider: ProvidersService.Delete removes matching UserIdentity
// rows itself (a Provider can be deleted independently of any protocol-
// specific row it once had), so a hard FK would only get in the way here.
type UserIdentity struct {
	database.EntityBase

	UserID uuid.UUID `gorm:"type:uuid;not null;index"`

	ProviderType string
	ProviderID   uuid.UUID `gorm:"type:uuid;uniqueIndex:idx_user_identity_provider_subject;not null"`
	Subject      string    `gorm:"uniqueIndex:idx_user_identity_provider_subject;not null"`

	// Email is cached at link time for display/debugging — CompleteExternalLogin
	// always re-derives auth decisions from the live ExternalIdentity, never
	// from this cached copy.
	Email string
}

func (UserIdentity) TableName() string {
	return "auth_user_identities"
}
