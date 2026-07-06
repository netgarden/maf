package services

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/netgarden/maf/auth/dto"
	"github.com/netgarden/maf/auth/entities"
	"github.com/netgarden/maf/locks"
	"github.com/netgarden/maf/security/passwords"

	_ "github.com/jackc/pgx/v5/stdlib"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// fakeCredentialsMailer records SendTemplate calls in-memory instead of
// touching a real mailer — CreateUser only depends on the narrow
// CredentialsMailer interface, so no real maf/mailer.Service is needed to
// test the wiring.
type fakeCredentialsMailer struct {
	calls []fakeCredentialsMailerCall
	err   error
}

type fakeCredentialsMailerCall struct {
	templateID  string
	to, cc, bcc []string
	data        any
}

func (f *fakeCredentialsMailer) SendTemplate(templateID string, to, cc, bcc []string, data any) error {
	f.calls = append(f.calls, fakeCredentialsMailerCall{templateID: templateID, to: to, cc: cc, bcc: bcc, data: data})
	return f.err
}

// testDSN/testDSNErr are set once by TestMain, before any test in this
// package runs — see TestMain for why the container itself is started
// there rather than per-test.
var (
	testDSN    string
	testDSNErr error
)

// TestMain starts a single disposable Postgres container for this
// package's entire test run (instead of requiring a human to have already
// run `createdb`/started a long-lived Postgres beforehand) and tears it
// down once, after every test has finished — cheaper than a
// container-per-test and matches this suite's previous "one long-lived
// dev Postgres shared by the whole run" performance characteristics.
// testDB (below) resets its tables between individual tests, which is what
// actually gives each test isolation from the others.
func TestMain(m *testing.M) {
	ctx := context.Background()

	pg, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
	)
	if err != nil {
		// Docker unavailable (or some other startup failure) — record it so
		// testDB can t.Skip each test individually instead of aborting the
		// whole binary.
		testDSNErr = err
		os.Exit(m.Run())
	}

	testDSN, testDSNErr = pg.ConnectionString(ctx, "sslmode=disable")
	if testDSNErr == nil {
		testDSNErr = waitForPostgresReady(testDSN)
	}

	code := m.Run()
	_ = pg.Terminate(ctx)
	os.Exit(code)
}

// waitForPostgresReady retries a plain ping for a few seconds — the
// container's own readiness signal (log line + port check) fires slightly
// before the mapped port reliably accepts connections in this environment,
// so the first real connection attempt can otherwise land in that gap.
func waitForPostgresReady(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	deadline := time.Now().Add(20 * time.Second)
	var pingErr error
	for time.Now().Before(deadline) {
		if pingErr = db.Ping(); pingErr == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return pingErr
}

// testDB connects to the Postgres container TestMain started and migrates
// the tables EnsureAdminExists needs. Its concurrency guarantee (a
// Postgres advisory lock, via locks.Service) can't be meaningfully
// verified against a mock, hence a real database rather than one.
func testDB(t *testing.T) *gorm.DB {
	t.Helper()

	if testDSNErr != nil {
		t.Skipf("postgres testcontainer unavailable (Docker required): %v", testDSNErr)
	}

	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	if err := db.AutoMigrate(&entities.User{}, &locks.Lock{}); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}

	reset := func() {
		db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&entities.User{})
		db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&locks.Lock{})
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
			replicaDB, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
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

func TestCreateUser_SendCredentialsEmail_SendsTemplate(t *testing.T) {
	db := testDB(t)
	svc := newTestUsersService(t, db)
	mailer := &fakeCredentialsMailer{}
	svc.SetMailer(mailer, "https://example.com/login")

	created, err := svc.CreateUser(&dto.UserCreateDTO{
		Username:             "alice",
		Password:             "s3cr3t",
		Email:                "alice@example.com",
		SendCredentialsEmail: true,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created == nil {
		t.Fatal("expected the user to be created")
	}

	if len(mailer.calls) != 1 {
		t.Fatalf("expected exactly 1 SendTemplate call, got %d", len(mailer.calls))
	}
	call := mailer.calls[0]
	if call.templateID != NewUserCredentialsTemplateID {
		t.Errorf("templateID = %q, want %q", call.templateID, NewUserCredentialsTemplateID)
	}
	if len(call.to) != 1 || call.to[0] != "alice@example.com" {
		t.Errorf("to = %v, want [alice@example.com]", call.to)
	}
	data, ok := call.data.(NewUserCredentialsData)
	if !ok {
		t.Fatalf("data = %#v, want NewUserCredentialsData", call.data)
	}
	if data.Username != "alice" || data.TemporaryPassword != "s3cr3t" || data.LoginURL != "https://example.com/login" {
		t.Errorf("unexpected template data: %#v", data)
	}
}

// A CreateUser call without the flag set (the default) must never touch
// the mailer, whether or not one is wired — this is what makes the
// feature opt-in per request rather than "on whenever a mailer exists."
func TestCreateUser_WithoutSendCredentialsEmail_DoesNotSend(t *testing.T) {
	db := testDB(t)
	svc := newTestUsersService(t, db)
	mailer := &fakeCredentialsMailer{}
	svc.SetMailer(mailer, "")

	if _, err := svc.CreateUser(&dto.UserCreateDTO{
		Username: "bob",
		Password: "s3cr3t",
		Email:    "bob@example.com",
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if len(mailer.calls) != 0 {
		t.Errorf("expected no SendTemplate calls, got %d", len(mailer.calls))
	}
}

// A nil CredentialsMailer (the zero value — an app that never registers a
// "mailer" module) must make SendCredentialsEmail a silent no-op rather
// than a nil-pointer panic.
func TestCreateUser_SendCredentialsEmail_NilMailerIsNoop(t *testing.T) {
	db := testDB(t)
	svc := newTestUsersService(t, db)

	created, err := svc.CreateUser(&dto.UserCreateDTO{
		Username:             "carol",
		Password:             "s3cr3t",
		Email:                "carol@example.com",
		SendCredentialsEmail: true,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created == nil {
		t.Fatal("expected the user to be created")
	}
}

// A failed send must not undo the already-created user — the email is
// a best-effort notification, not part of the user's own correctness.
func TestCreateUser_SendCredentialsEmail_SendErrorDoesNotFailCreate(t *testing.T) {
	db := testDB(t)
	svc := newTestUsersService(t, db)
	mailer := &fakeCredentialsMailer{err: errors.New("mailer unavailable")}
	svc.SetMailer(mailer, "")

	created, err := svc.CreateUser(&dto.UserCreateDTO{
		Username:             "dave",
		Password:             "s3cr3t",
		Email:                "dave@example.com",
		SendCredentialsEmail: true,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created == nil {
		t.Fatal("expected the user to still be created despite the send error")
	}

	found, err := svc.GetUserByUsername("dave")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if found == nil {
		t.Error("expected the user to be persisted despite the send error")
	}
}
