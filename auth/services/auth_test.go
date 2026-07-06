package services

import (
	"errors"
	"strings"
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
	_ usersRepository               = (*mockUsers)(nil)
	_ sessionsRepository            = (*mockSessions)(nil)
	_ passwordResetTokensRepository = (*mockPasswordResetTokens)(nil)
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
	sessions        map[string]*entities.Session
	createErr       error
	deleteErr       error
	getErr          error
	updateErr       error
	deleteByUserErr error

	deletedByUserID []string
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

func (m *mockSessions) DeleteSessionsByUserID(userID string) error {
	if m.deleteByUserErr != nil {
		return m.deleteByUserErr
	}
	m.deletedByUserID = append(m.deletedByUserID, userID)
	for id, s := range m.sessions {
		if s.UserID.String() == userID {
			delete(m.sessions, id)
		}
	}
	return nil
}

type mockPasswordResetTokens struct {
	// tokens maps tokenHash -> userID for every unconsumed, unexpired
	// token — Consume checks expiresAt itself so tests can seed already-
	// expired entries to exercise that path.
	tokens map[string]mockResetToken

	createErr error
	deleted   []uuid.UUID // userIDs DeleteUnusedForUser was called with
}

type mockResetToken struct {
	userID    uuid.UUID
	expiresAt time.Time
	used      bool
}

func (m *mockPasswordResetTokens) Create(userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	if m.createErr != nil {
		return m.createErr
	}
	if m.tokens == nil {
		m.tokens = make(map[string]mockResetToken)
	}
	m.tokens[tokenHash] = mockResetToken{userID: userID, expiresAt: expiresAt}
	return nil
}

func (m *mockPasswordResetTokens) Consume(tokenHash string, now time.Time) (uuid.UUID, bool, error) {
	token, found := m.tokens[tokenHash]
	if !found || token.used || now.After(token.expiresAt) {
		return uuid.Nil, false, nil
	}
	token.used = true
	m.tokens[tokenHash] = token
	return token.userID, true, nil
}

func (m *mockPasswordResetTokens) DeleteUnusedForUser(userID uuid.UUID) error {
	m.deleted = append(m.deleted, userID)
	for hash, token := range m.tokens {
		if token.userID == userID && !token.used {
			delete(m.tokens, hash)
		}
	}
	return nil
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
		"auth.passwordReset.tokenTTL":      time.Hour,
		"auth.passwordReset.enabled":       true,
	})
}

// newCfgPasswordResetDisabled is newCfg with auth.passwordReset.enabled
// flipped off, for exercising the administrative disable switch.
func newCfgPasswordResetDisabled() *maf.Config {
	return maf.NewConfig(map[string]any{
		"auth.token.ttl":                   15 * time.Minute,
		"auth.session.ttl":                 7 * 24 * time.Hour,
		"auth.session.cookie.name":         "session",
		"auth.session.cookie.path":         "/auth/refresh",
		"auth.session.cookie.force_secure": false,
		"auth.passwordReset.tokenTTL":      time.Hour,
		"auth.passwordReset.enabled":       false,
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
	return NewAuthService(newCfg(), testSecret, users, sessions, &mockPasswordResetTokens{}, newPM())
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
	svc := NewAuthService(newCfg(), testSecret, users, sessions, &mockPasswordResetTokens{}, pm)

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
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, &mockPasswordResetTokens{}, pm)

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
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, &mockPasswordResetTokens{}, pm)

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
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{createErr: errors.New("db full")}, &mockPasswordResetTokens{}, pm)

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
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, &mockPasswordResetTokens{}, pm)

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
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, &mockPasswordResetTokens{}, pm)

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
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, &mockPasswordResetTokens{}, pm)

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

// --- RequestPasswordReset / ConfirmPasswordReset ---

func TestRequestPasswordReset_NoMailerWired_IsNoop(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "secret", true, pm)
	users := &mockUsers{byUsername: map[string]*entities.User{"alice": user}}
	tokens := &mockPasswordResetTokens{}
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, tokens, pm)
	// Deliberately no svc.SetMailer(...) call.

	if err := svc.RequestPasswordReset("alice"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens.tokens) != 0 {
		t.Error("expected no token to be created when no mailer is wired")
	}
}

