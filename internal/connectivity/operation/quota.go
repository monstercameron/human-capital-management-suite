package operation

// INTG-015: rate-aware fair connector scheduling.
//
// Nothing before this file gave the operation kernel any notion of a
// vendor's rate limit, concurrency cap or fairness across tenants,
// connections, resources and criticality. That let one tenant or one
// resource flood a connection's queue, let a 429 storm multiply retries
// (each throttled response just looked like an ordinary FAILURE and
// minted another attempt), let an unconfigured provider quota be treated
// as unlimited, and let a backlog of low-priority work starve a P0
// operation that arrived after it filled every concurrency slot.
//
// ConnectorLedger and Schedule close that gap. The shape deliberately
// mirrors EVENT-003's outbox.ResourceLedger/Schedule (a sorted candidate
// pool admitted against shared, mutex-guarded capacity, with the same
// criticality-first/FIFO tie-break and the same idempotent
// claim-by-identity fencing), because that is the right general pattern
// for "arbitrate a pool of candidates against shared capacity". The two
// packages are not merged and no type is imported across the boundary,
// for two reasons: connectivity/operation cannot depend on data/outbox
// without inverting the module's layering, and outbox's "no policy names
// this resource" convention is deliberately unbounded (there is no vendor
// on the other end of an internal resource to punish a guessed cap),
// which is the exact opposite of what a real external vendor's API
// requires here. ConnectorQuota/UnknownConnectorQuota encode that
// inversion explicitly: an unconfigured connection fails closed to a
// conservative bound, never to unlimited.
//
// A provider's own rate-limit signal (an HTTP 429 with a reset window)
// is modeled as a first-class event via Observe429, which unconditionally
// overrides ordinary admission for that connection until the vendor's own
// reset time, regardless of whether a rolling Limit/Window budget is even
// configured: a provider that says "stop" is authoritative over any
// config we happened to write down.

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ConnectorQuota is what one vendor connection is known to allow: a
// bounded number of admissions per reset Window (the provider's own
// rolling rate limit) and a bounded number of concurrently reserved
// dispatch slots.
//
// The wholly zero value means UNDECLARED, not unlimited, and resolves to
// UnknownConnectorQuota. Naming a connection in ConnectorPolicy without
// stating its quota is not a measurement of the vendor, so it gets the
// same conservative bound as a connection nobody named at all -- a
// ConnectorPolicy{PerTenantShare: 5} written by someone who simply had
// not looked up the rate limit must not become unlimited vendor traffic.
// Once either dimension is stated the caller has engaged with the quota,
// so the other may be left zero to mean unbounded. A negative Limit or
// MaxConcurrent is the explicit "this connection genuinely has no vendor
// bound" escape hatch, which a caller has to choose deliberately.
type ConnectorQuota struct {
	Limit         int
	Window        time.Duration
	MaxConcurrent int
}

// undeclared reports whether no dimension of the quota was ever stated.
func (q ConnectorQuota) undeclared() bool {
	return q.Limit == 0 && q.MaxConcurrent == 0
}

// UnknownConnectorQuota is the conservative bound applied to any
// connection ConnectorLedger's policy does not name. The REFACTOR
// invariant is that reserved capacity never exceeds the vendor quota; for
// a connection nobody has measured yet, the only safe assumption is the
// smallest one, not unlimited.
var UnknownConnectorQuota = ConnectorQuota{Limit: 1, Window: time.Minute, MaxConcurrent: 1}

// ConnectorPolicy is one connection's admission policy: its vendor quota,
// how much of that quota one tenant or one external resource may occupy,
// and how many of its concurrent slots are reserved so a P0/P1 operation
// can never be shut out by a P2-P4 backlog that got there first.
// PerTenantShare and PerResourceShare of zero mean that share is
// unbounded (still capped by Quota.MaxConcurrent); ReserveForP0P1 of zero
// means no slots are set aside for high criticality.
type ConnectorPolicy struct {
	Quota            ConnectorQuota
	PerTenantShare   int
	PerResourceShare int
	ReserveForP0P1   int
}

