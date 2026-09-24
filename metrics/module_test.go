package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/netgarden/maf"
	"github.com/netgarden/rrpc"
)

// newServer builds the module from config values and serves it through a real
// rrpc.Server, wired the way maf/rrpc-server does it (modules, then observers).
func newServer(t *testing.T, values map[string]any, opts ...Option) (*Module, http.Handler) {
	t.Helper()
	cfg := map[string]any{
		"metrics.enabled":    false,
		"metrics.token":      "",
		"metrics.allowedIps": []string{},
		"metrics.window":     time.Minute,
	}
	for k, v := range values {
		cfg[k] = v
	}
	m := NewModule(append([]Option{WithNamespace("test")}, opts...)...)
	m.SetConfig(maf.NewConfig(cfg))
	if err := m.Initialize(); err != nil {
		t.Fatal(err)
	}
	srv := rrpc.NewServer()
	srv.Observe(m.GetRRPCObservers()...)
	for _, mod := range m.GetRRPCModules() {
		if err := srv.RegisterModule(mod); err != nil {
			t.Fatal(err)
		}
	}
	return m, srv
}

func scrape(h http.Handler, remote, authorization string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/metrics", nil)
	r.RemoteAddr = remote + ":4321"
	if authorization != "" {
		r.Header.Set("Authorization", authorization)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestScrapeServesExpositionText(t *testing.T) {
	m, h := newServer(t, map[string]any{"metrics.enabled": true})
	m.Service().NewCounterVec("test_things_total", "Things.", "kind").WithLabelValues("a").Add(3)

	w := scrape(h, "203.0.113.9", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want the Prometheus text format", ct)
	}
	body := w.Body.String()
	for _, want := range []string{`test_things_total{kind="a"} 3`, "go_goroutines", "process_"} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape lacks %q", want)
		}
	}
}

func TestDisabledLooksLikeAnUnknownRoute(t *testing.T) {
	_, h := newServer(t, nil)
	w := scrape(h, "203.0.113.9", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestTokenIsRequiredWhenConfigured(t *testing.T) {
	_, h := newServer(t, map[string]any{"metrics.enabled": true, "metrics.token": "s3cret"})

	for name, auth := range map[string]string{
		"no header":    "",
		"wrong token":  "Bearer nope",
		"wrong scheme": "Basic s3cret",
		"empty bearer": "Bearer ",
		"prefix only":  "Bearer s3cre",
	} {
		if w := scrape(h, "203.0.113.9", auth); w.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, w.Code)
		}
	}
	for _, auth := range []string{"Bearer s3cret", "bearer s3cret"} {
		if w := scrape(h, "203.0.113.9", auth); w.Code != http.StatusOK {
			t.Errorf("%q: status = %d, want 200", auth, w.Code)
		}
	}
}

func TestAllowedIPsRestrictTheSource(t *testing.T) {
	_, h := newServer(t, map[string]any{
		"metrics.enabled":    true,
		"metrics.allowedIps": []string{"203.0.113.0/24", "198.51.100.9"},
	})
	for remote, want := range map[string]int{
		"203.0.113.77": http.StatusOK,
		"198.51.100.9": http.StatusOK,
		"198.51.100.8": http.StatusNotFound,
		"192.0.2.1":    http.StatusNotFound,
	} {
		if w := scrape(h, remote, ""); w.Code != want {
			t.Errorf("from %s: status = %d, want %d", remote, w.Code, want)
		}
	}
}

func TestTokenAndIPBothMustPassAndFailIdentically(t *testing.T) {
	_, h := newServer(t, map[string]any{
		"metrics.enabled":    true,
		"metrics.token":      "s3cret",
		"metrics.allowedIps": []string{"203.0.113.9"},
	})

	if w := scrape(h, "203.0.113.9", "Bearer s3cret"); w.Code != http.StatusOK {
		t.Fatalf("right token from the right IP: status = %d", w.Code)
	}
	wrongToken := scrape(h, "203.0.113.9", "Bearer nope")
	wrongIP := scrape(h, "192.0.2.1", "Bearer s3cret")
	unknown := httptest.NewRecorder()
	h.ServeHTTP(unknown, httptest.NewRequest("GET", "/metrics/nope", nil))

	// No oracle: which check failed (or whether the endpoint exists) is not observable.
	for name, w := range map[string]*httptest.ResponseRecorder{"wrong token": wrongToken, "wrong IP": wrongIP} {
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, w.Code)
		}
	}
	if wrongToken.Body.String() != wrongIP.Body.String() {
		t.Errorf("wrong token and wrong IP answer differently:\n%s\n%s", wrongToken.Body, wrongIP.Body)
	}
}

func TestBadConfigFailsStartup(t *testing.T) {
	for name, values := range map[string]map[string]any{
		"bad ip":      {"metrics.enabled": true, "metrics.allowedIps": []string{"not-an-ip"}},
		"zero window": {"metrics.window": time.Duration(0)},
	} {
		m := NewModule()
		cfg := map[string]any{"metrics.enabled": false, "metrics.token": "", "metrics.allowedIps": []string{}, "metrics.window": time.Minute}
		for k, v := range values {
			cfg[k] = v
		}
		m.SetConfig(maf.NewConfig(cfg))
		if err := m.Initialize(); err == nil {
			t.Errorf("%s: expected Initialize to fail", name)
		}
	}
}

func TestHTTPMetricsUseRouteTemplatesAndBoundedLabels(t *testing.T) {
	m, h := newServer(t, map[string]any{"metrics.enabled": true})
	_ = m

	// Requests to paths that match nothing, and a weird method: none may mint a series.
	for _, p := range []string{"/a", "/b/c", "/wp-admin.php"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", p, nil))
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("BREW", "/coffee", nil))
	scrape(h, "203.0.113.9", "")

	body := scrape(h, "203.0.113.9", "").Body.String()
	for _, want := range []string{
		`test_http_requests_total{method="GET",route="unmatched",status="404"} 3`,
		`test_http_requests_total{method="OTHER",route="unmatched",status="404"} 1`,
		`test_http_requests_total{method="GET",route="/metrics",status="200"} 1`,
		`test_http_response_bytes_max{method="GET",route="unmatched"}`,
		`test_http_response_bytes_count{method="GET",route="unmatched"} 3`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape lacks %q\n%s", want, grep(body, "test_http_requests_total"))
		}
	}
	for _, leaked := range []string{"wp-admin", "/coffee", "/b/c"} {
		if strings.Contains(body, leaked) {
			t.Errorf("a raw request path (%q) leaked into a label", leaked)
		}
	}
}

func grep(s, sub string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, sub) {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
