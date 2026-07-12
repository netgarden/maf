package rrpcauth

import (
	"net/http/httptest"
	"testing"

	"github.com/netgarden/maf/rrpc-auth/rpc"
	"github.com/netgarden/rrpc"
)

func TestIsAuthPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/api/auth/login", true},
		{"/api/auth/refresh", true},
		{"/api/auth/providers", true},
		{"/api/auth/oidc/okta/login", true},
		{"/api/auth/oidc/okta/callback", true},
		{"/api/admin/oidc/providers", false},
		{"/api/admin/providers", false},
		{"/api/admin/users", false},
		{"/api/some/other/path", false},
	}
	for _, c := range cases {
		if got := isAuthPath(c.path); got != c.want {
			t.Errorf("isAuthPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func noopNext(ctx *rrpc.Context) error { return nil }

func TestNewAuthorizationMiddleware_AllowsOIDCLoginFlowWithoutToken(t *testing.T) {
	mw := NewAuthorizationMiddleware(true, nil)

	for _, path := range []string{
		"/api/auth/providers",
		"/api/auth/oidc/okta/login",
		"/api/auth/oidc/okta/callback",
	} {
		req := httptest.NewRequest("GET", path, nil)
		ctx := rrpc.NewContext(req, httptest.NewRecorder(), nil)

		if err := mw(ctx, noopNext); err != nil {
			t.Errorf("path %q: expected no error (public login route), got %v", path, err)
		}
	}
}

func TestNewAuthorizationMiddleware_StillGatesAdminProviderRoutes(t *testing.T) {
	mw := NewAuthorizationMiddleware(true, nil)

	for _, path := range []string{"/api/admin/oidc/providers", "/api/admin/providers"} {
		req := httptest.NewRequest("GET", path, nil)
		ctx := rrpc.NewContext(req, httptest.NewRecorder(), nil)

		err := mw(ctx, noopNext)
		if err != rpc.ErrUnauthorized {
			t.Errorf("path %q: expected rpc.ErrUnauthorized, got %v", path, err)
		}
	}
}

func TestNewAuthorizationMiddleware_UnrelatedPathStillGated(t *testing.T) {
	mw := NewAuthorizationMiddleware(true, nil)

	req := httptest.NewRequest("GET", "/api/some/protected/path", nil)
	ctx := rrpc.NewContext(req, httptest.NewRecorder(), nil)

	if err := mw(ctx, noopNext); err != rpc.ErrUnauthorized {
		t.Errorf("expected rpc.ErrUnauthorized for an unrelated protected path, got %v", err)
	}
}
