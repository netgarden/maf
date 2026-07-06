package services

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/netgarden/maf"
	"github.com/netgarden/maf/auth/dto"
	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/security/passwords"
)

func NewAuthService(
	config *maf.Config,
	secret string,
	usersService usersRepository,
	sessionsService sessionsRepository,
	resetTokens passwordResetTokensRepository,
	passwordsManager *passwords.Manager,
) *AuthService {
	return &AuthService{
		config:           config,
		secret:           secret,
		usersService:     usersService,
		sessionsService:  sessionsService,
		resetTokens:      resetTokens,
		passwordsManager: passwordsManager,
	}
}

type AuthService struct {
	config           *maf.Config
	secret           string
	usersService     usersRepository
	sessionsService  sessionsRepository
	resetTokens      passwordResetTokensRepository
	passwordsManager *passwords.Manager

	mailer       TemplateMailer
	resetBaseURL string
}

// SetMailer wires optional password-reset email delivery. Called by
// rrpc-auth's Module.Initialize() only when a "mailer" module is also
// registered by the consuming application; leave unset (the default) to
// make RequestPasswordReset a silent no-op — see RequestPasswordReset.
func (s *AuthService) SetMailer(mailer TemplateMailer, baseURL string) {
	s.mailer = mailer
	s.resetBaseURL = baseURL
}

// Login validates credentials, creates a DB session, and returns a short-lived
// access token (to be sent in the JSON response) and a long-lived refresh token
// cookie. Returns ("", nil, nil) when credentials are invalid.
// secure should be true when the request arrived over HTTPS.
func (s *AuthService) Login(req *dto.CredentialsLoginRequest, clientIP, userAgent string, secure bool) (string, *http.Cookie, error) {
	user, err := s.validateCredentials(req)
	if err != nil {
		return "", nil, err
	}
	if user == nil {
		return "", nil, nil
	}

	session, err := s.sessionsService.CreateSession(user.ID.String(), clientIP, userAgent)
	if err != nil {
		return "", nil, err
	}

	accessToken, err := createAccessToken(s.accessSecret(), user.ID.String(), s.accessTTL())
	if err != nil {
		return "", nil, err
	}

	return accessToken, s.issueRefreshCookie(session, secure), nil
}

// Refresh looks up the session by ID (stored directly in the refresh cookie),
// and returns a new short-lived access token. Returns ("", nil) when the session
// no longer exists (revoked or expired from DB).
func (s *AuthService) Refresh(sessionID string) (string, error) {
	session, err := s.sessionsService.GetSession(sessionID)
	if err != nil {
		return "", err
	}
	if session == nil {
		return "", nil
	}

	if err = s.sessionsService.UpdateSession(sessionID); err != nil {
		return "", err
	}

	return createAccessToken(s.accessSecret(), session.UserID.String(), s.accessTTL())
}

// ValidateAccess parses an access token and returns the corresponding user.
func (s *AuthService) ValidateAccess(accessToken string) (*entities.User, error) {
	claims, err := parseAccessToken(s.accessSecret(), accessToken)
	if err != nil {
		return nil, err
	}
	return s.usersService.GetUser(claims.UserID)
}

// ChangePassword verifies currentPassword against the stored hash and, if
// correct, replaces it with a hash of newPassword. Returns (false, nil) when
// currentPassword does not match.
func (s *AuthService) ChangePassword(userID, currentPassword, newPassword string) (bool, error) {
	user, err := s.usersService.GetUser(userID)
	if err != nil {
		return false, err
	}
	if user == nil {
		return false, nil
	}

	ok, err := s.passwordsManager.Verify(user.Password, currentPassword)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	return true, s.usersService.UpdatePassword(userID, s.passwordsManager.Encode(newPassword))
}

// RequestPasswordReset always succeeds from the caller's perspective — it
// deliberately never reveals whether username exists, is active, whether
// a mailer is even configured, or whether the feature is administratively
// disabled, since a differing response would let an attacker enumerate
// valid usernames. It's a genuine no-op (no token created, no email sent)
// whenever auth.passwordReset.enabled is false, the consuming application
// hasn't wired a mailer via SetMailer, or username doesn't match an
// active user.
func (s *AuthService) RequestPasswordReset(username string) error {
	if s.mailer == nil || !s.config.GetBool("auth.passwordReset.enabled") {
		return nil
	}

	user, err := s.usersService.GetUserByUsername(username)
	if err != nil {
		return err
	}
	if user == nil || !user.Active {
		return nil
	}

	// Drop any previously issued, still-valid token for this user first —
	// a user should never have more than one live reset link outstanding.
	if err := s.resetTokens.DeleteUnusedForUser(user.ID); err != nil {
		return err
	}

	rawToken, tokenHash, err := generateResetToken()
	if err != nil {
		return err
	}

	ttl := s.config.GetDuration("auth.passwordReset.tokenTTL")
	if err := s.resetTokens.Create(user.ID, tokenHash, time.Now().Add(ttl)); err != nil {
		return err
	}

	resetURL := s.resetBaseURL
	if resetURL != "" {
		sep := "?"
		if strings.Contains(resetURL, "?") {
			sep = "&"
		}
		resetURL += sep + "token=" + url.QueryEscape(rawToken)
	}

	return s.mailer.SendTemplate(PasswordResetTemplateID, []string{user.Email}, nil, nil, PasswordResetData{
		Username: user.Username,
		ResetURL: resetURL,
	})
}

