package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Stuck detection (WF-RUN-020) names nodes that stopped making expected
// progress. It is a pure evaluation over supplied observations: it opens no
// incident, writes no row and mutates no business state. Callers open or
// link exactly one incident or work item per finding, deduplicated by
// [StuckFinding.IncidentKey].
//
// Two readings are deliberate. A long legal wait is never stuck by age
// alone: a finding requires at least one missed lease, timer, signal,
// retry, SLA or idle expectation. And a poison node — repeated failures
// whose next retry never arrives — is stuck even when every other signal
// looks patient.

// StuckKind names one expected-progress signal whose absence can strand a
// node.
type StuckKind string

// Expected-progress signals.
const (
	StuckKindLease  StuckKind = "LEASE_EXPIRY"
	StuckKindTimer  StuckKind = "TIMER_OVERDUE"
	StuckKindSignal StuckKind = "SIGNAL_OVERDUE"
	StuckKindRetry  StuckKind = "RETRY_OVERDUE"
	StuckKindIdle   StuckKind = "IDLE_BUDGET_EXCEEDED"
	StuckKindSLA    StuckKind = "SLA_BREACH"
)

// StuckKindsAll returns every expected-progress signal.
func StuckKindsAll() []StuckKind {
	return []StuckKind{
		StuckKindLease, StuckKindTimer, StuckKindSignal,
		StuckKindRetry, StuckKindIdle, StuckKindSLA,
	}
}

// Valid reports whether k is a declared expected-progress signal.
func (k StuckKind) Valid() bool {
	switch k {
	case StuckKindLease, StuckKindTimer, StuckKindSignal,
		StuckKindRetry, StuckKindIdle, StuckKindSLA:
		return true
	}
	return false
}

// Stuck refusal codes.
const (
	// CodeStuckScanRefused reports a scan the detector refuses to run:
	// zero clock, missing tenant, cross-tenant observation, unknown state
	// or corrupt timestamps.
	CodeStuckScanRefused = "STUCK_SCAN_REFUSED"
)

// StuckExpectation states what progress a node in one state must show.
// States without an entry use the detector's default budget with every
// kind and no poison threshold.
type StuckExpectation struct {
	State NodeStatus
	// MaxIdle bounds silence in a live state. Exceeding it misses
	// StuckKindIdle; reaching it exactly does not.
	MaxIdle time.Duration
	// Kinds selects which signals apply to this state.
	Kinds []StuckKind
	// PoisonAfter marks a node poisoned once Attempts reach it while the
	// retry signal is missed. Zero disables poison marking.
	PoisonAfter int
}

// StuckDetector evaluates node observations against per-state expected
// progress. The zero value is not usable; build one with
// [NewStuckDetector]. A built detector holds no mutable state and is safe
// for concurrent use.
type StuckDetector struct {
	byState        map[NodeStatus]StuckExpectation
	defaultMaxIdle time.Duration
}

