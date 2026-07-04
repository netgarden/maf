# jobs

`github.com/netgarden/maf/jobs` — recurring background work, coordinated
across multiple instances of an application sharing one database via
[`github.com/netgarden/maf/locks`](../locks), so only one instance runs a
given job at a time.

This is a from-scratch, fixed reimplementation of a module found in
`gate`. That original had several real bugs — see "What changed from the
original" below if you're comparing against it.

## Concepts

A job is identified by `(HandlerID, TargetID)`. `TargetID` is empty for a
single global job (e.g. "the session cleaner"); give it a non-empty,
per-resource ID to run one job per target (e.g. one sync job per configured
provider). A `Handler` is registered per `HandlerID` and does the actual
work:

```go
type SyncHandler struct{ /* ... */ }

func (h *SyncHandler) Run(ctx context.Context, job *jobs.Job) {
    // do the work; respect ctx.Done()
}
```

## Usage

As a maf module (register `locks.NewModule()` before it):

```go
modules = append(modules, locks.NewModule())
modules = append(modules, jobs.NewModule())

// after Initialize():
jobsModule := manager.GetModule("jobs").(*jobs.Module)
service := jobsModule.GetService()

service.RegisterHandler("provider-sync", &SyncHandler{})

// One job per provider — Interval and Timeout are in seconds.
service.AddJob("provider-sync", provider.ID, "Sync "+provider.Name, 3600, 1200)
// ... provider.Active toggled off, or provider deleted:
service.RemoveJob("provider-sync", provider.ID)
```

Or construct a `Service` directly with `jobs.NewService(db, locksService)`
if you don't need it wired through maf.

## The Timeout field, honestly

`Job.Timeout` (seconds) does two things:

1. It bounds the `context.Context` passed to `Handler.Run`. **This only
   works if your handler actually checks `ctx.Done()`/`ctx.Err()`** — Go
   has no way to forcibly stop a goroutine that ignores its context. A
   handler that never checks it will keep running past Timeout.
2. It sets the lock lease taken while the job runs. If the instance
   running a job crashes outright (process dies, not just a slow handler),
   the lease — and therefore the job — becomes available to another
   instance after Timeout, not after the (usually much longer) Interval.

So: Timeout is a real, load-bearing value for crash recovery, and a
best-effort, cooperative one for a handler that's merely slow. Write
handlers that respect their context.

One consequence: `JobStateRunning` (see its doc comment) is only ever
observable when `Timeout > Interval`, which is an unusual configuration —
with the more typical `Timeout <= Interval`, a concurrent lock attempt
within `Interval` of the last run always reports `JobStateUpdate` first.
Both states are handled identically by callers, so this only affects
observability if you're logging/displaying job state, not scheduling
correctness.

## Concurrency characteristics

Jobs use `locks.JobsLockNamespace` — a distinct namespace from any locks
your own code takes directly, so job scheduling doesn't serialize against
unrelated lock usage (see the "Concurrency characteristics" note in
`maf/locks`'s README for what sharing a namespace costs).

The in-process scheduler (`Manager`) keeps jobs in a
[`github.com/netgarden/orderedlist`](../../orderedlist) ordered by next-run
time, so it can always cheaply check "what's due" without scanning every
job. The DB round-trip to actually acquire a job's lock happens *without*
holding the scheduler's mutex, so `AddJob`/`UpdateJob`/`RemoveJob` are never
blocked behind an in-flight run.

## What changed from the original (`gate`'s `jobs` module)

If you're comparing against the module this was migrated from:

- `getJobByTarget` had a `db.Where("target_id = ?")` with no bound
  argument — a SQL syntax error for every non-empty `TargetID`, which broke
  `AddJob`/`UpdateJob`/`RemoveJob` for every job that wasn't the single
  global one. Fixed.
- `UpdateJob`/`RemoveJob` took the coordination lock on one transaction but
  read/wrote through a separate, non-transactional connection, so the lock
  didn't actually protect what it was meant to. Fixed — everything now
  happens on the one transaction holding the lock.
- The lock lease was based on `Interval` instead of `Timeout` (see above).
  Fixed.
- `Handler.Run` took no context, so "Timeout" was pure configuration with
  no effect on execution at all. Fixed — see above for what this does and
  doesn't guarantee.
- The scheduler held its mutex across the synchronous DB round-trip to
  acquire a job's lock, blocking every other scheduler operation for that
  duration. Fixed — see "Concurrency characteristics".
- Rescheduling after a run reused a captured, possibly-stale copy of the
  job rather than the current one, which could leave a duplicate entry in
  the schedule if `UpdateJob`/`RemoveJob` ran concurrently with a job's
  execution. Fixed (see `TestManager_ConcurrentUpdateDuringRun_NoDuplicateSchedule`).
- The scheduler's running flag was a plain `bool` read/written from
  different goroutines with no synchronization. Now an `atomic.Bool`.
- It depended on a hand-rolled ordered list with two bugs of its own — see
  `github.com/netgarden/orderedlist`'s README. Now uses that instead.
- `rand.Int63n(job.Interval)` (initial-run jitter) panics for
  `Interval <= 0`. `AddJob`/`UpdateJob` now reject non-positive intervals
  outright instead of letting that panic surface later.

## Testing

`Manager`'s scheduling logic is tested in-memory against a mock (no
database needed — see `manager_test.go`). `Service`'s database-backed
behavior needs a real Postgres:

```bash
createdb maf_jobs_test  # once
MAF_JOBS_TEST_DSN="host=localhost user=citadel password=citadel dbname=maf_jobs_test port=5432 sslmode=disable" \
  go test ./... -race
```

Tests are skipped (not failed) when `MAF_JOBS_TEST_DSN` is unset. Run with
`-race` — this package has real concurrency to get right.
