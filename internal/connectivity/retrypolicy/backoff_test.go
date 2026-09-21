package retrypolicy

import (
	"math/rand/v2"
	"testing"
	"time"
)

func constRnd(v float64) func() float64 { return func() float64 { return v } }

func TestBackoff_DefaultsAndEnvelope(t *testing.T) {
	var b Backoff
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 2 * time.Second},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{9, 512 * time.Second},
		{10, 10 * time.Minute},
		{11, 10 * time.Minute},
		{1 << 30, 10 * time.Minute},
	}
	for _, tc := range cases {
		if got := b.Envelope(tc.attempt); got != tc.want {
			t.Fatalf("Envelope(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestBackoff_ExhaustionAtMaxAttempts(t *testing.T) {
	b := Backoff{MaxAttempts: 3}
	for attempt := 1; attempt < 3; attempt++ {
		if _, give := b.Next(attempt, 0, constRnd(0.5)); !give {
			t.Fatalf("attempt %d: give = false, want true", attempt)
		}
	}
	for _, attempt := range []int{3, 4, 100} {
		d, give := b.Next(attempt, time.Minute, constRnd(0.5))
		if give || d != 0 {
			t.Fatalf("attempt %d: (%v, %v), want (0, false)", attempt, d, give)
		}
	}
	// Default MaxAttempts is 12.
	if _, give := (Backoff{}).Next(11, 0, nil); !give {
		t.Fatal("default attempt 11 exhausted")
	}
	if _, give := (Backoff{}).Next(12, 0, nil); give {
		t.Fatal("default attempt 12 not exhausted")
	}
}

func TestBackoff_FullJitterDeterministic(t *testing.T) {
	b := Backoff{}
	if d, _ := b.Next(3, 0, constRnd(0.25)); d != 2*time.Second {
		t.Fatalf("full jitter 0.25 of 8s = %v, want 2s", d)
	}
	if d, _ := b.Next(3, 0, constRnd(0)); d != 0 {
		t.Fatalf("full jitter 0 = %v, want 0", d)
	}
	if d, _ := b.Next(3, 0, nil); d != 8*time.Second {
		t.Fatalf("nil rnd = %v, want envelope 8s", d)
	}
	// Out-of-range rnd values are clamped, never negative or over the envelope.
	if d, _ := b.Next(3, 0, constRnd(-3)); d != 0 {
		t.Fatalf("negative rnd = %v, want 0", d)
	}
	if d, _ := b.Next(3, 0, constRnd(7)); d > 8*time.Second || d < 7*time.Second {
		t.Fatalf("rnd>1 = %v, want just under 8s", d)
	}
}

func TestBackoff_EqualJitter(t *testing.T) {
	b := Backoff{Jitter: JitterEqual}
	if d, _ := b.Next(3, 0, constRnd(0)); d != 4*time.Second {
		t.Fatalf("equal jitter floor = %v, want 4s", d)
	}
	if d, _ := b.Next(3, 0, constRnd(0.5)); d != 6*time.Second {
		t.Fatalf("equal jitter mid = %v, want 6s", d)
	}
}

func TestBackoff_RetryAfter(t *testing.T) {
	b := Backoff{}
	// Floor: jitter would give 0, the provider asked for 30s.
	if d, give := b.Next(1, 30*time.Second, constRnd(0)); !give || d != 30*time.Second {
		t.Fatalf("retry-after floor = (%v, %v), want 30s", d, give)
	}
	// Jitter larger than Retry-After wins.
	if d, _ := b.Next(9, time.Second, constRnd(0.5)); d != 256*time.Second {
		t.Fatalf("jitter over retry-after = %v, want 256s", d)
	}
	// Retry-After above Max is honored up to MaxRetryAfter.
	if d, _ := b.Next(1, 20*time.Minute, constRnd(0)); d != 20*time.Minute {
		t.Fatalf("retry-after above Max = %v, want 20m", d)
	}
	if d, _ := b.Next(1, 5*time.Hour, constRnd(0)); d != time.Hour {
		t.Fatalf("retry-after ceiling = %v, want 1h", d)
	}
	custom := Backoff{MaxRetryAfter: 2 * time.Hour}
	if d, _ := custom.Next(1, 5*time.Hour, constRnd(0)); d != 2*time.Hour {
		t.Fatalf("custom retry-after ceiling = %v, want 2h", d)
	}
	// Negative Retry-After is ignored.
	if d, _ := b.Next(1, -time.Minute, constRnd(0.5)); d != time.Second {
		t.Fatalf("negative retry-after = %v, want 1s", d)
	}
}

func TestBackoff_OddConfigNormalized(t *testing.T) {
	b := Backoff{Base: time.Minute, Max: time.Second, Multiplier: 0.5}
	if got := b.Envelope(5); got != time.Minute {
		t.Fatalf("Max below Base: envelope = %v, want Base", got)
	}
	constant := Backoff{Multiplier: 1}
	if constant.Envelope(1) != constant.Envelope(11) {
		t.Fatal("multiplier 1 is not a constant schedule")
	}
}

// TestBackoff_Properties: delays are never negative, never exceed the cap
// (except the documented Retry-After ceiling), and the envelope is monotone.
func TestBackoff_Properties(t *testing.T) {
	src := rand.New(rand.NewPCG(1, 2))
	for trial := 0; trial < 2000; trial++ {
		b := Backoff{
			Base:        time.Duration(src.Int64N(int64(time.Minute))) + 1,
			Max:         time.Duration(src.Int64N(int64(time.Hour))) + 1,
			Multiplier:  1 + src.Float64()*9,
			Jitter:      Jitter(src.IntN(2)),
			MaxAttempts: 1 + src.IntN(200),
		}
		n := b.withDefaults()
		prev := time.Duration(0)
		for attempt := 1; attempt <= 250; attempt++ {
			env := b.Envelope(attempt)
			if env < prev {
				t.Fatalf("%+v: envelope not monotone at %d: %v < %v", b, attempt, env, prev)
			}
			if env > n.Max || env <= 0 {
				t.Fatalf("%+v: envelope %v out of (0, Max]", b, env)
			}
			prev = env
			var retryAfter time.Duration
			if src.IntN(3) == 0 {
				retryAfter = time.Duration(src.Int64N(int64(3 * time.Hour)))
			}
			d, give := b.Next(attempt, retryAfter, src.Float64)
			if give != (attempt < n.MaxAttempts) {
				t.Fatalf("%+v: attempt %d give=%v", b, attempt, give)
			}
			if d < 0 {
				t.Fatalf("%+v: negative delay %v", b, d)
			}
			limit := n.Max
			if retryAfter > n.Max {
				limit = n.MaxRetryAfter
			}
			if d > limit {
				t.Fatalf("%+v: delay %v exceeds %v (retryAfter %v)", b, d, limit, retryAfter)
			}
			if give && retryAfter > 0 && d < min(retryAfter, n.MaxRetryAfter) {
				t.Fatalf("%+v: delay %v below Retry-After %v", b, d, retryAfter)
			}
			if give && retryAfter == 0 && d > env {
				t.Fatalf("%+v: delay %v above envelope %v", b, d, env)
			}
		}
	}
}
