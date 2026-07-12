package rpc

import (
	"github.com/netgarden/rrpc"
)

// -- Structs ---------------------------------------------

type CreateOIDCProviderRequest struct {
	Slug             string   `json:"slug"`
	Name             string   `json:"name"`
	IssuerUrl        string   `json:"issuerUrl"`
	ClientId         string   `json:"clientId"`
	ClientSecret     string   `json:"clientSecret"`
	Scopes           string   `json:"scopes"`
	Enabled          bool     `json:"enabled"`
	AdminClaimPath   string   `json:"adminClaimPath"`
	AdminClaimValues []string `json:"adminClaimValues"`
}

type OIDCProviderItem struct {
	Id               string   `json:"id"`
	Slug             string   `json:"slug"`
	Name             string   `json:"name"`
	IssuerUrl        string   `json:"issuerUrl"`
	ClientId         string   `json:"clientId"`
	Scopes           string   `json:"scopes"`
	Enabled          bool     `json:"enabled"`
	AdminClaimPath   string   `json:"adminClaimPath"`
	AdminClaimValues []string `json:"adminClaimValues"`
}

type UpdateOIDCProviderRequest struct {
	Name             string   `json:"name"`
	IssuerUrl        string   `json:"issuerUrl"`
	ClientId         string   `json:"clientId"`
	ClientSecret     string   `json:"clientSecret"`
	Scopes           string   `json:"scopes"`
	Enabled          bool     `json:"enabled"`
	AdminClaimPath   string   `json:"adminClaimPath"`
	AdminClaimValues []string `json:"adminClaimValues"`
}

// -- Services --------------------------------------------

type OIDCAuthService interface {
	Login(ctx *rrpc.Context) error
	Callback(ctx *rrpc.Context) error
}

type OIDCProvidersService interface {
	Get(ctx *rrpc.Context) (*OIDCProviderItem, error)
	Create(ctx *rrpc.Context, request *CreateOIDCProviderRequest) (*OIDCProviderItem, error)
	Update(ctx *rrpc.Context, request *UpdateOIDCProviderRequest) (*OIDCProviderItem, error)
}

// -- Errors ----------------------------------------------

var (
	ErrOIDCLoginFailed = rrpc.RRPCError{Code: 9, Name: "OIDCLoginFailed", Message: "oidc login failed", HTTPStatus: 400}

	ErrOIDCProviderAlreadyExists = rrpc.RRPCError{Code: 8, Name: "OIDCProviderAlreadyExists", Message: "an oidc provider with this slug already exists", HTTPStatus: 409}

	ErrOIDCProviderNotFound = rrpc.RRPCError{Code: 7, Name: "OIDCProviderNotFound", Message: "oidc provider not found", HTTPStatus: 404}
)
