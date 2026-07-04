package services

import (
	"log/slog"
	"net/http"
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
	passwordsManager *passwords.Manager,
) *AuthService {
	return &AuthService{
		config:           config,
		secret:           secret,
		usersService:     usersService,
		sessionsService:  sessionsService,
		passwordsManager: passwordsManager,
	}
}

type AuthService struct {
	config           *maf.Config
	secret           string
	usersService     usersRepository
	sessionsService  sessionsRepository
	passwordsManager *passwords.Manager
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
