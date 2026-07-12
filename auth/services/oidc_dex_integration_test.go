package services

import (
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/netgarden/maf"
	"github.com/netgarden/maf/auth/dto"
	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/locks"
	"github.com/netgarden/maf/security/encryption"
	"github.com/testcontainers/testcontainers-go"
	"golang.org/x/crypto/bcrypt"
)

// This file exercises OIDCAuthService's full protocol handshake
// (BeginAuth -> browser redirect -> IdP login -> callback -> CompleteAuth)
// against a real, independent OIDC server (Dex, github.com/dexidp/dex) run
// via testcontainers, rather than mocking any part of the OIDC protocol
// itself. Everything downstream of the browser round-trip — discovery,
// PKCE, state/nonce validation, ID token signature/issuer/audience
// verification, JIT user provisioning, session issuance — runs for real
// against real Postgres (see testDB in users_test.go) and a real Dex
// instance. Unit-level coverage of the individual pieces (state token
// round-trip, admin-claim mapping, redirect URI building) lives in
// oidc_test.go; this file is the one place that proves those pieces are
// wired together correctly end to end.

const (
	dexImage = "ghcr.io/dexidp/dex:v2.41.1"

	dexTestClientID     = "citadel-test"
	dexTestClientSecret = "test-client-secret"

	dexTestUserEmail    = "alice@example.com"
	dexTestUserPassword = "correct-horse-battery-staple"
	dexTestUsername     = "alice"
	// dexTestUserID becomes the OIDC "sub" claim Dex issues for this user —
	// asserted against directly below to confirm CompleteAuth's Subject
	// extraction is correct, not just "non-empty".
	dexTestUserID = "08a8684b-db88-4b73-90a9-3cd1661f5466"
)

