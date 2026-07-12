package impl

import (
	"errors"

	mafauthentities "github.com/netgarden/maf/auth/entities"
	mafauth "github.com/netgarden/maf/auth/services"
	"github.com/netgarden/maf/datatables"
	"github.com/netgarden/maf/rrpc-auth/rpc"
	"github.com/netgarden/rrpc"
)

// NewProvidersService backs the generic, protocol-agnostic provider admin
// actions: list every registered provider regardless of type, enable/
// disable, delete. Protocol-specific create/update (which need fields only
// that protocol has, e.g. OIDC's issuer/client/secret) live on their own
// service instead — see impl/oidc_providers.go.
func NewProvidersService(svc *mafauth.ProvidersService) rpc.ProvidersService {
	return &ProvidersServiceImpl{providers: svc}
}

type ProvidersServiceImpl struct {
	providers *mafauth.ProvidersService
}

func (s *ProvidersServiceImpl) List(ctx *rrpc.Context, req *rpc.DatatableRequest) (*rpc.ListProvidersResponse, error) {
	result, err := s.providers.ListPage(datatables.Query{
		Page:     req.Page,
		PageSize: req.PageSize,
		SortBy:   req.SortBy,
		SortDir:  datatables.SortDir(req.SortDir),
		Filters:  req.Filters,
	})
	if err != nil {
		if errors.Is(err, datatables.ErrUnknownSortColumn) || errors.Is(err, datatables.ErrUnknownFilterColumn) {
			return nil, rrpc.ErrRrpcBadRequest.WithCause(err)
		}
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}

	items := make([]rpc.ProviderItem, 0, len(result.Items))
	for _, p := range result.Items {
		items = append(items, toGenericProviderItem(p))
	}

	return &rpc.ListProvidersResponse{
		Providers: items,
		PageInfo: rpc.DatatablePageInfo{
			TotalCount: int(result.TotalCount),
			Page:       req.Page,
			PageSize:   req.PageSize,
		},
	}, nil
}

func (s *ProvidersServiceImpl) SetEnabled(ctx *rrpc.Context, req *rpc.SetProviderEnabledRequest) error {
	id := ctx.Params().GetString("id")
	found, err := s.providers.SetEnabled(id, req.Enabled)
	if err != nil {
		return rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if !found {
		return rpc.ErrProviderNotFound
	}
	return nil
}

func (s *ProvidersServiceImpl) Delete(ctx *rrpc.Context) error {
	id := ctx.Params().GetString("id")
	found, err := s.providers.Delete(id)
	if err != nil {
		return rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if !found {
		return rpc.ErrProviderNotFound
	}
	return nil
}

func toGenericProviderItem(p *mafauthentities.Provider) rpc.ProviderItem {
	return rpc.ProviderItem{
		Id:      p.ID.String(),
		Type:    p.Type,
		Slug:    p.Slug,
		Name:    p.Name,
		Enabled: p.Enabled,
	}
}

// NewPublicProvidersService backs the one public, unauthenticated endpoint
// a login page needs regardless of protocol: which enabled providers exist,
// so it can render a "continue with {name}" button per provider and route
// each to its own protocol-specific login endpoint using Type
// (/api/auth/oidc/{slug}/login today; /api/auth/saml/{slug}/login,
// /api/auth/ldap/{slug}/login etc. later). See rrpc-auth/middleware.go's
// authPaths — "api/auth/providers" must stay in that exemption list.
func NewPublicProvidersService(svc *mafauth.ProvidersService) rpc.PublicProvidersService {
	return &PublicProvidersServiceImpl{providers: svc}
}

type PublicProvidersServiceImpl struct {
	providers *mafauth.ProvidersService
}

func (s *PublicProvidersServiceImpl) List(ctx *rrpc.Context) (*rpc.ListPublicProvidersResponse, error) {
	providers, err := s.providers.ListEnabled()
	if err != nil {
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}

	items := make([]rpc.PublicProviderSummary, 0, len(providers))
	for _, p := range providers {
		items = append(items, rpc.PublicProviderSummary{Type: p.Type, Slug: p.Slug, Name: p.Name})
	}
	return &rpc.ListPublicProvidersResponse{Providers: items}, nil
}
