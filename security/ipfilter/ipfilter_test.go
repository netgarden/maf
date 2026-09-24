package ipfilter

import (
	"net"
	"net/http/httptest"
	"testing"
)

func TestNormalizeTurnsBareIPsIntoCIDRs(t *testing.T) {
	out, err := Normalize([]string{"203.0.113.4", "203.0.113.0/24", "2001:db8::1", ""}, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"203.0.113.4/32", "203.0.113.0/24", "2001:db8::1/128"}
	if len(out) != len(want) {
		t.Fatalf("out = %v", out)
	}
	for i, w := range want {
		if out[i] != w {
			t.Errorf("out[%d] = %q, want %q", i, out[i], w)
		}
	}
}

func TestNormalizeRejectsGarbageAndTooManyEntries(t *testing.T) {
	if _, err := Normalize([]string{"not-an-ip"}, 0); err == nil {
		t.Error("expected an error for a malformed entry")
	}
	if _, err := Normalize([]string{"10.0.0.1", "10.0.0.2"}, 1); err == nil {
		t.Error("expected an error above the maximum")
	}
	if _, err := Normalize([]string{"10.0.0.1", "10.0.0.2"}, 0); err != nil {
		t.Errorf("max 0 means unlimited, got %v", err)
	}
}

func TestAllowedEmptyMeansAnywhere(t *testing.T) {
	if !Allowed(nil, net.ParseIP("203.0.113.1")) {
		t.Error("empty allowlist should accept any source")
	}
}

func TestAllowedMatchesCIDRAndExact(t *testing.T) {
	allowed, err := Normalize([]string{"203.0.113.0/24", "198.51.100.9", "2001:db8::/32"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		ip   string
		want bool
	}{
		{"203.0.113.5", true},
		{"203.0.113.255", true},
		{"203.0.114.1", false},
		{"198.51.100.9", true},
		{"198.51.100.10", false},
		{"2001:db8::42", true},
		{"2001:db9::1", false},
	}
	for _, c := range cases {
		if got := Allowed(allowed, net.ParseIP(c.ip)); got != c.want {
			t.Errorf("Allowed(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}

func TestAllowedRejectsUnparseableSourceWhenRestricted(t *testing.T) {
	if Allowed([]string{"10.0.0.0/8"}, nil) {
		t.Error("an unknown source must not pass a restricted allowlist")
	}
}

func TestSourceIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	for remote, want := range map[string]string{
		"203.0.113.7:5555": "203.0.113.7",
		"[2001:db8::1]:80": "2001:db8::1",
		"203.0.113.7":      "203.0.113.7",
		"garbage":          "",
	} {
		r.RemoteAddr = remote
		if got := SourceIP(r); got != want {
			t.Errorf("SourceIP(%q) = %q, want %q", remote, got, want)
		}
	}
}
