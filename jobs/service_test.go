package jobs

import (
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/netgarden/maf/locks"
)

// testDB connects to a real Postgres database and migrates the jobs and
// locks tables. See README.md for what MAF_JOBS_TEST_DSN should point at.
func testDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("MAF_JOBS_TEST_DSN")
	if dsn == "" {
		t.Skip("MAF_JOBS_TEST_DSN not set; skipping jobs integration tests (see README.md)")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	if err := db.AutoMigrate(&Job{}, &locks.Lock{}); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}

	if err := db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&Job{}).Error; err != nil {
		t.Fatalf("failed to reset jobs table: %v", err)
	}
	if err := db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&locks.Lock{}).Error; err != nil {
		t.Fatalf("failed to reset locks table: %v", err)
	}

	t.Cleanup(func() {
		db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&Job{})
		db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&locks.Lock{})
	})

	return db
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	db := testDB(t)
	svc := NewService(db, locks.NewService(db))
	t.Cleanup(svc.Stop)
	return svc
}

func TestAddJob_GlobalJob_EmptyTargetID(t *testing.T) {
	svc := newTestService(t)

	job, err := svc.AddJob("h1", "", "Global Job", 60, 30)
	if err != nil {
		t.Fatalf("AddJob returned error: %v", err)
	}
	if job == nil || job.TargetID != nil {
		t.Fatalf("expected a job with nil TargetID, got %+v", job)
	}
}

func TestAddJob_WithTargetID(t *testing.T) {
	// Regression test for the getJobByTarget bug: db.Where("target_id = ?")
	// with no bound argument produced a SQL syntax error for every
	// non-empty targetID, which is the common case (e.g. one job per
	// provider/tenant/resource).
	svc := newTestService(t)

	job, err := svc.AddJob("h1", "target-123", "Per-target Job", 60, 30)
	if err != nil {
		t.Fatalf("AddJob with a non-empty targetID returned error: %v", err)
	}
	if job == nil || job.TargetID == nil || *job.TargetID != "target-123" {
		t.Fatalf("expected job.TargetID == \"target-123\", got %+v", job)
	}
}

func TestAddJob_Idempotent(t *testing.T) {
	svc := newTestService(t)

	first, err := svc.AddJob("h1", "target-123", "Label A", 60, 30)
	if err != nil {
		t.Fatalf("first AddJob returned error: %v", err)
	}

	second, err := svc.AddJob("h1", "target-123", "Label B", 60, 30)
	if err != nil {
		t.Fatalf("second AddJob returned error: %v", err)
	}
	if second.ID != first.ID {
		t.Fatal("expected AddJob to return the existing job rather than creating a duplicate")
	}
	if second.Label != "Label A" {
		t.Fatal("expected AddJob not to modify an already-existing job")
	}
}

func TestAddJob_RejectsNonPositiveInterval(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.AddJob("h1", "", "Bad Job", 0, 30); err == nil {
		t.Fatal("expected AddJob to reject a zero interval")
	}
	if _, err := svc.AddJob("h1", "", "Bad Job", -5, 30); err == nil {
		t.Fatal("expected AddJob to reject a negative interval")
	}
}

func TestUpdateJob_WithTargetID(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.AddJob("h1", "target-123", "Original", 60, 30); err != nil {
		t.Fatalf("AddJob returned error: %v", err)
	}

	updated, err := svc.UpdateJob("h1", "target-123", "Updated", 120, 60)
	if err != nil {
		t.Fatalf("UpdateJob with a non-empty targetID returned error: %v", err)
	}
	if updated.Label != "Updated" || updated.Interval != 120 || updated.Timeout != 60 {
		t.Fatalf("expected fields to be updated, got %+v", updated)
	}

	// Confirm the write actually landed in the DB (regression check for
	// UpdateJob writing through a different, non-transactional connection
	// than the one that read/locked the row).
	fresh, err := svc.GetJob(updated.ID.String())
	if err != nil {
		t.Fatalf("GetJob returned error: %v", err)
	}
	if fresh.Label != "Updated" || fresh.Interval != 120 {
		t.Fatalf("expected the update to be persisted, got %+v", fresh)
	}
}

func TestUpdateJob_NotFound(t *testing.T) {
	svc := newTestService(t)

	if _, err := svc.UpdateJob("h1", "does-not-exist", "X", 60, 30); err == nil {
		t.Fatal("expected UpdateJob to fail for a job that doesn't exist")
	}
}

func TestRemoveJob_WithTargetID(t *testing.T) {
	// Regression test: RemoveJob previously read and deleted via a
	// different connection than the one holding the coordination lock,
	// and hit the same getJobByTarget SQL bug for non-empty targetIDs.
	svc := newTestService(t)

	job, err := svc.AddJob("h1", "target-123", "To Remove", 60, 30)
	if err != nil {
		t.Fatalf("AddJob returned error: %v", err)
	}

	if err := svc.RemoveJob("h1", "target-123"); err != nil {
		t.Fatalf("RemoveJob returned error: %v", err)
	}

	gone, err := svc.GetJob(job.ID.String())
	if err != nil {
		t.Fatalf("GetJob returned error: %v", err)
	}
	if gone != nil {
		t.Fatal("expected the job to be deleted from the database")
	}
}

func TestRemoveJob_NotFoundIsNoop(t *testing.T) {
	svc := newTestService(t)

	if err := svc.RemoveJob("h1", "does-not-exist"); err != nil {
		t.Fatalf("expected removing a non-existent job to be a no-op, got error: %v", err)
	}
}