// TestOIDCFullFlow_AgainstRealDexProvider drives the entire authorization
// code + PKCE flow as a browser would: fetch the authorize URL BeginAuth
// produced, follow Dex's redirect to its login form, submit credentials,
// follow Dex's redirect back to our (non-listening — see redirectURI
// below) callback URL, and feed the resulting code+state into CompleteAuth
// exactly as the rrpc impl layer would.
func TestOIDCFullFlow_AgainstRealDexProvider(t *testing.T) {
	db := testDB(t) // skips the whole test if Docker is unavailable

	pm := newPM()
	locksService := locks.NewService(db)
	usersService := NewUsersService(db, pm, locksService)
	sessionsService := NewSessionsService(db)
	resetTokensService := NewPasswordResetTokensService(db)
	identitiesService := NewUserIdentitiesService(db)

	cfg := maf.NewConfig(map[string]any{
		"auth.token.ttl":                    15 * time.Minute,
		"auth.session.ttl":                  7 * 24 * time.Hour,
		"auth.session.cookie.name":          "session",
		"auth.session.cookie.path":          "/",
		"auth.session.cookie.force_secure":  false,
		"auth.password.enabled":             true,
		"auth.oidc.autoLinkByVerifiedEmail": true,
		// The callback never actually needs to be reachable — CompleteAuth
		// is invoked directly below instead of via a real listening HTTP
		// server, so this only has to match what's registered as Dex's
		// staticClient redirectURI (see startDex).
		"auth.oidc.callbackBaseUrl": "http://127.0.0.1:9",
	})

	authService := NewAuthService(cfg, testSecret, usersService, sessionsService, resetTokensService, pm, identitiesService)
	providersService := NewOIDCProvidersService(db, encryption.NewManager("test-encryption-secret"))
	oidcAuthService := NewOIDCAuthService(cfg, testSecret, providersService, authService)

	const providerSlug = "dex-test"
	redirectURI := oidcAuthService.buildRedirectURI(providerSlug)

	issuerURL := startDex(t, redirectURI)

	provider, err := providersService.Create(&dto.OIDCProviderCreateDTO{
		Slug:         providerSlug,
		Name:         "Dex Test",
		IssuerURL:    issuerURL,
		ClientID:     dexTestClientID,
		ClientSecret: dexTestClientSecret,
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("providersService.Create: %v", err)
	}
	if provider == nil {
		t.Fatal("expected the test provider to be created")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	authURL, stateCookie, err := oidcAuthService.BeginAuth(ctx, providerSlug, "", false)
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}

	code, state := runDexBrowserLogin(t, authURL, redirectURI)

	accessToken, refreshCookie, returnTo, err := oidcAuthService.CompleteAuth(
		ctx, code, state, stateCookie.Value, "127.0.0.1", "integration-test-agent", false,
	)
	if err != nil {
		t.Fatalf("CompleteAuth: %v", err)
	}
	if accessToken == "" {
		t.Fatal("expected a non-empty access token")
	}
	if refreshCookie == nil {
		t.Fatal("expected a refresh cookie")
	}
	if returnTo != "" {
		t.Errorf("returnTo = %q, want empty (BeginAuth was called with an empty returnTo)", returnTo)
	}

	user, err := usersService.GetUserByEmail(dexTestUserEmail)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if user == nil {
		t.Fatal("expected CompleteAuth to have JIT-provisioned a user")
	}
	if !user.Active {
		t.Error("JIT-provisioned user should be Active")
	}

	// Dex's own "sub" claim is an opaque, connector-specific encoding (not
	// literally dexTestUserID — it base64-encodes a small
	// {userID, connectorID} structure internally), so this looks the
	// UserIdentity row up by (user, provider) rather than predicting
	// Dex's internal subject value.
	var linked entities.UserIdentity
	err = db.Where("user_id = ? AND provider_type = ? AND provider_id = ?", user.ID, "oidc", provider.ID).First(&linked).Error
	if err != nil {
		t.Fatalf("expected a UserIdentity row linking the real Dex identity to the JIT-provisioned user: %v", err)
	}
	if linked.Subject == "" {
		t.Error("linked UserIdentity has an empty Subject")
	}

	session, err := sessionsService.GetSession(refreshCookie.Value)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session == nil {
		t.Fatal("expected CompleteAuth's refresh cookie to reference a real, persisted session")
	}
	if session.UserID != user.ID {
		t.Errorf("session.UserID = %s, want %s", session.UserID, user.ID)
	}

	gotUser, err := authService.ValidateAccess(accessToken)
	if err != nil {
		t.Fatalf("the access token CompleteAuth minted does not validate: %v", err)
	}
	if gotUser == nil || gotUser.ID != user.ID {
		t.Errorf("access token resolves to user %v, want %s", gotUser, user.ID)
	}
}

// startDex starts a real Dex OIDC server in a container, pre-configured
// with one static test user and one static OAuth client whose redirect URI
// is redirectURI. It returns the issuer URL to register as the
// entities.OIDCProvider.IssuerURL.
//
// Dex's issuer must exactly match the address our test process reaches it
// through (standard OIDC discovery requirement) — since that address is
// only known once the container's host port is chosen, this reserves a
// free host port first and binds the container's port 5556 to that exact
// port, rather than letting Docker pick a random one.
func startDex(t *testing.T, redirectURI string) string {
	t.Helper()
	ctx := context.Background()

	hostPort := freeTCPPort(t)
	issuerURL := fmt.Sprintf("http://127.0.0.1:%d/dex", hostPort)

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(dexTestUserPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
	}

	configYAML := fmt.Sprintf(`
issuer: %s

storage:
  type: sqlite3
  config:
    file: ":memory:"

web:
  http: 0.0.0.0:5556

oauth2:
  responseTypes: ["code"]
  skipApprovalScreen: true

enablePasswordDB: true

staticPasswords:
- email: %q
  hash: %q
  username: %q
  userID: %q

staticClients:
- id: %q
  secret: %q
  name: "Citadel Integration Test Client"
  redirectURIs:
  - %q
`, issuerURL, dexTestUserEmail, passwordHash, dexTestUsername, dexTestUserID, dexTestClientID, dexTestClientSecret, redirectURI)

	configFile := filepath.Join(t.TempDir(), "dex-config.yaml")
	if err := os.WriteFile(configFile, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write dex config: %v", err)
	}

	req := testcontainers.ContainerRequest{
		Image:        dexImage,
		ExposedPorts: []string{"5556/tcp"},
		Cmd:          []string{"dex", "serve", "/etc/dex/config.yaml"},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      configFile,
				ContainerFilePath: "/etc/dex/config.yaml",
				FileMode:          0o644,
			},
		},
		HostConfigModifier: func(hostConfig *container.HostConfig) {
			hostConfig.PortBindings = network.PortMap{
				network.MustParsePort("5556/tcp"): []network.PortBinding{
					{
						HostIP:   netip.MustParseAddr("127.0.0.1"),
						HostPort: strconv.Itoa(hostPort),
					},
				},
			}
		},
	}

	dexContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start dex container: %v", err)
	}
	t.Cleanup(func() {
		_ = dexContainer.Terminate(context.Background())
	})

	if err := waitForDexReady(issuerURL); err != nil {
		t.Fatalf("dex did not become ready: %v", err)
	}

	return issuerURL
}

