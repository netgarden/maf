package services

import (
	"errors"
	"testing"
	"time"

	"github.com/netgarden/maf"
	"github.com/netgarden/maf/auth/dto"
	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/database"
	"github.com/netgarden/maf/security/passwords"
	uuid "github.com/satori/go.uuid"
)

// compile-time interface checks
var (
	_ usersRepository    = (*mockUsers)(nil)
	_ sessionsRepository = (*mockSessions)(nil)
)

// --- mocks ---

type mockUsers struct {
	byUsername      map[string]*entities.User
	byID            map[string]*entities.User
	err             error
	updatedPassword string
	updateErr       error
}

func (m *mockUsers) GetUser(id string) (*entities.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.byID[id], nil
}

func (m *mockUsers) GetUserByUsername(username string) (*entities.User, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.byUsername[username], nil
}

func (m *mockUsers) UpdatePassword(id, passwordHash string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.updatedPassword = passwordHash
	return nil
}

type mockSessions struct {
	sessions  map[string]*entities.Session
	createErr error
	deleteErr error
	getErr    error
	updateErr error
}

func (m *mockSessions) CreateSession(userID, clientIP, userAgent string) (*entities.Session, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	uid, _ := uuid.FromString(userID)
	s := &entities.Session{
		EntityBase: database.EntityBase{ID: uuid.NewV4()},
		UserID:     uid,
		ClientIP:   clientIP,
		Agent:      userAgent,
	}
	if m.sessions == nil {
		m.sessions = make(map[string]*entities.Session)
	}
	m.sessions[s.ID.String()] = s
	return s, nil
}

func (m *mockSessions) DeleteSession(id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.sessions, id)
	return nil
}

func (m *mockSessions) GetSession(id string) (*entities.Session, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.sessions[id], nil
}

func (m *mockSessions) UpdateSession(id string) error {
	return m.updateErr
}

// --- test helpers ---

func newPM() *passwords.Manager { return passwords.NewManager() }

func newCfg() *maf.Config {
	return maf.NewConfig(map[string]any{
		"auth.token.ttl":                   15 * time.Minute,
		"auth.session.ttl":                 7 * 24 * time.Hour,
		"auth.session.cookie.name":         "session",
		"auth.session.cookie.path":         "/auth/refresh",
		"auth.session.cookie.force_secure": false,
	})
}

func newUser(username, plainPassword string, active bool, pm *passwords.Manager) *entities.User {
	return &entities.User{
		EntityBase: database.EntityBase{ID: uuid.NewV4()},
		Username:   username,
		Password:   pm.Encode(plainPassword),
		Email:      username + "@example.com",
		Active:     active,
	}
}

func newAuthSvc(users usersRepository, sessions sessionsRepository) *AuthService {
	return NewAuthService(newCfg(), testSecret, users, sessions, newPM())
}

func loginReq(username, password string) *dto.CredentialsLoginRequest {
	return &dto.CredentialsLoginRequest{Username: username, Password: password}
}

// --- Login ---

func TestLogin_Success(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "secret", true, pm)
	users := &mockUsers{
		byUsername: map[string]*entities.User{"alice": user},
		byID:       map[string]*entities.User{user.ID.String(): user},
	}
	sessions := &mockSessions{}
	svc := NewAuthService(newCfg(), testSecret, users, sessions, pm)

	accessToken, cookie, err := svc.Login(loginReq("alice", "secret"), "1.2.3.4", "TestAgent", false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accessToken == "" {
		t.Fatal("expected a non-empty access token")
	}
	if cookie == nil {
		t.Fatal("expected a refresh cookie")
	}
	if !cookie.HttpOnly {
		t.Error("refresh cookie must be HttpOnly")
	}
	if cookie.Name != "session" {
		t.Errorf("cookie name: want 'refreshToken', got %q", cookie.Name)
	}
	if cookie.Path != "/auth/refresh" {
		t.Errorf("cookie path: want '/auth/refresh', got %q", cookie.Path)
	}
	if cookie.MaxAge <= 0 {
		t.Errorf("cookie MaxAge must be positive, got %d", cookie.MaxAge)
	}
	// The refresh cookie value is the plain session ID.
	if sessions.sessions[cookie.Value] == nil {
		t.Error("refresh cookie value does not match any created session")
	}
	// The access token must be parseable separately.
	gotUser, err := svc.ValidateAccess(accessToken)
	if err != nil {
		t.Fatalf("access token is invalid: %v", err)
	}
	if gotUser == nil || gotUser.Username != "alice" {
		t.Errorf("access token user: want 'alice', got %v", gotUser)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "secret", true, pm)
	users := &mockUsers{byUsername: map[string]*entities.User{"alice": user}}
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, pm)

	accessToken, cookie, err := svc.Login(loginReq("alice", "wrong"), "", "", false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accessToken != "" || cookie != nil {
		t.Error("expected empty token and nil cookie on failed login")
	}
}

