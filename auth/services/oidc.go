package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/netgarden/maf"
	uuid "github.com/satori/go.uuid"
	"golang.org/x/oauth2"
)

// oidcStateCookieName is the transient, signed-JWT cookie BeginAuth issues
// and CompleteAuth reads back — see oidcStateClaims. Path is scoped to
// every provider's login/callback endpoints (not per-slug) since it's a
// single-purpose value with no reason to be readable elsewhere.
const oidcStateCookieName = "oidc_state"
const oidcStatePath = "/api/auth/oidc"
const oidcStateTTL = 10 * time.Minute

var (
	// ErrOIDCProviderNotFound covers both "no such slug" and "slug exists
	// but is administratively disabled" — deliberately not distinguished,
	// same convention entities.User.Active/GetUserByUsername use elsewhere
	// in this package.
	ErrOIDCProviderNotFound = errors.New("oidc provider not found or disabled")
	// ErrOIDCStateInvalid covers a missing/expired/tampered state cookie or
	// a state cookie whose "st" claim doesn't match the callback's state
	// query param — including the benign multi-tab case where a second
	// BeginAuth in another tab overwrote the cookie. The caller should
	// surface a generic "please try logging in again" rather than
	// distinguishing the cause.
	ErrOIDCStateInvalid = errors.New("invalid or expired oidc login state")
)

// oidcStateClaims is packed into a short-lived, HMAC-signed (not
// encrypted — see identity.go's design notes) JWT set as an HttpOnly,
// SameSite=Lax cookie during BeginAuth and read back during CompleteAuth.
// A signed cookie needs no server-side storage/cleanup job and works
// identically across replicas; the only requirement is integrity (the
// PKCE verifier is client-visible by design), not confidentiality.
type oidcStateClaims struct {
	State       string `json:"st"`
	Nonce       string `json:"nc"`
	Verifier    string `json:"cv"`
	ProviderID  string `json:"pid"`
	RedirectURI string `json:"ru"`
	ReturnTo    string `json:"rt"`
	jwt.RegisteredClaims
}

func createOIDCStateToken(secret string, claims oidcStateClaims, ttl time.Duration) (string, error) {
	now := time.Now()
	claims.RegisteredClaims = jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		IssuedAt:  jwt.NewNumericDate(now),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func parseOIDCStateToken(secret, token string) (*oidcStateClaims, error) {
	claims := &oidcStateClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, hmacKeyFunc(secret))
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("invalid oidc state token")
	}
	return claims, nil
}

func NewOIDCAuthService(config *maf.Config, secret string, providers *OIDCProvidersService, authService *AuthService) *OIDCAuthService {
	return &OIDCAuthService{config: config, secret: secret, providers: providers, authService: authService}
}

// OIDCAuthService drives the OIDC-specific redirect handshake
// (BeginAuth/CompleteAuth) and, once it has a verified identity, hands off
// to AuthService.CompleteExternalLogin — the protocol-agnostic seam shared
// with any future non-OIDC external-login protocol. BeginAuth/CompleteAuth
// themselves are NOT part of that shared contract: they're this specific
// redirect-based flow's shape (SAML would likely reuse it; a synchronous
// protocol like LDAP wouldn't need it at all — see identity.go).
type OIDCAuthService struct {
	config      *maf.Config
	secret      string
	providers   *OIDCProvidersService
	authService *AuthService

	discoveryMu sync.Mutex
	discovery   map[uuid.UUID]cachedOIDCDiscovery
}

type cachedOIDCDiscovery struct {
	updatedAt time.Time
	provider  *oidc.Provider
}

// BeginAuth resolves providerSlug, generates state/nonce/PKCE, and returns
// the URL to redirect the browser to plus the state cookie to set
// alongside that redirect.
func (s *OIDCAuthService) BeginAuth(ctx context.Context, providerSlug, returnTo string, secure bool) (string, *http.Cookie, error) {
	provider, err := s.providers.GetBySlug(providerSlug)
	if err != nil {
		return "", nil, err
	}
	if provider == nil || !provider.Enabled {
		return "", nil, ErrOIDCProviderNotFound
	}

	redirectURI := s.buildRedirectURI(providerSlug)
	oauth2Config, err := s.oauth2Config(ctx, provider, redirectURI)
	if err != nil {
		return "", nil, err
	}

	state, err := randomURLSafeString(24)
	if err != nil {
		return "", nil, err
	}
	nonce, err := randomURLSafeString(24)
	if err != nil {
		return "", nil, err
	}
	verifier := oauth2.GenerateVerifier()

	stateToken, err := createOIDCStateToken(s.stateSecret(), oidcStateClaims{
		State:       state,
		Nonce:       nonce,
		Verifier:    verifier,
		ProviderID:  provider.ID.String(),
		RedirectURI: redirectURI,
		ReturnTo:    returnTo,
	}, oidcStateTTL)
	if err != nil {
		return "", nil, err
	}

	authURL := oauth2Config.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))

	cookie := &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    stateToken,
		MaxAge:   int(oidcStateTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		Path:     oidcStatePath,
		// Lax, not Strict: the browser arrives back at the callback via a
		// cross-site redirect from the IdP — Strict would drop the cookie.
		SameSite: http.SameSiteLaxMode,
	}
	return authURL, cookie, nil
}