// waitForDexReady polls the discovery document rather than relying on a
// log line — the exact readiness signal already used for Postgres in
// users_test.go's waitForPostgresReady.
func waitForDexReady(issuerURL string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(issuerURL + "/.well-known/openid-configuration")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("unexpected status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return lastErr
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// runDexBrowserLogin drives the browser side of the flow against a real
// Dex server: GET the authorize URL, follow Dex's redirect to its local
// login form, parse the form's action URL out of the HTML (rather than
// assuming a specific path shape, which is a Dex implementation detail),
// POST credentials, and follow any further redirects until one lands back
// on redirectURI carrying ?code=&state=.
func runDexBrowserLogin(t *testing.T, authURL, redirectURI string) (code, state string) {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(authURL)
	if err != nil {
		t.Fatalf("GET authorize URL: %v", err)
	}
	loginPageURL := requireRedirect(t, resp, "authorize")

	// Dex hops through more than one redirect before actually rendering the
	// login form (e.g. /auth/local?req=... -> /auth/local/login?...) — the
	// exact number of hops is a Dex implementation detail, so follow
	// however many there are rather than assuming exactly one.
	formPageURL, body := followGETUntilOK(t, client, loginPageURL.String(), 5)
	actionURL := extractFormAction(t, string(body), formPageURL)

	resp, err = client.PostForm(actionURL.String(), url.Values{
		"login":    {dexTestUserEmail},
		"password": {dexTestUserPassword},
	})
	if err != nil {
		t.Fatalf("POST dex login form: %v", err)
	}

	// Dex may hop through /approval before landing on redirectURI (it
	// won't, given skipApprovalScreen: true, but following defensively
	// keeps this robust to that config detail changing).
	for i := 0; i < 5; i++ {
		next := requireRedirect(t, resp, "dex login/approval")
		if strings.HasPrefix(next.String(), redirectURI) {
			query := next.Query()
			code = query.Get("code")
			state = query.Get("state")
			if code == "" || state == "" {
				t.Fatalf("callback redirect %s missing code/state", next.String())
			}
			return code, state
		}
		resp, err = client.Get(next.String())
		if err != nil {
			t.Fatalf("GET %s: %v", next.String(), err)
		}
	}

	t.Fatalf("did not reach %s within 5 redirects after submitting credentials", redirectURI)
	return "", ""
}

// followGETUntilOK follows 3xx redirects (GET only) starting at startURL
// until it gets a non-redirect response, returning that response's final
// URL and body. Used for the hop(s) between Dex's authorize redirect and
// its actual login form.
func followGETUntilOK(t *testing.T, client *http.Client, startURL string, maxHops int) (finalURL *url.URL, body []byte) {
	t.Helper()
	current := startURL
	for i := 0; i < maxHops; i++ {
		resp, err := client.Get(current)
		if err != nil {
			t.Fatalf("GET %s: %v", current, err)
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := requireRedirect(t, resp, "follow GET redirect")
			current = loc.String()
			continue
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("read response body from %s: %v", current, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: status %d, body: %s", current, resp.StatusCode, b)
		}
		u, err := url.Parse(current)
		if err != nil {
			t.Fatalf("parse %q: %v", current, err)
		}
		return u, b
	}
	t.Fatalf("too many redirects starting from %s", startURL)
	return nil, nil
}

func requireRedirect(t *testing.T, resp *http.Response, step string) *url.URL {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s: expected a redirect, got status %d: %s", step, resp.StatusCode, body)
	}
	loc, err := resp.Location()
	if err != nil {
		t.Fatalf("%s: response had no Location header: %v", step, err)
	}
	return loc
}

var formActionRe = regexp.MustCompile(`(?i)<form[^>]*\baction="([^"]*)"`)

func extractFormAction(t *testing.T, page string, formPageURL *url.URL) *url.URL {
	t.Helper()
	match := formActionRe.FindStringSubmatch(page)
	if match == nil {
		t.Fatalf("could not find a <form action=...> in dex's login page:\n%s", page)
	}
	// The attribute value is HTML-entity-encoded in the markup (Dex writes
	// "?back=&amp;state=..."); un-escaping first is required or url.Parse
	// mangles "&amp;state=" into a single bogus "amp;state" query key,
	// which is silently dropped rather than the real "state" — Dex then
	// can't find the login session server-side and returns 400 "User
	// session error" instead of a redirect.
	action, err := url.Parse(html.UnescapeString(match[1]))
	if err != nil {
		t.Fatalf("parse form action %q: %v", match[1], err)
	}
	return formPageURL.ResolveReference(action)
}
