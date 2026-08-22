package locks

import (
	"context"
	"testing"
	"time"
)

// fastElectorConfig keeps these tests quick — real callers would use much
// longer intervals (seconds, not tens of milliseconds).
var fastElectorConfig = ElectorConfig{
	RenewInterval: 20 * time.Millisecond,
	CheckInterval: 20 * time.Millisecond,
	LeaseTimeout:  200 * time.Millisecond,
}

// waitForLeader polls until e reports leadership or deadline elapses.
func waitForLeader(t *testing.T, e *Elector) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !e.IsLeader() {
		if time.Now().After(deadline) {
			t.Fatal("elector never became leader")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestElector_BecomesLeader(t *testing.T) {
	svc := NewService(testDB(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e := NewElector(svc, SystemLockNamespace, "elect-a", fastElectorConfig)
	go e.Run(ctx)

	waitForLeader(t, e)
}

func TestElector_OnlyOneOfTwoLeads(t *testing.T) {
	svc := NewService(testDB(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e1 := NewElector(svc, SystemLockNamespace, "elect-b", fastElectorConfig)
	e2 := NewElector(svc, SystemLockNamespace, "elect-b", fastElectorConfig)
	go e1.Run(ctx)
	go e2.Run(ctx)

	// Give both several renew/check cycles to settle.
	time.Sleep(300 * time.Millisecond)

	if e1.IsLeader() == e2.IsLeader() {
		t.Fatalf("expected exactly one of the two electors to be leader, got e1=%v e2=%v", e1.IsLeader(), e2.IsLeader())
	}
}

func TestElector_TakesOverStaleLease(t *testing.T) {
	svc := NewService(testDB(t))

	// Simulate a leader that acquired the lease and then died without ever
	// releasing it - a short lease with nothing renewing it.
	if _, err := svc.TryLock(SystemLockNamespace, "elect-c", 50*time.Millisecond); err != nil {
		t.Fatalf("setup TryLock failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e := NewElector(svc, SystemLockNamespace, "elect-c", fastElectorConfig)
	go e.Run(ctx)

	waitForLeader(t, e)
}

func TestElector_ReleasesOnCancel(t *testing.T) {
	svc := NewService(testDB(t))
	ctx, cancel := context.WithCancel(context.Background())

	e := NewElector(svc, SystemLockNamespace, "elect-d", fastElectorConfig)
	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()

	waitForLeader(t, e)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}

	// The lease should be free immediately, not after LeaseTimeout - proves
	// Run actually released it rather than just stopping its own loop.
	lock, err := svc.TryLock(SystemLockNamespace, "elect-d", time.Minute)
	if err != nil {
		t.Fatalf("TryLock returned error: %v", err)
	}
	if lock == nil {
		t.Fatal("expected the lease to be free immediately after Run returned")
	}
}
