# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository structure

MAF (Modular Application Framework) is a Go framework for building modular applications. It lives at `github.com/netgarden/maf`. Each module is a **separate Go module** with its own `go.mod`, using local `replace` directives to reference siblings during development:

```
*.go        # Core framework — Module/Manager/Application interfaces  (github.com/netgarden/maf)
auth/       # Authentication module (sessions, users, passwords)       (github.com/netgarden/maf/auth)
database/   # GORM/PostgreSQL integration                              (github.com/netgarden/maf/database)
logging/    # slog setup                                               (github.com/netgarden/maf/logging)
security/   # Password hashing, secrets                                (github.com/netgarden/maf/security)
web/        # Echo HTTP server + Pongo2 templates                      (github.com/netgarden/maf/web)
datatables/ # DataTables server-side processing helper                 (github.com/netgarden/maf/datatables)
mergefs/    # Merges multiple fs.FS instances                          (github.com/netgarden/maf/mergefs)
rrpc-server/# rrpc server module                                       (github.com/netgarden/maf/rrpc-server)
rrpc-auth/  # rrpc auth integration                                    (github.com/netgarden/maf/rrpc-auth)
config/     # WIP — not yet integrated into the framework              (github.com/netgarden/maf/config)
scripts/    # replace-add.sh / replace-remove.sh — manage replace directives
```

## Build and development commands

Since there is no workspace-level `go.mod`, operate per-module:

```bash
# Add local replace directives (run once after cloning)
./scripts/replace-add.sh

# Build a specific module
cd auth && go build ./...

# Tidy dependencies in a module
cd auth && go mod tidy

# Run a single file/binary (if an entrypoint exists in the consuming app)
go run main.go

# Before tagging a release — strip local replaces so consumers can use the published versions
./scripts/replace-remove.sh
```

Note: `rrpc-server` and `rrpc-auth` also require a manual replace for `github.com/netgarden/rrpc` pointing to your local clone of that repo.

No tests exist yet in the repository.

## Core architecture

### Module lifecycle

The `Manager` (`maf/manager.go`) drives a fixed startup sequence. Modules opt into each phase by implementing the corresponding interface:

| Phase | Interface | Called when |
|---|---|---|
| 1. Config | `ModuleSetConfig` | `CreateConfig()` → load → `SetConfig()` |
| 2. Logging | `ModuleLoggingProvider` | `SetupLogging()` |
| 3. PreInitialize | `ModulePreInitialize` | `PreInitialize()` |
| 4. Initialize | `ModuleInitialize` | `Initialize()` |
| 5. PostInitialize | `ModulePostInitialize` | `PostInitialize()` |
| 6. PreStart | `ModulePreStart` | `PreStart()` |
| 7. Start | `ModuleStart` | `Start()` |
| Shutdown | `ModuleStop` | `Stop()` (reverse order) |

All interfaces are in `maf/module.go`. A module only needs to implement the phases it uses.

### Defining a module

```go
type Module struct {
    manager *maf.Manager
    config  *Config
}

func (m *Module) GetID() string   { return "mymodule" }
func (m *Module) GetName() string { return "MyModule" }
func (m *Module) SetManager(manager *maf.Manager) { m.manager = manager }

// Config support:
func (m *Module) CreateConfig() interface{} { return NewConfig() }
func (m *Module) SetConfig(cfg interface{}) { m.config = cfg.(*Config) }
```

### Configuration loading

Each module's config struct uses `yaml` and `envconfig` tags. After YAML file loading, env vars override using the pattern `{APP_ID}_{MODULE_ID}_{FIELD}` (all uppercase):

```go
type Config struct {
    DSN string `yaml:"dsn" envconfig:"DSN"`
}
// For app "myapp" + module "database": MYAPP_DATABASE_DSN
```

The YAML config file path is set via the `configFile` field on `Manager` — currently no public setter exists (known gap).

### Cross-module dependencies

Modules access other modules at `Initialize()` time via the Manager. The pattern used throughout the codebase is a runtime type assertion:

```go
func (m *Module) Initialize() error {
    securityModule := m.manager.GetModule("security").(*security.Module)
    m.secret = securityModule.GetConfig().Secret
}
```

This creates a compile-time import dependency between modules. The proposed improvement is a `ModuleConfigProvider` interface + typed registry — see the prior design discussion for details.

### Integration patterns (no direct imports needed)

**Database integration** — implement interfaces from `database/interfaces.go`:
- `database.Consumer` → `SetDB(db *gorm.DB)` — receives the live connection during `PreInitialize`
- `database.EntitiesProvider` → `GetDBEntities() []interface{}` — auto-migrated GORM models
- `database.PreMigrationConsumer` → `DBPreMigration(db *gorm.DB) error` — raw SQL before migration

**Web integration** — implement interfaces from `web/interfaces.go`:
- `web.WebModule` → `GetWebControllers() []Controller` — registers Echo route groups
- `web.WebModuleStaticFSProvider` → static files (merged across all modules via `mergefs`)
- `web.WebModuleTemplatesFSProvider` → Pongo2 templates (also merged)
- `web.WebModuleTemplateTagsProvider` / `WebModuleTemplateFiltersProvider` → custom template extensions

The `web` and `database` modules **discover** implementors by iterating `manager.GetModulesList()` and checking interface satisfaction — no registration step needed.

### Application entrypoint

```go
type App struct{}
func (a *App) GetID() string      { return "myapp" }
func (a *App) GetName() string    { return "My App" }
func (a *App) GetModules() []maf.Module {
    return []maf.Module{
        logging.NewModule(),
        database.NewModule(),
        security.NewModule(),
        auth.NewModule(),
        web.NewModule(&web.ModuleConfig{...}),
    }
}

func main() {
    manager := maf.New(&App{})
    if err := manager.Start(true); err != nil {
        log.Fatal(err)
    }
}
```

Module order in `GetModules()` determines initialization order — dependencies must appear before dependents.

## Known gaps

- `Manager.configFile` has no public setter — YAML config loading is wired but unreachable
- `logging/module.go` has `setupLogging()` (lowercase) but the interface requires `SetupLogging()` — the logging module does not currently satisfy `ModuleLoggingProvider`
- `config/` package is incomplete (WIP, not integrated)
- No tests exist anywhere in the repository