func TestRunJob_LockLeaseUsesTimeoutNotInterval(t *testing.T) {
	// Regression test: the lock lease taken while a job runs must be based
	// on job.Timeout (the max expected run duration), not job.Interval
	// (how often it repeats) — otherwise a crashed instance's lock can
	// outlive the job's own timeout by a large margin.
	svc := newTestService(t)

	job, err := svc.AddJob("h1", "", "Timeout Lease Job", 3600, 1)
	if err != nil {
		t.Fatalf("AddJob returned error: %v", err)
	}

	res, err := svc.lockJob(job.ID.String())
	if err != nil {
		t.Fatalf("lockJob returned error: %v", err)
	}
	if res.state != JobStateOK || res.lock == nil {
		t.Fatalf("expected to acquire the job lock, got state=%v", res.state)
	}

	leaseLength := res.lock.ExpiresAt.Sub(res.lock.CreatedAt)
	if leaseLength > 2*time.Second {
		t.Fatalf("expected the lock lease to be based on the 1s timeout, got a %v lease", leaseLength)
	}
}

func TestLockJob_RunningWhenAlreadyLocked(t *testing.T) {
	svc := newTestService(t)

	// JobStateRunning is only reachable when Timeout > Interval — see the
	// doc comment on JobStateRunning for why: lockJob checks "was this
	// started too recently" (Interval-based) before "is it locked right
	// now" (Timeout-based lease).
	job, err := svc.AddJob("h1", "", "Contended Job", 1, 60)
	if err != nil {
		t.Fatalf("AddJob returned error: %v", err)
	}

	first, err := svc.lockJob(job.ID.String())
	if err != nil || first.state != JobStateOK {
		t.Fatalf("expected first lockJob to succeed, got state=%v err=%v", first.state, err)
	}

	time.Sleep(1100 * time.Millisecond)

	second, err := svc.lockJob(job.ID.String())
	if err != nil {
		t.Fatalf("second lockJob returned error: %v", err)
	}
	if second.state != JobStateRunning {
		t.Fatalf("expected second lockJob to report JobStateRunning, got %v", second.state)
	}
}

func TestLockJob_UpdateWhenRunTooRecent(t *testing.T) {
	svc := newTestService(t)

	job, err := svc.AddJob("h1", "", "Frequent Job", 3600, 60)
	if err != nil {
		t.Fatalf("AddJob returned error: %v", err)
	}

	first, err := svc.lockJob(job.ID.String())
	if err != nil || first.state != JobStateOK {
		t.Fatalf("expected first lockJob to succeed, got state=%v err=%v", first.state, err)
	}
	if err := svc.unlockJob(job.ID.String(), nil, first.lock); err != nil {
		t.Fatalf("unlockJob returned error: %v", err)
	}

	// The job just ran and its interval (3600s) hasn't elapsed, so a
	// second attempt should report JobStateUpdate rather than running it
	// again immediately.
	second, err := svc.lockJob(job.ID.String())
	if err != nil {
		t.Fatalf("second lockJob returned error: %v", err)
	}
	if second.state != JobStateUpdate {
		t.Fatalf("expected JobStateUpdate, got %v", second.state)
	}
}

func TestUnlockJob_RecordsSuccessAndFailure(t *testing.T) {
	svc := newTestService(t)

	job, err := svc.AddJob("h1", "", "Result Job", 60, 60)
	if err != nil {
		t.Fatalf("AddJob returned error: %v", err)
	}

	locked, err := svc.lockJob(job.ID.String())
	if err != nil || locked.state != JobStateOK {
		t.Fatalf("expected lockJob to succeed, got state=%v err=%v", locked.state, err)
	}

	if err := svc.unlockJob(job.ID.String(), nil, locked.lock); err != nil {
		t.Fatalf("unlockJob (success) returned error: %v", err)
	}

	after, err := svc.GetJob(job.ID.String())
	if err != nil {
		t.Fatalf("GetJob returned error: %v", err)
	}
	if after.LastRunEnd == nil {
		t.Fatal("expected LastRunEnd to be set after unlockJob")
	}
	if after.LastRunError != nil {
		t.Fatalf("expected no error recorded, got %q", *after.LastRunError)
	}
	if after.LastSuccessfulRunEnd == nil {
		t.Fatal("expected LastSuccessfulRunEnd to be set for a successful run")
	}

	// A separate job's failing run must record the error and leave its
	// "last successful run" fields untouched.
	failJob, err := svc.AddJob("h1", "fail-target", "Failing Job", 60, 60)
	if err != nil {
		t.Fatalf("AddJob returned error: %v", err)
	}

	lockedFail, err := svc.lockJob(failJob.ID.String())
	if err != nil || lockedFail.state != JobStateOK {
		t.Fatalf("expected lockJob to succeed, got state=%v err=%v", lockedFail.state, err)
	}

	jobErr := "boom"
	if err := svc.unlockJob(failJob.ID.String(), &jobErr, lockedFail.lock); err != nil {
		t.Fatalf("unlockJob (failure) returned error: %v", err)
	}

	afterFail, err := svc.GetJob(failJob.ID.String())
	if err != nil {
		t.Fatalf("GetJob returned error: %v", err)
	}
	if afterFail.LastRunError == nil || *afterFail.LastRunError != "boom" {
		t.Fatalf("expected LastRunError to be recorded, got %+v", afterFail.LastRunError)
	}
	if afterFail.LastSuccessfulRunEnd != nil {
		t.Fatal("expected no successful-run fields to be set for a failing run")
	}
}
