package services

import (
	"os"
	"sync"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/locks"
	"github.com/netgarden/maf/security/passwords"
)

// testDB connects to a real Postgres database and migrates the tables
// EnsureAdminExists needs. Its concurrency guarantee (a Postgres advisory
// lock, via locks.Service) can't be meaningfully verified against a mock —
// see README.md (or set MAF_AUTH_TEST_DSN directly) for how to run these.
func testDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("MAF_AUTH_TEST_DSN")
	if dsn == "" {
		t.Skip("MAF_AUTH_TEST_DSN not set; skipping auth integration tests, e.g.:\n" +
			`MAF_AUTH_TEST_DSN="host=localhost user=citadel password=citadel dbname=maf_auth_test port=5432 sslmode=disable" go test ./...`)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	if err := db.AutoMigrate(&entities.User{}, &locks.Lock{}); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}

	reset := func() {
		db.Exec("DELETE FROM auth_users")
		db.Exec("DELETE FROM locks")
	}
	reset()
	t.Cleanup(reset)

	return db
}

func newTestUsersService(t *testing.T, db *gorm.DB) *UsersService {
	t.Helper()
	return NewUsersService(db, passwords.NewManager(), locks.NewService(db))
}

func TestEnsureAdminExists_CreatesWhenNoneExists(t *testing.T) {
	db := testDB(t)
	svc := newTestUsersService(t, db)

	if err := svc.EnsureAdminExists("admin"); err != nil {
		t.Fatalf("EnsureAdminExists: %v", err)
	}

	admin, err := svc.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if admin == nil {
		t.Fatal("expected a default admin user to have been created")
	}
	if !admin.Admin {
		t.Error("expected the created user to have Admin == true")
	}
	if !admin.Active {
		t.Error("expected the created user to have Active == true")
	}
}

func TestEnsureAdminExists_NoopWhenAdminAlreadyExists(t *testing.T) {
	db := testDB(t)
	svc := newTestUsersService(t, db)

	// An admin under a *different* username should already satisfy "an
	// admin exists" — EnsureAdminExists must not create a second one.
	existing := &entities.User{Username: "root", Admin: true, Active: true}
	if err := db.Create(existing).Error; err != nil {
		t.Fatalf("failed to seed existing admin: %v", err)
	}

	if err := svc.EnsureAdminExists("admin"); err != nil {
		t.Fatalf("EnsureAdminExists: %v", err)
	}

	created, err := svc.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if created != nil {
		t.Error("expected no default \"admin\" user to be created when an admin already exists")
	}
}

// TestEnsureAdminExists_ConcurrentReplicasCreateExactlyOneAdmin is the
// literal regression test for "no race condition when two replicas start
// at the same time": many goroutines, each with its own DB connection
// (standing in for separate replicas), call EnsureAdminExists concurrently
// against an empty table. Exactly one admin user must exist afterward.
func TestEnsureAdminExists_ConcurrentReplicasCreateExactlyOneAdmin(t *testing.T) {
	db := testDB(t)
	dsn := os.Getenv("MAF_AUTH_TEST_DSN")

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()

			// A separate *gorm.DB (and so a separate connection) per
			// goroutine, so this genuinely exercises cross-connection
			// serialization rather than relying on Go-level mutexes that
			// wouldn't exist across real, separate replica processes.
			replicaDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				errs[i] = err
				return
			}
			svc := newTestUsersService(t, replicaDB)
			errs[i] = svc.EnsureAdminExists("admin")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("replica %d: EnsureAdminExists returned error: %v", i, err)
		}
	}

	var count int64
	if err := db.Model(&entities.User{}).Where("admin = ?", true).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 admin user after %d concurrent replicas raced to create it, got %d", n, count)
	}
}

func TestEnsureAdminExists_UsesConfiguredPassword(t *testing.T) {
	db := testDB(t)
	pm := passwords.NewManager()
	svc := NewUsersService(db, pm, locks.NewService(db))

	if err := svc.EnsureAdminExists("s3cr3t"); err != nil {
		t.Fatalf("EnsureAdminExists: %v", err)
	}

	admin, err := svc.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if admin == nil {
		t.Fatal("expected the admin user to exist")
	}
	ok, err := pm.Verify(admin.Password, "s3cr3t")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Error("expected the stored password hash to verify against the configured default password")
	}
}