// ConfirmPasswordReset validates token and, if valid (exists, unused,
// unexpired), sets newPassword and invalidates every existing session for
// that user — same "password changed, log in again everywhere" behavior a
// security-conscious reset should have. Returns (false, nil) for every
// invalid-token case (unknown, expired, already used, or the feature has
// been administratively disabled since the token was issued) rather than
// distinguishing them, so a guesser learns nothing from the response —
// mirrors ChangePassword's (bool, error) shape for the same reason.
func (s *AuthService) ConfirmPasswordReset(token, newPassword string) (bool, error) {
	if !s.config.GetBool("auth.passwordReset.enabled") {
		return false, nil
	}

	userID, ok, err := s.resetTokens.Consume(hashResetToken(token), time.Now())
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	if err := s.usersService.UpdatePassword(userID.String(), s.passwordsManager.Encode(newPassword)); err != nil {
		return false, err
	}

	return true, s.sessionsService.DeleteSessionsByUserID(userID.String())
}

// ParseAccessToken validates the access token cryptographically and returns
// the user ID embedded in it. No DB lookup is performed.
func (s *AuthService) ParseAccessToken(accessToken string) (string, error) {
	claims, err := parseAccessToken(s.accessSecret(), accessToken)
	if err != nil {
		return "", err
	}
	return claims.UserID, nil
}

// GetUser returns the user record by ID.
func (s *AuthService) GetUser(id string) (*entities.User, error) {
	return s.usersService.GetUser(id)
}

// Logout deletes the DB session and returns a cookie that clears the refresh token.
// secure should be true when the request arrived over HTTPS.
func (s *AuthService) Logout(sessionID string, secure bool) *http.Cookie {
	if sessionID != "" {
		if err := s.sessionsService.DeleteSession(sessionID); err != nil {
			slog.Warn("failed to delete session on logout",
				slog.String("session_id", sessionID),
				slog.Any("error", err),
			)
		}
	}
	return s.clearRefreshCookie(secure)
}

func (s *AuthService) SessionCookieName() string {
	return s.config.GetString("auth.session.cookie.name")
}

func (s *AuthService) RefreshCookiePath() string {
	return s.config.GetString("auth.session.cookie.path")
}

func (s *AuthService) ForceSecureCookie() bool {
	return s.config.GetBool("auth.session.cookie.force_secure")
}

func (s *AuthService) accessSecret() string { return s.secret + ":access" }

func (s *AuthService) accessTTL() time.Duration {
	return s.config.GetDuration("auth.token.ttl")
}

func (s *AuthService) refreshTTL() time.Duration {
	return s.config.GetDuration("auth.session.ttl")
}

func (s *AuthService) issueRefreshCookie(session *entities.Session, secure bool) *http.Cookie {
	return s.createRefreshCookie(session.ID.String(), secure)
}

func (s *AuthService) clearRefreshCookie(secure bool) *http.Cookie {
	return s.createRefreshCookie("", secure)
}

func (s *AuthService) createRefreshCookie(tokenValue string, secure bool) *http.Cookie {
	ttl := s.refreshTTL()
	maxAge := -1
	if tokenValue != "" {
		maxAge = int(ttl.Seconds())
	}
	return &http.Cookie{
		Name:     s.SessionCookieName(),
		Value:    tokenValue,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure || s.ForceSecureCookie(),
		Path:     s.RefreshCookiePath(),
		SameSite: http.SameSiteStrictMode,
	}
}

func (s *AuthService) validateCredentials(req *dto.CredentialsLoginRequest) (*entities.User, error) {
	user, err := s.usersService.GetUserByUsername(req.Username)
	if err != nil {
		return nil, err
	}
	if user == nil {
		slog.Debug("user not found", slog.String("username", req.Username))
		return nil, nil
	}
	if !user.Active {
		slog.Debug("user not active", slog.String("username", req.Username))
		return nil, nil
	}

	ok, err := s.passwordsManager.Verify(user.Password, req.Password)
	if err != nil {
		return nil, err
	}
	if !ok {
		slog.Debug("wrong password", slog.String("username", req.Username))
		return nil, nil
	}

	return user, nil
}
