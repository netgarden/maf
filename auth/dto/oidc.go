package dto

// OIDCProviderCreateDTO is the admin-supplied shape for registering a new
// OIDC provider — see services.OIDCProvidersService.Create.
type OIDCProviderCreateDTO struct {
	Slug             string
	Name             string
	IssuerURL        string
	ClientID         string
	ClientSecret     string
	Scopes           string
	Enabled          bool
	AdminClaimPath   string
	AdminClaimValues []string
}

// OIDCProviderUpdateDTO is the admin-supplied shape for editing an existing
// provider. ClientSecret, if non-empty, replaces the stored secret; an
// empty value means "keep the existing one" — the encrypted secret is
// never round-tripped back to an admin UI to display, so there's nothing
// else a client could echo back here.
type OIDCProviderUpdateDTO struct {
	Name             string
	IssuerURL        string
	ClientID         string
	ClientSecret     string
	Scopes           string
	Enabled          bool
	AdminClaimPath   string
	AdminClaimValues []string
}
