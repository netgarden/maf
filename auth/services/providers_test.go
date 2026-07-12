package services

import (
	"errors"
	"testing"

	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/datatables"
	uuid "github.com/satori/go.uuid"
	"gorm.io/gorm"
)

func TestProvidersService_ListPage_ListsEveryType(t *testing.T) {
	db := testDB(t)
	oidcSvc := newTestOIDCProvidersService(db)
	providersSvc := NewProvidersService(db)

	if _, err := oidcSvc.Create(createDTO("okta")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	result, err := providersSvc.ListPage(datatables.Query{})
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if result.TotalCount != 1 || len(result.Items) != 1 {
		t.Fatalf("ListPage = %+v, want exactly one provider", result)
	}
	if result.Items[0].Slug != "okta" || result.Items[0].Type != providerTypeOIDC {
		t.Errorf("unexpected provider in list: %+v", result.Items[0])
	}
}

func TestProvidersService_ListEnabled(t *testing.T) {
	db := testDB(t)
	oidcSvc := newTestOIDCProvidersService(db)
	providersSvc := NewProvidersService(db)

	enabledDTO := createDTO("okta")
	disabledDTO := createDTO("legacy")
	disabledDTO.Enabled = false

	if _, err := oidcSvc.Create(enabledDTO); err != nil {
		t.Fatalf("Create enabled: %v", err)
	}
	if _, err := oidcSvc.Create(disabledDTO); err != nil {
		t.Fatalf("Create disabled: %v", err)
	}

	providers, err := providersSvc.ListEnabled()
	if err != nil {
		t.Fatalf("ListEnabled: %v", err)
	}
	if len(providers) != 1 || providers[0].Slug != "okta" {
		t.Errorf("ListEnabled = %+v, want only the enabled provider", providers)
	}
}

func TestProvidersService_SetEnabled(t *testing.T) {
	db := testDB(t)
	oidcSvc := newTestOIDCProvidersService(db)
	providersSvc := NewProvidersService(db)

	createReq := createDTO("okta")
	createReq.Enabled = true
	created, err := oidcSvc.Create(createReq)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := providersSvc.SetEnabled(created.ID.String(), false)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if !found {
		t.Fatal("expected SetEnabled to report found=true")
	}

	got, err := providersSvc.Get(created.ID.String())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Enabled {
		t.Error("expected Enabled to be false after SetEnabled(false)")
	}
}

func TestProvidersService_SetEnabled_UnknownID_ReturnsFalse(t *testing.T) {
	db := testDB(t)
	providersSvc := NewProvidersService(db)

	found, err := providersSvc.SetEnabled(uuid.NewV4().String(), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected found=false for an unknown id")
	}
}

// TestProvidersService_Delete_CascadesOIDCProviderAndUserIdentity is the
// one place that actually exercises the DB-level ON DELETE CASCADE foreign
// key from auth_oidc_providers to auth_providers (see
// entities.OIDCProvider's doc comment) — every other test only asserts
// through the Go API, which wouldn't catch a migration that failed to
// create the constraint at all.
func TestProvidersService_Delete_CascadesOIDCProviderAndUserIdentity(t *testing.T) {
	db := testDB(t)
	oidcSvc := newTestOIDCProvidersService(db)
	providersSvc := NewProvidersService(db)
	identities := NewUserIdentitiesService(db)

	provider, err := oidcSvc.Create(createDTO("okta"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := identities.Create(uuid.NewV4(), providerTypeOIDC, provider.ID, "sub-1", "a@example.com"); err != nil {
		t.Fatalf("identities.Create: %v", err)
	}

	deleted, err := providersSvc.Delete(provider.ID.String())
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !deleted {
		t.Fatal("expected Delete to report deleted=true")
	}

	stillThere, err := providersSvc.Get(provider.ID.String())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stillThere != nil {
		t.Error("expected the generic Provider row to be gone")
	}

	err = db.Where("provider_id = ?", provider.ID).First(&entities.OIDCProvider{}).Error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("expected the OIDCProvider row to be cascade-deleted by the DB, got err=%v", err)
	}

	var identityCount int64
	if err := db.Model(&entities.UserIdentity{}).Where("provider_id = ?", provider.ID).Count(&identityCount).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if identityCount != 0 {
		t.Errorf("expected linked UserIdentity rows to be cleaned up, found %d remaining", identityCount)
	}
}

func TestProvidersService_Delete_UnknownID_ReturnsFalse(t *testing.T) {
	db := testDB(t)
	providersSvc := NewProvidersService(db)

	deleted, err := providersSvc.Delete(uuid.NewV4().String())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted {
		t.Error("expected deleted=false for an unknown id")
	}
}