// CompleteAuth validates the callback (state, nonce, PKCE, ID token
// signature/issuer/audience), then hands the resulting identity to
// AuthService.CompleteExternalLogin. Returns the same
// (accessToken, refreshCookie) shape CompleteExternalLogin does, plus the
// returnTo path BeginAuth was called with (or "" for the SPA default).
func (s *OIDCAuthService) CompleteAuth(ctx context.Context, code, stateParam, stateCookieValue, clientIP, userAgent string, secure bool) (accessToken string, refreshCookie *http.Cookie, returnTo string, err error) {
	claims, err := parseOIDCStateToken(s.stateSecret(), stateCookieValue)
	if err != nil || claims.State != stateParam {
		return "", nil, "", ErrOIDCStateInvalid
	}

	provider, err := s.providers.Get(claims.ProviderID)
	if err != nil {
		return "", nil, "", err
	}
	// Rechecked here, not just in BeginAuth: an admin can disable a
	// provider mid-flow, a real race on an admin-editable-at-runtime
	// registry, not a theoretical one.
	if provider == nil || !provider.Enabled {
		return "", nil, "", ErrOIDCProviderNotFound
	}

	oauth2Config, err := s.oauth2Config(ctx, provider, claims.RedirectURI)
	if err != nil {
		return "", nil, "", err
	}

	token, err := oauth2Config.Exchange(ctx, code, oauth2.VerifierOption(claims.Verifier))
	if err != nil {
		return "", nil, "", fmt.Errorf("oidc code exchange: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return "", nil, "", errors.New("oidc token response missing id_token")
	}

	discovered, err := s.discoverProvider(ctx, provider)
	if err != nil {
		return "", nil, "", err
	}
	idToken, err := discovered.Verifier(&oidc.Config{ClientID: provider.OIDC.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {
		return "", nil, "", fmt.Errorf("oidc id_token verification: %w", err)
	}
	if idToken.Nonce != claims.Nonce {
		return "", nil, "", errors.New("oidc id_token nonce mismatch")
	}

	var rawClaims map[string]any
	if err := idToken.Claims(&rawClaims); err != nil {
		return "", nil, "", err
	}

	identity := ExternalIdentity{
		ProviderType:  "oidc",
		ProviderID:    provider.ID,
		Subject:       idToken.Subject,
		Email:         stringClaim(rawClaims, "email"),
		EmailVerified: boolClaim(rawClaims, "email_verified"),
		FirstName:     stringClaim(rawClaims, "given_name"),
		LastName:      stringClaim(rawClaims, "family_name"),
		Admin:         evaluateAdminClaim(&provider.Provider, rawClaims),
	}

	accessToken, refreshCookie, err = s.authService.CompleteExternalLogin(identity, clientIP, userAgent, secure)
	return accessToken, refreshCookie, claims.ReturnTo, err
}

func (s *OIDCAuthService) oauth2Config(ctx context.Context, provider *OIDCProviderDetail, redirectURI string) (*oauth2.Config, error) {
	discovered, err := s.discoverProvider(ctx, provider)
	if err != nil {
		return nil, err
	}
	clientSecret, err := s.providers.DecryptClientSecret(&provider.OIDC)
	if err != nil {
		return nil, err
	}
	return &oauth2.Config{
		ClientID:     provider.OIDC.ClientID,
		ClientSecret: clientSecret,
		Endpoint:     discovered.Endpoint(),
		RedirectURL:  redirectURI,
		Scopes:       strings.Fields(provider.OIDC.Scopes),
	}, nil
}

// discoverProvider caches go-oidc's issuer discovery (endpoints + JWKS) per
// provider, invalidated whenever the OIDCProvider row's own UpdatedAt moves
// forward (not the generic Provider row's — editing IssuerURL only touches
// the OIDC-specific row) — so an admin editing IssuerURL doesn't leave a
// stale discovery document cached indefinitely.
func (s *OIDCAuthService) discoverProvider(ctx context.Context, provider *OIDCProviderDetail) (*oidc.Provider, error) {
	s.discoveryMu.Lock()
	cached, ok := s.discovery[provider.ID]
	s.discoveryMu.Unlock()
	if ok && (provider.OIDC.UpdatedAt == nil || !provider.OIDC.UpdatedAt.After(cached.updatedAt)) {
		return cached.provider, nil
	}

	discovered, err := oidc.NewProvider(ctx, provider.OIDC.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery for %q: %w", provider.Slug, err)
	}

	s.discoveryMu.Lock()
	if s.discovery == nil {
		s.discovery = make(map[uuid.UUID]cachedOIDCDiscovery)
	}
	s.discovery[provider.ID] = cachedOIDCDiscovery{updatedAt: time.Now(), provider: discovered}
	s.discoveryMu.Unlock()

	return discovered, nil
}

// buildRedirectURI mirrors AuthService.RequestPasswordReset's
// auth.passwordReset.baseUrl pattern: the external origin a provider
// redirects back to lives in config, not in each request, since it must
// exactly match what's registered with the IdP.
func (s *OIDCAuthService) buildRedirectURI(providerSlug string) string {
	base := strings.TrimRight(s.config.GetString("auth.oidc.callbackBaseUrl"), "/")
	return base + "/api/auth/oidc/" + providerSlug + "/callback"
}

func (s *OIDCAuthService) stateSecret() string { return s.secret + ":oidc-state" }

// StateCookieName exposes oidcStateCookieName to callers (the rrpc impl
// layer) that need to clear the cookie after CompleteAuth, on both the
// success and failure paths.
func (s *OIDCAuthService) StateCookieName() string { return oidcStateCookieName }

// StateCookiePath exposes oidcStatePath so callers can build a matching
// clear-cookie response — see StateCookieName.
func (s *OIDCAuthService) StateCookiePath() string { return oidcStatePath }

func stringClaim(claims map[string]any, key string) string {
	v, _ := claims[key].(string)
	return v
}

func boolClaim(claims map[string]any, key string) bool {
	v, _ := claims[key].(bool)
	return v
}

func randomURLSafeString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
