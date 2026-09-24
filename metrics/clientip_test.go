package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netgarden/maf/security/ipfilter"
)

func scrapeVia(h http.Handler, peer, forwarded string) int {
	r := httptest.NewRequest("GET", "/metrics", nil)
	r.RemoteAddr = peer
	if forwarded != "" {
		r.Header.Set("X-Forwarded-For", forwarded)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

// Behind a reverse proxy the allowlist must judge the client, not the proxy;
// and only when the peer really is a trusted proxy.
func TestAllowlistUsesTheClientBehindATrustedProxy(t *testing.T) {
	resolver := &ipfilter.Resolver{}
	proxies, err := ipfilter.ParseProxies([]string{"10.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	resolver.Set(proxies)
	_, h := newServer(t, map[string]any{
		"metrics.enabled":    true,
		"metrics.allowedIps": []string{"198.51.100.0/24"}, // the monitoring host
	}, WithClientIP(resolver.ClientIP))

	for name, tc := range map[string]struct {
		peer, forwarded string
		want            int
	}{
		"allowed client via the proxy":    {"10.0.0.1:1", "198.51.100.7", http.StatusOK},
		"other client via the proxy":      {"10.0.0.1:1", "203.0.113.9", http.StatusNotFound},
		"the proxy itself is not on it":   {"10.0.0.1:1", "", http.StatusNotFound},
		"forged prefix, real client out":  {"10.0.0.1:1", "198.51.100.7, 203.0.113.9", http.StatusNotFound},
		"header from an untrusted peer":   {"203.0.113.9:1", "198.51.100.7", http.StatusNotFound},
		"allowed peer, no proxy involved": {"198.51.100.7:1", "", http.StatusOK},
	} {
		if got := scrapeVia(h, tc.peer, tc.forwarded); got != tc.want {
			t.Errorf("%s: status = %d, want %d", name, got, tc.want)
		}
	}
}

// Without the option the source is the TCP peer, as before.
func TestAllowlistDefaultsToTheTCPPeer(t *testing.T) {
	_, h := newServer(t, map[string]any{
		"metrics.enabled":    true,
		"metrics.allowedIps": []string{"198.51.100.0/24"},
	})
	if got := scrapeVia(h, "203.0.113.9:1", "198.51.100.7"); got != http.StatusNotFound {
		t.Errorf("a forwarded header must not be believed by default: status = %d", got)
	}
	if got := scrapeVia(h, "198.51.100.7:1", ""); got != http.StatusOK {
		t.Errorf("the peer itself: status = %d", got)
	}
}