// Schedule admission/refusal reasons. A matrix test asserts on these, so
// they are stable, exported constants rather than ad hoc strings.
const (
	ScheduleReasonAlreadyReserved       = "ALREADY_RESERVED"
	ScheduleReasonRateLimited           = "RATE_LIMITED"
	ScheduleReasonServerFault           = "SERVER_FAULT_BACKOFF"
	ScheduleReasonConcurrencyLimited    = "CONCURRENCY_LIMITED"
	ScheduleReasonTenantShareExceeded   = "TENANT_SHARE_EXCEEDED"
	ScheduleReasonResourceShareExceeded = "RESOURCE_SHARE_EXCEEDED"
	ScheduleReasonCriticalityReserved   = "CRITICALITY_RESERVED"
)

// ScheduleCandidate is one unit of scheduling input: an operation's
// tenant/connection/resource/criticality identity plus when it was queued.
type ScheduleCandidate struct {
	OperationID  uuid.UUID
	TenantID     string
	ConnectionID string
	ResourceKey  string
	Criticality  string
	QueuedAt     time.Time
}

// CandidateFromOperation derives a ScheduleCandidate from a governed
// Operation, so a caller driving the existing Plan/Queue/Lease lifecycle
// never has to duplicate an operation's identity by hand.
func CandidateFromOperation(op Operation, queuedAt time.Time) ScheduleCandidate {
	return ScheduleCandidate{
		OperationID:  op.OperationID,
		TenantID:     op.TenantID,
		ConnectionID: op.ConnectionID,
		ResourceKey:  op.ExternalResourceKey,
		Criticality:  op.Criticality,
		QueuedAt:     queuedAt,
	}
}

// ScheduleDecision is one candidate's outcome from a Schedule pass.
// QueueAge and PredictedCompletion are always populated, admitted or not:
// GREEN requires both to be exposed, not only on the deferred path.
type ScheduleDecision struct {
	Candidate           ScheduleCandidate
	Admitted            bool
	Reason              string
	QueueAge            time.Duration
	PredictedCompletion time.Time
}

// ScheduleOutcome is one scheduling pass's outcome. Admitted candidates
// hold a reserved slot now; Deferred candidates should be resubmitted on
// a later pass once capacity, a tenant/resource share or a vendor's rate
// window frees up.
type ScheduleOutcome struct {
	Admitted []ScheduleDecision
	Deferred []ScheduleDecision
}

func criticalityRank(c string) int {
	switch c {
	case "P0":
		return 0
	case "P1":
		return 1
	case "P2":
		return 2
	case "P3":
		return 3
	case "P4":
		return 4
	default:
		return 5
	}
}

func sortScheduleCandidates(candidates []ScheduleCandidate) []ScheduleCandidate {
	sorted := append([]ScheduleCandidate(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if ra, rb := criticalityRank(a.Criticality), criticalityRank(b.Criticality); ra != rb {
			return ra < rb
		}
		if !a.QueuedAt.Equal(b.QueuedAt) {
			return a.QueuedAt.Before(b.QueuedAt)
		}
		return a.OperationID.String() < b.OperationID.String()
	})
	return sorted
}

// QuotaWindow is the shared fixed-window counter used by connector dispatch
// and inbound application-credential admission.
type QuotaWindow struct {
	Used    int
	StartAt time.Time
	ResetAt time.Time
}

// AdvanceQuotaWindow opens the current fixed window or preserves an active
// one. The returned value has a zero Used count when a new window begins.
func AdvanceQuotaWindow(window QuotaWindow, now time.Time, duration time.Duration) QuotaWindow {
	if duration <= 0 {
		duration = time.Minute
	}
	if window.StartAt.IsZero() || !now.Before(window.ResetAt) {
		start := now.Truncate(duration)
		return QuotaWindow{StartAt: start, ResetAt: start.Add(duration)}
	}
	return window
}

