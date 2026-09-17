package pseudonym

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// LimitCode is the closed ANON-006 denial vocabulary.
type LimitCode string

const (
	LimitDuplicate   LimitCode = "DUPLICATE"
	LimitRateLimited LimitCode = "RATE_LIMITED"
	LimitReplay      LimitCode = "REPLAY"
	LimitStaleProof  LimitCode = "STALE_PROOF"
	LimitRejected    LimitCode = "REJECTED"
)

func (c LimitCode) Valid() bool {
	switch c {
	case LimitDuplicate, LimitRateLimited, LimitReplay, LimitStaleProof, LimitRejected:
		return true
	default:
		return false
	}
}

var (
	// ErrLimitRejected identifies a throttled anonymous submission.
	ErrLimitRejected = errors.New("pseudonym: submission limit rejected")
	// ErrLimitPolicy identifies an invalid limiter policy.
	ErrLimitPolicy = errors.New("pseudonym: invalid limiter policy")
)

// LimitRejection is the stable ANON-006 failure shape. It carries the scope
// and code but never the token: denials must not become oracles.
type LimitRejection struct {
	Scope  string
	Code   LimitCode
	Reason string
}

func (r *LimitRejection) Error() string {
	return fmt.Sprintf("%s: scope=%s code=%s: %s", ErrLimitRejected, r.Scope, r.Code, r.Reason)
}

// Unwrap exposes the limiter sentinel to errors.Is.
func (r *LimitRejection) Unwrap() error { return ErrLimitRejected }

// SubmissionProof is the freshness envelope on an anonymous submission: an
// issue instant plus a single-use nonce. The limiter, not the caller,
// decides whether the proof is fresh.
type SubmissionProof struct {
	IssuedAt time.Time
	Nonce    string
}

// ScopePolicy bounds anonymous submissions inside one scope. Buckets are
// keyed by scope-bound blinded tokens, so the same token in two scopes
// cannot be joined through this limiter.
type ScopePolicy struct {
	Scope          string
	Window         time.Duration
	MaxAttempts    int
	ProofSkew      time.Duration
	ReviewQueue    string
	MaxBuckets     int
	MaxSeenDigests int
}

func (p ScopePolicy) Validate() error {
	if strings.TrimSpace(p.Scope) == "" || strings.TrimSpace(p.ReviewQueue) == "" {
		return fmt.Errorf("%w: scope and review queue are required", ErrLimitPolicy)
	}
	if p.Window <= 0 || p.MaxAttempts <= 0 || p.ProofSkew < 0 || p.MaxBuckets <= 0 || p.MaxSeenDigests <= 0 {
		return fmt.Errorf("%w: window, allowances and capacity must be positive", ErrLimitPolicy)
	}
	return nil
}

// Review is the false-positive route every throttled submission receives.
// Automation limits never become silent accusations.
type Review struct {
	Scope      string
	BucketHash string
	Code       LimitCode
	Queue      string
	At         time.Time
}

// ScopeCounts is the operational per-scope view. Entries carry bucket hashes
// and counts only: raw blinded tokens are never exposed.
type ScopeCounts struct {
	Scope    string
	Attempts int
	Entries  []BucketCount
}

// BucketCount is one bucket hash with its window attempt count.
type BucketCount struct {
	BucketHash string
	Attempts   int
}

// ScopeLimiter is the ANON-006 in-memory throttle. It stores only
// scope-bound key hashes, payload digests and proof nonces: no global
// identity, no fingerprint, no linkable subject.
type ScopeLimiter struct {
	mu       sync.Mutex
	policy   ScopePolicy
	attempts map[string][]time.Time
	seen     map[string]time.Time
	nonces   map[string]time.Time
	reviews  []Review
}

// NewScopeLimiter builds a limiter for exactly one scope.
func NewScopeLimiter(policy ScopePolicy) (*ScopeLimiter, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &ScopeLimiter{
		policy:   policy,
		attempts: map[string][]time.Time{},
		seen:     map[string]time.Time{},
		nonces:   map[string]time.Time{},
	}, nil
}

// BucketKey derives the scope-bound bucket for a blinded token. The raw
// token never leaves the caller: only this hash is stored.
func (l *ScopeLimiter) BucketKey(scope, blindedToken string) string {
	sum := sha256.Sum256([]byte(scope + "\x00" + blindedToken))
	return "bucket:" + hex.EncodeToString(sum[:])
}

// RetainedTokens always reports empty: the limiter retains no raw tokens.
func (l *ScopeLimiter) RetainedTokens() []string { return nil }

func (l *ScopeLimiter) prune(now time.Time) {
	cutoff := now.Add(-l.policy.Window)
	for k, times := range l.attempts {
		kept := times[:0]
		for _, t := range times {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(l.attempts, k)
		} else {
			l.attempts[k] = kept
		}
	}
	for k, t := range l.seen {
		if !t.After(cutoff) {
			delete(l.seen, k)
		}
	}
	for k, t := range l.nonces {
		if !t.After(now.Add(-l.policy.Window - l.policy.ProofSkew)) {
			delete(l.nonces, k)
		}
	}
}

