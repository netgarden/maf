package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Common bucket layouts.
var (
	// SizeBuckets suits byte sizes: 64 B to 16 MiB, x4 per bucket.
	SizeBuckets = prometheus.ExponentialBuckets(64, 4, 10)
	// LongDurationBuckets suits work that takes seconds to an hour (an agent
	// turn): 0.5 s to ~68 min, x2 per bucket.
	LongDurationBuckets = prometheus.ExponentialBuckets(0.5, 2, 14)
)

// Service is what other modules use to define metrics. All collectors it
// creates are registered on its own Registry (never the process-global
// default), which is what the scrape endpoint serves.
//
// A metric name must be unique in the registry; registering one twice panics,
// like prometheus.MustRegister: it is a programming error found at startup.
type Service struct {
	registry *prometheus.Registry
	window   time.Duration
	now      func() time.Time
}

// NewService creates a Service with the Go runtime and process collectors
// registered. window is the rolling window of min/max gauges.
func NewService(window time.Duration) *Service {
	return newService(window, time.Now)
}

func newService(window time.Duration, now func() time.Time) *Service {
	s := &Service{registry: prometheus.NewRegistry(), window: window, now: now}
	s.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return s
}

// Registry is the registry behind the scrape endpoint, for collectors this
// package has no helper for.
func (s *Service) Registry() *prometheus.Registry { return s.registry }

// Window is the rolling window of min/max gauges.
func (s *Service) Window() time.Duration { return s.window }

func (s *Service) NewCounterVec(name, help string, labels ...string) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)
	s.registry.MustRegister(c)
	return c
}

func (s *Service) NewGaugeVec(name, help string, labels ...string) *prometheus.GaugeVec {
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labels)
	s.registry.MustRegister(g)
	return g
}

func (s *Service) NewHistogramVec(name, help string, buckets []float64, labels ...string) *prometheus.HistogramVec {
	h := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: help, Buckets: buckets}, labels)
	s.registry.MustRegister(h)
	return h
}

// NewMinMaxVec registers <name>_min and <name>_max gauges over the rolling window.
func (s *Service) NewMinMaxVec(name, help string, labels ...string) *MinMaxVec {
	v := newMinMaxVec(name, help, s.window, s.now, labels)
	s.registry.MustRegister(v)
	return v
}

// NewDistribution registers a histogram <name> and the rolling gauges
// <name>_min and <name>_max, all with the same labels.
func (s *Service) NewDistribution(name, help string, buckets []float64, labels ...string) *Distribution {
	return &Distribution{
		hist: s.NewHistogramVec(name, help, buckets, labels...),
		mm:   s.NewMinMaxVec(name, help, labels...),
	}
}

// RegisterInfo exposes a constant 1-valued gauge whose labels carry
// information (a build/version "info" metric), e.g.
// RegisterInfo("iris_build_info", "Build information.", map[string]string{"version": v}).
func (s *Service) RegisterInfo(name, help string, labels map[string]string) {
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: name, Help: help, ConstLabels: labels})
	g.Set(1)
	s.registry.MustRegister(g)
}
