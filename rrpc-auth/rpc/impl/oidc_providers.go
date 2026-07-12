package impl

import (
	mafauthdto "github.com/netgarden/maf/auth/dto"
	mafauth "github.com/netgarden/maf/auth/services"
	"github.com/netgarden/maf/rrpc-auth/rpc"
	"github.com/netgarden/rrpc"
)

// NewOIDCProvidersService backs the OIDC-specific half of provider admin
// CRUD (create/update — issuer/client ID/secret/scopes). Listing, enabling/
// disabling, and deleting a provider are protocol-agnostic actions and live
// on the generic Providers service instead — see impl/providers.go and
// mafauth.ProvidersService.
func NewOIDCProvidersService(svc *mafauth.OIDCProvidersService) rpc.OIDCProvidersService {
	return &OIDCProvidersServiceImpl{providers: svc}
}

type OIDCProvidersServiceImpl struct {
	providers *mafauth.OIDCProvidersService
}

// Get returns the OIDC-specific fields (issuer/client/scopes/admin-claim
// mapping) an edit form needs to prefill itself — the generic
// Providers.list used for the admin table only carries
// id/type/slug/name/enabled, not any protocol-specific field.
func (s *OIDCProvidersServiceImpl) Get(ctx *rrpc.Context) (*rpc.OIDCProviderItem, error) {
	id := ctx.Params().GetString("id")
	provider, err := s.providers.Get(id)
	if err != nil {
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if provider == nil {
		return nil, rpc.ErrOIDCProviderNotFound
	}
	item := toProviderItem(provider)
	return &item, nil
}

// Create's request carries the plaintext ClientSecret over the wire once
// (admin CRUD is /api/admin/*, protected the same way users.rrpc's Users
// CRUD is) — OIDCProvidersService.Create encrypts it before it ever
// touches the database; it's never sent back out again (see toProviderItem).
func (s *OIDCProvidersServiceImpl) Create(ctx *rrpc.Context, req *rpc.CreateOIDCProviderRequest) (*rpc.OIDCProviderItem, error) {
	created, err := s.providers.Create(&mafauthdto.OIDCProviderCreateDTO{
		Slug:             req.Slug,
		Name:             req.Name,
		IssuerURL:        req.IssuerUrl,
		ClientID:         req.ClientId,
		ClientSecret:     req.ClientSecret,
		Scopes:           req.Scopes,
		Enabled:          req.Enabled,
		AdminClaimPath:   req.AdminClaimPath,
		AdminClaimValues: req.AdminClaimValues,
	})
	if err != nil {
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if created == nil {
		return nil, rpc.ErrOIDCProviderAlreadyExists
	}
	item := toProviderItem(created)
	return &item, nil
}

// Update's req.ClientSecret is blank whenever an admin didn't intend to
// rotate the secret — see dto.OIDCProviderUpdateDTO and the admin UI,
// which never receives the current plaintext secret to echo back.
func (s *OIDCProvidersServiceImpl) Update(ctx *rrpc.Context, req *rpc.UpdateOIDCProviderRequest) (*rpc.OIDCProviderItem, error) {
	id := ctx.Params().GetString("id")
	updated, err := s.providers.Update(id, &mafauthdto.OIDCProviderUpdateDTO{
		Name:             req.Name,
		IssuerURL:        req.IssuerUrl,
		ClientID:         req.ClientId,
		ClientSecret:     req.ClientSecret,
		Scopes:           req.Scopes,
		Enabled:          req.Enabled,
		AdminClaimPath:   req.AdminClaimPath,
		AdminClaimValues: req.AdminClaimValues,
	})
	if err != nil {
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if updated == nil {
		return nil, rpc.ErrOIDCProviderNotFound
	}
	item := toProviderItem(updated)
	return &item, nil
}

// toProviderItem deliberately never includes ClientSecretEncrypted (or a
// decrypted form of it) — the admin CRUD API can set a provider's secret,
// never read it back. id/slug/name/enabled/admin-claim fields come from the
// joined generic Provider row, everything else from the OIDC-specific one
// — see mafauth.OIDCProviderDetail.
func toProviderItem(p *mafauth.OIDCProviderDetail) rpc.OIDCProviderItem {
	return rpc.OIDCProviderItem{
		Id:               p.ID.String(),
		Slug:             p.Slug,
		Name:             p.Name,
		IssuerUrl:        p.OIDC.IssuerURL,
		ClientId:         p.OIDC.ClientID,
		Scopes:           p.OIDC.Scopes,
		Enabled:          p.Enabled,
		AdminClaimPath:   p.AdminClaimPath,
		AdminClaimValues: p.AdminClaimValues,
	}
}
