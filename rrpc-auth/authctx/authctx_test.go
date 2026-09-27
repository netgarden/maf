package authctx

import (
	"net/http/httptest"
	"testing"

	"github.com/netgarden/rrpc"
)

func newTestContext() *rrpc.Context {
	req := httptest.NewRequest("GET", "/", nil)
	return rrpc.NewContext(req, httptest.NewRecorder(), rrpc.NewParams())
}

func TestGetUserID_Unset(t *testing.T) {
	ctx := newTestContext()
	if _, ok := GetUserID(ctx); ok {
		t.Error("expected ok=false before SetUserID is called")
	}
}

func TestSetUserID_RoundTrips(t *testing.T) {
	ctx := newTestContext()
	SetUserID(ctx, "user-123")

	id, ok := GetUserID(ctx)
	if !ok || id != "user-123" {
		t.Errorf("GetUserID() = (%q, %v), want (\"user-123\", true)", id, ok)
	}
}

// TestUserIDAndAuthMethodKeysDoNotCollide guards the exact bug the two
// distinctly-named key types (userIDKeyType/authMethodKeyType) exist to
// prevent: a single shared empty-struct type for every context key would
// make ctx.Set(key1, "session-id") and ctx.Set(key2, AuthMethodApiKey)
// silently overwrite each other, since two contextKey{} values of the same
// type are equal regardless of which var declared them.
func TestUserIDAndAuthMethodKeysDoNotCollide(t *testing.T) {
	ctx := newTestContext()
	SetUserID(ctx, "user-123")
	SetAuthMethod(ctx, AuthMethodApiKey)

	if id, ok := GetUserID(ctx); !ok || id != "user-123" {
		t.Errorf("GetUserID() = (%q, %v), want (\"user-123\", true) — SetAuthMethod must not clobber it", id, ok)
	}
	if m, ok := GetAuthMethod(ctx); !ok || m != AuthMethodApiKey {
		t.Errorf("GetAuthMethod() = (%q, %v), want (%q, true)", m, ok, AuthMethodApiKey)
	}
}

func TestIsApiKeyAuth(t *testing.T) {
	cases := []struct {
		name   string
		set    func(*rrpc.Context)
		expect bool
	}{
		{"unauthenticated", func(*rrpc.Context) {}, false},
		{"session", func(ctx *rrpc.Context) { SetAuthMethod(ctx, AuthMethodSession) }, false},
		{"api key", func(ctx *rrpc.Context) { SetAuthMethod(ctx, AuthMethodApiKey) }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := newTestContext()
			c.set(ctx)
			if got := IsApiKeyAuth(ctx); got != c.expect {
				t.Errorf("IsApiKeyAuth() = %v, want %v", got, c.expect)
			}
		})
	}
}