// QuotaRetryAfterSeconds returns the whole-second delay until reset, rounded
// up so a caller never retries before the quota window actually resets.
func QuotaRetryAfterSeconds(now, reset time.Time) int {
	remaining := reset.Sub(now)
	if remaining <= 0 {
		return 1
	}
	seconds := int(math.Ceil(remaining.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}

type connectorWindow = QuotaWindow

type connectorClaim struct {
	connection  string
	tenant      string
	resource    string
	criticality string
}

// ConnectorLedger is the admission tracker for one or more vendor
// connections. It is safe for concurrent use: multiple goroutines standing
// in for concurrent dispatch workers share one ledger so a connection's
// quota is enforced across all of them, not per caller.
type ConnectorLedger struct {
	mu               sync.Mutex
	policy           map[string]ConnectorPolicy
	inFlight         map[string]int
	byTenant         map[string]map[string]int
	byResource       map[string]map[string]int
	lowPriority      map[string]int
	windows          map[string]*connectorWindow
	throttledUntil   map[string]time.Time
	serverFaultUntil map[string]time.Time
	claims           map[uuid.UUID]connectorClaim
}

// NewConnectorLedger builds a ledger from the configured per-connection
// policy. A connection absent from policy is not unbounded: it falls back
// to UnknownConnectorQuota the first time it is scheduled.
func NewConnectorLedger(policy map[string]ConnectorPolicy) *ConnectorLedger {
	p := make(map[string]ConnectorPolicy, len(policy))
	for connection, cp := range policy {
		p[connection] = cp
	}
	return &ConnectorLedger{
		policy:           p,
		inFlight:         make(map[string]int),
		byTenant:         make(map[string]map[string]int),
		byResource:       make(map[string]map[string]int),
		lowPriority:      make(map[string]int),
		windows:          make(map[string]*connectorWindow),
		throttledUntil:   make(map[string]time.Time),
		serverFaultUntil: make(map[string]time.Time),
		claims:           make(map[uuid.UUID]connectorClaim),
	}
}

// policyFor returns the effective policy for a connection. The caller
// must hold l.mu. ok reports whether the connection was explicitly
// configured; when it is false the returned policy is the fail-closed
// UnknownConnectorQuota, not an unlimited zero value.
func (l *ConnectorLedger) policyFor(connection string) (ConnectorPolicy, bool) {
	if p, ok := l.policy[connection]; ok {
		// Named but never measured is still an unknown quota. Falling back
		// here rather than at the call sites keeps every check -- rate
		// window, concurrency, shares, criticality reserve -- reading one
		// resolved quota.
		if p.Quota.undeclared() {
			p.Quota = UnknownConnectorQuota
		}
		return p, true
	}
	return ConnectorPolicy{Quota: UnknownConnectorQuota}, false
}

func (l *ConnectorLedger) quotaFor(connection string) ConnectorQuota {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, _ := l.policyFor(connection)
	return p.Quota
}

// windowFor returns the current rolling rate window for a connection,
// lazily starting a fresh one when none exists yet or the prior one has
// elapsed. The caller must hold l.mu.
func (l *ConnectorLedger) windowFor(connection string, now time.Time, window time.Duration) *connectorWindow {
	w, ok := l.windows[connection]
	if !ok {
		w = &connectorWindow{}
		l.windows[connection] = w
	}
	*w = AdvanceQuotaWindow(*w, now, window)
	return w
}

// Observe429 folds a provider's own throttling signal into the ledger:
// every admission attempt for connection is refused until now+retryAfter,
// regardless of whether a rolling Limit/Window budget is configured for
// it. This is what stops a 429 storm from multiplying retries: the very
// next attempt, made immediately by a naive caller, is refused rather
// than dispatched.
func (l *ConnectorLedger) Observe429(connection string, now time.Time, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if retryAfter <= 0 {
		retryAfter = time.Second
	}
	l.throttledUntil[connection] = now.Add(retryAfter)
}

// ThrottledUntil reports the vendor-reported backoff deadline for a
// connection, if one is currently in effect.
func (l *ConnectorLedger) ThrottledUntil(connection string) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	until, ok := l.throttledUntil[connection]
	return until, ok
}

// ObserveServerFault folds a provider-side 5xx/server-error signal into the
// ledger, deliberately distinct from Observe429: a 5xx carries no vendor-
// declared reset the way a 429's Retry-After does, so the backoff here is a
// bound this package chooses, never a promise the provider made. It is kept
// in its own map rather than reusing throttledUntil so a 429 and a 5xx
// observed on the same connection are never conflated -- a monitoring
// caller (or the SECURITY test) can tell "the vendor said stop" from "the
// vendor is broken" apart, and a later real 429 reset cannot silently erase
// an unrelated fault backoff or vice versa. Every admission attempt for
// connection is refused until now+backoff, exactly like a 429, which is
// what stops a 5xx storm from multiplying retries the same way Observe429
// stops a 429 storm.
func (l *ConnectorLedger) ObserveServerFault(connection string, now time.Time, backoff time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if backoff <= 0 {
		backoff = time.Second
	}
	l.serverFaultUntil[connection] = now.Add(backoff)
}

// ServerFaultUntil reports the self-chosen backoff deadline for a
// connection after a server-side fault, if one is currently in effect.
func (l *ConnectorLedger) ServerFaultUntil(connection string) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	until, ok := l.serverFaultUntil[connection]
	return until, ok
}

