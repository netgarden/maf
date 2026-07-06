# mailer

`github.com/netgarden/maf/mailer` — queues emails in a database table and
delivers them asynchronously over SMTP, retrying failures with a bounded
(default 1 week) retry window. Safe under multiple replicas of an
application sharing one database: no email is ever sent twice, even if an
admin-triggered "retry now" call lands on a different replica than the one
currently processing the queue.

`Email.BodyText`/`BodyHTML` are encrypted at rest (AES-256-GCM, via
`maf/security/encryption`) — see "Encryption at rest" below.

## Usage

As a maf module (register `locks.NewModule()`, `jobs.NewModule()`, and
`security.NewModule()` before it — `mailer` depends on `jobs` for its
recurring queue-processing tick, which itself depends on `locks`, and on
`security` for the encryption key used to encrypt queued email bodies at
rest):

```go
modules = append(modules, locks.NewModule())
modules = append(modules, jobs.NewModule())
modules = append(modules, security.NewModule())
modules = append(modules, mailer.NewModule())

// after Initialize():
mailerModule := manager.GetModule("mailer").(*mailer.Module)
service := mailerModule.GetService()

email, err := service.Send(&mailer.SendRequest{
    To:      []string{"user@example.com"},
    Subject: "Welcome",
    BodyText: "Hello!",
})
```

`EnqueueTx(tx, req)` enqueues inside an existing transaction, so a rollback
of the business action that triggered the email also rolls back the
enqueue — useful when sending mail is a side effect of some other write
(e.g. user creation).

`Send` (not `EnqueueTx`) additionally tries to deliver the email right
away, in the background, instead of waiting for the next queue tick (up to
`mailer.queue.tickInterval` away) — bounded by
`mailer.queue.directSendTimeout`. `EnqueueTx` never does this: its row isn't
guaranteed to be committed when it returns (that depends on the caller's own
transaction), so attempting delivery there could send mail for an action
that later rolls back. If the direct attempt doesn't finish in time or
fails, the row is simply left queued exactly as before, and the normal
tick/backoff machinery sends it — see "Concurrency" below for why this can
never race the tick into a double send.

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

