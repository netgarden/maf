package impl

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authdto "github.com/netgarden/maf/auth/dto"
	authentities "github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/rrpc-auth/authctx"
	"github.com/netgarden/maf/rrpc-auth/rpc"
	"github.com/netgarden/rrpc"
)

// mockAuth implements authService for testing.
type mockAuth struct {
	login      func(*authdto.CredentialsLoginRequest, string, string, bool) (string, *http.Cookie, error)
	logout     func(string, bool) *http.Cookie
	refresh    func(string) (string, error)
	getUser    func(string) (*authentities.User, error)
	cookieName string
}

func (m *mockAuth) Login(req *authdto.CredentialsLoginRequest, clientIP, userAgent string, secure bool) (string, *http.Cookie, error) {
	if m.login != nil {
		return m.login(req, clientIP, userAgent, secure)
	}
	return "access.token", m.cookie("session-id"), nil
}

func (m *mockAuth) Logout(sessionID string, secure bool) *http.Cookie {
	if m.logout != nil {
		return m.logout(sessionID, secure)
	}
	return &http.Cookie{Name: m.SessionCookieName(), Value: "", MaxAge: -1}
}

func (m *mockAuth) Refresh(sessionID string) (string, error) {
	if m.refresh != nil {
		return m.refresh(sessionID)
	}
	return "new.access.token", nil
}

func (m *mockAuth) GetUser(id string) (*authentities.User, error) {
	if m.getUser != nil {
		return m.getUser(id)
	}
	return &authentities.User{Username: "default"}, nil
}

func (m *mockAuth) SessionCookieName() string {
	if m.cookieName != "" {
		return m.cookieName
	}
	return "session"
}

func (m *mockAuth) cookie(value string) *http.Cookie {
	return &http.Cookie{Name: m.SessionCookieName(), Value: value, HttpOnly: true, Path: "/"}
}

// meContext creates an rrpc.Context with userID pre-set, simulating a request
// that has already passed through the auth middleware.
func meContext(userID string) (*rrpc.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest("GET", "/api/auth/me", nil)
	w := httptest.NewRecorder()
	ctx := rrpc.NewContext(req, w, rrpc.NewParams())
	authctx.SetUserID(ctx, userID)
	return ctx, w
}

// --- test infrastructure ---

func newTestServer(t *testing.T, mock *mockAuth) http.Handler {
	t.Helper()
	mod := rpc.NewModule()
	mod.SetAuthService(&AuthServiceImpl{auth: mock})
	srv := rrpc.NewServer()
	if err := srv.RegisterModule(mod); err != nil {
		t.Fatal(err)
	}
	return srv
}

