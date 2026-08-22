package locks

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

	_ "github.com/jackc/pgx/v5/stdlib"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

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
// testDB (below) resets the locks table between individual tests, which is
// what actually gives each test isolation from the others.
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
// the locks table. Advisory locks are a Postgres-specific feature, so this
// package's correctness cannot be meaningfully verified against a mock.
func testDB(t *testing.T) *gorm.DB {
	t.Helper()

	if testDSNErr != nil {
		t.Skipf("postgres testcontainer unavailable (Docker required): %v", testDSNErr)
	}

	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	if err := db.AutoMigrate(&Lock{}); err != nil {
		t.Fatalf("failed to migrate locks table: %v", err)
	}

	if err := db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&Lock{}).Error; err != nil {
		t.Fatalf("failed to reset locks table: %v", err)
	}

	t.Cleanup(func() {
		db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&Lock{})
	})

	return db
}

func TestTryLock_AcquiresWhenFree(t *testing.T) {
	svc := NewService(testDB(t))

	lock, err := svc.TryLock(SystemLockNamespace, "res-a", time.Minute)
	if err != nil {
		t.Fatalf("TryLock returned error: %v", err)
	}
	if lock == nil {
		t.Fatal("expected TryLock to acquire a free lock, got nil")
	}
	if lock.Key == "" {
		t.Error("expected a non-empty lease key")
	}
}

func TestTryLock_FailsWhenHeld(t *testing.T) {
	svc := NewService(testDB(t))

	if _, err := svc.TryLock(SystemLockNamespace, "res-b", time.Minute); err != nil {
		t.Fatalf("first TryLock returned error: %v", err)
	}

	lock, err := svc.TryLock(SystemLockNamespace, "res-b", time.Minute)
	if err != nil {
		t.Fatalf("second TryLock returned error: %v", err)
	}
	if lock != nil {
		t.Fatal("expected second TryLock on an already-held lock to fail (nil, nil)")
	}
}