func TestRequestPasswordReset_Disabled_IsNoop(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "secret", true, pm)
	users := &mockUsers{byUsername: map[string]*entities.User{"alice": user}}
	tokens := &mockPasswordResetTokens{}
	svc := NewAuthService(newCfgPasswordResetDisabled(), testSecret, users, &mockSessions{}, tokens, pm)
	mailer := &fakeCredentialsMailer{}
	svc.SetMailer(mailer, "")

	if err := svc.RequestPasswordReset("alice"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tokens.tokens) != 0 {
		t.Error("expected no token to be created while the feature is disabled")
	}
	if len(mailer.calls) != 0 {
		t.Error("expected no email to be sent while the feature is disabled")
	}
}

func TestConfirmPasswordReset_Disabled_Fails(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "oldpass", true, pm)
	rawToken, tokenHash, _ := generateResetToken()
	tokens := &mockPasswordResetTokens{
		tokens: map[string]mockResetToken{
			tokenHash: {userID: user.ID, expiresAt: time.Now().Add(time.Hour)},
		},
	}
	users := &mockUsers{byID: map[string]*entities.User{user.ID.String(): user}}
	svc := NewAuthService(newCfgPasswordResetDisabled(), testSecret, users, &mockSessions{}, tokens, pm)

	ok, err := svc.ConfirmPasswordReset(rawToken, "newpass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false while the feature is disabled, even with an otherwise-valid token")
	}
	if users.updatedPassword != "" {
		t.Error("password must not be updated while the feature is disabled")
	}
}

func TestRequestPasswordReset_UnknownUsername_IsNoop(t *testing.T) {
	users := &mockUsers{byUsername: map[string]*entities.User{}}
	tokens := &mockPasswordResetTokens{}
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, tokens, newPM())
	mailer := &fakeCredentialsMailer{}
	svc.SetMailer(mailer, "")

	if err := svc.RequestPasswordReset("nobody"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mailer.calls) != 0 {
		t.Error("expected no email to be sent for an unknown username")
	}
	if len(tokens.tokens) != 0 {
		t.Error("expected no token to be created for an unknown username")
	}
}

func TestRequestPasswordReset_InactiveUser_IsNoop(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "secret", false, pm)
	users := &mockUsers{byUsername: map[string]*entities.User{"alice": user}}
	tokens := &mockPasswordResetTokens{}
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, tokens, pm)
	mailer := &fakeCredentialsMailer{}
	svc.SetMailer(mailer, "")

	if err := svc.RequestPasswordReset("alice"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mailer.calls) != 0 {
		t.Error("expected no email to be sent for an inactive user")
	}
}

func TestRequestPasswordReset_HappyPath_SendsTemplateAndCreatesToken(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "secret", true, pm)
	users := &mockUsers{byUsername: map[string]*entities.User{"alice": user}}
	tokens := &mockPasswordResetTokens{}
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, tokens, pm)
	mailer := &fakeCredentialsMailer{}
	svc.SetMailer(mailer, "https://example.com/reset-password/")

	if err := svc.RequestPasswordReset("alice"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tokens.tokens) != 1 {
		t.Fatalf("expected exactly 1 token to be created, got %d", len(tokens.tokens))
	}
	if len(mailer.calls) != 1 {
		t.Fatalf("expected exactly 1 SendTemplate call, got %d", len(mailer.calls))
	}
	call := mailer.calls[0]
	if call.templateID != PasswordResetTemplateID {
		t.Errorf("templateID = %q, want %q", call.templateID, PasswordResetTemplateID)
	}
	if len(call.to) != 1 || call.to[0] != user.Email {
		t.Errorf("to = %v, want [%s]", call.to, user.Email)
	}
	data, ok := call.data.(PasswordResetData)
	if !ok {
		t.Fatalf("data = %#v, want PasswordResetData", call.data)
	}
	if data.Username != "alice" {
		t.Errorf("data.Username = %q, want alice", data.Username)
	}
	if !strings.HasPrefix(data.ResetURL, "https://example.com/reset-password/?token=") {
		t.Errorf("ResetURL = %q, want a URL with a token query param", data.ResetURL)
	}
}