func do(t *testing.T, h http.Handler, method, path, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func doWithHeaders(t *testing.T, h http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func doWithCookie(t *testing.T, h http.Handler, method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func decodeJSON[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("failed to decode JSON: %v — body: %s", err, w.Body)
	}
	return v
}

func findCookie(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// --- POST /auth/login ---

func TestLogin_SetsRefreshCookieAndReturnsAccessToken(t *testing.T) {
	mock := &mockAuth{
		login: func(req *authdto.CredentialsLoginRequest, clientIP, userAgent string, secure bool) (string, *http.Cookie, error) {
			return "access.jwt", &http.Cookie{
				Name: "session", Value: "sess-id", HttpOnly: true, Path: "/api/auth/refresh",
			}, nil
		},
	}
	h := newTestServer(t, mock)

	w := do(t, h, "POST", "/api/auth/login", "application/json", `{"username":"alice","password":"secret"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}
	resp := decodeJSON[rpc.LoginResponse](t, w)
	if resp.AccessToken != "access.jwt" {
		t.Errorf("accessToken: want 'access.jwt', got %q", resp.AccessToken)
	}
	c := findCookie(w, "session")
	if c == nil {
		t.Fatal("expected session cookie to be set")
	}
	if !c.HttpOnly {
		t.Error("session cookie must be HttpOnly")
	}
}

func TestLogin_ForwardsCredentialsToAuthService(t *testing.T) {
	var gotReq *authdto.CredentialsLoginRequest
	mock := &mockAuth{
		login: func(req *authdto.CredentialsLoginRequest, _, _ string, _ bool) (string, *http.Cookie, error) {
			gotReq = req
			return "tok", &http.Cookie{Name: "session"}, nil
		},
	}
	h := newTestServer(t, mock)

	do(t, h, "POST", "/api/auth/login", "application/json", `{"username":"bob","password":"hunter2"}`)

	if gotReq == nil {
		t.Fatal("Login was not called")
	}
	if gotReq.Username != "bob" {
		t.Errorf("username: want 'bob', got %q", gotReq.Username)
	}
	if gotReq.Password != "hunter2" {
		t.Errorf("password: want 'hunter2', got %q", gotReq.Password)
	}
}

func TestLogin_BadCredentials_Returns401(t *testing.T) {
	mock := &mockAuth{
		login: func(_ *authdto.CredentialsLoginRequest, _, _ string, _ bool) (string, *http.Cookie, error) {
			return "", nil, nil
		},
	}
	h := newTestServer(t, mock)

	w := do(t, h, "POST", "/api/auth/login", "application/json", `{"username":"alice","password":"wrong"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestLogin_ServiceError_Returns500(t *testing.T) {
	mock := &mockAuth{
		login: func(_ *authdto.CredentialsLoginRequest, _, _ string, _ bool) (string, *http.Cookie, error) {
			return "", nil, errors.New("db down")
		},
	}
	h := newTestServer(t, mock)

	w := do(t, h, "POST", "/api/auth/login", "application/json", `{"username":"alice","password":"secret"}`)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestLogin_SecureFlagFalseOverHTTP(t *testing.T) {
	var gotSecure bool
	mock := &mockAuth{
		login: func(_ *authdto.CredentialsLoginRequest, _, _ string, secure bool) (string, *http.Cookie, error) {
			gotSecure = secure
			return "tok", &http.Cookie{Name: "session"}, nil
		},
	}
	h := newTestServer(t, mock)

	// Plain HTTP request (no TLS, no X-Forwarded-Proto) → secure = false
	do(t, h, "POST", "/api/auth/login", "application/json", `{"username":"a","password":"b"}`)

	if gotSecure {
		t.Error("expected secure=false for plain HTTP request")
	}
}

// --- POST /auth/logout ---

func TestLogout_ReadsSessionCookieAndCallsService(t *testing.T) {
	var gotSessionID string
	mock := &mockAuth{
		logout: func(sessionID string, _ bool) *http.Cookie {
			gotSessionID = sessionID
			return &http.Cookie{Name: "session", Value: "", MaxAge: -1, Path: "/"}
		},
	}
	h := newTestServer(t, mock)

	w := doWithCookie(t, h, "POST", "/api/auth/logout", &http.Cookie{Name: "session", Value: "my-session-id"})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}
	if gotSessionID != "my-session-id" {
		t.Errorf("sessionID: want 'my-session-id', got %q", gotSessionID)
	}
	c := findCookie(w, "session")
	if c == nil {
		t.Fatal("expected session cookie in response")
	}
	if c.MaxAge != -1 {
		t.Errorf("MaxAge: want -1 (delete), got %d", c.MaxAge)
	}
}

func TestLogout_WithoutCookie_PassesEmptySessionID(t *testing.T) {
	var gotSessionID string
	mock := &mockAuth{
		logout: func(sessionID string, _ bool) *http.Cookie {
			gotSessionID = sessionID
			return &http.Cookie{Name: "session", Value: "", MaxAge: -1}
		},
	}
	h := newTestServer(t, mock)

	w := do(t, h, "POST", "/api/auth/logout", "", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}
	if gotSessionID != "" {
		t.Errorf("expected empty sessionID without cookie, got %q", gotSessionID)
	}
}

// --- POST /auth/refresh ---

func TestRefresh_ReadsSessionCookieAndReturnsNewToken(t *testing.T) {
	var gotSessionID string
	mock := &mockAuth{
		refresh: func(sessionID string) (string, error) {
			gotSessionID = sessionID
			return "new.access.token", nil
		},
	}
	h := newTestServer(t, mock)

	w := doWithCookie(t, h, "POST", "/api/auth/refresh", &http.Cookie{Name: "session", Value: "sess-abc"})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}
	if gotSessionID != "sess-abc" {
		t.Errorf("sessionID: want 'sess-abc', got %q", gotSessionID)
	}
	resp := decodeJSON[rpc.RefreshResponse](t, w)
	if resp.AccessToken != "new.access.token" {
		t.Errorf("accessToken: want 'new.access.token', got %q", resp.AccessToken)
	}
}

func TestRefresh_NoCookie_Returns401(t *testing.T) {
	h := newTestServer(t, &mockAuth{})
	w := do(t, h, "POST", "/api/auth/refresh", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRefresh_SessionNotFound_Returns401(t *testing.T) {
	mock := &mockAuth{
		refresh: func(string) (string, error) { return "", nil },
	}
	h := newTestServer(t, mock)

	w := doWithCookie(t, h, "POST", "/api/auth/refresh", &http.Cookie{Name: "session", Value: "expired"})

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRefresh_ServiceError_Returns500(t *testing.T) {
	mock := &mockAuth{
		refresh: func(string) (string, error) { return "", errors.New("db down") },
	}
	h := newTestServer(t, mock)

	w := doWithCookie(t, h, "POST", "/api/auth/refresh", &http.Cookie{Name: "session", Value: "sess-id"})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// --- GET /auth/me ---
// Me is tested by calling AuthServiceImpl.Me directly with a pre-built rrpc.Context,
// simulating a request that has already passed through the auth middleware.

func TestMe_ReturnsUserFromContext(t *testing.T) {
	var gotUserID string
	svc := &AuthServiceImpl{auth: &mockAuth{
		getUser: func(id string) (*authentities.User, error) {
			gotUserID = id
			return &authentities.User{Username: "alice", Email: "alice@example.com", Admin: false}, nil
		},
	}}

	ctx, _ := meContext("user-123")
	resp, err := svc.Me(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotUserID != "user-123" {
		t.Errorf("user ID: want 'user-123', got %q", gotUserID)
	}
	if resp.Username != "alice" {
		t.Errorf("username: want 'alice', got %q", resp.Username)
	}
	if resp.Email != "alice@example.com" {
		t.Errorf("email: want 'alice@example.com', got %q", resp.Email)
	}
}

func TestMe_NoUserInContext_Returns401(t *testing.T) {
	svc := &AuthServiceImpl{auth: &mockAuth{}}
	req := httptest.NewRequest("GET", "/api/auth/me", nil)
	w := httptest.NewRecorder()
	ctx := rrpc.NewContext(req, w, rrpc.NewParams())

	_, err := svc.Me(ctx)

	if err == nil {
		t.Fatal("expected error when user ID is not in context")
	}
}

func TestMe_UserNotFound_Returns401(t *testing.T) {
	svc := &AuthServiceImpl{auth: &mockAuth{
		getUser: func(string) (*authentities.User, error) { return nil, nil },
	}}

	ctx, _ := meContext("ghost-user")
	_, err := svc.Me(ctx)

	if err == nil {
		t.Fatal("expected error when user is not found")
	}
}

func TestMe_DBError_Returns500(t *testing.T) {
	svc := &AuthServiceImpl{auth: &mockAuth{
		getUser: func(string) (*authentities.User, error) { return nil, errors.New("db down") },
	}}

	ctx, _ := meContext("user-123")
	_, err := svc.Me(ctx)

	if err == nil {
		t.Fatal("expected error on DB failure")
	}
}
