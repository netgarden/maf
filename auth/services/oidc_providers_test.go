package services

import (
	"testing"

	"github.com/netgarden/maf/auth/dto"
	"github.com/netgarden/maf/security/encryption"
	uuid "github.com/satori/go.uuid"
	"gorm.io/gorm"
)

func newTestOIDCProvidersService(db *gorm.DB) *OIDCProvidersService {
	return NewOIDCProvidersService(db, encryption.NewManager("test-encryption-secret"))
}

func createDTO(slug string) *dto.OIDCProviderCreateDTO {
	return &dto.OIDCProviderCreateDTO{
		Slug:         slug,
		Name:         "Test IdP",
		IssuerURL:    "https://idp.example.com",
		ClientID:     "client-123",
		ClientSecret: "s3cret",
		Enabled:      true,
	}
}

func TestOIDCProvidersService_Create_EncryptsSecretAndDefaultsScopes(t *testing.T) {
	db := testDB(t)
	svc := newTestOIDCProvidersService(db)

	provider, err := svc.Create(createDTO("okta"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if provider == nil {
		t.Fatal("expected a created provider")
	}
	if provider.Type != providerTypeOIDC {
		t.Errorf("Type = %q, want %q", provider.Type, providerTypeOIDC)
	}
	if provider.OIDC.Scopes != defaultOIDCScopes {
		t.Errorf("Scopes = %q, want default %q", provider.OIDC.Scopes, defaultOIDCScopes)
	}
	if provider.OIDC.ProviderID != provider.ID {
		t.Errorf("OIDC.ProviderID = %s, want the generic provider's ID %s", provider.OIDC.ProviderID, provider.ID)
	}
	if string(provider.OIDC.ClientSecretEncrypted) == "s3cret" {
		t.Error("ClientSecretEncrypted must not be the plaintext secret")
	}

	plaintext, err := svc.DecryptClientSecret(&provider.OIDC)
	if err != nil {
		t.Fatalf("DecryptClientSecret: %v", err)
	}
	if plaintext != "s3cret" {
		t.Errorf("decrypted secret = %q, want %q", plaintext, "s3cret")
	}
}

func TestOIDCProvidersService_Create_DuplicateSlug_ReturnsNil(t *testing.T) {
	db := testDB(t)
	svc := newTestOIDCProvidersService(db)

	if _, err := svc.Create(createDTO("okta")); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	dup, err := svc.Create(createDTO("okta"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dup != nil {
		t.Error("expected nil for a duplicate slug")
	}
}

func TestOIDCProvidersService_GetBySlug(t *testing.T) {
	db := testDB(t)
	svc := newTestOIDCProvidersService(db)

	created, err := svc.Create(createDTO("okta"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := svc.GetBySlug("okta")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if found == nil || found.ID != created.ID {
		t.Fatalf("GetBySlug: want provider %s, got %+v", created.ID, found)
	}
	if found.OIDC.IssuerURL != created.OIDC.IssuerURL {
		t.Errorf("GetBySlug OIDC.IssuerURL = %q, want %q", found.OIDC.IssuerURL, created.OIDC.IssuerURL)
	}

	missing, err := svc.GetBySlug("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if missing != nil {
		t.Error("expected nil for an unknown slug")
	}
}

func TestOIDCProvidersService_ListEnabled_OnlyReturnsEnabledOIDCProviders(t *testing.T) {
	db := testDB(t)
	svc := newTestOIDCProvidersService(db)

	enabledDTO := createDTO("okta")
	disabledDTO := createDTO("legacy")
	disabledDTO.Enabled = false

	if _, err := svc.Create(enabledDTO); err != nil {
		t.Fatalf("Create enabled: %v", err)
	}
	if _, err := svc.Create(disabledDTO); err != nil {
		t.Fatalf("Create disabled: %v", err)
	}

	providers, err := svc.ListEnabled()
	if err != nil {
		t.Fatalf("ListEnabled: %v", err)
	}
	if len(providers) != 1 || providers[0].Slug != "okta" {
		t.Errorf("ListEnabled = %+v, want only the enabled provider", providers)
	}
}

func TestOIDCProvidersService_Update_BlankSecretKeepsExisting(t *testing.T) {
	db := testDB(t)
	svc := newTestOIDCProvidersService(db)

	provider, err := svc.Create(createDTO("okta"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Update(provider.ID.String(), &dto.OIDCProviderUpdateDTO{
		Name:      "Renamed IdP",
		IssuerURL: provider.OIDC.IssuerURL,
		ClientID:  provider.OIDC.ClientID,
		// ClientSecret intentionally left blank.
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "Renamed IdP" {
		t.Errorf("Name = %q, want %q", updated.Name, "Renamed IdP")
	}

	plaintext, err := svc.DecryptClientSecret(&updated.OIDC)
	if err != nil {
		t.Fatalf("DecryptClientSecret: %v", err)
	}
	if plaintext != "s3cret" {
		t.Errorf("secret should be unchanged by a blank update, got %q", plaintext)
	}
}

func TestOIDCProvidersService_Update_NonBlankSecretRotatesIt(t *testing.T) {
	db := testDB(t)
	svc := newTestOIDCProvidersService(db)

	provider, err := svc.Create(createDTO("okta"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Update(provider.ID.String(), &dto.OIDCProviderUpdateDTO{
		Name:         provider.Name,
		IssuerURL:    provider.OIDC.IssuerURL,
		ClientID:     provider.OIDC.ClientID,
		ClientSecret: "new-secret",
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	plaintext, err := svc.DecryptClientSecret(&updated.OIDC)
	if err != nil {
		t.Fatalf("DecryptClientSecret: %v", err)
	}
	if plaintext != "new-secret" {
		t.Errorf("secret = %q, want rotated value %q", plaintext, "new-secret")
	}
}

func TestOIDCProvidersService_Update_UnknownID_ReturnsNil(t *testing.T) {
	db := testDB(t)
	svc := newTestOIDCProvidersService(db)

	updated, err := svc.Update(uuid.NewV4().String(), &dto.OIDCProviderUpdateDTO{Name: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated != nil {
		t.Error("expected nil for an unknown provider id")
	}
}
