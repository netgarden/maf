# metrics

`github.com/netgarden/maf/metrics` — Prometheus metrics for maf applications:
a registry other modules define collectors on, a scrape endpoint served
through [`rrpc-server`](../rrpc-server), and HTTP request metrics for every
rrpc route. Needs `rrpc-server` in the application (the endpoint and the
request observer are rrpc things); it does not use `maf/web`.

## Usage

Register it before the modules that define metrics, and have those modules
declare it as a dependency:

```go
func (a *App) GetModules() []maf.Module {
    return []maf.Module{
        // ...
        metrics.NewModule(metrics.WithNamespace("myapp")),
        rrpcserver.NewModule(),
        &Module{}, // yours
    }
}

func (m *Module) GetDependencies() []string { return []string{"metrics"} }

func (m *Module) Initialize() error {
    svc := m.manager.GetModule("metrics").(*metrics.Module).Service()
    m.jobs = svc.NewCounterVec("myapp_jobs_total", "Jobs run.", "outcome")
    m.size = svc.NewDistribution("myapp_payload_bytes", "Payload size.", metrics.SizeBuckets, "kind")
    return nil
}
```

`Service` creates and registers counters, gauges, histograms, "info" gauges
and two composites:

- `NewMinMaxVec` — `<name>_min` / `<name>_max` gauges over a rolling window
  (`metrics.window`, default 5 min). Prometheus has no literal min/max
  (histograms only estimate quantiles) and "max since start" is useless after
  the first outlier. A label combination with no observation in the window
  disappears instead of reporting a stale value.
- `NewDistribution` — a histogram (count/sum give the average, buckets give
  quantiles) plus the rolling min/max above, fed by one `Observe`.

Collectors are always created (so instrumented code never checks whether
metrics are on); only the endpoint and the HTTP metrics depend on
`metrics.enabled`.

## Configuration

| Key | Default | Meaning |
|---|---|---|
| `metrics.enabled` | `false` | serve `GET /metrics` and record HTTP metrics |
| `metrics.token` | empty | bearer token required on scrapes; empty = none |
| `metrics.allowedIps` | empty | comma-separated IPs/CIDR ranges that may scrape; empty = any |
| `metrics.window` | `5m` | window of the `*_min` / `*_max` gauges |

The endpoint is outside user authentication. When a token and an allowlist
are both set, both must pass. A failed check (or a disabled module) answers
exactly like an unknown route, so it does not reveal the endpoint exists. The
allowlist matches the TCP peer address (`RemoteAddr`; `X-Forwarded-For` is
never trusted), so behind a reverse proxy it only ever sees the proxy.

```yaml
scrape_configs:
  - job_name: myapp
    authorization: { credentials: "<metrics.token>" }   # Bearer by default
    static_configs: [{ targets: ["myapp:8000"] }]
```

## HTTP metrics

Recorded for every request the rrpc server handles, via
`rrpc.ResponseObserver` (so errors and unmatched routes are included):

- `<ns>_http_requests_total{method,route,status}`
- `<ns>_http_request_duration_seconds{method,route}` (a WebSocket counts until it closes)
- `<ns>_http_response_bytes{method,route}` plus `_min` / `_max`

`route` is the rrpc path template (`/hooks/{token:string}`), never the raw
path, and `unmatched` when nothing matched; `method` is one of the standard
verbs or `OTHER`. Neither a secret in a URL nor a scanner can create series.

## Development

`rpc/` is generated from `rpc/def/module.rrpc`: `make generate` (never
hand-edit). `go test ./...` needs no database or network.
