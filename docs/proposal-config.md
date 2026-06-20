# Proposal: Schema-Driven Unified Config

## Design goals

- Modules declare what they need (key, type, default, required)
- One flat `Config` object on the Manager, loaded once before any module initializes
- Dot-notation keys, shared across all modules — cross-module access is just `cfg.GetString("security.secret")`, no imports
- Load order: schema defaults → YAML file → ENV (each overrides the previous)

---

## Overview of moving parts

```
maf/config.go         — ConfigStore interface + ModuleConfigLoader interface
maf/manager.go        — new initConfig() phase, GetConfig(), SetConfigStore()

config/item.go        — Item struct, Type enum
config/config.go      — Config struct (implements ConfigStore)
config/loader.go      — YAML flattener + ENV resolver + Required validation
config/provider.go    — SchemaProvider interface (discovered by the config module)
config/module.go      — Module: collects schemas, runs loader, stores result
```

---

## `maf/config.go` — two new interfaces in the core package

```go
package maf

import "time"

// ConfigStore is the read API exposed on Manager. Defined here so maf has
// no import dependency on the config package.
type ConfigStore interface {
    GetString(key string) string
    GetInt(key string) int
    GetFloat64(key string) float64
    GetBool(key string) bool
    GetDuration(key string) time.Duration
    GetStringSlice(key string) []string
    Sub(prefix string) ConfigStore
}

// ModuleConfigLoader is implemented by the module responsible for building
// the ConfigStore. The manager calls LoadConfig() in a dedicated phase
// before initLogging() and any PreInitialize/Initialize hooks, so all
// modules can access config from the earliest lifecycle phase.
type ModuleConfigLoader interface {
    Module
    LoadConfig() error
}
```

---

## `maf/manager.go` — additions

```go
// New field on Manager:
configStore ConfigStore

// New public methods:
func (m *Manager) GetApplication() Application { return m.application }
func (m *Manager) SetConfigStore(store ConfigStore) { m.configStore = store }
func (m *Manager) GetConfig() ConfigStore { return m.configStore }

// New phase inserted between initModules() and initLogging():
func (m *Manager) initConfig() error {
    for el := m.modules.Front(); el != nil; el = el.Next() {
        loader, ok := el.Value.(ModuleConfigLoader)
        if !ok {
            continue
        }
        slog.Info("Loading config using module: " + loader.GetName())
        if err := loader.LoadConfig(); err != nil {
            return err
        }
    }
    return nil
}
```

Updated `start()` sequence:

```go
func (m *Manager) start() error {
    if err := m.initModules();      err != nil { return err }
    if err := m.initConfig();       err != nil { return err }  // new — replaces initConfiguration()
    if err := m.initLogging();      err != nil { return err }
    if err := m.doPreInitialize();  err != nil { return err }
    if err := m.doInitialize();     err != nil { return err }
    if err := m.doPostInitialize(); err != nil { return err }
    if err := m.doPreStart();       err != nil { return err }
    if err := m.doStart();          err != nil { return err }
}
```

---

## `config/item.go`

```go
package config

import "time"

type Type int

const (
    String Type = iota
    Int
    Float64
    Bool
    Duration
    StringSlice
)

type Item struct {
    Key      string // dot-notation: "auth.sessionTTL", "security.secret"
    Type     Type
    Default  any  // nil = no default; typed value matching Type
    Required bool // startup error if no value and no default
}

// Helpers for legible schema declarations:
func StringItem(key, defaultVal string) Item {
    return Item{Key: key, Type: String, Default: defaultVal}
}
func RequiredString(key string) Item {
    return Item{Key: key, Type: String, Required: true}
}
func IntItem(key string, defaultVal int) Item {
    return Item{Key: key, Type: Int, Default: defaultVal}
}
func DurationItem(key string, defaultVal time.Duration) Item {
    return Item{Key: key, Type: Duration, Default: defaultVal}
}
func BoolItem(key string, defaultVal bool) Item {
    return Item{Key: key, Type: Bool, Default: defaultVal}
}
```

---

## `config/config.go`

