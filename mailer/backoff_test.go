package mailer

import (
	"testing"
	"time"
)

func testRetryConfig() RetryConfig {
	return RetryConfig{
		InitialInterval: time.Minute,
		Multiplier:      3,
		MaxInterval:     6 * time.Hour,
		MaxAge:          7 * 24 * time.Hour,
	}
}

func TestNextAttemptDelay_ExponentialGrowth(t *testing.T) {
	cfg := testRetryConfig()

	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, time.Minute},
		{2, 3 * time.Minute},
		{3, 9 * time.Minute},
		{4, 27 * time.Minute},
		{5, 81 * time.Minute},
	}

	for _, tc := range cases {
		got := NextAttemptDelay(tc.attempt, cfg)
		if got != tc.want {
			t.Errorf("attempt %d: got %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestNextAttemptDelay_CapsAtMaxInterval(t *testing.T) {
	cfg := testRetryConfig()

	got := NextAttemptDelay(10, cfg)
	if got != cfg.MaxInterval {
		t.Errorf("expected delay to cap at %v, got %v", cfg.MaxInterval, got)
	}
}

func TestNextAttemptDelay_ClampsBelowOne(t *testing.T) {
	cfg := testRetryConfig()

	got := NextAttemptDelay(0, cfg)
	want := NextAttemptDelay(1, cfg)
	if got != want {
		t.Errorf("attempt 0 should behave like attempt 1: got %v, want %v", got, want)
	}
}

func TestWithJitter_StaysWithinBounds(t *testing.T) {
	d := time.Hour

	for i := 0; i < 1000; i++ {
		got := WithJitter(d)
		min := time.Duration(float64(d) * 0.8)
		max := time.Duration(float64(d) * 1.2)
		if got < min || got > max {
			t.Fatalf("jittered duration %v out of bounds [%v, %v]", got, min, max)
		}
	}
}

func TestHasExceededMaxAge(t *testing.T) {
	cfg := testRetryConfig()

	notExceeded := time.Now().Add(-1 * time.Hour)
	if HasExceededMaxAge(notExceeded, cfg) {
		t.Error("expected 1 hour old email to not have exceeded a 7 day max age")
	}

	exceeded := time.Now().Add(-8 * 24 * time.Hour)
	if !HasExceededMaxAge(exceeded, cfg) {
		t.Error("expected 8 day old email to have exceeded a 7 day max age")
	}

	boundary := time.Now().Add(-cfg.MaxAge - time.Second)
	if !HasExceededMaxAge(boundary, cfg) {
		t.Error("expected an email just past the max age boundary to have exceeded it")
	}
}
