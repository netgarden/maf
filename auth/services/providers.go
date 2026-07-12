package services

import (
	"errors"

	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/datatables"
	"gorm.io/gorm"
)

func NewProvidersService(db *gorm.DB) *ProvidersService {
	return &ProvidersService{db: db}
}

// ProvidersService is the generic, protocol-agnostic half of provider
// management: list every registered provider regardless of type, and the
// handful of actions that make sense for any of them (enable/disable,
// delete). Protocol-specific setup (an OIDC issuer/client/secret, a future
// SAML/LDAP equivalent) is a separate *XxxProvidersService that owns its
// own table (see OIDCProvidersService) — this is deliberately the one
// place a new provider type doesn't need its own list/enable/disable/
// delete implementation.
type ProvidersService struct {
	db *gorm.DB
}

var providerColumns = []datatables.Column{
	{Name: "type", DBColumn: "type", Sortable: true, FilterOperator: datatables.Eq},
	{Name: "slug", DBColumn: "slug", Sortable: true, FilterOperator: datatables.Contains},
	{Name: "name", DBColumn: "name", Sortable: true, FilterOperator: datatables.Contains},
	{Name: "enabled", DBColumn: "enabled", Sortable: true},
}

// ListPage returns a filtered, sorted, paginated page of every provider
// (any type), for the admin CRUD UI — same shape as UsersService.ListUsersPage.
func (s *ProvidersService) ListPage(q datatables.Query) (*datatables.Result[*entities.Provider], error) {
	db := s.db.Model(&entities.Provider{})
	if q.SortBy == "" {
		db = db.Order("name")
	}
	return datatables.Apply[*entities.Provider](db, providerColumns, q)
}

// ListEnabled returns every Enabled provider of any type — the generic
// building block a public "pick a login method" endpoint can use once more
// than one protocol is registered; see impl.OIDCAuthServiceImpl.ListProviders
// for the OIDC-only endpoint that exists today.
func (s *ProvidersService) ListEnabled() ([]*entities.Provider, error) {
	var providers []*entities.Provider
	err := s.db.Where("enabled = ?", true).Order("name").Find(&providers).Error
	return providers, err
}

func (s *ProvidersService) Get(id string) (*entities.Provider, error) {
	return s.getProvider(s.db, "id", id)
}

func (s *ProvidersService) GetBySlug(slug string) (*entities.Provider, error) {
	return s.getProvider(s.db, "slug", slug)
}

// SetEnabled toggles a provider on/off without touching any protocol-
// specific configuration — the one action every provider type shares that
// isn't a full edit. Returns (false, nil) when id doesn't exist.
func (s *ProvidersService) SetEnabled(id string, enabled bool) (bool, error) {
	result := s.db.Model(&entities.Provider{}).Where("id = ?", id).Update("enabled", enabled)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// Delete removes a provider and everything that references it. The
// protocol-specific row (e.g. entities.OIDCProvider) is cleaned up by
// Postgres itself via ON DELETE CASCADE on its foreign key — see that
// type's doc comment; UserIdentity has no such FK by design, so it's
// deleted here explicitly, transactionally with the provider row itself.
// Returns (false, nil) when id doesn't exist.
func (s *ProvidersService) Delete(id string) (bool, error) {
	var deleted bool
	err := s.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ?", id).Delete(&entities.Provider{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		deleted = true
		return tx.Where("provider_id = ?", id).Delete(&entities.UserIdentity{}).Error
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}

func (*ProvidersService) getProvider(db *gorm.DB, field, value string) (*entities.Provider, error) {
	provider := &entities.Provider{}
	err := db.First(provider, field+" = ?", value).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return provider, nil
}