```go
package config

import (
    "time"
    "netgarden.dev/maf/maf"
)

type Config struct {
    values map[string]any
    prefix string
}

func (c *Config) fullKey(key string) string {
    if c.prefix == "" {
        return key
    }
    return c.prefix + "." + key
}

func (c *Config) GetString(key string) string {
    v, _ := c.values[c.fullKey(key)]
    s, _ := v.(string)
    return s
}

func (c *Config) GetInt(key string) int {
    v, _ := c.values[c.fullKey(key)]
    n, _ := v.(int)
    return n
}

func (c *Config) GetFloat64(key string) float64 {
    v, _ := c.values[c.fullKey(key)]
    f, _ := v.(float64)
    return f
}

func (c *Config) GetBool(key string) bool {
    v, _ := c.values[c.fullKey(key)]
    b, _ := v.(bool)
    return b
}

func (c *Config) GetDuration(key string) time.Duration {
    v, _ := c.values[c.fullKey(key)]
    d, _ := v.(time.Duration)
    return d
}

func (c *Config) GetStringSlice(key string) []string {
    v, _ := c.values[c.fullKey(key)]
    sl, _ := v.([]string)
    return sl
}

// Sub returns a Config view scoped to the given prefix.
// cfg.Sub("auth").GetString("sessionTTL") == cfg.GetString("auth.sessionTTL")
func (c *Config) Sub(prefix string) maf.ConfigStore {
    full := prefix
    if c.prefix != "" {
        full = c.prefix + "." + prefix
    }
    return &Config{values: c.values, prefix: full}
}
```

---

## `config/loader.go`

```go
package config

import (
    "fmt"
    "gopkg.in/yaml.v3"
    "os"
    "strconv"
    "strings"
    "time"
)

type Loader struct {
    yamlPath string
    appID    string
}

func NewLoader(yamlPath, appID string) *Loader {
    return &Loader{yamlPath: yamlPath, appID: appID}
}

func (l *Loader) Load(schema []Item) (*Config, error) {
    values := make(map[string]any)

    // 1. Defaults
    for _, item := range schema {
        if item.Default != nil {
            values[item.Key] = item.Default
        }
    }

    // 2. YAML file
    if l.yamlPath != "" {
        if err := l.loadYAML(values); err != nil {
            return nil, fmt.Errorf("config yaml: %w", err)
        }
    }

    // 3. Post-process YAML strings for Duration-typed items.
    // YAML has no native duration type; values like "30m" arrive as strings.
    for _, item := range schema {
        if item.Type != Duration {
            continue
        }
        v, ok := values[item.Key]
        if !ok {
            continue
        }
        if s, isStr := v.(string); isStr {
            d, err := time.ParseDuration(s)
            if err != nil {
                return nil, fmt.Errorf("config key %q: invalid duration %q", item.Key, s)
            }
            values[item.Key] = d
        }
    }

    // 4. ENV overrides — only for keys declared in schema
    for _, item := range schema {
        envKey := l.envKey(item.Key)
        raw, ok := os.LookupEnv(envKey)
        if !ok {
            continue
        }
        coerced, err := coerce(raw, item.Type)
        if err != nil {
            return nil, fmt.Errorf("env %s: %w", envKey, err)
        }
        values[item.Key] = coerced
    }

    // 5. Validate required
    var missing []string
    for _, item := range schema {
        if item.Required {
            if _, ok := values[item.Key]; !ok {
                missing = append(missing, item.Key)
            }
        }
    }
    if len(missing) > 0 {
        return nil, fmt.Errorf("required config keys not set: %s", strings.Join(missing, ", "))
    }

    return &Config{values: values}, nil
}

// envKey maps "auth.sessionTTL" → "MYAPP_AUTH_SESSIONTTL"
func (l *Loader) envKey(key string) string {
    s := strings.ReplaceAll(key, ".", "_")
    return strings.ToUpper(l.appID) + "_" + strings.ToUpper(s)
}

func (l *Loader) loadYAML(dst map[string]any) error {
    f, err := os.Open(l.yamlPath)
    if err != nil {
        return err
    }
    defer f.Close()

    raw := make(map[string]any)
    if err := yaml.NewDecoder(f).Decode(&raw); err != nil {
        return err
    }
    flattenMap("", raw, dst)
    return nil
}

func flattenMap(prefix string, src map[string]any, dst map[string]any) {
    for k, v := range src {
        key := k
        if prefix != "" {
            key = prefix + "." + k
        }
        if nested, ok := v.(map[string]any); ok {
            flattenMap(key, nested, dst)
        } else {
            dst[key] = v
        }
    }
}

func coerce(s string, t Type) (any, error) {
    switch t {
    case String:
        return s, nil
    case Int:
        return strconv.Atoi(s)
    case Float64:
        return strconv.ParseFloat(s, 64)
    case Bool:
        return strconv.ParseBool(s)
    case Duration:
        return time.ParseDuration(s)
    case StringSlice:
        return strings.Split(s, ","), nil
    default:
        return nil, fmt.Errorf("unknown type %d", t)
    }
}
```

