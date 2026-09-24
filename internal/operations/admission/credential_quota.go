package admission

import (
	"errors"
	"strings"
	"sync"
	"time"

	quotaoperation "github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
)

// CredentialIdentity is the verified machine-client scope used by the
// inbound API quota. ClientID is stable across short-lived access-token
// rotation; the verified subject and token digest are deliberately excluded.
type CredentialIdentity struct {
	TenantID string
	ClientID string
}

// CredentialPolicy sets a request rate window and a longer total quota
// window. A request consumes one unit from both budgets.
type CredentialPolicy struct {
	RateLimit   int
	RateWindow  time.Duration
	QuotaLimit  int
	QuotaWindow time.Duration
}

// DefaultCredentialPolicy bounds inbound API credentials to 600 requests per
// minute and 100,000 requests per UTC day. Deployments may pass a stricter
// policy to the limiter when their application registry has client-specific
// service tiers.
func DefaultCredentialPolicy() CredentialPolicy {
	return CredentialPolicy{RateLimit: 600, RateWindow: time.Minute, QuotaLimit: 100_000, QuotaWindow: 24 * time.Hour}
}

type CredentialOutcome string

const (
	CredentialAdmit         CredentialOutcome = "ADMIT"
	CredentialRateLimited   CredentialOutcome = "RATE_LIMITED"
	CredentialQuotaExceeded CredentialOutcome = "QUOTA_EXCEEDED"
)

var ErrInvalidCredentialQuota = errors.New("admission: invalid credential quota input")

// CredentialUsage is telemetry for exactly one verified identity. It omits
// other keys and exposes only the digest supplied by the trusted verifier.
type CredentialUsage struct {
	Identity     CredentialIdentity
	RateUsed     int
	RateLimit    int
	RateResetAt  time.Time
	QuotaUsed    int
	QuotaLimit   int
	QuotaResetAt time.Time
}

type CredentialDecision struct {
	Outcome    CredentialOutcome
	Reason     string
	RetryAfter int
	Usage      CredentialUsage
}

type credentialKey struct {
	tenant string
	client string
}

type credentialWindow = quotaoperation.QuotaWindow

// CredentialLimiter owns bounded-window counters for the platform's inbound
// API. Its mutex makes concurrent admissions atomic within this process.
// Callers should create one limiter at the composition root and supply the
// verified principal identity and a consistent policy on every call.
type CredentialLimiter struct {
	mu          sync.Mutex
	entries     map[credentialKey]credentialCounters
	nextCleanup time.Time
}

type credentialCounters struct {
	rate        credentialWindow
	quota       credentialWindow
	rateWindow  time.Duration
	quotaWindow time.Duration
}

func NewCredentialLimiter() *CredentialLimiter {
	return &CredentialLimiter{entries: make(map[credentialKey]credentialCounters)}
}

// Admit checks both windows and atomically consumes one unit if admitted.
// Time is supplied by the caller to keep boundary behavior explicit and
// deterministic in tests.
func (l *CredentialLimiter) Admit(now time.Time, identity CredentialIdentity, policy CredentialPolicy) (CredentialDecision, error) {
	if l == nil || strings.TrimSpace(identity.TenantID) == "" || strings.TrimSpace(identity.ClientID) == "" || policy.RateLimit <= 0 || policy.QuotaLimit <= 0 || policy.RateWindow <= 0 || policy.QuotaWindow <= 0 {
		return CredentialDecision{}, ErrInvalidCredentialQuota
	}
	key := credentialKey{tenant: identity.TenantID, client: identity.ClientID}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = make(map[credentialKey]credentialCounters)
	}
	l.pruneExpired(now)
	counters := l.entries[key]
	counters.rate = advanceCredentialWindow(counters.rate, now, policy.RateWindow)
	counters.quota = advanceCredentialWindow(counters.quota, now, policy.QuotaWindow)
	counters.rateWindow, counters.quotaWindow = policy.RateWindow, policy.QuotaWindow
	usage := credentialUsage(identity, counters, policy)
	d := CredentialDecision{Outcome: CredentialAdmit, Reason: "WITHIN_CREDENTIAL_LIMITS", Usage: usage}
	if counters.rate.Used >= policy.RateLimit {
		d.Outcome, d.Reason = CredentialRateLimited, "CREDENTIAL_RATE_LIMIT"
		d.RetryAfter = quotaoperation.QuotaRetryAfterSeconds(now, usage.RateResetAt)
	} else if counters.quota.Used >= policy.QuotaLimit {
		d.Outcome, d.Reason = CredentialQuotaExceeded, "CREDENTIAL_QUOTA_EXCEEDED"
		d.RetryAfter = quotaoperation.QuotaRetryAfterSeconds(now, usage.QuotaResetAt)
	} else {
		counters.rate.Used++
		counters.quota.Used++
		usage = credentialUsage(identity, counters, policy)
		d.Usage = usage
	}
	l.entries[key] = counters
	return d, nil
}

// Usage returns counters for the exact identity requested. It does not
// enumerate identities, and the composite key prevents tenant collisions.
func (l *CredentialLimiter) Usage(now time.Time, identity CredentialIdentity, policy CredentialPolicy) (CredentialUsage, bool, error) {
	if l == nil || strings.TrimSpace(identity.TenantID) == "" || strings.TrimSpace(identity.ClientID) == "" || policy.RateLimit <= 0 || policy.QuotaLimit <= 0 || policy.RateWindow <= 0 || policy.QuotaWindow <= 0 {
		return CredentialUsage{}, false, ErrInvalidCredentialQuota
	}
	key := credentialKey{tenant: identity.TenantID, client: identity.ClientID}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneExpired(now)
	counters, ok := l.entries[key]
	if !ok {
		return CredentialUsage{}, false, nil
	}
	counters.rate = advanceCredentialWindow(counters.rate, now, policy.RateWindow)
	counters.quota = advanceCredentialWindow(counters.quota, now, policy.QuotaWindow)
	counters.rateWindow, counters.quotaWindow = policy.RateWindow, policy.QuotaWindow
	l.entries[key] = counters
	return credentialUsage(identity, counters, policy), true, nil
}

func (l *CredentialLimiter) pruneExpired(now time.Time) {
	if !l.nextCleanup.IsZero() && now.Before(l.nextCleanup) {
		return
	}
	for key, counters := range l.entries {
		if !now.Before(counters.rate.StartAt.Add(counters.rateWindow)) && !now.Before(counters.quota.StartAt.Add(counters.quotaWindow)) {
			delete(l.entries, key)
		}
	}
	l.nextCleanup = now.Add(time.Minute)
}

func advanceCredentialWindow(window credentialWindow, now time.Time, duration time.Duration) credentialWindow {
	return quotaoperation.AdvanceQuotaWindow(window, now, duration)
}

func credentialUsage(identity CredentialIdentity, counters credentialCounters, policy CredentialPolicy) CredentialUsage {
	return CredentialUsage{
		Identity: identity, RateUsed: counters.rate.Used, RateLimit: policy.RateLimit,
		RateResetAt: counters.rate.ResetAt,
		QuotaUsed:   counters.quota.Used, QuotaLimit: policy.QuotaLimit,
		QuotaResetAt: counters.quota.ResetAt,
	}
}
