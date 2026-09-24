package ipfilter

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
)

// KeywordPrivate stands, in a Proxies list, for every loopback, link-local and
// private address: 127.0.0.0/8, ::1, 169.254.0.0/16, fe80::/10, the RFC 1918
// ranges (10/8, 172.16/12, 192.168/16) and fc00::/7. That is where a reverse
// proxy normally sits (the same host, a container network, the LAN).
const KeywordPrivate = "private"

var privateRanges = []string{
	"127.0.0.0/8", "::1/128", "169.254.0.0/16", "fe80::/10",
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7",
}

// maxForwardedHops bounds how much of an X-Forwarded-For header is looked at:
// a legitimate chain is a few hops, and the header is client-controlled.
const maxForwardedHops = 32

// Proxies is the set of reverse proxies whose X-Forwarded-For header is
// believed. The zero value (and nil) trusts nobody.
//
// X-Forwarded-For is written by whoever sends the request: a client can put any
// address in it. It is therefore only ever read when the TCP peer itself is a
// trusted proxy, and then only the part that proxies added: the chain is
// walked from the right, through trusted proxies, and the first address that is
// not one is the client. Anything the client wrote to the left of that is
// ignored.
type Proxies struct {
	nets []*net.IPNet
}

// ParseProxies builds a Proxies from IPs, CIDR ranges and KeywordPrivate.
// Empty entries are dropped, so an empty list (or one empty string, which is
// what an empty environment variable becomes) trusts no proxy at all.
func ParseProxies(list []string) (*Proxies, error) {
	var expanded []string
	for _, s := range list {
		s = strings.TrimSpace(s)
		switch {
		case s == "":
		case strings.EqualFold(s, KeywordPrivate):
			expanded = append(expanded, privateRanges...)
		default:
			expanded = append(expanded, s)
		}
	}
	cidrs, err := Normalize(expanded, 0)
	if err != nil {
		return nil, fmt.Errorf("trusted proxies: %w", err)
	}
	p := &Proxies{}
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("trusted proxies: %w", err)
		}
		p.nets = append(p.nets, n)
	}
	return p, nil
}

// Trusts reports whether ip is one of the trusted proxies.
func (p *Proxies) Trusts(ip net.IP) bool {
	if p == nil || ip == nil {
		return false
	}
	for _, n := range p.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP returns the address the request really came from: the TCP peer,
// unless the peer is a trusted proxy, in which case the X-Forwarded-For chain
// it (and the proxies before it) added is followed back to the first address
// that is not a trusted proxy. It returns "" only when the peer address itself
// cannot be parsed.
//
// If the chain is malformed (an entry that is not an address) the walk stops at
// the last good hop: garbage is never returned as an address.
func (p *Proxies) ClientIP(r *http.Request) string {
	peer := net.ParseIP(SourceIP(r))
	if peer == nil {
		return ""
	}
	cur := peer
	if p.Trusts(cur) {
		hops := forwardedFor(r)
		for i := len(hops) - 1; i >= 0 && p.Trusts(cur); i-- {
			ip := parseHop(hops[i])
			if ip == nil {
				break
			}
			cur = ip
		}
	}
	return cur.String()
}

// forwardedFor returns the addresses of every X-Forwarded-For header line,
// in order (the header may be repeated, and each line is comma-separated).
// At most the last maxForwardedHops are kept: the end is what proxies added.
func forwardedFor(r *http.Request) []string {
	var hops []string
	for _, line := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(line, ",") {
			if part = strings.TrimSpace(part); part != "" {
				hops = append(hops, part)
			}
		}
	}
	if len(hops) > maxForwardedHops {
		hops = hops[len(hops)-maxForwardedHops:]
	}
	return hops
}

// parseHop reads one X-Forwarded-For entry: an IP, optionally with a port, and
// with brackets around an IPv6 address.
func parseHop(s string) net.IP {
	if ip := net.ParseIP(s); ip != nil {
		return ip
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		return net.ParseIP(host)
	}
	return net.ParseIP(strings.Trim(s, "[]"))
}

// Resolver hands out the client address for a request, using whatever Proxies
// was set last. It exists so the code that needs an address (the webhooks
// endpoint, the metrics scrape check) can be given one function when an
// application is assembled, before the configuration that says which proxies
// to trust has been read. Until Set is called it trusts no proxy and returns
// the TCP peer. Safe for concurrent use.
type Resolver struct {
	p atomic.Pointer[Proxies]
}

// Set replaces the trusted proxies (nil = trust none).
func (r *Resolver) Set(p *Proxies) { r.p.Store(p) }

// ClientIP is Proxies.ClientIP with the current Proxies.
func (r *Resolver) ClientIP(req *http.Request) string {
	if r == nil {
		return SourceIP(req)
	}
	return r.p.Load().ClientIP(req)
}
