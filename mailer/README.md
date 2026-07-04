# mailer

`github.com/netgarden/maf/mailer` — queues emails in a database table and
delivers them asynchronously over SMTP, retrying failures with a bounded
(default 1 week) retry window. Safe under multiple replicas of an
application sharing one database: no email is ever sent twice, even if an
admin-triggered "retry now" call lands on a different replica than the one
currently processing the queue.

## Usage

As a maf module (register `locks.NewModule()` and `jobs.NewModule()`
before it — `mailer` depends on `jobs` for its recurring queue-processing
tick, which itself depends on `locks`):

```go
modules = append(modules, locks.NewModule())
modules = append(modules, jobs.NewModule())
modules = append(modules, mailer.NewModule())

// after Initialize():
mailerModule := manager.GetModule("mailer").(*mailer.Module)
service := mailerModule.GetService()

email, err := service.Enqueue(&mailer.EnqueueRequest{
    To:      []string{"user@example.com"},
    Subject: "Welcome",
    BodyText: "Hello!",
})
```

`EnqueueTx(tx, req)` enqueues inside an existing transaction, so a rollback
of the business action that triggered the email also rolls back the
enqueue — useful when sending mail is a side effect of some other write
(e.g. user creation).

## Templates

A module registers a template **in code**, at its own `Initialize()` —
this is what makes it work out of the box. An administrator can later
override its content at runtime via the admin RRPC API; the override, if
present, always wins over the registered default.

```go
mailerModule.GetService().RegisterTemplate(mailer.TemplateDefault{
    ID:          "auth.new-user-credentials",
    Subject:     "Your account has been created",
    BodyText:    "Hello {{.Username}},\n\nYour temporary password is: {{.TemporaryPassword}}\n\nLog in at {{.LoginURL}}.",
    Description: "Variables: Username, TemporaryPassword, LoginURL",
})

email, err := mailerModule.GetService().EnqueueTemplate(
    "auth.new-user-credentials",
    []string{user.Email}, nil, nil,
    map[string]any{"Username": user.Username, "TemporaryPassword": tempPassword, "LoginURL": loginURL},
)
```

Template IDs should follow the convention `<moduleID>.<name>` (e.g.
`auth.new-user-credentials`) to avoid collisions between modules — not
enforced by code, just convention. `EnqueueTemplate` renders once,
synchronously, at enqueue time and then calls the exact same `EnqueueTx` a
direct `Enqueue` call would — an email already queued is never affected by
a template edit made after it was enqueued, and there's no special-casing
for templated vs. direct emails anywhere in the queue/retry machinery.

Subject and plain-text bodies render via `text/template`; HTML bodies
render via `html/template`, which auto-escapes template data — this
matters because `data` can contain admin- or user-supplied strings (e.g. a
username) being interpolated into an HTML email. Both a module's own
default and any admin-supplied override are validated (parsed, not
rendered) before being accepted — a module's invalid hardcoded default
fails at `RegisterTemplate` (surfacing at that module's own `Initialize()`
rather than at first send), and an admin's invalid override is rejected by
the RRPC `updateTemplate` call rather than being stored and failing later,
silently, in the background.

## Admin API

Exposed at `api/admin/mailer` (list/get/retry/cancel queued emails,
list/get/update/reset templates). **This module implements no
authentication or authorization of its own** — it relies entirely on the
host application already gating `/api/admin/*` behind an admin-only
middleware, exactly like `maf/rrpc-auth`'s own `Users` service (at
`api/admin/users`) already does. If your application doesn't have such a
middleware, these routes are unprotected.

## Concurrency: how no email gets sent twice

