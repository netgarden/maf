package rpc

import (
	"github.com/netgarden/rrpc"
)

// -- Structs ---------------------------------------------

type ListProvidersResponse struct {
	Providers []ProviderItem    `json:"providers"`
	PageInfo  DatatablePageInfo `json:"pageInfo"`
}

type ListPublicProvidersResponse struct {
	Providers []PublicProviderSummary `json:"providers"`
}

type ProviderItem struct {
	Id      string `json:"id"`
	Type    string `json:"type"`
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type PublicProviderSummary struct {
	Type string `json:"type"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type SetProviderEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

// -- Services --------------------------------------------

type ProvidersService interface {
	List(ctx *rrpc.Context, request *DatatableRequest) (*ListProvidersResponse, error)
	SetEnabled(ctx *rrpc.Context, request *SetProviderEnabledRequest) error
	Delete(ctx *rrpc.Context) error
}

type PublicProvidersService interface {
	List(ctx *rrpc.Context) (*ListPublicProvidersResponse, error)
}

// -- Errors ----------------------------------------------

var (
	ErrProviderNotFound = rrpc.RRPCError{Code: 10, Name: "ProviderNotFound", Message: "provider not found", HTTPStatus: 404}
)