func TestTryLock_SucceedsAfterExpiry(t *testing.T) {
	svc := NewService(testDB(t))

	if _, err := svc.TryLock(SystemLockNamespace, "res-c", 50*time.Millisecond); err != nil {
		t.Fatalf("first TryLock returned error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	lock, err := svc.TryLock(SystemLockNamespace, "res-c", time.Minute)
	if err != nil {
		t.Fatalf("TryLock after expiry returned error: %v", err)
	}
	if lock == nil {
		t.Fatal("expected TryLock to acquire the lock once the previous lease expired")
	}
}

func TestUnlock_RemovesLock(t *testing.T) {
	svc := NewService(testDB(t))

	lock, err := svc.TryLock(SystemLockNamespace, "res-d", time.Minute)
	if err != nil || lock == nil {
		t.Fatalf("setup TryLock failed: lock=%v err=%v", lock, err)
	}

	if err := svc.Unlock(lock); err != nil {
		t.Fatalf("Unlock returned error: %v", err)
	}

	reacquired, err := svc.TryLock(SystemLockNamespace, "res-d", time.Minute)
	if err != nil {
		t.Fatalf("TryLock after Unlock returned error: %v", err)
	}
	if reacquired == nil {
		t.Fatal("expected TryLock to succeed immediately after Unlock")
	}
}

func TestUnlock_StaleKeyIsNoop(t *testing.T) {
	svc := NewService(testDB(t))

	staleLock, err := svc.TryLock(SystemLockNamespace, "res-e", 10*time.Millisecond)
	if err != nil || staleLock == nil {
		t.Fatalf("setup TryLock failed: lock=%v err=%v", staleLock, err)
	}

	time.Sleep(30 * time.Millisecond)

	freshLock, err := svc.TryLock(SystemLockNamespace, "res-e", time.Minute)
	if err != nil || freshLock == nil {
		t.Fatalf("re-acquire after expiry failed: lock=%v err=%v", freshLock, err)
	}

	// Unlocking with the stale (expired-and-superseded) lease must not
	// delete the newer lease that replaced it.
	if err := svc.Unlock(staleLock); err != nil {
		t.Fatalf("Unlock with stale key returned error: %v", err)
	}

	stillHeld, err := svc.TryLock(SystemLockNamespace, "res-e", time.Minute)
	if err != nil {
		t.Fatalf("TryLock returned error: %v", err)
	}
	if stillHeld != nil {
		t.Fatal("stale Unlock incorrectly removed the current lease")
	}
}

func TestRenew_ExtendsWhenOwned(t *testing.T) {
	svc := NewService(testDB(t))

	lock, err := svc.TryLock(SystemLockNamespace, "res-renew-a", 100*time.Millisecond)
	if err != nil || lock == nil {
		t.Fatalf("setup TryLock failed: lock=%v err=%v", lock, err)
	}
	originalExpiry := lock.ExpiresAt

	renewed, err := svc.Renew(lock, time.Minute)
	if err != nil {
		t.Fatalf("Renew returned error: %v", err)
	}
	if renewed == nil {
		t.Fatal("expected Renew to succeed for the current owner")
	}
	if !renewed.ExpiresAt.After(originalExpiry) {
		t.Errorf("expected Renew to push ExpiresAt forward, got %v (was %v)", renewed.ExpiresAt, originalExpiry)
	}

	// A stale TryLock attempt (i.e. someone else) must still fail — Renew
	// actually extended the real row, not just returned a fresh-looking
	// value without persisting it.
	if other, err := svc.TryLock(SystemLockNamespace, "res-renew-a", time.Minute); err != nil {
		t.Fatalf("TryLock returned error: %v", err)
	} else if other != nil {
		t.Fatal("expected the lease to still be held after Renew")
	}
}

func TestRenew_NoopWhenLeaseTakenOver(t *testing.T) {
	svc := NewService(testDB(t))

	staleLock, err := svc.TryLock(SystemLockNamespace, "res-renew-b", 10*time.Millisecond)
	if err != nil || staleLock == nil {
		t.Fatalf("setup TryLock failed: lock=%v err=%v", staleLock, err)
	}

	time.Sleep(30 * time.Millisecond)

	if _, err := svc.TryLock(SystemLockNamespace, "res-renew-b", time.Minute); err != nil {
		t.Fatalf("takeover TryLock returned error: %v", err)
	}

	renewed, err := svc.Renew(staleLock, time.Minute)
	if err != nil {
		t.Fatalf("Renew returned error: %v", err)
	}
	if renewed != nil {
		t.Fatal("expected Renew with a superseded key to return (nil, nil)")
	}
}

func TestNamespaces_AreIndependent(t *testing.T) {
	svc := NewService(testDB(t))

	const otherNamespace LockNamespace = 1

	if _, err := svc.TryLock(SystemLockNamespace, "res-f", time.Minute); err != nil {
		t.Fatalf("TryLock in namespace 0 returned error: %v", err)
	}

	lock, err := svc.TryLock(otherNamespace, "res-f", time.Minute)
	if err != nil {
		t.Fatalf("TryLock in namespace 1 returned error: %v", err)
	}
	if lock == nil {
		t.Fatal("expected the same id in a different namespace to lock independently")
	}
	if lock.Namespace != otherNamespace {
		t.Errorf("expected stored lock.Namespace == %d, got %d", otherNamespace, lock.Namespace)
	}
}

func TestLock_RetriesAndSucceedsAfterExpiry(t *testing.T) {
	svc := NewService(testDB(t))

	held, err := svc.TryLock(SystemLockNamespace, "res-g", 300*time.Millisecond)
	if err != nil || held == nil {
		t.Fatalf("setup TryLock failed: lock=%v err=%v", held, err)
	}

	start := time.Now()
	lock, err := svc.Lock(SystemLockNamespace, "res-g", time.Minute, 2*time.Second)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Lock returned error: %v", err)
	}
	if lock == nil {
		t.Fatal("expected Lock to eventually acquire the lease")
	}
	// Regression guard for the retry-loop bug: this must retry and succeed
	// promptly once the held lease expires, not after the full 2s timeout.
	if elapsed >= 2*time.Second {
		t.Errorf("expected Lock to retry promptly around the ~300ms lease expiry, took %v (== full timeout)", elapsed)
	}
	if elapsed < 250*time.Millisecond {
		t.Errorf("expected Lock to wait at least until the held lease expired, took only %v", elapsed)
	}
}

func TestLock_TimesOutWhenNeverReleased(t *testing.T) {
	svc := NewService(testDB(t))

	if _, err := svc.TryLock(SystemLockNamespace, "res-h", time.Minute); err != nil {
		t.Fatalf("setup TryLock failed: %v", err)
	}

	start := time.Now()
	lock, err := svc.Lock(SystemLockNamespace, "res-h", time.Minute, 200*time.Millisecond)
	elapsed := time.Since(start)

	if lock != nil {
		t.Fatal("expected Lock to fail while the lease is held for longer than the timeout")
	}
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed < 200*time.Millisecond {
		t.Errorf("expected Lock to wait out the full timeout before giving up, took only %v", elapsed)
	}
}

func TestRunExclusive_PropagatesFnResult(t *testing.T) {
	svc := NewService(testDB(t))

	err := svc.RunExclusive(SystemLockNamespace, func(tx *gorm.DB) error {
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error from a successful fn, got: %v", err)
	}

	sentinel := errors.New("fn failed")
	err = svc.RunExclusive(SystemLockNamespace, func(tx *gorm.DB) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected fn's own error to propagate, got: %v", err)
	}
}

func TestRunExclusive_SerializesConcurrentCallers(t *testing.T) {
	svc := NewService(testDB(t))

	const n = 5
	var mu sync.Mutex
	var order []int
	inside := make(chan struct{}, 1) // buffered 1: overflows (panics on send) if two callers are ever inside at once

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			err := svc.RunExclusive(SystemLockNamespace, func(tx *gorm.DB) error {
				select {
				case inside <- struct{}{}:
				default:
					t.Error("two RunExclusive callers were inside fn at the same time")
					return nil
				}
				time.Sleep(20 * time.Millisecond)
				<-inside

				mu.Lock()
				order = append(order, i)
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Errorf("RunExclusive: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if len(order) != n {
		t.Fatalf("expected all %d callers to complete, got %d", n, len(order))
	}
}
