package rrpcauth

import (
	"net/http"
	"slices"
	"strings"

	"github.com/netgarden/maf/auth/services"
	"github.com/netgarden/maf/rrpc-auth/authctx"
	"github.com/netgarden/maf/rrpc-auth/rpc"
	"github.com/netgarden/rrpc"
)

// NewAuthenticationMiddleware validates the Bearer token when present and stores
// the user ID in context. It never rejects a request — identity extraction is
// best-effort so that public routes also benefit from knowing the caller.
func NewAuthenticationMiddleware(authSvc *services.AuthService) rrpc.Middleware {
	return func(ctx *rrpc.Context, next func(*rrpc.Context) error) error {
		if token := extractBearerToken(ctx.Request()); token != "" {
			if userID, err := authSvc.ParseAccessToken(token); err == nil {
				authctx.SetUserID(ctx, userID)
			}
		}
		return next(ctx)
	}
}

// extractBearerToken reads the bearer token from the Authorization
// header, falling back to the Sec-WebSocket-Protocol header for
// WebSocket upgrade requests (a "@Stream"/"@InputStream"/"@OutputStream"
// rrpc method, see rrpc/websocket.go) - a browser's native WebSocket
// constructor can't set arbitrary headers on the handshake request, so
// the generated TS client instead offers the token as a subprotocol
// (new WebSocket(url, ["bearer", token])), which arrives here as
// "bearer, <token>". This runs as ordinary rrpc middleware, before any
// upgrade attempt, so it works the same way regardless of whether the
// matched route turns out to be a streaming one.
func extractBearerToken(r *http.Request) string {
	if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	if proto := r.Header.Get("Sec-WebSocket-Protocol"); proto != "" {
		parts := strings.Split(proto, ",")
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "bearer" {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

// authPaths are the rrpc-auth module's own exact-match endpoints, exempted
// when allowAuthPaths is true. requestPasswordReset/confirmPasswordReset
// are necessarily public too — they're how a logged-out user recovers
// access in the first place.
var authPaths = []string{
	"/api/auth/login",
	"/api/auth/logout",
	"/api/auth/refresh",
	"/api/auth/requestPasswordReset",
	"/api/auth/confirmPasswordReset",
	// PublicProviders.list — the generic, protocol-agnostic "which enabled
	// providers exist" endpoint a login page calls to render its provider
	// picker regardless of how many protocols (OIDC, future SAML/LDAP) are
	// registered; see rpc/def/providers.rrpc.
	"/api/auth/providers",
}

// authPathPrefixes covers rrpc-auth's own endpoints whose path contains a
// variable segment ({slug:string}), so an exact-match list (authPaths)
// can't express them. OIDCAuth's whole api/auth/oidc/* tree is public by
// necessity — it's the login flow a logged-out browser has to reach (the
// authorize redirect and the callback) — same status as login/logout/
// refresh above. This deliberately does NOT cover api/admin/oidc/providers
// (the admin CRUD registry), which still requires the consuming app's
// normal /api/admin/* protection.
var authPathPrefixes = []string{
	"/api/auth/oidc/",
}

// NewAuthorizationMiddleware returns a middleware that enforces authentication
// on every request for which isPublic returns false. It must run after the
// authentication middleware so the user ID is already in context.
//
// allowAuthPaths — when true, the rrpc-auth module's own endpoints
// (login, logout, refresh, requestPasswordReset, confirmPasswordReset, the
// public provider picker, and the whole api/auth/oidc/* login-flow tree)
// are automatically allowed without a token.
//
// isPublic — called for every request; return true to allow access without
// a token (e.g. signup, health-check). May be nil when allowAuthPaths alone
// is sufficient.
func NewAuthorizationMiddleware(allowAuthPaths bool, isPublic func(*rrpc.Context) bool) rrpc.Middleware {
	return func(ctx *rrpc.Context, next func(*rrpc.Context) error) error {
		if allowAuthPaths && isAuthPath(ctx.Request().URL.Path) {
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

func isAuthPath(path string) bool {
	if slices.Contains(authPaths, path) {
		return true
	}
	for _, prefix := range authPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
