package rpc

import (
	"github.com/netgarden/rrpc"
)

// -- Structs ---------------------------------------------

type ApiKeyItem struct {
	Id         string   `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	CreatedAt  string   `json:"createdAt"`
	LastUsedAt *string  `json:"lastUsedAt,omitempty"`
	ExpiresAt  *string  `json:"expiresAt,omitempty"`
	AllowedIps []string `json:"allowedIps"`
}

type CreateApiKeyRequest struct {
	Name       string   `json:"name"`
	ExpiresAt  *string  `json:"expiresAt,omitempty"`
	AllowedIps []string `json:"allowedIps,omitempty"`
}

type CreateApiKeyResponse struct {
	Id         string   `json:"id"`
	Name       string   `json:"name"`
	Token      string   `json:"token"`
	Prefix     string   `json:"prefix"`
	CreatedAt  string   `json:"createdAt"`
	ExpiresAt  *string  `json:"expiresAt,omitempty"`
	AllowedIps []string `json:"allowedIps"`
}

type ListApiKeysResponse struct {
	Keys []ApiKeyItem `json:"keys"`
}

// -- Services --------------------------------------------

type ApiKeysService interface {
	List(ctx *rrpc.Context) (*ListApiKeysResponse, error)
	Create(ctx *rrpc.Context, request *CreateApiKeyRequest) (*CreateApiKeyResponse, error)
	Delete(ctx *rrpc.Context) error
}

// -- Errors ----------------------------------------------

var (
	ErrApiKeyNotFound = rrpc.RRPCError{Code: 7, Name: "ApiKeyNotFound", Message: "API key not found", HTTPStatus: 404}

	ErrApiKeyRestricted = rrpc.RRPCError{Code: 8, Name: "ApiKeyRestricted", Message: "an API key cannot perform this action", HTTPStatus: 403}
)