Two things could otherwise cause a double send: two replicas' recurring
ticks racing each other, and an admin-triggered "retry now" call racing a
tick. `jobs` already solves the first problem generically (only one
replica's tick runs at a time, coordinated via `locks`), but that alone
doesn't solve the second — an admin API call isn't part of any tick.

So the actual claim mechanism is per-row, not per-tick: `mailer_emails` has
`status`/`claim_token`/`claimed_at`/`claim_expires_at` columns, and both
the batch tick claim and a single-row admin retry/cancel are conditional
`UPDATE`s that only affect a row if it's currently in a claimable state.
Whichever commits first wins; the other affects 0 rows. The batch claim
additionally uses `SELECT ... FOR UPDATE SKIP LOCKED` so multiple
concurrent claimers (in principle, if `batchSize`/`tickInterval` were tuned
such that overlapping ticks were possible) partition the eligible rows
between them instead of blocking on each other.

This intentionally does **not** use `locks` for per-email coordination —
`locks`'s Postgres-advisory-lock model fits "run this one named task
exclusively" (what `jobs` uses it for), not "claim N independent rows out
of many without serializing all claimers on a single mutex," which is what
a queue table needs. `SKIP LOCKED` is the standard tool for that shape and
claims a whole batch in one round trip instead of an acquire/release pair
per row.

A replica that crashes mid-send doesn't leave its claimed rows stuck: once
`claim_expires_at` (set to `mailer.queue.claimTimeout` at claim time) is in
the past, the row is claimable again by the next tick — the same
lease-expiry model `jobs` itself uses for its own per-job lock, just
applied per email row instead of per job.

## Configuration

| Key | Type | Default | Notes |
|---|---|---|---|
| `mailer.smtp.host` | string | — (required) | |
| `mailer.smtp.port` | int | `587` | |
| `mailer.smtp.username` | string | — | empty = no auth |
| `mailer.smtp.password` | string | — | |
| `mailer.smtp.encryption` | string | `starttls` | `none` \| `starttls` \| `tls` |
| `mailer.smtp.from` | string | — (required) | |
| `mailer.smtp.timeout` | duration | `30s` | per-connection SMTP timeout |
| `mailer.retry.initialInterval` | duration | `1m` | delay before the 2nd attempt |
| `mailer.retry.multiplier` | float | `3.0` | exponential backoff factor |
| `mailer.retry.maxInterval` | duration | `6h` | backoff cap |
| `mailer.retry.maxAge` | duration | `168h` (1 week) | give up permanently past this age |
| `mailer.queue.batchSize` | int | `20` | emails claimed per tick |
| `mailer.queue.claimTimeout` | duration | `2m` | claim lease / jobs lock lease |
| `mailer.queue.tickInterval` | duration | `15s` | how often the queue is checked |

`smtp.encryption` is validated at startup (`Initialize()`), not at first
send — an unknown value fails the application to start rather than failing
silently later.

## A known gap in the underlying `.rrpc` DSL

`def/mailer.rrpc` uses plain, always-present string fields (e.g.
`lastError string`, empty = unset) instead of the DSL's documented `field?
type` optional-field syntax — that syntax isn't actually implemented
anywhere in `rrpc-def`'s lexer/parser (confirmed: it's mentioned in
`rrpc`'s own README but no `.rrpc` file anywhere in this workspace has ever
used it, and attempting to does not parse). Implementing it properly would
mean generator changes in both `rrpc-gen-go` and `rrpc-gen-ts` (pointer
types, `omitempty`) beyond this module's own scope.

## Testing

Pure logic (`backoff.go`, `mime.go`, `templates.go`'s render/validate path)
is tested with no database or network — see `backoff_test.go`,
`mime_test.go`, `templates_test.go`. `Service`'s database-backed behavior,
including the concurrent-claim proof, needs a real Postgres:

```bash
createdb maf_mailer_test  # once
MAF_MAILER_TEST_DSN="host=localhost user=citadel password=citadel dbname=maf_mailer_test port=5432 sslmode=disable" \
  go test ./... -race
```

Tests are skipped (not failed) when `MAF_MAILER_TEST_DSN` is unset. Run
with `-race` — the claim path has real concurrency to get right.