// TryReserve reserves one slot for candidate against its connection's
// quota, honoring the connection-wide concurrency cap, the connection's
// rolling rate window, any active provider-reported 429 backoff, the
// candidate tenant's and resource's configured share, and the
// reservation that keeps P0/P1 operations from being shut out of a
// connection a P2-P4 backlog already filled. It returns false with a
// stable reason when refused, and refuses outright (ScheduleReasonAlready
// Reserved) if OperationID was already admitted by a concurrent or
// earlier call: the same operation can never hold two slots out of one
// ledger.
func (l *ConnectorLedger) TryReserve(now time.Time, c ScheduleCandidate) (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, already := l.claims[c.OperationID]; already {
		return false, ScheduleReasonAlreadyReserved
	}
	if until, throttled := l.throttledUntil[c.ConnectionID]; throttled && now.Before(until) {
		return false, ScheduleReasonRateLimited
	}
	if until, faulted := l.serverFaultUntil[c.ConnectionID]; faulted && now.Before(until) {
		return false, ScheduleReasonServerFault
	}
	policy, _ := l.policyFor(c.ConnectionID)
	quota := policy.Quota

	var win *connectorWindow
	if quota.Limit > 0 {
		win = l.windowFor(c.ConnectionID, now, quota.Window)
		if win.Used >= quota.Limit {
			return false, ScheduleReasonRateLimited
		}
	}
	if quota.MaxConcurrent > 0 && l.inFlight[c.ConnectionID] >= quota.MaxConcurrent {
		return false, ScheduleReasonConcurrencyLimited
	}
	if policy.PerTenantShare > 0 && l.byTenant[c.ConnectionID][c.TenantID] >= policy.PerTenantShare {
		return false, ScheduleReasonTenantShareExceeded
	}
	if policy.PerResourceShare > 0 && l.byResource[c.ConnectionID][c.ResourceKey] >= policy.PerResourceShare {
		return false, ScheduleReasonResourceShareExceeded
	}
	if criticalityRank(c.Criticality) > 1 && policy.ReserveForP0P1 > 0 && quota.MaxConcurrent > 0 {
		allowedForLow := quota.MaxConcurrent - policy.ReserveForP0P1
		if allowedForLow < 0 {
			allowedForLow = 0
		}
		if l.lowPriority[c.ConnectionID] >= allowedForLow {
			return false, ScheduleReasonCriticalityReserved
		}
	}

	if win != nil {
		win.Used++
	}
	l.inFlight[c.ConnectionID]++
	if l.byTenant[c.ConnectionID] == nil {
		l.byTenant[c.ConnectionID] = make(map[string]int)
	}
	l.byTenant[c.ConnectionID][c.TenantID]++
	if l.byResource[c.ConnectionID] == nil {
		l.byResource[c.ConnectionID] = make(map[string]int)
	}
	l.byResource[c.ConnectionID][c.ResourceKey]++
	if criticalityRank(c.Criticality) > 1 {
		l.lowPriority[c.ConnectionID]++
	}
	l.claims[c.OperationID] = connectorClaim{connection: c.ConnectionID, tenant: c.TenantID, resource: c.ResourceKey, criticality: c.Criticality}
	return true, ""
}