func (l *ScopeLimiter) deny(code LimitCode, bucket string, now time.Time, reason string) error {
	l.reviews = append(l.reviews, Review{
		Scope: l.policy.Scope, BucketHash: bucket, Code: code, Queue: l.policy.ReviewQueue, At: now.UTC(),
	})
	return &LimitRejection{Scope: l.policy.Scope, Code: code, Reason: reason}
}

// Check admits or rejects one anonymous submission. Duplicate payloads,
// bursts, stale proofs and replayed nonces are rejected; every rejection
// opens a false-positive review.
func (l *ScopeLimiter) Check(now time.Time, blindedToken, payloadDigest string, proof SubmissionProof) error {
	if strings.TrimSpace(blindedToken) == "" || strings.TrimSpace(payloadDigest) == "" {
		return &LimitRejection{Scope: l.policy.Scope, Code: LimitRejected, Reason: "token and payload digest are required"}
	}
	if strings.TrimSpace(proof.Nonce) == "" || proof.IssuedAt.IsZero() {
		return &LimitRejection{Scope: l.policy.Scope, Code: LimitRejected, Reason: "submission proof is required"}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.IsZero() {
		return &LimitRejection{Scope: l.policy.Scope, Code: LimitRejected, Reason: "reference time is required"}
	}
	l.prune(now)
	bucket := l.BucketKey(l.policy.Scope, blindedToken)
	skew := proof.IssuedAt.Sub(now)
	if skew < 0 {
		skew = -skew
	}
	if skew > l.policy.Window+l.policy.ProofSkew {
		return l.deny(LimitStaleProof, bucket, now, "submission proof is stale")
	}
	nonceKey := l.BucketKey("nonce", proof.Nonce)
	if _, dup := l.nonces[nonceKey]; dup {
		return l.deny(LimitReplay, bucket, now, "submission proof was already used")
	}
	seenKey := bucket + "\x00" + payloadDigest
	if _, dup := l.seen[seenKey]; dup {
		l.attempts[bucket] = append(l.attempts[bucket], now)
		return l.deny(LimitDuplicate, bucket, now, "payload was already submitted in this window")
	}
	if len(l.attempts[bucket]) >= l.policy.MaxAttempts {
		return l.deny(LimitRateLimited, bucket, now, "submission budget is exhausted for this window")
	}
	if len(l.attempts) >= l.policy.MaxBuckets || len(l.seen) >= l.policy.MaxSeenDigests {
		return l.deny(LimitRateLimited, bucket, now, "limiter capacity is exhausted for this window")
	}
	l.nonces[nonceKey] = now
	l.seen[seenKey] = now
	l.attempts[bucket] = append(l.attempts[bucket], now)
	return nil
}

// Reviews returns the false-positive review queue in order.
func (l *ScopeLimiter) Reviews() []Review {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Review(nil), l.reviews...)
}

// Counts returns the operational per-scope view without tokens.
func (l *ScopeLimiter) Counts(scope string) ScopeCounts {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := ScopeCounts{Scope: scope}
	if scope != l.policy.Scope {
		return out
	}
	for bucket, times := range l.attempts {
		out.Attempts += len(times)
		out.Entries = append(out.Entries, BucketCount{BucketHash: bucket, Attempts: len(times)})
	}
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].BucketHash < out.Entries[j].BucketHash })
	return out
}

// LimiterSnapshot is the failover form of limiter state: hashes and times
// only, never tokens.
type LimiterSnapshot struct {
	Scope    string
	Attempts map[string][]time.Time
	Seen     map[string]time.Time
	Nonces   map[string]time.Time
	Reviews  []Review
}

// Snapshot exports limiter state for failover.
func (l *ScopeLimiter) Snapshot() (LimiterSnapshot, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	snap := LimiterSnapshot{
		Scope:    l.policy.Scope,
		Attempts: map[string][]time.Time{},
		Seen:     map[string]time.Time{},
		Nonces:   map[string]time.Time{},
		Reviews:  append([]Review(nil), l.reviews...),
	}
	for k, v := range l.attempts {
		snap.Attempts[k] = append([]time.Time(nil), v...)
	}
	for k, v := range l.seen {
		snap.Seen[k] = v
	}
	for k, v := range l.nonces {
		snap.Nonces[k] = v
	}
	return snap, nil
}

// RestoreLimiter rebuilds a limiter from a snapshot for the same scope.
func RestoreLimiter(policy ScopePolicy, snap LimiterSnapshot) (*ScopeLimiter, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if snap.Scope != "" && snap.Scope != policy.Scope {
		return nil, fmt.Errorf("%w: snapshot scope %q does not match %q", ErrLimitPolicy, snap.Scope, policy.Scope)
	}
	lim := &ScopeLimiter{
		policy:   policy,
		attempts: map[string][]time.Time{},
		seen:     map[string]time.Time{},
		nonces:   map[string]time.Time{},
		reviews:  append([]Review(nil), snap.Reviews...),
	}
	for k, v := range snap.Attempts {
		lim.attempts[k] = append([]time.Time(nil), v...)
	}
	for k, v := range snap.Seen {
		lim.seen[k] = v
	}
	for k, v := range snap.Nonces {
		lim.nonces[k] = v
	}
	return lim, nil
}