// NewStuckDetector compiles per-state expectations. Terminal states never
// strand — a finished node cannot be stuck — so they are refused here
// rather than silently ignored on every scan.
func NewStuckDetector(exp []StuckExpectation, defaultMaxIdle time.Duration) (StuckDetector, error) {
	if defaultMaxIdle <= 0 {
		return StuckDetector{}, refuse(CodeStuckScanRefused, "", "", "default idle budget must be positive")
	}
	byState := make(map[NodeStatus]StuckExpectation, len(exp))
	for i, e := range exp {
		switch {
		case !e.State.Valid():
			return StuckDetector{}, refuse(CodeStuckScanRefused, "", "", "expectation %d names unknown state %q", i, e.State)
		case e.State.Terminal():
			return StuckDetector{}, refuse(CodeStuckScanRefused, "", "", "expectation %d covers terminal state %q, which cannot strand", i, e.State)
		case e.MaxIdle <= 0:
			return StuckDetector{}, refuse(CodeStuckScanRefused, "", "", "expectation %d for %q needs a positive idle budget", i, e.State)
		case len(e.Kinds) == 0:
			return StuckDetector{}, refuse(CodeStuckScanRefused, "", "", "expectation %d for %q selects no signal", i, e.State)
		case e.PoisonAfter < 0:
			return StuckDetector{}, refuse(CodeStuckScanRefused, "", "", "expectation %d for %q needs a non-negative poison threshold", i, e.State)
		}
		seen := map[StuckKind]bool{}
		for _, k := range e.Kinds {
			if !k.Valid() {
				return StuckDetector{}, refuse(CodeStuckScanRefused, "", "", "expectation %d for %q names unknown signal %q", i, e.State, k)
			}
			if seen[k] {
				return StuckDetector{}, refuse(CodeStuckScanRefused, "", "", "expectation %d for %q names signal %q twice", i, e.State, k)
			}
			seen[k] = true
		}
		if _, dup := byState[e.State]; dup {
			return StuckDetector{}, refuse(CodeStuckScanRefused, "", "", "state %q has two expectations", e.State)
		}
		byState[e.State] = e
	}
	return StuckDetector{byState: byState, defaultMaxIdle: defaultMaxIdle}, nil
}

// NodeObservation is one node's progress evidence at scan time. Timestamp
// pointers are nil when the node awaits nothing of that kind.
type NodeObservation struct {
	Tenant     uuid.UUID
	InstanceID uuid.UUID
	NodeID     string
	State      NodeStatus

	LastProgressAt time.Time

	LeaseExpiresAt   *time.Time
	NextTimerAt      *time.Time
	ExpectedSignalAt *time.Time
	NextRetryAt      *time.Time
	SLADeadline      *time.Time

	Attempts int
}

// StuckFinding is one stranded node with the evidence a single linked
// incident or work item needs. IncidentKey is deterministic per
// tenant, instance, node and missed-signal set, so repeated scans dedupe
// to one open or link.
type StuckFinding struct {
	Tenant     uuid.UUID
	InstanceID uuid.UUID
	NodeID     string
	State      NodeStatus

	IdleFor  time.Duration
	Missed   []StuckKind
	Poisoned bool

	Evidence    []string
	IncidentKey string
}

// Detect scans observations for stranded nodes without touching business
// state. Terminal nodes are skipped; anything else with no missed signal
// is patient, however old.
func (d StuckDetector) Detect(at time.Time, tenant uuid.UUID, obs []NodeObservation) ([]StuckFinding, error) {
	if at.IsZero() {
		return nil, refuse(CodeStuckScanRefused, "", "", "scan clock is not set")
	}
	if tenant == uuid.Nil {
		return nil, refuse(CodeStuckScanRefused, "", "", "scan tenant is not set")
	}
	var out []StuckFinding
	for i := range obs {
		o := obs[i]
		if o.Tenant != tenant {
			// A cross-tenant observation looks exactly like a missing
			// instance: the scan learns nothing about other tenants.
			return nil, refuse(CodeInstanceNotFound, o.InstanceID.String(), o.NodeID, "instance is not visible to the scan tenant")
		}
		if !o.State.Valid() {
			return nil, refuse(CodeStuckScanRefused, o.InstanceID.String(), o.NodeID, "unknown node state %q", o.State)
		}
		if o.State.Terminal() {
			continue
		}
		if o.NodeID == "" {
			return nil, refuse(CodeStuckScanRefused, o.InstanceID.String(), "", "node identity is required")
		}
		if o.LastProgressAt.IsZero() {
			return nil, refuse(CodeStuckScanRefused, o.InstanceID.String(), o.NodeID, "last progress is not recorded")
		}
		if o.LastProgressAt.After(at) {
			return nil, refuse(CodeStuckScanRefused, o.InstanceID.String(), o.NodeID, "last progress is after the scan clock")
		}
		missed := d.missed(o, at)
		if len(missed) == 0 {
			continue
		}
		out = append(out, buildFinding(d, tenant, o, at, missed))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].InstanceID != out[j].InstanceID {
			return out[i].InstanceID.String() < out[j].InstanceID.String()
		}
		return out[i].NodeID < out[j].NodeID
	})
	return out, nil
}

