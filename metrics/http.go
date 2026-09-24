package metrics

import (
	"net/http"
	"strconv"

	"github.com/netgarden/rrpc"
	"github.com/prometheus/client_golang/prometheus"
)

// httpObserver returns the rrpc.ResponseObserver behind the HTTP metrics:
//
//	<ns>_http_requests_total{method,route,status}
//	<ns>_http_request_duration_seconds{method,route}
//	<ns>_http_response_bytes{method,route}            (+ _min/_max)
//
// route is the rrpc path template ("/hooks/{token:string}"), never the raw
// path, and "unmatched" for a request that matched no route, so neither a
// token in a URL nor a scanner probing random paths can create series.
func (s *Service) httpObserver(prefix string) rrpc.ResponseObserver {
	requests := s.NewCounterVec(prefix+"http_requests_total", "HTTP requests handled.", "method", "route", "status")
	duration := s.NewHistogramVec(prefix+"http_request_duration_seconds", "HTTP request duration (a WebSocket counts until it closes).",
		prometheus.DefBuckets, "method", "route")
	size := s.NewDistribution(prefix+"http_response_bytes", "HTTP response body size.", SizeBuckets, "method", "route")

	return func(r *http.Request, info rrpc.ResponseInfo) {
		method, route := methodLabel(r.Method), info.Route
		if route == "" {
			route = "unmatched"
		}
		requests.WithLabelValues(method, route, strconv.Itoa(info.Status)).Inc()
		duration.WithLabelValues(method, route).Observe(info.Duration.Seconds())
		size.Observe(float64(info.Bytes), method, route)
	}
}

// methodLabel keeps the method label to a fixed set; the method is client-chosen.
func methodLabel(m string) string {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodPatch, http.MethodHead, http.MethodOptions:
		return m
	}
	return "OTHER"
}
