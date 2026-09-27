package authctx

import "github.com/netgarden/rrpc"

// Each context key gets its own distinctly-named empty-struct type. Two
// context keys of the *same* type would be equal (contextKey{} == contextKey{}
// for any single empty-struct type), so a single shared type here — fine
// while there was only one key — would silently collide once a second one
// was added: ctx.Set/ctx.Get would treat them as the same key.
type userIDKeyType struct{}
type authMethodKeyType struct{}

var userIDKey = userIDKeyType{}
var authMethodKey = authMethodKeyType{}

// SetUserID stores the authenticated user ID in the rrpc context.
// Called by auth middleware after successful token validation.
func SetUserID(ctx *rrpc.Context, userID string) {
	ctx.Set(userIDKey, userID)
}

// GetUserID reads the authenticated user ID placed in the context by the
// auth middleware. Returns ("", false) when the request is unauthenticated.
func GetUserID(ctx *rrpc.Context) (string, bool) {
	v, ok := ctx.Get(userIDKey)
	if !ok {
		return "", false
	}
	userID, ok := v.(string)
	return userID, ok
}

// AuthMethod says how a request's identity was established — not who it is
// (that's GetUserID), but which credential proved it. Some actions are
// restricted to one method regardless of the caller's own privilege; see
// IsApiKeyAuth.
type AuthMethod string

const (
	AuthMethodSession AuthMethod = "session"
	AuthMethodApiKey  AuthMethod = "apikey"
)

// SetAuthMethod records how a request authenticated, alongside SetUserID.
// Called by the auth middleware on every successful validation.
func SetAuthMethod(ctx *rrpc.Context, m AuthMethod) {
	ctx.Set(authMethodKey, m)
}

// GetAuthMethod reads the method SetAuthMethod recorded. Returns ("", false)
// when the request is unauthenticated.
func GetAuthMethod(ctx *rrpc.Context) (AuthMethod, bool) {
	v, ok := ctx.Get(authMethodKey)
	if !ok {
		return "", false
	}
	m, ok := v.(AuthMethod)
	return m, ok
}

// IsApiKeyAuth reports whether the current request authenticated with an API
// key rather than a session. A handful of credential-administration actions
// (minting a new API key, creating an admin user, changing anyone's admin
// flag) are refused for this method regardless of the caller's own
// privilege — see rpc/impl/users.go and rpc/impl/api_keys.go.
func IsApiKeyAuth(ctx *rrpc.Context) bool {
	m, ok := GetAuthMethod(ctx)
	return ok && m == AuthMethodApiKey
}
