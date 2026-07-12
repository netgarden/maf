package impl

import (
	"errors"
	"net/http"
	"strings"

	mafauth "github.com/netgarden/maf/auth/services"
	"github.com/netgarden/maf/rrpc-auth/rpc"
	"github.com/netgarden/rrpc"
)

func NewOIDCAuthService(oidcAuth *mafauth.OIDCAuthService) rpc.OIDCAuthService {
	return &OIDCAuthServiceImpl{oidcAuth: oidcAuth}
}

type OIDCAuthServiceImpl struct {
	oidcAuth *mafauth.OIDCAuthService
}

// Login redirects the browser to the provider's authorize endpoint, having
// first set the transient state cookie BeginAuth produced — this is a
// plain browser navigation (not an XHR the frontend awaits), same as
// StaticServiceImpl.Get writing straight to ctx.Response() elsewhere in
// this codebase.
func (s *OIDCAuthServiceImpl) Login(ctx *rrpc.Context) error {
	slug := ctx.Params().GetString("slug")
	returnTo := sanitizeReturnTo(ctx.Request().URL.Query().Get("returnTo"))

	authURL, stateCookie, err := s.oidcAuth.BeginAuth(ctx.Request().Context(), slug, returnTo, ctx.IsHTTPS())
	if err != nil {
		if errors.Is(err, mafauth.ErrOIDCProviderNotFound) {
			return rpc.ErrOIDCProviderNotFound
		}
		return rrpc.ErrRrpcInternalError.WithCause(err)
	}

	http.SetCookie(ctx.Response(), stateCookie)
	http.Redirect(ctx.Response(), ctx.Request(), authURL, http.StatusFound)
	return nil
}

// Callback completes the flow and redirects back into the SPA — the
// existing frontend auth store's on-mount restore()/refresh() picks up the
// refresh cookie set here with no further handshake needed.
func (s *OIDCAuthServiceImpl) Callback(ctx *rrpc.Context) error {
	r := ctx.Request()
	query := r.URL.Query()
	code := query.Get("code")
	state := query.Get("state")

	var stateCookieValue string
	if cookie, err := r.Cookie(s.oidcAuth.StateCookieName()); err == nil {
		stateCookieValue = cookie.Value
	}
	// Clear the state cookie unconditionally (success or failure) — it's
	// single-use and must never be replayed.
	http.SetCookie(ctx.Response(), s.clearStateCookie(ctx.IsHTTPS()))

	accessToken, refreshCookie, returnTo, err := s.oidcAuth.CompleteAuth(
		r.Context(), code, state, stateCookieValue, r.RemoteAddr, r.Header.Get("User-Agent"), ctx.IsHTTPS(),
	)
	if err != nil {
		if errors.Is(err, mafauth.ErrOIDCProviderNotFound) || errors.Is(err, mafauth.ErrOIDCStateInvalid) {
			return rpc.ErrOIDCLoginFailed
		}
		return rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if accessToken == "" {
		// CompleteExternalLogin's fail-closed convention (deactivated user
		// etc.) — same "don't distinguish the cause" rule password Login
		// follows.
		return rpc.ErrOIDCLoginFailed
	}

	http.SetCookie(ctx.Response(), refreshCookie)

	redirectTarget := returnTo
	if redirectTarget == "" {
		redirectTarget = "/"
	}
	http.Redirect(ctx.Response(), ctx.Request(), redirectTarget, http.StatusFound)
	return nil
}

func (s *OIDCAuthServiceImpl) clearStateCookie(secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     s.oidcAuth.StateCookieName(),
		Value:    "",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		Path:     s.oidcAuth.StateCookiePath(),
		SameSite: http.SameSiteLaxMode,
	}
}

// sanitizeReturnTo restricts an admin/user-suppliable "come back here after
// login" query param to a same-origin relative path — otherwise a crafted
// login link (returnTo=https://evil.example.com or returnTo=//evil.example.com)
// could turn a legitimate login flow into an open redirect.
func sanitizeReturnTo(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.Contains(raw, "://") {
		return ""
	}
	return raw
}
