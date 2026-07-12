package entities

import "github.com/netgarden/maf/database"

// Provider is the generic, protocol-agnostic row every external identity
// provider has one of — OIDC today, SAML/LDAP expected later (see
// auth/services/identity.go's ExternalIdentity). It carries every field
// that's meaningful regardless of protocol (display name, enabled/disabled,
// the optional admin-claim mapping) plus the one field admin/login flows
// actually route on: Slug. Protocol-specific config (an OIDC issuer/client
// ID/secret, a future SAML IdP metadata URL, a future LDAP bind DN/host)
// lives in its own table — see entities.OIDCProvider — linked back here by
// ProviderID, so a single generic service (services.ProvidersService) can
// list/enable/disable/delete any provider without knowing its protocol,
// while protocol-specific services own the fields only they need.
type Provider struct {
	database.EntityBase

	// Type names which protocol-specific table (and services.*AuthService)
	// this provider belongs to — "oidc" today.
	Type string `gorm:"index;not null"`

	Slug    string `gorm:"uniqueIndex;not null"`
	Name    string
	Enabled bool

	// AdminClaimPath/AdminClaimValues optionally auto-set User.Admin from a
	// claim/attribute the protocol's identity assertion carries, re-evaluated
	// on every login — see AuthService.CompleteExternalLogin. AdminClaimPath
	// is a flat claim name only (e.g. "groups"); nested/dotted paths (e.g.
	// Keycloak's resource_access.<client>.roles) are a known v1 limitation.
	// An empty AdminClaimPath disables the mapping entirely for this
	// provider. Lives here rather than per-protocol since every protocol
	// resolves to the same generic ExternalIdentity.Claims shape the mapping
	// is evaluated against.
	AdminClaimPath   string
	AdminClaimValues []string `gorm:"serializer:json;type:text"`
}

func (Provider) TableName() string {
	return "auth_providers"
}
