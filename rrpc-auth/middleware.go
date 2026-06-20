package rrpcauth

import (
	"slices"
	"strings"

	"github.com/netgarden/rrpc"
	"github.com/netgarden/maf/auth/services"
	"github.com/netgarden/maf/rrpc-auth/authctx"
	"github.com/netgarden/maf/rrpc-auth/rpc"
)

// NewAuthenticationMiddleware validates the Bearer token when present and stores
// the user ID in context. It never rejects a request — identity extraction is
// best-effort so that public routes also benefit from knowing the caller.
func NewAuthenticationMiddleware(authSvc *services.AuthService) rrpc.Middleware {
	return func(ctx *rrpc.Context, next func(*rrpc.Context) error) error {
		authHeader := ctx.Request().Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if userID, err := authSvc.ParseAccessToken(token); err == nil {
				authctx.SetUserID(ctx, userID)
			}
		}
		return next(ctx)
	}
}

// authPaths are the rrpc-auth module's own endpoints, exempted when
// allowAuthPaths is true.
var authPaths = []string{
	"/api/auth/login",
	"/api/auth/logout",
	"/api/auth/refresh",
}

// NewAuthorizationMiddleware returns a middleware that enforces authentication
// on every request for which isPublic returns false. It must run after the
// authentication middleware so the user ID is already in context.
//
// allowAuthPaths — when true, the rrpc-auth module's own endpoints
// (login, logout, refresh) are automatically allowed without a token.
//
// isPublic — called for every request; return true to allow access without
// a token (e.g. signup, health-check). May be nil when allowAuthPaths alone
// is sufficient.
func NewAuthorizationMiddleware(allowAuthPaths bool, isPublic func(*rrpc.Context) bool) rrpc.Middleware {
	return func(ctx *rrpc.Context, next func(*rrpc.Context) error) error {
		if allowAuthPaths && slices.Contains(authPaths, ctx.Request().URL.Path) {
			return next(ctx)
		}
		if isPublic != nil && isPublic(ctx) {
			return next(ctx)
		}
		if _, ok := authctx.GetUserID(ctx); !ok {
			return rpc.ErrUnauthorized
		}
		return next(ctx)
	}
}