func TestLogin_UnknownUser(t *testing.T) {
	svc := newAuthSvc(&mockUsers{byUsername: map[string]*entities.User{}}, &mockSessions{})

	accessToken, cookie, err := svc.Login(loginReq("nobody", "pass"), "", "", false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accessToken != "" || cookie != nil {
		t.Error("expected empty token and nil cookie for unknown user")
	}
}

func TestLogin_InactiveUser(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "secret", false, pm)
	users := &mockUsers{byUsername: map[string]*entities.User{"alice": user}}
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, pm)

	accessToken, cookie, err := svc.Login(loginReq("alice", "secret"), "", "", false)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accessToken != "" || cookie != nil {
		t.Error("expected empty token and nil cookie for inactive user")
	}
}

func TestLogin_DBError(t *testing.T) {
	svc := newAuthSvc(&mockUsers{err: errors.New("db down")}, &mockSessions{})
	_, _, err := svc.Login(loginReq("alice", "secret"), "", "", false)
	if err == nil {
		t.Fatal("expected error when DB fails")
	}
}

func TestLogin_SessionCreationError(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "secret", true, pm)
	users := &mockUsers{byUsername: map[string]*entities.User{"alice": user}}
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{createErr: errors.New("db full")}, pm)

	_, _, err := svc.Login(loginReq("alice", "secret"), "", "", false)
	if err == nil {
		t.Fatal("expected error when session creation fails")
	}
}

// --- Refresh ---

func TestRefresh_ReturnsNewAccessToken(t *testing.T) {
	sessionID := uuid.NewV4()
	userID := uuid.NewV4()
	sessions := &mockSessions{
		sessions: map[string]*entities.Session{
			sessionID.String(): {
				EntityBase: database.EntityBase{ID: sessionID},
				UserID:     userID,
			},
		},
	}
	svc := newAuthSvc(&mockUsers{}, sessions)

	newAccessToken, err := svc.Refresh(sessionID.String())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newAccessToken == "" {
		t.Fatal("expected a new access token")
	}
	claims, err := parseAccessToken(svc.accessSecret(), newAccessToken)
	if err != nil {
		t.Fatalf("new access token is invalid: %v", err)
	}
	if claims.UserID != userID.String() {
		t.Errorf("user ID in new access token: want %q, got %q", userID.String(), claims.UserID)
	}
}

func TestRefresh_SessionNotFound_ReturnsEmpty(t *testing.T) {
	svc := newAuthSvc(&mockUsers{}, &mockSessions{sessions: map[string]*entities.Session{}})

	token, err := svc.Refresh("nonexistent-session-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Error("expected empty token for missing session")
	}
}

func TestRefresh_DBError(t *testing.T) {
	svc := newAuthSvc(&mockUsers{}, &mockSessions{getErr: errors.New("db down")})
	_, err := svc.Refresh("some-session-id")
	if err == nil {
		t.Fatal("expected error when DB fails")
	}
}

// --- ValidateAccess ---

func TestValidateAccess_ReturnsUser(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "secret", true, pm)
	users := &mockUsers{byID: map[string]*entities.User{user.ID.String(): user}}
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, pm)

	accessToken, _ := createAccessToken(svc.accessSecret(), user.ID.String(), time.Minute)

	gotUser, err := svc.ValidateAccess(accessToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotUser == nil {
		t.Fatal("expected a user")
	}
	if gotUser.Username != "alice" {
		t.Errorf("username: want 'alice', got %q", gotUser.Username)
	}
}

