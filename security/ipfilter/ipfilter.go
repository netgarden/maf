// Package ipfilter validates and matches source-IP allowlists: lists of plain
// IPs and CIDR ranges, where an empty list means "any source".
//
// Normalize once when a list is stored or configured, then Allowed on every
// request only ever has to deal with CIDRs.
//
// The address handed to Allowed is only as trustworthy as where it came from:
// http.Request.RemoteAddr behind a reverse proxy is the proxy's address, which
// silently defeats an allowlist. Nothing here trusts X-Forwarded-For.
package ipfilter

import (
	"fmt"
	"net"
	"net/http"
)

// Normalize validates an allowlist and returns it with every bare IP turned
// into a /32 (or /128 for IPv6) CIDR. Empty entries are dropped. It fails when
// an entry is neither an IP nor a CIDR range, or when the list has more than
// max entries (max <= 0 means unlimited).
func Normalize(in []string, max int) ([]string, error) {
	if max > 0 && len(in) > max {
		return nil, fmt.Errorf("at most %d allowed IPs/ranges", max)
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(s); err == nil {
			out = append(out, s)
			continue
		}
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, fmt.Errorf("%q is not a valid IP or CIDR range", s)
		}
		if ip.To4() != nil {
			out = append(out, ip.String()+"/32")
		} else {
			out = append(out, ip.String()+"/128")
		}
	}
	return out, nil
}

// Allowed reports whether remote matches the allowlist. An empty list means
// "any source". Entries must have been through Normalize; a malformed one
// simply never matches.
func Allowed(allowed []string, remote net.IP) bool {
	if len(allowed) == 0 {
		return true
	}
	if remote == nil {
		return false
	}
	for _, s := range allowed {
		if _, ipNet, err := net.ParseCIDR(s); err == nil && ipNet.Contains(remote) {
			return true
		}
	}
	return false
}

// SourceIP returns the address a request's TCP connection came from
// (r.RemoteAddr without the port), or "" when it cannot be parsed.
func SourceIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		if net.ParseIP(r.RemoteAddr) != nil {
			return r.RemoteAddr
		}
		return ""
	}
	return host
}
