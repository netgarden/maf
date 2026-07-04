package authctx

import "github.com/netgarden/rrpc"

type contextKey struct{}

var userIDKey = contextKey{}

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
