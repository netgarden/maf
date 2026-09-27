package impl

import (
	"time"

	mafauth "github.com/netgarden/maf/auth/services"
	"github.com/netgarden/maf/rrpc-auth/authctx"
	"github.com/netgarden/maf/rrpc-auth/rpc"
	"github.com/netgarden/rrpc"
	uuid "github.com/satori/go.uuid"
)

func NewApiKeysService(svc *mafauth.ApiKeysService) rpc.ApiKeysService {
	return &ApiKeysServiceImpl{keys: svc}
}

type ApiKeysServiceImpl struct {
	keys *mafauth.ApiKeysService
}

// callerID resolves the current request's own user ID as a uuid.UUID — every
// ApiKeys method is self-service only, always scoped to the caller, never a
// request-supplied user id.
func callerID(ctx *rrpc.Context) (uuid.UUID, error) {
	id, ok := authctx.GetUserID(ctx)
	if !ok {
		return uuid.Nil, rpc.ErrUnauthorized
	}
	uid, err := uuid.FromString(id)
	if err != nil {
		return uuid.Nil, rpc.ErrUnauthorized
	}
	return uid, nil
}

func (s *ApiKeysServiceImpl) List(ctx *rrpc.Context) (*rpc.ListApiKeysResponse, error) {
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}

	keys, err := s.keys.List(userID)
	if err != nil {
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}

	items := make([]rpc.ApiKeyItem, 0, len(keys))
	for _, k := range keys {
		items = append(items, rpc.ApiKeyItem{
			Id:         k.ID.String(),
			Name:       k.Name,
			Prefix:     k.Prefix,
			CreatedAt:  k.CreatedAt.Format(time.RFC3339),
			LastUsedAt: formatOptionalTime(k.LastUsedAt),
			ExpiresAt:  formatOptionalTime(k.ExpiresAt),
			AllowedIps: nonEmpty(k.AllowedIPs),
		})
	}
	return &rpc.ListApiKeysResponse{Keys: items}, nil
}

// Create is refused unconditionally for a request authenticated by API key —
// even one belonging to an admin — so a leaked or agent-held key can never
// mint itself a replacement or perpetuate its own access. Minting a new key
// requires an actual session (password/OIDC login).
func (s *ApiKeysServiceImpl) Create(ctx *rrpc.Context, req *rpc.CreateApiKeyRequest) (*rpc.CreateApiKeyResponse, error) {
	if authctx.IsApiKeyAuth(ctx) {
		return nil, rpc.ErrApiKeyRestricted
	}
	userID, err := callerID(ctx)
	if err != nil {
		return nil, err
	}

	expiresAt, err := parseOptionalTime(req.ExpiresAt)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("invalid expiresAt: %w", err)
	}

	plaintext, key, err := s.keys.Create(userID, req.Name, expiresAt, req.AllowedIps)
	if err != nil {
		// The only validation Create itself does is the AllowedIPs
		// normalisation (mafauth.CheckApiKeyAllowedIPs) — a client input
		// error, not a server fault.
		return nil, rrpc.ErrRrpcBadRequest.WithCause(err)
	}

	return &rpc.CreateApiKeyResponse{
		Id:         key.ID.String(),
		Name:       key.Name,
		Token:      plaintext,
		Prefix:     key.Prefix,
		CreatedAt:  key.CreatedAt.Format(time.RFC3339),
		ExpiresAt:  formatOptionalTime(key.ExpiresAt),
		AllowedIps: nonEmpty(key.AllowedIPs),
	}, nil
}

// Delete (revoke) is deliberately NOT restricted for API-key auth — an agent
// should be able to retire its own compromised key. Only minting a new one
// (Create, above) is refused.
func (s *ApiKeysServiceImpl) Delete(ctx *rrpc.Context) error {
	userID, err := callerID(ctx)
	if err != nil {
		return err
	}
	id, err := uuid.FromString(ctx.Params().GetString("id"))
	if err != nil {
		return rpc.ErrApiKeyNotFound
	}

	found, err := s.keys.Revoke(userID, id)
	if err != nil {
		return rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if !found {
		return rpc.ErrApiKeyNotFound
	}
	return nil
}

func formatOptionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

func parseOptionalTime(s *string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// nonEmpty turns a nil slice into an empty (non-nil) one so the response
// always carries allowedIps: [] rather than allowedIps: null (the rrpc field
// isn't declared @optional).
func nonEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
