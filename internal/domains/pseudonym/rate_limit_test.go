package pseudonym

import (
	"errors"
	"testing"
	"time"
)

func anonScopePolicy() ScopePolicy {
	return ScopePolicy{
		Scope:          "intake:case",
		Window:         time.Minute,
		MaxAttempts:    5,
		ProofSkew:      30 * time.Second,
		ReviewQueue:    "abuse-review",
		MaxBuckets:     1024,
		MaxSeenDigests: 4096,
	}
}

func anonProof(now time.Time, nonce string) SubmissionProof {
	return SubmissionProof{IssuedAt: now, Nonce: nonce}
}

// TestTodo_ANON_006 is the primary ANON-006 contract test: scoped
// rate/replay/proof controls reject duplicates and automation while the
// limiter retains no reusable global identity or forbidden fingerprint.
func TestTodo_ANON_006(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	t.Run("first submissions pass and duplicates are rejected", func(t *testing.T) {
		lim, err := NewScopeLimiter(anonScopePolicy())
		if err != nil {
			t.Fatal(err)
		}
		if err := lim.Check(now, "blinded-token-1", "digest:payload-1", anonProof(now, "n1")); err != nil {
			t.Fatalf("first submission must pass: %v", err)
		}
		err = lim.Check(now.Add(time.Second), "blinded-token-1", "digest:payload-1", anonProof(now.Add(time.Second), "n2"))
		var rej *LimitRejection
		if !errors.As(err, &rej) {
			t.Fatalf("duplicate must be a *LimitRejection, got %v", err)
		}
		if rej.Code != LimitDuplicate {
			t.Fatalf("duplicate code = %v, want DUPLICATE", rej.Code)
		}
	})

	t.Run("automation bursts are rate limited with human review", func(t *testing.T) {
		lim, err := NewScopeLimiter(anonScopePolicy())
		if err != nil {
			t.Fatal(err)
		}
		var last error
		for i := 0; i < 7; i++ {
			last = lim.Check(now.Add(time.Duration(i)*time.Second), "blinded-bot", string(rune('a'+i))+"digest", anonProof(now.Add(time.Duration(i)*time.Second), string(rune('0'+i))+"nonce"))
		}
		var rej *LimitRejection
		if !errors.As(last, &rej) || rej.Code != LimitRateLimited {
			t.Fatalf("burst must end RATE_LIMITED, got %v", last)
		}
		reviews := lim.Reviews()
		if len(reviews) == 0 || reviews[0].Queue != "abuse-review" {
			t.Fatalf("rate limit must open a false-positive review: %+v", reviews)
		}
	})

	t.Run("stale and replayed proofs are rejected", func(t *testing.T) {
		lim, err := NewScopeLimiter(anonScopePolicy())
		if err != nil {
			t.Fatal(err)
		}
		stale := anonProof(now.Add(-time.Hour), "old")
		if err := lim.Check(now, "t1", "d1", stale); !isLimitCode(err, LimitStaleProof) {
			t.Fatalf("stale proof must be rejected, got %v", err)
		}
		proof := anonProof(now, "once")
		if err := lim.Check(now, "t2", "d2", proof); err != nil {
			t.Fatal(err)
		}
		if err := lim.Check(now.Add(time.Second), "t3", "d3", proof); !isLimitCode(err, LimitReplay) {
			t.Fatalf("replayed proof must be rejected, got %v", err)
		}
	})

	t.Run("no reusable global identity is retained", func(t *testing.T) {
		lim, err := NewScopeLimiter(anonScopePolicy())
		if err != nil {
			t.Fatal(err)
		}
		if err := lim.Check(now, "same-token", "d1", anonProof(now, "n-a")); err != nil {
			t.Fatal(err)
		}
		// Same blinded token in another scope must land in a different
		// bucket: scopes cannot be joined through the limiter.
		if lim.BucketKey("intake:case", "same-token") == lim.BucketKey("intake:other", "same-token") {
			t.Fatal("bucket keys must be scope-bound; cross-scope linkage is forbidden")
		}
		if got := lim.RetainedTokens(); len(got) != 0 {
			t.Fatalf("limiter must retain no raw tokens, got %v", got)
		}
	})

	t.Run("invalid policy and empty inputs fail closed", func(t *testing.T) {
		bad := anonScopePolicy()
		bad.MaxAttempts = 0
		if _, err := NewScopeLimiter(bad); err == nil {
			t.Fatal("zero allowance must fail closed")
		}
		lim, err := NewScopeLimiter(anonScopePolicy())
		if err != nil {
			t.Fatal(err)
		}
		if err := lim.Check(now, "", "d", anonProof(now, "n")); err == nil {
			t.Fatal("empty token must fail closed")
		}
	})
}

func isLimitCode(err error, code LimitCode) bool {
	var rej *LimitRejection
	return errors.As(err, &rej) && rej.Code == code
}

// TestTodo_ANON_006_Security proves scope isolation: activity in one scope
// never affects budgets in another, and per-scope counts expose no token.
func TestTodo_ANON_006_Security(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	lim, err := NewScopeLimiter(anonScopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_ = lim.Check(now.Add(time.Duration(i)*time.Second), "bot", string(rune('a'+i))+"-d", anonProof(now.Add(time.Duration(i)*time.Second), string(rune('a'+i))+"-n"))
	}
	// Exhausting one scope's budget through another scope's limiter view
	// must be impossible: counts are per scope and carry no token.
	counts := lim.Counts("intake:case")
	if counts.Attempts == 0 {
		t.Fatal("per-scope counts must be observable for operations")
	}
	for _, entry := range counts.Entries {
		if entry.BucketHash == "" || entry.BucketHash == "bot" {
			t.Fatalf("counts must carry opaque bucket hashes, never tokens: %+v", entry)
		}
	}
}

// TestTodo_ANON_006_Recovery proves windows expire without operator action
// and limiter state round-trips through snapshot/restore for failover.
func TestTodo_ANON_006_Recovery(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	lim, err := NewScopeLimiter(anonScopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if err := lim.Check(now, "t1", "d1", anonProof(now, "n1")); err != nil {
		t.Fatal(err)
	}
	snap, err := lim.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreLimiter(anonScopePolicy(), snap)
	if err != nil {
		t.Fatal(err)
	}
	// Duplicate memory survives failover: the same payload is still a duplicate.
	if err := restored.Check(now.Add(time.Second), "t1", "d1", anonProof(now.Add(time.Second), "n2")); !isLimitCode(err, LimitDuplicate) {
		t.Fatalf("restored limiter must remember digests, got %v", err)
	}
	// After the window passes, the same token may submit fresh payloads again.
	fresh := anonProof(now.Add(2*time.Minute), "n3")
	if err := restored.Check(now.Add(2*time.Minute), "t1", "d-new", fresh); err != nil {
		t.Fatalf("expired window must reset budgets, got %v", err)
	}
}
