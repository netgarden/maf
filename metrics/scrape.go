package metrics

import (
	"crypto/sha256"
	"crypto/subtle"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/netgarden/maf/security/ipfilter"
	"github.com/netgarden/rrpc"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// scrapeService implements rpc.MetricsService.
type scrapeService struct {
	handler http.Handler
	// allowed is a normalised ipfilter allowlist; empty means any source.
	allowed []string
	// tokenHash is sha256(token); nil means no token is required. Hashing
	// first makes the comparison constant-time regardless of length.
	tokenHash *[sha256.Size]byte
	// clientIP says where a request came from (the TCP peer unless the
	// application supplied something smarter).
	clientIP func(*http.Request) string
}

func newScrapeService(s *Service, token string, allowed []string, clientIP func(*http.Request) string) *scrapeService {
	if clientIP == nil {
		clientIP = ipfilter.SourceIP
	}
	svc := &scrapeService{
		handler:  promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}),
		allowed:  allowed,
		clientIP: clientIP,
	}
	if token != "" {
		h := sha256.Sum256([]byte(token))
		svc.tokenHash = &h
	}
	return svc
}

// Scrape serves the exposition text. The rpc method receives an io.Writer
// (its output is declared as bytes) which is left unused: promhttp writes to
// the response itself, so gzip and the Content-Type are negotiated for us.
func (s *scrapeService) Scrape(ctx *rrpc.Context, _ io.Writer) error {
	r := ctx.Request()
	if !s.authorized(r) {
		// Exactly what an unknown path looks like, whichever check failed:
		// a caller can't tell "wrong token" from "wrong IP" from "no such endpoint".
		return rrpc.ErrRrpcBadRoute.WithCausef("no rrpc method defined for path %v", r.URL.Path)
	}
	s.handler.ServeHTTP(ctx.Response(), r)
	return nil
}

func (s *scrapeService) authorized(r *http.Request) bool {
	ipOK := len(s.allowed) == 0 || ipfilter.Allowed(s.allowed, net.ParseIP(s.clientIP(r)))

	tokenOK := true
	if s.tokenHash != nil {
		got := sha256.Sum256([]byte(bearerToken(r)))
		tokenOK = subtle.ConstantTimeCompare(got[:], s.tokenHash[:]) == 1
	}
	return ipOK && tokenOK
}

// bearerToken returns the token of an "Authorization: Bearer <token>" header, or "".
func bearerToken(r *http.Request) string {
	const prefix = "bearer "
	h := r.Header.Get("Authorization")
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}
