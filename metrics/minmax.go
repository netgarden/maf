package metrics

import (
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	// windowSlots is how many time slices the window is cut into: the
	// reported min/max cover between (slots-1)/slots of the window and the
	// full window, and expire slice by slice rather than all at once.
	windowSlots = 10

	// DefaultMaxSeries bounds how many distinct label combinations one
	// MinMaxVec tracks; observations for further combinations are dropped
	// (the histogram of a Distribution still counts them). It is a safety
	// net against an unbounded label, not a budget to plan around.
	DefaultMaxSeries = 1000
)

// MinMaxVec exposes the smallest and largest observed value over a rolling
// window as two gauges, <name>_min and <name>_max. Prometheus histograms
// cannot give a literal min or max (only quantile estimates), and a plain
// "max since process start" gauge is useless after the first outlier; this
// one forgets old values.
//
// A label combination with no observation inside the window is not exported
// at all (no stale zero), so `absent()` alerts and gaps in graphs mean "no
// traffic".
type MinMaxVec struct {
	minDesc, maxDesc *prometheus.Desc
	labels           []string
	window           time.Duration
	step             time.Duration
	now              func() time.Time
	maxSeries        int

	mu     sync.Mutex
	series map[string]*mmSeries
}

type mmSlot struct {
	start    time.Time
	min, max float64
	used     bool
}

type mmSeries struct {
	labelValues []string
	slots       [windowSlots]mmSlot
}

func newMinMaxVec(name, help string, window time.Duration, now func() time.Time, labels []string) *MinMaxVec {
	step := window / windowSlots
	if step <= 0 {
		step = time.Nanosecond
	}
	return &MinMaxVec{
		minDesc:   prometheus.NewDesc(name+"_min", help+" (minimum over the rolling window)", labels, nil),
		maxDesc:   prometheus.NewDesc(name+"_max", help+" (maximum over the rolling window)", labels, nil),
		labels:    labels,
		window:    window,
		step:      step,
		now:       now,
		maxSeries: DefaultMaxSeries,
		series:    map[string]*mmSeries{},
	}
}

// Observe records v for the given label values (one per label, in order).
// A wrong number of values is ignored: instrumentation must never take the
// server down.
func (v *MinMaxVec) Observe(val float64, labelValues ...string) {
	if len(labelValues) != len(v.labels) {
		return
	}
	now := v.now()
	slotStart := now.Truncate(v.step)
	idx := int((slotStart.UnixNano() / int64(v.step)) % windowSlots)
	key := strings.Join(labelValues, "\xff")

	v.mu.Lock()
	defer v.mu.Unlock()
	s, ok := v.series[key]
	if !ok {
		if len(v.series) >= v.maxSeries {
			return
		}
		s = &mmSeries{labelValues: append([]string(nil), labelValues...)}
		v.series[key] = s
	}
	slot := &s.slots[idx]
	if !slot.used || !slot.start.Equal(slotStart) {
		*slot = mmSlot{start: slotStart, min: val, max: val, used: true}
		return
	}
	if val < slot.min {
		slot.min = val
	}
	if val > slot.max {
		slot.max = val
	}
}

// Describe implements prometheus.Collector.
func (v *MinMaxVec) Describe(ch chan<- *prometheus.Desc) {
	ch <- v.minDesc
	ch <- v.maxDesc
}

// Collect implements prometheus.Collector.
func (v *MinMaxVec) Collect(ch chan<- prometheus.Metric) {
	cutoff := v.now().Add(-v.window)

	v.mu.Lock()
	defer v.mu.Unlock()
	for key, s := range v.series {
		var lo, hi float64
		found := false
		for i := range s.slots {
			slot := &s.slots[i]
			if !slot.used || !slot.start.After(cutoff) {
				continue
			}
			if !found || slot.min < lo {
				lo = slot.min
			}
			if !found || slot.max > hi {
				hi = slot.max
			}
			found = true
		}
		if !found {
			delete(v.series, key) // nothing inside the window: forget it, bounding memory
			continue
		}
		ch <- prometheus.MustNewConstMetric(v.minDesc, prometheus.GaugeValue, lo, s.labelValues...)
		ch <- prometheus.MustNewConstMetric(v.maxDesc, prometheus.GaugeValue, hi, s.labelValues...)
	}
}

// Distribution is a histogram (count, sum and so average, plus quantiles via
// buckets) with a rolling min and max next to it: three metric families,
// <name>, <name>_min and <name>_max, fed by one Observe.
type Distribution struct {
	hist *prometheus.HistogramVec
	mm   *MinMaxVec
}

// Observe records v for the given label values (one per label, in order).
func (d *Distribution) Observe(v float64, labelValues ...string) {
	if o, err := d.hist.GetMetricWithLabelValues(labelValues...); err == nil {
		o.Observe(v)
	}
	d.mm.Observe(v, labelValues...)
}