email, err := mailerModule.GetService().SendTemplate(
    "auth.new-user-credentials",
    []string{user.Email}, nil, nil,
    map[string]any{"Username": user.Username, "TemporaryPassword": tempPassword, "LoginURL": loginURL},
)
```

Template IDs should follow the convention `<moduleID>.<name>` (e.g.
`auth.new-user-credentials`) to avoid collisions between modules — not
enforced by code, just convention. `SendTemplate` renders once,
synchronously, at enqueue time and then calls the exact same `Send` a
direct call would (including its direct-delivery attempt) — an email
already queued is never affected by a template edit made after it was
enqueued, and there's no special-casing for templated vs. direct emails
anywhere in the queue/retry machinery.

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

Three things could otherwise cause a double send: two replicas' recurring
ticks racing each other, an admin-triggered "retry now" call racing a tick,
and now `Send`'s direct-delivery attempt racing a tick that claims the
same just-enqueued row first. `jobs` already solves the first problem
generically (only one replica's tick runs at a time, coordinated via
`locks`), but that alone doesn't solve the other two — neither an admin API
call nor `Send`'s background goroutine is part of any tick.

So the actual claim mechanism is per-row, not per-tick: `mailer_queue` has
`status`/`claim_token`/`claimed_at`/`claim_expires_at` columns, and the batch
tick claim, a single-row admin retry/cancel, and `Send`'s direct-send
claim (`claimByID`) are all conditional `UPDATE`s that only affect a row if
it's currently in a claimable state. Whichever commits first wins; the
other affects 0 rows — for `Send`, that just means the direct-send
goroutine quietly does nothing and the tick's own attempt (or its result)
stands. The batch claim additionally uses `SELECT ... FOR UPDATE SKIP
LOCKED` so multiple concurrent claimers (in principle, if
`batchSize`/`tickInterval` were tuned such that overlapping ticks were
possible) partition the eligible rows between them instead of blocking on
each other; `claimByID` doesn't need that, since a plain conditional
`UPDATE` by primary key is already atomic for a single row.

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

## Retention: cleaning up old emails

`mailer_queue` rows are never deleted by the send/retry machinery above —
only their `status` changes. Left alone, every email ever sent would stay
in the table forever. A second recurring job (`mailer-retention`,
registered in `Module.Initialize()` exactly like `mailer-tick`, coordinated
across replicas the same way) deletes old rows via `Service.PurgeOldEmails`
— but only **terminal-status** ones (`sent`, `cancelled`, `failed`);
`queued`/`sending` rows are never touched regardless of age, since those
are still active.

`sent`/`cancelled` rows are routine noise and get a short default window
(`mailer.retention.maxAge`, 30 days); `failed` (gave-up) rows are usually
worth investigating before they disappear, so they get a much longer one
(`mailer.retention.failedMaxAge`, 180 days) — two separate knobs, not one.
Each status is matched against its own dedicated timestamp column
(`sent_at`/`cancelled_at`/`gave_up_at`), not a generic "last updated" one.

This can, in principle, race an admin's retry/cancel call on the same row
(e.g. retrying a `failed` email right as it crosses `failedMaxAge`) — same
as every other conditional state transition in this file, whichever
commits first wins: the loser either finds nothing to retry (`ErrNotFound`)
or the row survives as freshly `queued` and the purge's `WHERE` simply no
longer matches it. No special handling needed.

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
| `mailer.queue.directSendTimeout` | duration | `10s` | how long `Send`'s direct-delivery attempt may run before being abandoned (the row is simply left queued for the next tick) |
| `mailer.retention.maxAge` | duration | `720h` (30 days) | how long `sent`/`cancelled` emails are kept before being purged |
| `mailer.retention.failedMaxAge` | duration | `4320h` (180 days) | how long `failed` (gave-up) emails are kept before being purged |
| `mailer.retention.tickInterval` | duration | `24h` | how often the retention purge runs |
| `mailer.retention.jobTimeout` | duration | `10m` | lock lease for one retention run (a `jobs` scheduling concept, unrelated to `queue.claimTimeout`) |

`smtp.encryption` is validated at startup (`Initialize()`), not at first
send — an unknown value fails the application to start rather than failing
silently later.

Also required: `security.encryption.key` (`maf/security`'s own config,
not `mailer`'s) — see "Encryption at rest" below.

## Encryption at rest

`Email.BodyText`/`BodyHTML` are encrypted (AES-256-GCM) before being
written to `mailer_queue`, using `maf/security`'s `GetEncryptionManager()`
— configured via `security.encryption.key`, **deliberately separate** from
`security.secret` (used elsewhere for JWT signing). This is entirely an
internal storage-layer detail: every public `Service` method (`Send`,
`GetEmail`, `ListEmails`, the batch claim feeding the actual SMTP send) is
still plaintext in, plaintext out — nothing about the public API changes.

Not encrypted, deliberately: `Subject` (the admin API's `ListEmailsFilter.Search`
does `subject ILIKE`, which wouldn't work against ciphertext), `To`/`Cc`/`Bcc`
(useful to see at a glance in the admin UI, and less sensitive than full
body content), and `Template.BodyText`/`BodyHTML` (templates are closer to
code/config than data — the actual sensitive content only exists once
rendered into a specific queued `Email`).

A row encrypted under one `security.encryption.key` cannot be decrypted
after that key changes without a data migration — `GetEmail`/`ListEmails`/
the queue tick will return a decrypt error for it, not silently corrupt or
crash (see `TestGetEmail_WrongKeyFailsToDecrypt`).

`maf/security/encryption.Manager` works in raw `[]byte` and prefixes its
output with an 8-byte numeric algorithm identifier, dispatching `Decrypt`
by that prefix — so if `Manager`'s default `Cryptor` is ever swapped for
something more secure in the future, old rows encrypted under the
previous algorithm keep decrypting correctly as long as that old `Cryptor`
stays registered (see `Manager.AddCryptor`/`AddDefaultCryptor` and
`TestManager_DecryptsUnderPreviousDefaultAfterCryptorChange` in that
package) — nothing in `mailer` itself needs to change for that.
Since `Email.BodyText`/`BodyHTML` are `string` (`TEXT` columns, not
`BYTEA`), `encryptBody`/`decryptBody` in `service.go` base64-encode the
encrypted bytes before storing them and decode before calling `Decrypt` —
that text-encoding step is `mailer`'s own concern, not
`encryption.Manager`'s.

## A known gap in the underlying `.rrpc` DSL

`rpc/def/mailer.rrpc` uses plain, always-present string fields (e.g.
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
