package ipfilter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func request(peer string, forwarded ...string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = peer
	for _, f := range forwarded {
		r.Header.Add("X-Forwarded-For", f)
	}
	return r
}

func mustProxies(t *testing.T, list ...string) *Proxies {
	t.Helper()
	p, err := ParseProxies(list)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestClientIPFollowsTheChainOnlyThroughTrustedProxies(t *testing.T) {
	p := mustProxies(t, "10.0.0.1", "10.0.0.2", "2001:db8:ffff::/48")

	for name, tc := range map[string]struct {
		req  *http.Request
		want string
	}{
		"no proxy, no header":           {request("203.0.113.9:1234"), "203.0.113.9"},
		"trusted proxy, no header":      {request("10.0.0.1:1234"), "10.0.0.1"},
		"one proxy":                     {request("10.0.0.1:1234", "198.51.100.7"), "198.51.100.7"},
		"two proxies":                   {request("10.0.0.1:1234", "198.51.100.7, 10.0.0.2"), "198.51.100.7"},
		"header on several lines":       {request("10.0.0.1:1234", "198.51.100.7", "10.0.0.2"), "198.51.100.7"},
		"ipv6 client":                   {request("10.0.0.1:1234", "2001:db8::5"), "2001:db8::5"},
		"ipv6 proxy":                    {request("[2001:db8:ffff::9]:443", "198.51.100.7"), "198.51.100.7"},
		"entries with ports":            {request("10.0.0.1:1234", "198.51.100.7:5555"), "198.51.100.7"},
		"bracketed ipv6 with port":      {request("10.0.0.1:1234", "[2001:db8::5]:5555"), "2001:db8::5"},
		"every hop is a proxy":          {request("10.0.0.1:1234", "10.0.0.2"), "10.0.0.2"},
		"peer without a port":           {request("10.0.0.1", "198.51.100.7"), "198.51.100.7"},
		"forged entries left of client": {request("10.0.0.1:1234", "6.6.6.6, 7.7.7.7, 198.51.100.7"), "198.51.100.7"},
	} {
		if got := p.ClientIP(tc.req); got != tc.want {
			t.Errorf("%s: ClientIP = %q, want %q", name, got, tc.want)
		}
	}
}

// The header is written by the client: from a peer that is not a trusted proxy
// it must never be read, whatever it says.
func TestClientIPIgnoresTheHeaderFromAnUntrustedPeer(t *testing.T) {
	p := mustProxies(t, "10.0.0.1")
	for peer, want := range map[string]string{
		"203.0.113.9:1":   "203.0.113.9", // a public address
		"10.0.0.99:1":     "10.0.0.99",   // private, but not one of the listed proxies
		"[2001:db8::1]:1": "2001:db8::1", // IPv6
	} {
		if got := p.ClientIP(request(peer, "198.51.100.7")); got != want {
			t.Errorf("peer %s: ClientIP = %q, want the peer itself (%q)", peer, got, want)
		}
	}
}

func TestClientIPStopsAtMalformedEntries(t *testing.T) {
	p := mustProxies(t, "10.0.0.0/8")
	// A hop that is not an address is tampering: return the last good hop, never the garbage.
	for name, tc := range map[string]struct {
		req  *http.Request
		want string
	}{
		"garbage only":        {request("10.0.0.1:1", "not-an-ip"), "10.0.0.1"},
		"garbage behind good": {request("10.0.0.1:1", "not-an-ip, 10.0.0.5"), "10.0.0.5"},
		"empty entries":       {request("10.0.0.1:1", ", ,198.51.100.7,"), "198.51.100.7"},
		"unknown":             {request("10.0.0.1:1", "unknown"), "10.0.0.1"},
	} {
		if got := p.ClientIP(tc.req); got != tc.want {
			t.Errorf("%s: ClientIP = %q, want %q", name, got, tc.want)
		}
	}
}

func TestClientIPLooksOnlyAtTheEndOfALongChain(t *testing.T) {
	p := mustProxies(t, "10.0.0.1")
	long := strings.Repeat("6.6.6.6, ", 500) + "198.51.100.7"
	if got := p.ClientIP(request("10.0.0.1:1", long)); got != "198.51.100.7" {
		t.Errorf("ClientIP = %q", got)
	}
}

func TestNoProxiesTrustsNobody(t *testing.T) {
	for name, p := range map[string]*Proxies{
		"nil":            nil,
		"empty list":     mustProxies(t),
		"empty variable": mustProxies(t, ""), // IRIS_..._TRUSTED= becomes [""]
		"blanks":         mustProxies(t, " ", ""),
	} {
		if got := p.ClientIP(request("10.0.0.1:1", "198.51.100.7")); got != "10.0.0.1" {
			t.Errorf("%s: ClientIP = %q, want the peer", name, got)
		}
	}
}

func TestPrivateKeywordCoversLoopbackAndPrivateRangesOnly(t *testing.T) {
	p := mustProxies(t, "PRIVATE")
	for ip, want := range map[string]string{
		"127.0.0.1:1": "198.51.100.7", "[::1]:1": "198.51.100.7", "10.1.2.3:1": "198.51.100.7", "172.16.5.5:1": "198.51.100.7",
		"172.31.255.255:1": "198.51.100.7", "192.168.0.10:1": "198.51.100.7", "[fd00::1]:1": "198.51.100.7", "169.254.1.1:1": "198.51.100.7",
		// public, and just outside the private ranges: not trusted
		"203.0.113.9:1": "203.0.113.9", "172.32.0.1:1": "172.32.0.1", "192.169.0.1:1": "192.169.0.1", "11.0.0.1:1": "11.0.0.1",
	} {
		if got := p.ClientIP(request(ip, "198.51.100.7")); got != want {
			t.Errorf("peer %s: ClientIP = %q, want %q", ip, got, want)
		}
	}
	// It combines with explicit entries.
	both := mustProxies(t, "private", "203.0.113.9")
	if got := both.ClientIP(request("203.0.113.9:1", "198.51.100.7")); got != "198.51.100.7" {
		t.Errorf("explicit proxy next to the keyword = %q", got)
	}
}

func TestParseProxiesRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"not-an-ip", "10.0.0.0/33", "privat"} {
		if _, err := ParseProxies([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestResolverTrustsNobodyUntilSet(t *testing.T) {
	r := &Resolver{}
	req := func() *http.Request { return request("10.0.0.1:1", "198.51.100.7") }
	if got := r.ClientIP(req()); got != "10.0.0.1" {
		t.Errorf("before Set = %q, want the peer", got)
	}
	r.Set(mustProxies(t, "private"))
	if got := r.ClientIP(req()); got != "198.51.100.7" {
		t.Errorf("after Set = %q", got)
	}
	r.Set(nil)
	if got := r.ClientIP(req()); got != "10.0.0.1" {
		t.Errorf("after Set(nil) = %q", got)
	}
	var none *Resolver // a nil Resolver is the plain peer too
	if got := none.ClientIP(req()); got != "10.0.0.1" {
		t.Errorf("nil resolver = %q", got)
	}
}

func TestClientIPOfAnUnparseablePeerIsEmpty(t *testing.T) {
	if got := mustProxies(t, "private").ClientIP(request("garbage", "198.51.100.7")); got != "" {
		t.Errorf("ClientIP = %q, want empty", got)
	}
}