func TestRequestPasswordReset_DeletesPreviousUnusedTokens(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "secret", true, pm)
	users := &mockUsers{byUsername: map[string]*entities.User{"alice": user}}
	tokens := &mockPasswordResetTokens{}
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, tokens, pm)
	svc.SetMailer(&fakeCredentialsMailer{}, "")

	if err := svc.RequestPasswordReset("alice"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := svc.RequestPasswordReset("alice"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tokens.tokens) != 1 {
		t.Errorf("expected exactly 1 live token after a second request, got %d", len(tokens.tokens))
	}
}

func TestConfirmPasswordReset_ValidToken_UpdatesPasswordAndClearsSessions(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "oldpass", true, pm)
	rawToken, tokenHash, err := generateResetToken()
	if err != nil {
		t.Fatalf("generateResetToken: %v", err)
	}
	tokens := &mockPasswordResetTokens{
		tokens: map[string]mockResetToken{
			tokenHash: {userID: user.ID, expiresAt: time.Now().Add(time.Hour)},
		},
	}
	users := &mockUsers{byID: map[string]*entities.User{user.ID.String(): user}}
	sessions := &mockSessions{
		sessions: map[string]*entities.Session{
			"session-1": {UserID: user.ID},
		},
	}
	svc := NewAuthService(newCfg(), testSecret, users, sessions, tokens, pm)

	ok, err := svc.ConfirmPasswordReset(rawToken, "newpass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for a valid token")
	}
	verified, _ := pm.Verify(users.updatedPassword, "newpass")
	if !verified {
		t.Error("stored hash does not match the new password")
	}
	if len(sessions.deletedByUserID) != 1 || sessions.deletedByUserID[0] != user.ID.String() {
		t.Errorf("expected DeleteSessionsByUserID(%s) to be called, got %v", user.ID.String(), sessions.deletedByUserID)
	}
}

func TestConfirmPasswordReset_UnknownToken_Fails(t *testing.T) {
	svc := newAuthSvc(&mockUsers{}, &mockSessions{})
	ok, err := svc.ConfirmPasswordReset("not-a-real-token", "newpass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for an unknown token")
	}
}

func TestConfirmPasswordReset_ExpiredToken_Fails(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "oldpass", true, pm)
	rawToken, tokenHash, _ := generateResetToken()
	tokens := &mockPasswordResetTokens{
		tokens: map[string]mockResetToken{
			tokenHash: {userID: user.ID, expiresAt: time.Now().Add(-time.Minute)},
		},
	}
	users := &mockUsers{byID: map[string]*entities.User{user.ID.String(): user}}
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, tokens, pm)

	ok, err := svc.ConfirmPasswordReset(rawToken, "newpass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for an expired token")
	}
	if users.updatedPassword != "" {
		t.Error("password must not be updated for an expired token")
	}
}

func TestConfirmPasswordReset_AlreadyUsedToken_Fails(t *testing.T) {
	pm := newPM()
	user := newUser("alice", "oldpass", true, pm)
	rawToken, tokenHash, _ := generateResetToken()
	tokens := &mockPasswordResetTokens{
		tokens: map[string]mockResetToken{
			tokenHash: {userID: user.ID, expiresAt: time.Now().Add(time.Hour)},
		},
	}
	users := &mockUsers{byID: map[string]*entities.User{user.ID.String(): user}}
	svc := NewAuthService(newCfg(), testSecret, users, &mockSessions{}, tokens, pm)

	firstOK, err := svc.ConfirmPasswordReset(rawToken, "newpass")
	if err != nil || !firstOK {
		t.Fatalf("expected first confirm to succeed: ok=%v err=%v", firstOK, err)
	}

	secondOK, err := svc.ConfirmPasswordReset(rawToken, "anotherpass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secondOK {
		t.Fatal("expected ok=false when reusing an already-consumed token")
	}
}
