package locks

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// testDB connects to a real Postgres database and migrates the locks table.
// Advisory locks are a Postgres-specific feature, so this package's
// correctness cannot be meaningfully verified against a mock — set
// MAF_LOCKS_TEST_DSN to run these tests, e.g.:
//
//	MAF_LOCKS_TEST_DSN="host=localhost user=citadel password=citadel dbname=maf_locks_test port=5432 sslmode=disable" go test ./...
func testDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("MAF_LOCKS_TEST_DSN")
	if dsn == "" {
		t.Skip("MAF_LOCKS_TEST_DSN not set; skipping locks integration tests (see README.md)")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
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
