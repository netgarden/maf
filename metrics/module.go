// Package metrics is the maf module that exposes Prometheus metrics: a
// registry other modules define their collectors on (Service), a scrape
// endpoint served through rrpc-server, and HTTP request metrics for every
// rrpc route.
//
// Register it before the modules that define metrics (they declare it in
// GetDependencies and fetch the Service in Initialize):
//
//	m := metrics.NewModule(metrics.WithNamespace("myapp"))
//	...
//	svc := manager.GetModule("metrics").(*metrics.Module).Service()
//	jobs := svc.NewCounterVec("myapp_jobs_total", "Jobs run.", "outcome")
//
// The scrape endpoint is GET /metrics, off unless metrics.enabled. It is
// outside any user authentication; protect it with metrics.token (bearer
// token) and/or metrics.allowedIps (sources), both optional, both required to
// pass when both are set. A failed check answers like an unknown route.
package metrics

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/netgarden/maf"
	"github.com/netgarden/maf/metrics/rpc"
	"github.com/netgarden/maf/security/ipfilter"
	"github.com/netgarden/rrpc"
)

// DefaultWindow is the default rolling window of min/max gauges.
const DefaultWindow = 5 * time.Minute

// Option configures NewModule.
type Option func(*Module)

// WithNamespace prefixes the module's own metric names ("<ns>_http_...").
func WithNamespace(ns string) Option {
	return func(m *Module) { m.namespace = ns }
}

// WithClientIP sets how the scrape source is determined, for metrics.allowedIps
// (default: the TCP peer). Behind a reverse proxy, pass
// (*ipfilter.Resolver).ClientIP so the real client is checked, not the proxy.
func WithClientIP(f func(*http.Request) string) Option {
	return func(m *Module) { m.clientIP = f }
}

func NewModule(opts ...Option) *Module {
	m := &Module{}
	for _, o := range opts {
		o(m)
	}
	return m
}

type Module struct {
	manager   *maf.Manager
	config    *maf.Config
	namespace string
	clientIP  func(*http.Request) string

	enabled bool
	scrape  *scrapeService
	service *Service
	observe rrpc.ResponseObserver
}

func (m *Module) GetID() string   { return "metrics" }
func (m *Module) GetName() string { return "Metrics" }

func (m *Module) SetManager(manager *maf.Manager) { m.manager = manager }

// Config keys (an app maps them to its own env names, e.g. IRIS_METRICS_ENABLED).
func (m *Module) GetConfigSchema() []maf.ConfigItem {
	return []maf.ConfigItem{
		// Serve GET /metrics. Off by default; collectors are still created so
		// instrumented code never has to care.
		{Name: "metrics.enabled", Type: maf.Bool, DefaultValue: false},
		// Bearer token required on scrapes; empty = none required.
		{Name: "metrics.token", Type: maf.String, DefaultValue: ""},
		// Comma-separated IPs/CIDR ranges allowed to scrape; empty = any source.
		// The source is the TCP peer unless the application passed WithClientIP:
		// without it, behind a reverse proxy this only sees the proxy.
		{Name: "metrics.allowedIps", Type: maf.StringSlice, DefaultValue: []string{}},
		// Rolling window of the *_min / *_max gauges.
		{Name: "metrics.window", Type: maf.Duration, DefaultValue: DefaultWindow},
	}
}

func (m *Module) SetConfig(config *maf.Config) { m.config = config }

func (m *Module) Initialize() error {
	cfg := m.config.Sub("metrics")

	window := cfg.GetDuration("window")
	if window <= 0 {
		return errors.New("metrics.window must be positive")
	}
	m.service = NewService(window)

	m.enabled = cfg.GetBool("enabled")
	if !m.enabled {
		return nil
	}
	allowed, err := ipfilter.Normalize(cfg.GetStringSlice("allowedIps"), 0)
	if err != nil {
		return fmt.Errorf("metrics.allowedIps: %w", err)
	}
	m.scrape = newScrapeService(m.service, cfg.GetString("token"), allowed, m.clientIP)

	prefix := ""
	if m.namespace != "" {
		prefix = m.namespace + "_"
	}
	m.observe = m.service.httpObserver(prefix)
	return nil
}

// Service is where other modules define their metrics; valid from this
// module's Initialize on (declare "metrics" in GetDependencies).
func (m *Module) Service() *Service { return m.service }

// Enabled reports whether the scrape endpoint is served.
func (m *Module) Enabled() bool { return m.enabled }

// GetRRPCModules registers the scrape endpoint, only when enabled: otherwise
// the path is genuinely unknown.
func (m *Module) GetRRPCModules() []rrpc.ServerModule {
	if !m.enabled {
		return nil
	}
	mod := rpc.NewModule()
	mod.SetMetricsService(m.scrape)
	return []rrpc.ServerModule{mod}
}

// GetRRPCObservers feeds the HTTP request metrics (only when enabled).
func (m *Module) GetRRPCObservers() []rrpc.ResponseObserver {
	if !m.enabled {
		return nil
	}
	return []rrpc.ResponseObserver{m.observe}
}
