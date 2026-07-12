package services

import (
	"testing"
	"time"

	"github.com/netgarden/maf"
	"github.com/netgarden/maf/auth/entities"
)

func TestOIDCStateToken_RoundTrip(t *testing.T) {
	claims := oidcStateClaims{
		State:       "state-1",
		Nonce:       "nonce-1",
		Verifier:    "verifier-1",
		ProviderID:  "provider-1",
		RedirectURI: "https://app.example.com/api/auth/oidc/okta/callback",
		ReturnTo:    "/dashboard",
	}
	token, err := createOIDCStateToken(testSecret, claims, oidcStateTTL)
	if err != nil {
		t.Fatalf("createOIDCStateToken: %v", err)
	}

	got, err := parseOIDCStateToken(testSecret, token)
	if err != nil {
		t.Fatalf("parseOIDCStateToken: %v", err)
	}
	if got.State != claims.State || got.Nonce != claims.Nonce || got.Verifier != claims.Verifier ||
		got.ProviderID != claims.ProviderID || got.RedirectURI != claims.RedirectURI || got.ReturnTo != claims.ReturnTo {
		t.Errorf("round-tripped claims = %+v, want %+v", got, claims)
	}
}

func TestOIDCStateToken_ExpiredRejected(t *testing.T) {
	token, err := createOIDCStateToken(testSecret, oidcStateClaims{State: "s"}, -time.Second)
	if err != nil {
		t.Fatalf("createOIDCStateToken: %v", err)
	}
	if _, err := parseOIDCStateToken(testSecret, token); err == nil {
		t.Fatal("expected an error for an expired state token")
	}
}

func TestOIDCStateToken_WrongSecretRejected(t *testing.T) {
	token, err := createOIDCStateToken(testSecret, oidcStateClaims{State: "s"}, oidcStateTTL)
	if err != nil {
		t.Fatalf("createOIDCStateToken: %v", err)
	}
	if _, err := parseOIDCStateToken("wrong-secret", token); err == nil {
		t.Fatal("expected an error when the token was signed with a different secret")
	}
}

func TestBuildRedirectURI(t *testing.T) {
	cfg := maf.NewConfig(map[string]any{
		"auth.oidc.callbackBaseUrl": "https://app.example.com/",
	})
	svc := &OIDCAuthService{config: cfg}

	got := svc.buildRedirectURI("okta")
	want := "https://app.example.com/api/auth/oidc/okta/callback"
	if got != want {
		t.Errorf("buildRedirectURI = %q, want %q", got, want)
	}
}

func TestEvaluateAdminClaim_NoMappingConfigured_ReturnsNil(t *testing.T) {
	provider := &entities.Provider{AdminClaimPath: ""}
	if got := evaluateAdminClaim(provider, map[string]any{"groups": []any{"admin"}}); got != nil {
		t.Errorf("expected nil, got %v", *got)
	}
}

func TestEvaluateAdminClaim_ArrayClaim_Matches(t *testing.T) {
	provider := &entities.Provider{AdminClaimPath: "groups", AdminClaimValues: []string{"admins"}}
	got := evaluateAdminClaim(provider, map[string]any{"groups": []any{"users", "admins"}})
	if got == nil || !*got {
		t.Errorf("expected true, got %v", got)
	}
}

func TestEvaluateAdminClaim_ArrayClaim_NoMatch_ReturnsFalse(t *testing.T) {
	provider := &entities.Provider{AdminClaimPath: "groups", AdminClaimValues: []string{"admins"}}
	got := evaluateAdminClaim(provider, map[string]any{"groups": []any{"users"}})
	if got == nil || *got {
		t.Errorf("expected false (not nil), got %v", got)
	}
}

func TestEvaluateAdminClaim_StringClaim_Matches(t *testing.T) {
	provider := &entities.Provider{AdminClaimPath: "role", AdminClaimValues: []string{"admin"}}
	got := evaluateAdminClaim(provider, map[string]any{"role": "admin"})
	if got == nil || !*got {
		t.Errorf("expected true, got %v", got)
	}
}

func TestEvaluateAdminClaim_MissingClaim_ReturnsFalse(t *testing.T) {
	// A mapping IS configured but the claim never came back from the IdP —
	// must resolve to false (revoke), not nil (leave alone), so losing a
	// group membership actually takes effect on next login.
	provider := &entities.Provider{AdminClaimPath: "groups", AdminClaimValues: []string{"admins"}}
	got := evaluateAdminClaim(provider, map[string]any{})
	if got == nil || *got {
		t.Errorf("expected false (not nil) when the mapped claim is absent, got %v", got)
	}
}

func TestStringClaimAndBoolClaim(t *testing.T) {
	claims := map[string]any{"email": "a@example.com", "email_verified": true}
	if got := stringClaim(claims, "email"); got != "a@example.com" {
		t.Errorf("stringClaim = %q", got)
	}
	if got := stringClaim(claims, "missing"); got != "" {
		t.Errorf("stringClaim for missing key = %q, want empty", got)
	}
	if !boolClaim(claims, "email_verified") {
		t.Error("boolClaim = false, want true")
	}
	if boolClaim(claims, "missing") {
		t.Error("boolClaim for missing key = true, want false")
	}
}
