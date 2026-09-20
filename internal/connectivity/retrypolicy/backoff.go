// Package retrypolicy is the retry policy HCM applies when it delivers outbox
// effects (promotion changes, access grants) to third-party providers such as
// payroll and IAM.
//
// Semantic owner: connectivity.
//
// It has two independent parts:
//
//   - [Backoff] answers "this message failed for the n-th time; how long until
//     it may be retried, and may it be retried at all?". It is a pure function
//     of its configuration, the attempt number, an optional provider
//     Retry-After, and an injected random source, so it is deterministic under
//     test.
//   - [Breaker] answers "is this provider healthy enough to send to right
//     now?". It is a per-provider circuit breaker fed only by transient
//     failures.
//
// The package holds no package-level mutable state; time and randomness are
// always injected.
package retrypolicy

import (
	"math"
	"time"
)

// Backoff defaults. A zero-valued field in [Backoff] takes these values.
const (
	DefaultBase          = 2 * time.Second
	DefaultMax           = 10 * time.Minute
	DefaultMultiplier    = 2.0
	DefaultMaxAttempts   = 12
	DefaultMaxRetryAfter = time.Hour
)

// Jitter selects how a delay is randomized inside its envelope.
type Jitter int

const (
	// JitterFull draws uniformly from [0, envelope). It is the zero value and
	// the default: it spreads a thundering herd of retries the most.
	JitterFull Jitter = iota
	// JitterEqual draws uniformly from [envelope/2, envelope): half the
	// envelope is guaranteed, the other half is randomized.
	JitterEqual
)

// Backoff is an exponential-backoff-with-jitter retry schedule. Zero fields
// take the Default* constants, so Backoff{} is a usable policy.
type Backoff struct {
	// Base is the envelope of the first retry (attempt 1).
	Base time.Duration
	// Max caps the envelope of every retry.
	Max time.Duration
	// Multiplier grows the envelope per attempt. Values below 1 (including
	// the zero value) mean DefaultMultiplier; exactly 1 is a constant
	// schedule.
	Multiplier float64
	// Jitter selects the randomization; the zero value is JitterFull.
	Jitter Jitter
	// MaxAttempts is the number of failures after which the message is
	// exhausted and must not be retried.
	MaxAttempts int
	// MaxRetryAfter bounds a provider Retry-After that exceeds Max. See
	// [Backoff.Next].
	MaxRetryAfter time.Duration
}

func (b Backoff) withDefaults() Backoff {
	if b.Base <= 0 {
		b.Base = DefaultBase
	}
	if b.Max <= 0 {
		b.Max = DefaultMax
	}
	if b.Max < b.Base {
		b.Max = b.Base
	}
	if b.Multiplier < 1 || math.IsNaN(b.Multiplier) || math.IsInf(b.Multiplier, 0) {
		b.Multiplier = DefaultMultiplier
	}
	if b.MaxAttempts <= 0 {
		b.MaxAttempts = DefaultMaxAttempts
	}
	if b.MaxRetryAfter <= 0 {
		b.MaxRetryAfter = DefaultMaxRetryAfter
	}
	if b.MaxRetryAfter < b.Max {
		b.MaxRetryAfter = b.Max
	}
	return b
}

// Envelope is the non-jittered upper bound of the delay after attempt
// failures: min(Max, Base*Multiplier^(attempt-1)). Attempts below 1 are
// treated as 1. It is monotone non-decreasing in attempt and never
// overflows: the exponent is evaluated in floating point and compared
// against Max before it is converted back to a Duration.
func (b Backoff) Envelope(attempt int) time.Duration {
	b = b.withDefaults()
	if attempt < 1 {
		attempt = 1
	}
	grown := float64(b.Base) * math.Pow(b.Multiplier, float64(attempt-1))
	if math.IsNaN(grown) || grown >= float64(b.Max) {
		return b.Max
	}
	return time.Duration(grown)
}

// Next returns the delay before the next delivery of a message that has
// failed attempt times (1-based), and whether it may be retried at all.
//
//   - give is false once attempt >= MaxAttempts: the message is exhausted and
//     the caller should park it (the delay is then zero).
//   - The jittered delay is drawn from the envelope (see [Backoff.Envelope])
//     using rnd, which must return values in [0, 1); out-of-range values are
//     clamped and a nil rnd means "no jitter" (the delay is the envelope).
//   - retryAfter is a provider Retry-After and acts as a floor: delay =
//     max(jittered, retryAfter). While retryAfter <= Max the result is still
//     within Max. When retryAfter > Max the provider's explicit instruction
//     outranks our own cap — retrying earlier than it asked is a guaranteed
//     failure that burns an attempt — so the delay is retryAfter clamped to
//     MaxRetryAfter instead, which bounds a buggy or hostile header. That is
//     the only case in which the delay exceeds Max.
//
// Next is deterministic for a deterministic rnd and never negative.
func (b Backoff) Next(attempt int, retryAfter time.Duration, rnd func() float64) (delay time.Duration, give bool) {
	b = b.withDefaults()
	if attempt < 1 {
		attempt = 1
	}
	if attempt >= b.MaxAttempts {
		return 0, false
	}
	envelope := b.Envelope(attempt)
	delay = envelope
	if rnd != nil {
		r := rnd()
		if math.IsNaN(r) || r < 0 {
			r = 0
		}
		if r >= 1 {
			r = math.Nextafter(1, 0)
		}
		switch b.Jitter {
		case JitterEqual:
			half := envelope / 2
			delay = half + time.Duration(r*float64(envelope-half))
		default:
			delay = time.Duration(r * float64(envelope))
		}
	}
	if retryAfter > 0 {
		if retryAfter > b.Max {
			return min(retryAfter, b.MaxRetryAfter), true
		}
		delay = max(delay, retryAfter)
	}
	return min(max(delay, 0), b.Max), true
}
