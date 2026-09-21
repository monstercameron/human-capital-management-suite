package retrypolicy

import (
	"sync"
	"time"
)

// Breaker defaults. A zero-valued field in [BreakerConfig] takes these.
const (
	DefaultFailureThreshold = 5
	DefaultCooldown         = 30 * time.Second
	DefaultMaxCooldown      = 10 * time.Minute
)

// Breaker states as reported by [Breaker.State].
const (
	StateClosed   = "closed"
	StateOpen     = "open"
	StateHalfOpen = "half_open"
)

// BreakerConfig configures a [Breaker].
type BreakerConfig struct {
	// FailureThreshold is the number of consecutive transient failures that
	// opens a closed breaker.
	FailureThreshold int
	// Cooldown is how long a freshly opened breaker refuses traffic before
	// it admits one probe.
	Cooldown time.Duration
	// MaxCooldown caps the cooldown, which doubles on every failed probe.
	MaxCooldown time.Duration
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Breaker is a per-provider circuit breaker. It is safe for concurrent use.
//
// Only TRANSIENT failures may be recorded as failures, and deciding which
// failures are transient is the caller's job: a permanent rejection (a 4xx
// validation error, say) is the payload's fault, not the provider's health,
// and must not trip the breaker — the caller abandons that message and
// records nothing (or records a success, since the provider answered).
//
// State machine:
//
//   - closed: every Allow is true. A failure increments the consecutive
//     failure count; reaching FailureThreshold opens the breaker with the
//     base Cooldown. A success resets the count.
//   - open: Allow is false and reports when a probe may be attempted. Once
//     the cooldown has elapsed the breaker becomes half-open.
//   - half_open: exactly one Allow call gets true (the probe); every other
//     caller gets false until that probe is recorded. A probe success closes
//     the breaker and resets both the count and the cooldown; a probe
//     failure reopens it with the cooldown doubled, capped at MaxCooldown.
//     A probe that is never reported (its caller crashed) would wedge the
//     breaker, so once a granted probe is older than the base Cooldown a new
//     probe is granted.
//
// Reports that arrive while the breaker is open (from calls admitted before
// it tripped) are ignored: they describe the provider before the trip and
// must not shorten or lengthen the cooldown.
type Breaker struct {
	threshold   int
	baseCool    time.Duration
	maxCool     time.Duration
	now         func() time.Time
	mu          sync.Mutex
	state       string
	failures    int
	cooldown    time.Duration
	retryAt     time.Time
	probeActive bool
	probeAt     time.Time
}

// NewBreaker builds a closed breaker.
func NewBreaker(cfg BreakerConfig) *Breaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = DefaultFailureThreshold
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = DefaultCooldown
	}
	if cfg.MaxCooldown <= 0 {
		cfg.MaxCooldown = DefaultMaxCooldown
	}
	if cfg.MaxCooldown < cfg.Cooldown {
		cfg.MaxCooldown = cfg.Cooldown
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Breaker{
		threshold: cfg.FailureThreshold,
		baseCool:  cfg.Cooldown,
		maxCool:   cfg.MaxCooldown,
		now:       cfg.Now,
		state:     StateClosed,
		cooldown:  cfg.Cooldown,
	}
}

// Allow reports whether a call to the provider may proceed. When ok is
// false, retryAt is the earliest time the caller should try again: the end
// of the cooldown while open, or one base Cooldown from now while another
// caller's half-open probe is outstanding. When ok is true retryAt is zero.
func (b *Breaker) Allow() (ok bool, retryAt time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	switch b.state {
	case StateClosed:
		return true, time.Time{}
	case StateOpen:
		if now.Before(b.retryAt) {
			return false, b.retryAt
		}
		b.state = StateHalfOpen
		b.probeActive = true
		b.probeAt = now
		return true, time.Time{}
	default: // half-open
		if !b.probeActive || !now.Before(b.probeAt.Add(b.baseCool)) {
			b.probeActive = true
			b.probeAt = now
			return true, time.Time{}
		}
		return false, now.Add(b.baseCool)
	}
}

// Record reports the outcome of a call Allow admitted. success=false must be
// used only for transient failures (see [Breaker]).
func (b *Breaker) Record(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	switch b.state {
	case StateClosed:
		if success {
			b.failures = 0
			return
		}
		b.failures++
		if b.failures >= b.threshold {
			b.open(now, b.baseCool)
		}
	case StateHalfOpen:
		if success {
			b.state = StateClosed
			b.failures = 0
			b.cooldown = b.baseCool
			b.probeActive = false
			return
		}
		b.open(now, min(b.cooldown*2, b.maxCool))
	default: // open: stale report from before the trip; ignored.
	}
}

func (b *Breaker) open(now time.Time, cooldown time.Duration) {
	b.state = StateOpen
	b.cooldown = cooldown
	b.retryAt = now.Add(cooldown)
	b.probeActive = false
	b.failures = 0
}

// State returns "closed", "open" or "half_open" for logs and metrics. An
// open breaker whose cooldown has elapsed still reports "open" until the
// next Allow admits the probe.
func (b *Breaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Cooldown returns the cooldown currently in force (the base Cooldown until
// a probe fails).
func (b *Breaker) Cooldown() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cooldown
}