func (d StuckDetector) expectation(state NodeStatus) (time.Duration, map[StuckKind]bool, int) {
	if e, ok := d.byState[state]; ok {
		kinds := make(map[StuckKind]bool, len(e.Kinds))
		for _, k := range e.Kinds {
			kinds[k] = true
		}
		return e.MaxIdle, kinds, e.PoisonAfter
	}
	kinds := make(map[StuckKind]bool, len(StuckKindsAll()))
	for _, k := range StuckKindsAll() {
		kinds[k] = true
	}
	return d.defaultMaxIdle, kinds, 0
}

func (d StuckDetector) missed(o NodeObservation, at time.Time) []StuckKind {
	maxIdle, kinds, _ := d.expectation(o.State)
	var missed []StuckKind
	add := func(ok bool, k StuckKind) {
		if ok && kinds[k] {
			missed = append(missed, k)
		}
	}
	// A lease held through its expiry instant is gone: the holder that
	// should have renewed it did not.
	add(o.LeaseExpiresAt != nil && !at.Before(*o.LeaseExpiresAt), StuckKindLease)
	// A timer fires at its instant; it is overdue only past it.
	add(o.NextTimerAt != nil && at.After(*o.NextTimerAt), StuckKindTimer)
	add(o.ExpectedSignalAt != nil && at.After(*o.ExpectedSignalAt), StuckKindSignal)
	add(o.NextRetryAt != nil && at.After(*o.NextRetryAt), StuckKindRetry)
	add(o.SLADeadline != nil && at.After(*o.SLADeadline), StuckKindSLA)
	// Silence past the budget misses the heartbeat; silence exactly at
	// the budget is still within it.
	add(at.Sub(o.LastProgressAt) > maxIdle, StuckKindIdle)
	sort.Slice(missed, func(i, j int) bool { return missed[i] < missed[j] })
	return missed
}

func buildFinding(d StuckDetector, tenant uuid.UUID, o NodeObservation, at time.Time, missed []StuckKind) StuckFinding {
	_, kinds, poisonAfter := d.expectation(o.State)
	poisoned := poisonAfter > 0 && o.Attempts >= poisonAfter && kinds[StuckKindRetry] && containsKind(missed, StuckKindRetry)
	evidence := make([]string, 0, len(missed)+1)
	evidence = append(evidence, fmt.Sprintf("idle %s in %s since %s", at.Sub(o.LastProgressAt).Round(time.Second), o.State, o.LastProgressAt.UTC().Format(time.RFC3339)))
	for _, k := range missed {
		evidence = append(evidence, "missed "+string(k))
	}
	if poisoned {
		evidence = append(evidence, fmt.Sprintf("poisoned after %d attempts", o.Attempts))
	}
	sum := sha256.Sum256([]byte(tenant.String() + "\x00" + o.InstanceID.String() + "\x00" + o.NodeID + "\x00" + joinKinds(missed)))
	return StuckFinding{
		Tenant:      tenant,
		InstanceID:  o.InstanceID,
		NodeID:      o.NodeID,
		State:       o.State,
		IdleFor:     at.Sub(o.LastProgressAt),
		Missed:      missed,
		Poisoned:    poisoned,
		Evidence:    evidence,
		IncidentKey: "stk:" + hex.EncodeToString(sum[:16]),
	}
}

func containsKind(kinds []StuckKind, want StuckKind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

func joinKinds(kinds []StuckKind) string {
	out := ""
	for i, k := range kinds {
		if i > 0 {
			out += ","
		}
		out += string(k)
	}
	return out
}
