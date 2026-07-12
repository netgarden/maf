package services

import (
	"errors"

	"github.com/netgarden/maf/auth/dto"
	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/security/encryption"
	"gorm.io/gorm"
)

// defaultOIDCScopes is used whenever an admin leaves Scopes blank when
// registering a provider.
const defaultOIDCScopes = "openid profile email"

// providerTypeOIDC is the entities.Provider.Type value for every row this
// service owns the protocol-specific half of.
const providerTypeOIDC = "oidc"

func NewOIDCProvidersService(db *gorm.DB, encryptionManager *encryption.Manager) *OIDCProvidersService {
	return &OIDCProvidersService{db: db, encryption: encryptionManager}
}

// OIDCProviderDetail joins the generic entities.Provider row with its
// OIDC-specific entities.OIDCProvider counterpart — the shape callers
// actually need (Slug/Name/Enabled/admin-claim mapping live on one table,
// IssuerURL/ClientID/secret/Scopes on the other). Provider is embedded so
// its fields promote directly (detail.Slug, detail.Enabled, ...); OIDC is
// named since embedding it too would make .ID ambiguous between the two
// tables' own primary keys.
type OIDCProviderDetail struct {
	entities.Provider
	OIDC entities.OIDCProvider
}

// OIDCProvidersService owns the OIDC-specific half of provider management
// (issuer/client/secret/scopes) plus the two-table Create/Update that keep
// entities.Provider and entities.OIDCProvider in sync. Generic actions that
// don't need any of that — list every provider regardless of type,
// enable/disable, delete — live on ProvidersService instead; see that
// type's doc comment for why the split exists. Client secrets are
// encrypted at rest via the injected encryption.Manager (the same one
// github.com/netgarden/maf/mailer uses for queued email bodies) and are
// never returned in plaintext except to OIDCAuthService, which needs it to
// talk to the provider's token endpoint.
type OIDCProvidersService struct {
	db         *gorm.DB
	encryption *encryption.Manager
}

func (s *OIDCProvidersService) Get(id string) (*OIDCProviderDetail, error) {
	return s.getDetail("id", id)
}

func (s *OIDCProvidersService) GetBySlug(slug string) (*OIDCProviderDetail, error) {
	return s.getDetail("slug", slug)
}

// ListEnabled returns every Enabled OIDC provider, for the public
// provider-picker endpoint (GET /api/auth/oidc/providers) — callers must
// only surface Slug/Name from the result, never anything OIDC-specific.
func (s *OIDCProvidersService) ListEnabled() ([]*entities.Provider, error) {
	var providers []*entities.Provider
	err := s.db.Where("type = ? AND enabled = ?", providerTypeOIDC, true).Order("name").Find(&providers).Error
	return providers, err
}

// Create registers a new provider, transactionally writing both the
// generic Provider row (Type: "oidc") and its OIDCProvider counterpart,
// and encrypting ClientSecret at rest. Returns (nil, nil) when slug is
// already taken (by any provider, of any type — Provider.Slug is globally
// unique).
func (s *OIDCProvidersService) Create(data *dto.OIDCProviderCreateDTO) (*OIDCProviderDetail, error) {
	existing, err := s.GetBySlug(data.Slug)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, nil
	}

	encryptedSecret, err := s.encryption.Encrypt([]byte(data.ClientSecret))
	if err != nil {
		return nil, err
	}

	scopes := data.Scopes
	if scopes == "" {
		scopes = defaultOIDCScopes
	}

	var detail OIDCProviderDetail
	err = s.db.Transaction(func(tx *gorm.DB) error {
		provider := &entities.Provider{
			Type:             providerTypeOIDC,
			Slug:             data.Slug,
			Name:             data.Name,
			Enabled:          data.Enabled,
			AdminClaimPath:   data.AdminClaimPath,
			AdminClaimValues: data.AdminClaimValues,
		}
		if err := tx.Create(provider).Error; err != nil {
			return err
		}

		oidc := &entities.OIDCProvider{
			ProviderID:            provider.ID,
			IssuerURL:             data.IssuerURL,
			ClientID:              data.ClientID,
			ClientSecretEncrypted: encryptedSecret,
			Scopes:                scopes,
		}
		if err := tx.Create(oidc).Error; err != nil {
			return err
		}

		detail = OIDCProviderDetail{Provider: *provider, OIDC: *oidc}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

// Update replaces every editable field across both tables. A blank
// data.ClientSecret leaves the stored secret untouched — see
// dto.OIDCProviderUpdateDTO. Returns (nil, nil) when id (the generic
// Provider ID) doesn't exist as an OIDC provider.
func (s *OIDCProvidersService) Update(id string, data *dto.OIDCProviderUpdateDTO) (*OIDCProviderDetail, error) {
	detail, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if detail == nil {
		return nil, nil
	}

	scopes := data.Scopes
	if scopes == "" {
		scopes = defaultOIDCScopes
	}

	err = s.db.Transaction(func(tx *gorm.DB) error {
		detail.Provider.Name = data.Name
		detail.Provider.Enabled = data.Enabled
		detail.Provider.AdminClaimPath = data.AdminClaimPath
		detail.Provider.AdminClaimValues = data.AdminClaimValues
		if err := tx.Save(&detail.Provider).Error; err != nil {
			return err
		}

		detail.OIDC.IssuerURL = data.IssuerURL
		detail.OIDC.ClientID = data.ClientID
		detail.OIDC.Scopes = scopes
		if data.ClientSecret != "" {
			encryptedSecret, err := s.encryption.Encrypt([]byte(data.ClientSecret))
			if err != nil {
				return err
			}
			detail.OIDC.ClientSecretEncrypted = encryptedSecret
		}
		return tx.Save(&detail.OIDC).Error
	})
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// DecryptClientSecret returns oidc's OAuth client secret in plaintext —
// used only by OIDCAuthService when it needs to talk to the provider's
// token endpoint, never exposed through the admin CRUD API.
func (s *OIDCProvidersService) DecryptClientSecret(oidc *entities.OIDCProvider) (string, error) {
	plaintext, err := s.encryption.Decrypt(oidc.ClientSecretEncrypted)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// getDetail loads the generic Provider row (filtered to Type: "oidc") by
// field=value, then its OIDC-specific counterpart by ProviderID. Returns
// (nil, nil) when no such provider exists. field is always a
// caller-supplied constant ("id"/"slug"), never external input.
func (s *OIDCProvidersService) getDetail(field, value string) (*OIDCProviderDetail, error) {
	provider := &entities.Provider{}
	err := s.db.Where(field+" = ? AND type = ?", value, providerTypeOIDC).First(provider).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	oidc := &entities.OIDCProvider{}
	if err := s.db.Where("provider_id = ?", provider.ID).First(oidc).Error; err != nil {
		// A Provider row of Type "oidc" always has a matching OIDCProvider
		// row (Create writes both transactionally) — anything else here,
		// including "not found", is a genuine consistency error.
		return nil, err
	}

	return &OIDCProviderDetail{Provider: *provider, OIDC: *oidc}, nil
}
