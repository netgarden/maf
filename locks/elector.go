package locks

import (
	"context"
	"sync/atomic"
	"time"
)

// ElectorConfig controls an Elector's cadence. There are no package-level
// defaults — different applications sharing this package will want
// different cadences (a fast-failover collector vs. an infrequent batch
// job), so every field is required.
type ElectorConfig struct {
	// RenewInterval is how often the current leader renews its lease.
	// Should be comfortably shorter than LeaseTimeout so a renewal isn't
	// lost to ordinary scheduling jitter.
	RenewInterval time.Duration
	// CheckInterval is how often a non-leader retries acquiring the lease.
	CheckInterval time.Duration
	// LeaseTimeout is how long a lease stays valid without being renewed —
	// this is both the window a crashed leader's lease takes to go stale
	// and the lease duration passed to TryLock/Renew.
	LeaseTimeout time.Duration
}

// Elector maintains a single replica's belief about whether it currently
// holds the named lease, continuously renewing it while it does and
// retrying acquisition while it doesn't — see Run. Unlike Service's own
// TryLock/Lock/Unlock (each a one-off operation the caller sequences
// itself), Elector is meant for "run this continuously on whichever
// replica currently holds leadership" — construct one, start Run in a
// goroutine, and poll IsLeader from whatever work needs to be leader-only.
type Elector struct {
	service   *Service
	namespace LockNamespace
	id        string
	cfg       ElectorConfig

	isLeader atomic.Bool
	lock     *Lock
}

// NewElector constructs an Elector for the lease identified by
// (namespace, id). Nothing is acquired until Run is called.
func NewElector(service *Service, namespace LockNamespace, id string, cfg ElectorConfig) *Elector {
	return &Elector{
		service:   service,
		namespace: namespace,
		id:        id,
		cfg:       cfg,
	}
}

// IsLeader reports whether this Elector currently believes it holds the
// lease. Safe to call from any goroutine while Run is active (or not
// running at all, in which case it's always false).
func (e *Elector) IsLeader() bool {
	return e.isLeader.Load()
}

// Run attempts to acquire or renew the lease on cfg's cadence until ctx is
// cancelled, then — if it currently holds the lease — releases it before
// returning, so the next replica can take over immediately rather than
// waiting out LeaseTimeout. Meant to be run in its own goroutine for the
// lifetime of the caller's leader-dependent work.
func (e *Elector) Run(ctx context.Context) {
	defer e.release()

	timer := time.NewTimer(0) // attempt immediately on start
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			timer.Reset(e.attempt())
		}
	}
}

// attempt makes one acquire-or-renew attempt and returns how long to wait
// before the next one.
func (e *Elector) attempt() time.Duration {
	if e.lock != nil {
		renewed, err := e.service.Renew(e.lock, e.cfg.LeaseTimeout)
		if err == nil && renewed != nil {
			e.lock = renewed
			e.isLeader.Store(true)
			return e.cfg.RenewInterval
		}
		// Renew failed outright, or the lease was already taken over by
		// someone else — either way this replica is no longer the leader.
		e.lock = nil
		e.isLeader.Store(false)
	}

	newLock, err := e.service.TryLock(e.namespace, e.id, e.cfg.LeaseTimeout)
	if err != nil || newLock == nil {
		e.isLeader.Store(false)
		return e.cfg.CheckInterval
	}

	e.lock = newLock
	e.isLeader.Store(true)
	return e.cfg.RenewInterval
}

// release unlocks the currently held lease, if any — see Run.
func (e *Elector) release() {
	if e.lock == nil {
		return
	}
	_ = e.service.Unlock(e.lock)
	e.lock = nil
	e.isLeader.Store(false)
}
