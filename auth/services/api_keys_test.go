package services

import (
	"testing"
	"time"

	"github.com/netgarden/maf/auth/entities"
	uuid "github.com/satori/go.uuid"
	"gorm.io/gorm"
)

// seedApiKeyUser creates a real auth_users row (ApiKey.UserID carries a DB
// foreign key into it) and returns its actual, DB-assigned id —
// EntityBase.BeforeCreate always overwrites whatever ID a caller sets, so
// an id has to be read back after Create rather than picked beforehand.
func seedApiKeyUser(t *testing.T, db *gorm.DB, username string) uuid.UUID {
	t.Helper()
	user := &entities.User{Username: username, Active: true}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("seed user %q: %v", username, err)
	}
	return user.ID
}

func TestApiKeysService_CreateAndFindByHash(t *testing.T) {
	db := testDB(t)
	svc := NewApiKeysService(db)
	userID := seedApiKeyUser(t, db, "alice")

	plaintext, key, err := svc.Create(userID, "my key", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if plaintext == "" {
		t.Fatal("expected a non-empty plaintext token")
	}
	if key.TokenHash == "" || key.TokenHash == plaintext {
		t.Errorf("TokenHash must be a hash, not the plaintext itself: %q", key.TokenHash)
	}
	if key.Prefix == "" {
		t.Error("expected a non-empty display prefix")
	}

	found, err := svc.FindByHash(hashApiKey(plaintext))
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find the key by its hash")
	}
	if found.User == nil || found.User.Username != "alice" {
		t.Errorf("expected FindByHash to preload the owning User, got %+v", found.User)
	}

	if unknown, err := svc.FindByHash("not-a-real-hash"); err != nil || unknown != nil {
		t.Errorf("FindByHash for an unknown hash: got (%v, %v), want (nil, nil)", unknown, err)
	}
}

func TestApiKeysService_List_ScopedToOwner(t *testing.T) {
	db := testDB(t)
	svc := NewApiKeysService(db)
	alice, bob := seedApiKeyUser(t, db, "alice"), seedApiKeyUser(t, db, "bob")
	if _, _, err := svc.Create(alice, "alice's key", nil, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, _, err := svc.Create(bob, "bob's key", nil, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}

	aliceKeys, err := svc.List(alice)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(aliceKeys) != 1 || aliceKeys[0].Name != "alice's key" {
		t.Errorf("alice's keys = %+v, want exactly her own", aliceKeys)
	}
}

func TestApiKeysService_Revoke_OnlyOwnerCan(t *testing.T) {
	db := testDB(t)
	svc := NewApiKeysService(db)
	alice, bob := seedApiKeyUser(t, db, "alice"), seedApiKeyUser(t, db, "bob")
	_, key, err := svc.Create(alice, "alice's key", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if ok, err := svc.Revoke(bob, key.ID); err != nil || ok {
		t.Errorf("bob revoking alice's key: got (%v, %v), want (false, nil)", ok, err)
	}
	still, err := svc.FindByHash(key.TokenHash)
	if err != nil || still == nil {
		t.Fatal("the key must still exist after another user's failed revoke attempt")
	}

	if ok, err := svc.Revoke(alice, key.ID); err != nil || !ok {
		t.Fatalf("alice revoking her own key: got (%v, %v), want (true, nil)", ok, err)
	}
	gone, err := svc.FindByHash(key.TokenHash)
	if err != nil || gone != nil {
		t.Error("the key must be gone after being revoked")
	}
}

func TestApiKeysService_Create_NormalisesAllowedIPs(t *testing.T) {
	db := testDB(t)
	svc := NewApiKeysService(db)
	userID := seedApiKeyUser(t, db, "alice")

	_, key, err := svc.Create(userID, "restricted", nil, []string{"203.0.113.9"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(key.AllowedIPs) != 1 || key.AllowedIPs[0] != "203.0.113.9/32" {
		t.Errorf("AllowedIPs = %v, want a bare IP normalised to /32", key.AllowedIPs)
	}
}

func TestApiKeysService_Create_RejectsGarbageAllowedIPs(t *testing.T) {
	db := testDB(t)
	svc := NewApiKeysService(db)
	userID := seedApiKeyUser(t, db, "alice")

	if _, _, err := svc.Create(userID, "bad", nil, []string{"not-an-ip"}); err == nil {
		t.Fatal("expected an error for an unparseable AllowedIPs entry")
	}
}

func TestApiKeysService_TouchLastUsed(t *testing.T) {
	db := testDB(t)
	svc := NewApiKeysService(db)
	userID := seedApiKeyUser(t, db, "alice")
	_, key, err := svc.Create(userID, "k", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if key.LastUsedAt != nil {
		t.Fatal("a fresh key must have no LastUsedAt")
	}

	before := time.Now()
	if err := svc.TouchLastUsed(key.ID); err != nil {
		t.Fatalf("TouchLastUsed: %v", err)
	}

	found, err := svc.FindByHash(key.TokenHash)
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}
	if found.LastUsedAt == nil || found.LastUsedAt.Before(before) {
		t.Errorf("LastUsedAt = %v, want a timestamp at or after %v", found.LastUsedAt, before)
	}
}
