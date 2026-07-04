# MAF — Modular Application Framework

MAF is a small Go framework for building applications out of independent,
composable modules. An application declares which modules it needs; MAF
wires them together, resolves the order they need to start in, and drives
each one through a shared lifecycle (config → logging → initialize →
start → stop).

It lives at `github.com/netgarden/maf`. Each piece of functionality —
authentication, database access, background jobs, and so on — is a
**separate Go module** with its own `go.mod`, so an application only pulls
in what it actually uses.

## Modules

| Module | What it does | Import path |
|---|---|---|
| *(root)* | Core framework — `Module`/`Manager`/`Application` interfaces | `github.com/netgarden/maf` |
| `auth` | Authentication: users, sessions, passwords | `github.com/netgarden/maf/auth` |
| `database` | GORM/PostgreSQL integration | `github.com/netgarden/maf/database` |
| `logging` | `slog` setup | `github.com/netgarden/maf/logging` |
| `security` | Password hashing, secrets | `github.com/netgarden/maf/security` |
| `web` | Echo HTTP server + Pongo2 templates | `github.com/netgarden/maf/web` |
| `datatables` | Server-side processing for DataTables | `github.com/netgarden/maf/datatables` |
| `mergefs` | Merges multiple `fs.FS` instances | `github.com/netgarden/maf/mergefs` |
| `rrpc-server` | rrpc server module | `github.com/netgarden/maf/rrpc-server` |
| `rrpc-auth` | rrpc auth integration | `github.com/netgarden/maf/rrpc-auth` |
| `locks` | Postgres advisory-lock-backed distributed lease locks | `github.com/netgarden/maf/locks` |
| `jobs` | Recurring background jobs, coordinated via `locks` | `github.com/netgarden/maf/jobs` |
| `config` | WIP — not yet integrated (and not present in this checkout; see Known limitations) | `github.com/netgarden/maf/config` |

`jobs` additionally depends on `github.com/netgarden/orderedlist`, a sibling repository (not part of this one).

## Getting started

Requires Go 1.24 or newer (a few modules pin higher versions in their own
`go.mod` — `rrpc-server`/`rrpc-auth` need 1.26+).

There's no workspace-level `go.mod`; each module is built and versioned
independently. For local development against your own checkout of all of
them, add local `replace` directives once after cloning:

```bash
./scripts/replace-add.sh
```

`rrpc-server`/`rrpc-auth` also need a manual replace for
`github.com/netgarden/rrpc`, and `jobs` needs one for
`github.com/netgarden/orderedlist` — both are separate repositories the
script doesn't reach into. Before tagging a release, strip the local
replaces back out so consumers resolve published versions instead:

```bash
./scripts/replace-remove.sh
```

### A minimal application

```go
package main

import (
	"log"

	"github.com/netgarden/maf"
	"github.com/netgarden/maf/database"
	"github.com/netgarden/maf/logging"
	"github.com/netgarden/maf/security"
)

type App struct{}

func (a *App) GetID() string             { return "myapp" }
func (a *App) GetName() string           { return "My App" }
func (a *App) GetConfigFilePath() string { return "" }

func (a *App) GetModules() []maf.Module {
	return []maf.Module{
		logging.NewModule(),
		database.NewModule(),
		security.NewModule(),
	}
}

func main() {
	if err := maf.New(&App{}).Start(true); err != nil {
		log.Fatal(err)
	}
}
```

## Writing a module

A module is any type implementing the small `Module` interface
(`GetID`, `GetName`, `SetManager`); it opts into further lifecycle phases —
config, logging setup, pre-initialize, initialize, post-initialize,
pre-start, start, stop — by additionally implementing the matching
interface from `module.go`. A module only needs to implement the phases it
uses.

```go
type Module struct {
	manager *maf.Manager
}

func (m *Module) GetID() string   { return "mymodule" }
func (m *Module) GetName() string { return "MyModule" }
func (m *Module) SetManager(manager *maf.Manager) { m.manager = manager }

func (m *Module) Initialize() error {
	// do setup, optionally reaching into other modules via m.manager
	return nil
}
```

### Depending on another module

If a module reaches into another one (`manager.GetModule("security")`),
declare that dependency by implementing `ModuleDependenciesProvider`:

```go
func (m *Module) GetDependencies() []string {
	return []string{"security"}
}
```

The `Manager` checks every declared dependency is registered, and
topologically sorts modules so dependencies always run before their
dependents through every lifecycle phase (and after them on the way back
down through `Stop`) — regardless of the order they were registered in.
Declaring it turns a missing dependency into a clear startup error instead
of a nil-pointer panic inside `Initialize`, and a circular dependency into
`circular module dependency detected: a -> b -> a` instead of undefined
behavior.

### Reaching other modules without an import

Some modules integrate with others by interface rather than by direct
lookup — `database` and `web` both discover implementors by scanning every
registered module for interface satisfaction, so there's no explicit
registration step:

- **Database**: implement `database.Consumer` (`SetDB`) to receive the live
  connection, and/or `database.EntitiesProvider` (`GetDBEntities`) to get
  your GORM models auto-migrated.
- **Web**: implement `web.WebModule` (`GetWebControllers`) to register Echo
  route groups, plus static file / template providers as needed.

## Testing

There's no workspace-level test command; run tests per module. Most
modules have none yet. Where they exist:

- `auth/services` — plain unit tests against small repository interfaces
  (mocked), no database needed.
- `locks` — integration tests against a real Postgres (advisory locks can't
  be meaningfully mocked): `MAF_LOCKS_TEST_DSN=... go test ./...`. Skipped,
  not failed, if the env var is unset. See `locks/README.md`.
- `jobs` — scheduler logic is tested in-memory against a mock, no database
  needed; DB-backed behavior needs `MAF_JOBS_TEST_DSN=... go test ./... -race`.
  See `jobs/README.md`.
- Core framework (this directory) — module lifecycle, dependency
  resolution, and cycle detection, all in-memory: `go test .`.

## Known limitations

- `Manager.configFile` has no public setter, so YAML config file loading is
  wired up internally but unreachable from application code — only
  environment-variable config overrides work today.
- `logging/module.go` implements `setupLogging()` (lowercase) but the
  `ModuleLoggingProvider` interface requires `SetupLogging()` — the logging
  module does not currently satisfy that interface.
- The `config/` module referenced by `web` and `datatables`'s `go.mod` files
  doesn't exist in this checkout, so those two modules fail to build
  standalone. Nothing else here depends on them.