// Release returns a reserved slot after a dispatch completes or is
// abandoned, so a later Schedule pass can reuse it. It only releases the
// reservation when tenant matches the tenant that made it: a foreign
// tenant supplying a guessed or observed operationID can neither free nor
// otherwise observe another tenant's reservation through this call. It
// reports whether a release actually happened.
func (l *ConnectorLedger) Release(operationID uuid.UUID, tenant string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	claim, ok := l.claims[operationID]
	if !ok || claim.tenant != tenant {
		return false
	}
	delete(l.claims, operationID)
	if l.inFlight[claim.connection] > 0 {
		l.inFlight[claim.connection]--
	}
	if byTenant := l.byTenant[claim.connection]; byTenant != nil && byTenant[claim.tenant] > 0 {
		byTenant[claim.tenant]--
	}
	if byResource := l.byResource[claim.connection]; byResource != nil && byResource[claim.resource] > 0 {
		byResource[claim.resource]--
	}
	if criticalityRank(claim.criticality) > 1 && l.lowPriority[claim.connection] > 0 {
		l.lowPriority[claim.connection]--
	}
	return true
}

// InFlight reports how many slots connection currently holds reserved.
func (l *ConnectorLedger) InFlight(connection string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inFlight[connection]
}

// predictConnectorCompletion estimates when a candidate at position
// (0-indexed, among candidates deferred for the same connection in this
// pass) would be admitted, given the connection's own throughput: once
// per Window, up to Limit (or, absent a configured Limit, MaxConcurrent)
// more candidates can be admitted. It is a deterministic, storage-free
// estimate appropriate to a kernel package with no clock of its own; it
// intentionally never claims more precision than "how many vendor
// windows must elapse".
func predictConnectorCompletion(now time.Time, position int, quota ConnectorQuota) time.Time {
	perWindow := quota.Limit
	if perWindow <= 0 {
		perWindow = quota.MaxConcurrent
	}
	if perWindow <= 0 {
		perWindow = 1
	}
	window := quota.Window
	if window <= 0 {
		window = time.Minute
	}
	windowsNeeded := position/perWindow + 1
	return now.Add(time.Duration(windowsNeeded) * window)
}

// Schedule orders candidates by criticality first - so a P0 operation is
// always considered, and admitted into any capacity ConnectorPolicy
// reserved for it, before any P2-P4 backlog regardless of which tenant or
// resource either belongs to - and admits them against ledger's shared,
// connection-scoped quota. It performs no I/O: it is the deterministic
// policy a caller applies to whatever candidates it has queued, or to
// candidates fed back in after a prior pass deferred them.
func (l *ConnectorLedger) Schedule(now time.Time, candidates []ScheduleCandidate) ScheduleOutcome {
	var out ScheduleOutcome
	deferredPosition := make(map[string]int)
	for _, c := range sortScheduleCandidates(candidates) {
		age := now.Sub(c.QueuedAt)
		if ok, reason := l.TryReserve(now, c); ok {
			out.Admitted = append(out.Admitted, ScheduleDecision{Candidate: c, Admitted: true, QueueAge: age, PredictedCompletion: now})
			continue
		} else {
			pos := deferredPosition[c.ConnectionID]
			deferredPosition[c.ConnectionID] = pos + 1
			predicted := predictConnectorCompletion(now, pos, l.quotaFor(c.ConnectionID))
			switch reason {
			case ScheduleReasonRateLimited:
				if until, throttled := l.ThrottledUntil(c.ConnectionID); throttled && until.After(now) {
					predicted = until
				}
			case ScheduleReasonServerFault:
				if until, faulted := l.ServerFaultUntil(c.ConnectionID); faulted && until.After(now) {
					predicted = until
				}
			}
			out.Deferred = append(out.Deferred, ScheduleDecision{Candidate: c, Admitted: false, Reason: reason, QueueAge: age, PredictedCompletion: predicted})
		}
	}
	return out
}
