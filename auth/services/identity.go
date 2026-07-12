package services

import (
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/netgarden/maf/auth/entities"
	uuid "github.com/satori/go.uuid"
	"gorm.io/gorm"
)

// ExternalIdentity is what any external authentication protocol (OIDC
// today; SAML/LDAP are expected to produce the same shape later) resolves
// a successful authentication down to, before AuthService.CompleteExternalLogin
// takes over the shared find-or-create-user + session-issuing pipeline that
// every login method — including plain username/password — converges on.
//
// Protocol-specific handshakes (OIDC's redirect/code-exchange dance, a
// future SAML response, or a synchronous LDAP bind) are deliberately not
// part of this contract: they all differ, but every one of them ends by
// constructing an ExternalIdentity and calling CompleteExternalLogin.
type ExternalIdentity struct {
	ProviderType string    // "oidc" (future: "saml", "ldap")
	ProviderID   uuid.UUID // the generic entities.Provider row's ID (auth_providers.id), not any protocol-specific table's own ID
	Subject      string    // stable per-provider identifier: OIDC "sub", future SAML NameID / LDAP DN

	Email         string
	EmailVerified bool
	FirstName     string
	LastName      string

	// Admin, if non-nil, is the result of evaluating the provider's
	// admin-claim mapping (entities.Provider.AdminClaimPath/AdminClaimValues,
	// see evaluateAdminClaim) against whatever claims/attributes the
	// protocol asserted — CompleteExternalLogin syncs User.Admin to this
	// value on every login when set. nil means "no mapping configured for
	// this provider, leave Admin alone".
	Admin *bool
}

func NewUserIdentitiesService(db *gorm.DB) *UserIdentitiesService {
	return &UserIdentitiesService{db: db}
}

type UserIdentitiesService struct {
	db *gorm.DB
}

func (s *UserIdentitiesService) FindByProviderSubject(providerType string, providerID uuid.UUID, subject string) (*entities.UserIdentity, error) {
	identity := &entities.UserIdentity{}
	err := s.db.Where(
		"provider_type = ? AND provider_id = ? AND subject = ?",
		providerType, providerID, subject,
	).First(identity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return identity, nil
}

func (s *UserIdentitiesService) Create(userID uuid.UUID, providerType string, providerID uuid.UUID, subject, email string) (*entities.UserIdentity, error) {
	identity := &entities.UserIdentity{
		UserID:       userID,
		ProviderType: providerType,
		ProviderID:   providerID,
		Subject:      subject,
		Email:        email,
	}
	if err := s.db.Create(identity).Error; err != nil {
		return nil, err
	}
	return identity, nil
}

// CompleteExternalLogin is the shared tail every external-login protocol
// converges on once it has produced an ExternalIdentity: resolve (or
// JIT-provision/link) the local User, sync the Admin flag if the protocol
// supplied a mapping result, then issue a session + access token +refresh
// cookie exactly the way password Login does. Returns ("", nil, nil) —
// the same "authentication failed" convention Login uses — when the
// resolved user is inactive; callers should not distinguish this from any
// other login failure.
func (s *AuthService) CompleteExternalLogin(identity ExternalIdentity, clientIP, userAgent string, secure bool) (string, *http.Cookie, error) {
	user, err := s.resolveExternalUser(identity)
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

// resolveExternalUser returns the local User to log in as, or (nil, nil)
// if the login should fail closed (deactivated user).
func (s *AuthService) resolveExternalUser(identity ExternalIdentity) (*entities.User, error) {
	linked, err := s.identities.FindByProviderSubject(identity.ProviderType, identity.ProviderID, identity.Subject)
	if err != nil {
		return nil, err
	}

	var user *entities.User
	if linked != nil {
		user, err = s.usersService.GetUser(linked.UserID.String())
		if err != nil {
			return nil, err
		}
		if user == nil {
			return nil, nil
		}
	} else {
		user, err = s.findOrCreateUserForNewIdentity(identity)
		if err != nil {
			return nil, err
		}
		if _, err := s.identities.Create(user.ID, identity.ProviderType, identity.ProviderID, identity.Subject, identity.Email); err != nil {
			return nil, err
		}
	}

	if !user.Active {
		slog.Debug("external login: user not active",
			slog.String("provider_type", identity.ProviderType),
			slog.String("subject", identity.Subject),
		)
		return nil, nil
	}

	if identity.Admin != nil && *identity.Admin != user.Admin {
		if err := s.usersService.SetAdmin(user.ID.String(), *identity.Admin); err != nil {
			return nil, err
		}
		user.Admin = *identity.Admin
	}

	return user, nil
}

// findOrCreateUserForNewIdentity handles the "no UserIdentity row yet"
// case: link to an existing local account by verified email when enabled
// (auth.oidc.autoLinkByVerifiedEmail, default true — never on an
// unverified email, since that would let an IdP that doesn't verify email
// hand an attacker someone else's existing account), otherwise JIT-create
// a fresh User.
func (s *AuthService) findOrCreateUserForNewIdentity(identity ExternalIdentity) (*entities.User, error) {
	if identity.Email != "" && identity.EmailVerified && s.autoLinkByVerifiedEmail() {
		existing, err := s.usersService.GetUserByEmail(identity.Email)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, nil
		}
	}

	return s.usersService.CreateExternalUser(identity.Email, identity.FirstName, identity.LastName)
}

func (s *AuthService) autoLinkByVerifiedEmail() bool {
	return s.config.GetBool("auth.oidc.autoLinkByVerifiedEmail")
}

// evaluateAdminClaim returns nil when provider has no admin-claim mapping
// configured (AdminClaimPath == ""), meaning "don't touch Admin" — leaving
// the flag purely locally managed for that provider. Otherwise it always
// returns a non-nil result (true or false), so losing a qualifying claim
// value at the IdP correctly revokes Admin on the next login rather than
// leaving a stale true in place. Generic across every protocol — it only
// ever looks at entities.Provider (never a protocol-specific table) and a
// plain map[string]any of whatever claims/attributes that protocol's
// service layer already normalized, so a future SAML/LDAP service reuses
// this unchanged.
func evaluateAdminClaim(provider *entities.Provider, claims map[string]any) *bool {
	if provider.AdminClaimPath == "" {
		return nil
	}
	grants := claimGrantsAdmin(claims[provider.AdminClaimPath], provider.AdminClaimValues)
	return &grants
}

func claimGrantsAdmin(value any, wanted []string) bool {
	switch v := value.(type) {
	case string:
		return slices.Contains(wanted, v)
	case []string:
		for _, s := range v {
			if slices.Contains(wanted, s) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && slices.Contains(wanted, s) {
				return true
			}
		}
	}
	return false
}