---

## `config/provider.go`

```go
package config

import "netgarden.dev/maf/maf"

// SchemaProvider is implemented by modules that declare config items.
// The config module discovers all providers during LoadConfig().
type SchemaProvider interface {
    maf.Module
    GetConfigSchema() []Item
}
```

---

## `config/module.go`

```go
package config

import "netgarden.dev/maf/maf"

func NewModule(yamlFile string) *Module {
    return &Module{yamlFile: yamlFile}
}

type Module struct {
    manager  *maf.Manager
    yamlFile string
    config   *Config
}

func (m *Module) GetID() string               { return "config" }
func (m *Module) GetName() string             { return "Config" }
func (m *Module) SetManager(mgr *maf.Manager) { m.manager = mgr }

func (m *Module) LoadConfig() error {
    schema := m.collectSchema()

    cfg, err := NewLoader(m.yamlFile, m.manager.GetApplication().GetID()).Load(schema)
    if err != nil {
        return err
    }

    m.config = cfg
    m.manager.SetConfigStore(cfg)
    return nil
}

func (m *Module) GetConfig() *Config { return m.config }

func (m *Module) collectSchema() []Item {
    var schema []Item
    for _, mod := range m.manager.GetModulesList() {
        if provider, ok := mod.(SchemaProvider); ok {
            schema = append(schema, provider.GetConfigSchema()...)
        }
    }
    return schema
}
```

---

## YAML file structure

```yaml
security:
  secret: "jwt-signing-key"

auth:
  sessionCookieName: session
  sessionTTL: 30m

database:
  dsn: postgres://localhost/mydb
  maxIdleConns: 5
```

ENV equivalents (app ID `myapp`):

```
MYAPP_SECURITY_SECRET=jwt-signing-key
MYAPP_AUTH_SESSIONTTL=30m
MYAPP_DATABASE_DSN=postgres://localhost/mydb
```

---

## Module usage

**Security module — declares and reads its own config:**

```go
func (m *Module) GetConfigSchema() []config.Item {
    return []config.Item{
        config.RequiredString("security.secret"),
        config.IntItem("security.bcryptCost", 12),
    }
}

func (m *Module) Initialize() error {
    cfg := m.manager.GetConfig().Sub("security")
    m.secret = cfg.GetString("secret")
    m.bcryptCost = cfg.GetInt("bcryptCost")
    return nil
}
```

**Auth module — reads cross-module config without importing the security package:**

```go
func (m *Module) GetConfigSchema() []config.Item {
    return []config.Item{
        config.StringItem("auth.sessionCookieName", "session"),
        config.DurationItem("auth.sessionTTL", 20*time.Minute),
    }
}

func (m *Module) Initialize() error {
    cfg := m.manager.GetConfig()
    authCfg := cfg.Sub("auth")

    m.sessionCookieName = authCfg.GetString("sessionCookieName")
    m.sessionTTL = authCfg.GetDuration("sessionTTL")
    m.jwtSecret = cfg.GetString("security.secret") // cross-module, no import
    return nil
}
```

**App wiring — config module must be first:**

```go
func (a *App) GetModules() []maf.Module {
    return []maf.Module{
        config.NewModule("config.yaml"), // first: LoadConfig() runs before everyone else
        logging.NewModule(),
        database.NewModule(),
        security.NewModule(),
        auth.NewModule(),
        web.NewModule(&web.ModuleConfig{...}),
    }
}
```

---

## Summary of what changes

| What | How |
|---|---|
| `maf`: add `ConfigStore` + `ModuleConfigLoader` interfaces | New `maf/config.go` (~25 lines) |
| `maf`: add `initConfig()` phase, `GetConfig()`, `SetConfigStore()`, `GetApplication()` | Edit `maf/manager.go` |
| `config`: full package implementation | New files: `item.go`, `config.go`, `loader.go`, `provider.go`, `module.go` |
| Old `ModuleSetConfig` + per-module config structs | Can be removed once modules migrate |