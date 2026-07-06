# locks

`github.com/netgarden/maf/locks` — distributed lease locks backed by a
Postgres table and `pg_advisory_xact_lock`. Use it to coordinate work across
multiple instances of an application that already shares a Postgres
database, without adding a separate lock service (Redis, etcd, ...).

## Concepts

A lock is identified by a `(Namespace, ID)` pair and stored as a single row.
Acquiring a lock creates a **lease**: a `CreatedAt`/`ExpiresAt` window plus a
random `Key`. The lease — not an open connection or session — is what makes
the lock "held": a lock is free again as soon as `ExpiresAt` passes, whether
or not the holder called `Unlock`. This means a crashed holder can't wedge
the lock forever, but it also means callers must pick a lease duration long
enough to cover the work they're protecting.

`Namespace` exists to partition unrelated locks (e.g. one namespace per
subsystem); `maf.locks.SystemLockNamespace` (`0`) is provided for
application-wide locks. See "Concurrency characteristics" below before
reusing one namespace for a large number of independent, hot locks.

## API

```go
service := locks.NewService(db) // db is a *gorm.DB already connected to Postgres

// Non-blocking: returns (nil, nil) immediately if the lock is currently held.
lock, err := service.TryLock(locks.SystemLockNamespace, "nightly-report", 5*time.Minute)
if lock == nil {
    // someone else is already running the nightly report
}

// Blocking: retries until acquired or the overall timeout elapses.
lock, err := service.Lock(locks.SystemLockNamespace, "nightly-report", 5*time.Minute, 30*time.Second)
if err != nil {
    // timed out (or a DB error) — nobody freed the lock within 30s
}

// Always unlock with the *same* Lock value TryLock/Lock returned — Unlock
// only removes the row if its Key still matches, so an unlock from a lease
// that has since expired and been replaced by someone else is a no-op.
defer service.Unlock(lock)

// For a one-off "run this exclusively across every replica" operation that
// fits in a single DB transaction (e.g. a startup bootstrap check-then-create),
// RunExclusive avoids managing a lease/unlock lifecycle at all: it opens a
// transaction, takes the namespace's advisory lock for its duration, runs fn,
// and releases the lock automatically when the transaction ends either way.
err := service.RunExclusive(locks.SystemLockNamespace, func(tx *gorm.DB) error {
    // only one replica's fn runs at a time for this namespace, cluster-wide
    return nil
})
```

As a maf module (registers the `Lock` entity for auto-migration):

```go
modules = append(modules, locks.NewModule())
// later, after Initialize():
locksModule := manager.GetModule("locks").(*locks.Module)
service := locksModule.GetService()
```

## Concurrency characteristics

Acquiring or releasing *any* lock in a namespace takes
`pg_advisory_xact_lock(namespace)` for the duration of that one
transaction — this is what makes the check-then-write race-free without
relying on row-level locking quirks. The cost is that every acquire/release
in a namespace serializes against every other one in that *same* namespace,
regardless of `ID`. That's the right trade-off for a modest number of
coarse, infrequent, application-wide locks (e.g. "only one instance runs
this migration/startup task"). It is not a good fit for many independent,
frequently-contended per-key locks — give those their own namespace, or
consider a different mechanism if the volume gets high.

Other things worth knowing:
- Expired lock rows are not proactively cleaned up; they're overwritten the
  next time someone acquires that same `(Namespace, ID)`, or removed by a
  matching `Unlock`. A namespace with many one-off `ID`s that are never
  retried will accumulate stale rows.
- `Lock`'s retry loop wakes up at the earlier of its own timeout and the
  current holder's lease expiry, so it retries promptly rather than
  sleeping past a lock that's already become free.

## Testing

Correctness here depends on real Postgres semantics (advisory locks,
transaction isolation) — there's no meaningful way to mock it, so the tests
are integration tests that need a real database. `TestMain`
(`service_test.go`) starts one itself via `testcontainers-go` (Docker
required), so no manual setup is needed:

```bash
go test ./...
```

Tests are skipped (not failed) if Docker isn't available.