func TestValidateAccess_ExpiredToken(t *testing.T) {
	svc := newAuthSvc(&mockUsers{}, &mockSessions{})
	expired, _ := createAccessToken(svc.accessSecret(), "user-1", -time.Second)
	_, err := svc.ValidateAccess(expired)
	if err == nil {
		t.Fatal("expected error for expired access token")
	}
}

func TestValidateAccess_InvalidToken(t *testing.T) {
	svc := newAuthSvc(&mockUsers{}, &mockSessions{})
	_, err := svc.ValidateAccess("not.a.token")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestValidateAccess_WrongSecret(t *testing.T) {
	svc := newAuthSvc(&mockUsers{}, &mockSessions{})
	// Token signed with the wrong derived secret.
	wrongToken, _ := createAccessToken("wrong:access", "user-1", time.Minute)
	_, err := svc.ValidateAccess(wrongToken)
	if err == nil {
		t.Fatal("expected error for token signed with wrong secret")
	}
}

// --- Logout ---

func TestLogout_DeletesSessionAndClearsCookie(t *testing.T) {
	sessionID := uuid.NewV4()
	sessions := &mockSessions{
		sessions: map[string]*entities.Session{
			sessionID.String(): {EntityBase: database.EntityBase{ID: sessionID}},
		},
	}
	svc := newAuthSvc(&mockUsers{}, sessions)

	cookie := svc.Logout(sessionID.String(), false)

	if cookie == nil {
		t.Fatal("expected a clear-cookie response")
	}
	if cookie.MaxAge != -1 {
		t.Errorf("MaxAge: want -1 (delete), got %d", cookie.MaxAge)
	}
	if cookie.Value != "" {
		t.Errorf("cookie value should be empty, got %q", cookie.Value)
	}
	if cookie.Name != "session" {
		t.Errorf("cookie name: want 'refreshToken', got %q", cookie.Name)
	}
	if sessions.sessions[sessionID.String()] != nil {
		t.Error("session should be deleted after logout")
	}
}

func TestLogout_EmptySessionID_StillClearsCookie(t *testing.T) {
	svc := newAuthSvc(&mockUsers{}, &mockSessions{})
	cookie := svc.Logout("", false)
	if cookie == nil || cookie.MaxAge != -1 {
		t.Error("expected a clear cookie even with empty session ID")
	}
}

// --- SessionCookieName ---

func TestSessionCookieName(t *testing.T) {
	svc := newAuthSvc(&mockUsers{}, &mockSessions{})
	if got := svc.SessionCookieName(); got != "session" {
		t.Errorf("want 'session', got %q", got)
	}
}

// --- ChangePassword ---

func TestChangePassword_Success(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "oldpass", true, pm)
	users := &mockUsers{
		byID: map[string]*entities.User{user.ID.String(): user},
	}
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, pm)

	ok, err := svc.ChangePassword(user.ID.String(), "oldpass", "newpass")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true on correct current password")
	}
	if users.updatedPassword == "" {
		t.Fatal("expected UpdatePassword to be called")
	}
	// new hash must verify against "newpass"
	verified, _ := pm.Verify(users.updatedPassword, "newpass")
	if !verified {
		t.Error("stored hash does not match the new password")
	}
}

func TestChangePassword_WrongCurrentPassword(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "oldpass", true, pm)
	users := &mockUsers{byID: map[string]*entities.User{user.ID.String(): user}}
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, pm)

	ok, err := svc.ChangePassword(user.ID.String(), "wrongpass", "newpass")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false on wrong current password")
	}
	if users.updatedPassword != "" {
		t.Error("UpdatePassword must not be called when current password is wrong")
	}
}

func TestChangePassword_UserNotFound(t *testing.T) {
	svc := newAuthSvc(&mockUsers{byID: map[string]*entities.User{}}, &mockSessions{})

	ok, err := svc.ChangePassword("nonexistent-id", "pass", "newpass")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false when user does not exist")
	}
}

func TestChangePassword_DBError(t *testing.T) {
	svc := newAuthSvc(&mockUsers{err: errors.New("db down")}, &mockSessions{})
	_, err := svc.ChangePassword("user-id", "pass", "newpass")
	if err == nil {
		t.Fatal("expected error when DB fails")
	}
}
