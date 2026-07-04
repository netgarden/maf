package mailer

import (
	"math"
	"math/rand"
	"time"
)

// RetryConfig controls how failed sends are rescheduled.
type RetryConfig struct {
	InitialInterval time.Duration
	Multiplier      float64
	MaxInterval     time.Duration
	MaxAge          time.Duration
}

// NextAttemptDelay returns the exponential backoff delay before the given
// attempt number (1-indexed: the delay before the 2nd attempt is
// NextAttemptDelay(1, cfg), matching Email.Attempts right after the first
// attempt has been recorded), capped at cfg.MaxInterval.
func NextAttemptDelay(attempt int, cfg RetryConfig) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := float64(cfg.InitialInterval) * math.Pow(cfg.Multiplier, float64(attempt-1))
	if max := float64(cfg.MaxInterval); delay > max {
		delay = max
	}
	return time.Duration(delay)
}

// WithJitter returns d adjusted by up to ±20%, so many emails failing in
// the same tick (e.g. the SMTP server was briefly down) don't all retry at
// exactly the same instant.
func WithJitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.8 + rand.Float64()*0.4))
}

// HasExceededMaxAge reports whether an email created at createdAt should
// stop retrying and be given up on permanently.
func HasExceededMaxAge(createdAt time.Time, cfg RetryConfig) bool {
	return time.Now().After(createdAt.Add(cfg.MaxAge))
}
