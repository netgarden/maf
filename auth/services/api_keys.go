package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/security/ipfilter"
	uuid "github.com/satori/go.uuid"
	"gorm.io/gorm"
)

// apiKeyTokenPrefix marks a plaintext API key so it's recognizable in logs
// and lets the authentication middleware route it straight to
// AuthService.ValidateApiKey without first attempting (and failing) a JWT
// parse. Deliberately distinct from iris's own (unprefixed) webhook tokens.
const apiKeyTokenPrefix = "pat_"

// MaxApiKeyAllowedIPs bounds how many entries a key's source-IP allowlist may have.
const MaxApiKeyAllowedIPs = 50

func NewApiKeysService(db *gorm.DB) *ApiKeysService {
	return &ApiKeysService{db: db}
}

type ApiKeysService struct {
	db *gorm.DB
}

// Create mints a fresh key for userID. plaintext is returned exactly once —
// only its hash is ever stored, the same way iris's own webhook tokens and
// this package's own password-reset tokens work. expiresAt nil means the
// key never expires; allowedIPs empty means any source IP may use it.
func (s *ApiKeysService) Create(userID uuid.UUID, name string, expiresAt *time.Time, allowedIPs []string) (plaintext string, key *entities.ApiKey, err error) {
	allowed, err := CheckApiKeyAllowedIPs(allowedIPs)
	if err != nil {
		return "", nil, err
	}

	plaintext, hash, prefix, err := newApiKey()
	if err != nil {
		return "", nil, err
	}

	key = &entities.ApiKey{
		UserID:     userID,
		Name:       name,
		TokenHash:  hash,
		Prefix:     prefix,
		ExpiresAt:  expiresAt,
		AllowedIPs: allowed,
	}
	if err := s.db.Create(key).Error; err != nil {
		return "", nil, err
	}
	return plaintext, key, nil
}

// List returns userID's own keys, newest first.
func (s *ApiKeysService) List(userID uuid.UUID) ([]entities.ApiKey, error) {
	var keys []entities.ApiKey
	err := s.db.Where("user_id = ?", userID).Order("created_at desc").Find(&keys).Error
	return keys, err
}

// Revoke deletes id, but only if it belongs to userID — a user can never
// revoke another user's key, even by guessing its id. Returns false (with a
// nil error) when no matching row was found, mirroring UsersService.DeleteUser.
func (s *ApiKeysService) Revoke(userID, id uuid.UUID) (bool, error) {
	result := s.db.Where("id = ? AND user_id = ?", id, userID).Delete(&entities.ApiKey{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// FindByHash looks up a key by its token hash, preloading the owning User
// (ValidateApiKey needs User.Active). Returns (nil, nil) for no match.
func (s *ApiKeysService) FindByHash(hash string) (*entities.ApiKey, error) {
	key := &entities.ApiKey{}
	err := s.db.Preload("User").Where("token_hash = ?", hash).First(key).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return key, nil
}

// TouchLastUsed best-effort records a successful use; a write failure here
// never fails the request that triggered it — see AuthService.ValidateApiKey.
func (s *ApiKeysService) TouchLastUsed(id uuid.UUID) error {
	return s.db.Model(&entities.ApiKey{}).Where("id = ?", id).Update("last_used_at", time.Now()).Error
}

// IsApiKeyToken reports whether token has the shape of an API key rather
// than a session JWT — the middleware layer (maf/rrpc-auth) uses this to
// route a bearer token to ValidateApiKey instead of wasting a doomed
// ParseAccessToken attempt on it. Exported (rather than the middleware
// duplicating the literal prefix itself) so the two can never drift apart.
func IsApiKeyToken(token string) bool {
	return strings.HasPrefix(token, apiKeyTokenPrefix)
}

// CheckApiKeyAllowedIPs validates and normalises an allowlist (see
// ipfilter.Normalize): each entry must be a plain IP or a CIDR range; a bare
// IP is stored as a /32 (or /128 for IPv6), the same convention iris's own
// webhook tokens use (iris/webhooks/ipfilter.go).
func CheckApiKeyAllowedIPs(in []string) ([]string, error) {
	return ipfilter.Normalize(in, MaxApiKeyAllowedIPs)
}

// apiKeyAllowed reports whether remote matches allowed. An empty allowlist
// means "any source" (the default, and the same as iris's webhook tokens).
func apiKeyAllowed(allowed []string, remote net.IP) bool {
	return ipfilter.Allowed(allowed, remote)
}

// newApiKey returns a fresh plaintext token (never stored), its sha256 hex
// hash, and a short display prefix.
func newApiKey() (plaintext, hash, prefix string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", "", err
	}
	plaintext = apiKeyTokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	hash = hashApiKey(plaintext)
	prefix = plaintext
	if len(prefix) > len(apiKeyTokenPrefix)+8 {
		prefix = prefix[:len(apiKeyTokenPrefix)+8]
	}
	return plaintext, hash, prefix, nil
}

func hashApiKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
