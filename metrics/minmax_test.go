package metrics

import (
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

// gauges collects a MinMaxVec into "name{labels}" -> value.
func gauges(t *testing.T, c prometheus.Collector) map[string]float64 {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatal(err)
	}
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, f := range families {
		for _, m := range f.Metric {
			key := f.GetName()
			for _, l := range m.Label {
				key += "{" + l.GetName() + "=" + l.GetValue() + "}"
			}
			out[key] = m.Gauge.GetValue()
		}
	}
	return out
}

func TestMinMaxTracksTheWindow(t *testing.T) {
	clk := newTestClock()
	svc := newService(10*time.Minute, clk.now)
	v := svc.NewMinMaxVec("size", "Sizes.", "kind")

	v.Observe(50, "a")
	v.Observe(10, "a")
	clk.advance(3 * time.Minute)
	v.Observe(200, "a")
	v.Observe(7, "b")

	got := gauges(t, v)
	want := map[string]float64{
		"size_min{kind=a}": 10, "size_max{kind=a}": 200,
		"size_min{kind=b}": 7, "size_max{kind=b}": 7,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %v, want %v (all: %v)", k, got[k], w, got)
		}
	}

	// The first slice (10 and 50) ages out of the 10-minute window; 200 stays.
	clk.advance(8 * time.Minute)
	got = gauges(t, v)
	if got["size_min{kind=a}"] != 200 || got["size_max{kind=a}"] != 200 {
		t.Errorf("after the old slice expired, kind=a = min %v max %v, want 200/200", got["size_min{kind=a}"], got["size_max{kind=a}"])
	}
}

func TestMinMaxForgetsSeriesWithNoRecentObservations(t *testing.T) {
	clk := newTestClock()
	svc := newService(time.Minute, clk.now)
	v := svc.NewMinMaxVec("size", "Sizes.", "kind")

	v.Observe(5, "a")
	clk.advance(2 * time.Minute)

	if got := gauges(t, v); len(got) != 0 {
		t.Errorf("an idle series must disappear, not linger as a stale value: %v", got)
	}
	if len(v.series) != 0 {
		t.Errorf("idle series should be dropped from memory, have %d", len(v.series))
	}
}

func TestMinMaxSlotIsReusedNotMixedWithTheOldOne(t *testing.T) {
	clk := newTestClock()
	svc := newService(10*time.Second, clk.now) // 1s slots
	v := svc.NewMinMaxVec("size", "Sizes.", "kind")

	v.Observe(1000, "a")
	clk.advance(10 * time.Second) // the ring wraps onto the same slot index
	v.Observe(3, "a")

	got := gauges(t, v)
	if got["size_max{kind=a}"] != 3 {
		t.Errorf("max = %v; the wrapped slot must start fresh, not keep the old 1000", got["size_max{kind=a}"])
	}
}

func TestMinMaxIgnoresWrongLabelCountAndCapsSeries(t *testing.T) {
	clk := newTestClock()
	svc := newService(time.Minute, clk.now)
	v := svc.NewMinMaxVec("size", "Sizes.", "kind")

	v.Observe(1) // no label value for a one-label vec: ignored, no panic
	v.Observe(1, "a", "b")
	if len(v.series) != 0 {
		t.Fatalf("mislabelled observations must be dropped, have %d series", len(v.series))
	}

	for i := 0; i < DefaultMaxSeries+50; i++ {
		v.Observe(1, strconv.Itoa(i))
	}
	if len(v.series) != DefaultMaxSeries {
		t.Errorf("series = %d, want the cap %d", len(v.series), DefaultMaxSeries)
	}
}

func TestDistributionFeedsHistogramAndMinMax(t *testing.T) {
	clk := newTestClock()
	svc := newService(time.Minute, clk.now)
	d := svc.NewDistribution("payload_bytes", "Payload size.", SizeBuckets, "kind")

	d.Observe(100, "x")
	d.Observe(900, "x")
	d.Observe(1, "x", "extra") // wrong label count: ignored everywhere

	if n := testutil.CollectAndCount(svc.Registry(), "payload_bytes"); n != 1 {
		t.Errorf("histogram series = %d, want 1", n)
	}
	got := gauges(t, d.mm)
	if got["payload_bytes_min{kind=x}"] != 100 || got["payload_bytes_max{kind=x}"] != 900 {
		t.Errorf("min/max = %v", got)
	}
}
