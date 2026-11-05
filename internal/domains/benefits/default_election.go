// WF-CAP-013: apply a plan's declared default for enrollment
// non-responders.
//
// When an open-enrollment or life-event window closes without an election,
// benefits.apply_default_election records exactly what the plan declared:
// roll the prior election forward, waive with the missed window cited as
// evidence, or enroll the declared core tier. The default is a normal
// BEN-005 election revision — digest-chained through RecordElection —
// returned beside the missed-window evidence, so a downstream consumer can
// tell a passive election from an active one and prove which window was
// missed.
//
// The caller proves the window closed: this function takes the closed
// window as input and refuses an open one. Deadline evaluation itself
// belongs to WF-EXT-012, whose deadline route invokes this operation.
package benefits

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DefaultKind is the closed vocabulary of plan-declared defaults.
type DefaultKind string

const (
	// DefaultRollover carries the worker's prior election into the new
	// window unchanged.
	DefaultRollover DefaultKind = "ROLLOVER"
	// DefaultWaive records a waiver citing the missed window as evidence.
	DefaultWaive DefaultKind = "WAIVE"
	// DefaultCoreCoverage enrolls the plan's declared core tier and costs.
	DefaultCoreCoverage DefaultKind = "CORE_COVERAGE"
)

func (k DefaultKind) valid() bool {
	switch k {
	case DefaultRollover, DefaultWaive, DefaultCoreCoverage:
		return true
	default:
		return false
	}
}

// DefaultElectionPolicy is one plan revision's declared default. It binds
// by plan ref and revision rather than by editing the plan: the catalogue
// stays read-only and the default travels as its own governed record.
type DefaultElectionPolicy struct {
	PlanRef      string
	PlanRevision string
	Kind         DefaultKind
	// CoreTier, EmployeeCost, EmployerCost and RateVersion apply to
	// CORE_COVERAGE only; other kinds must leave them empty.
	CoreTier     TierCode
	EmployeeCost values.Decimal
	EmployerCost values.Decimal
	RateVersion  string
}

// Validate rejects a policy that names no plan, no kind, or core fields it
// cannot use.
func (p DefaultElectionPolicy) Validate() error {
	if strings.TrimSpace(p.PlanRef) == "" || strings.TrimSpace(p.PlanRevision) == "" {
		return electionReject("election.default.plan", "MISSING", "default policy names no plan revision")
	}
	if !p.Kind.valid() {
		return electionReject("election.default.kind", "UNDECLARED", "default kind is not declared")
	}
	core := p.Kind == DefaultCoreCoverage
	if !core {
		if p.CoreTier != "" || p.RateVersion != "" {
			return electionReject("election.default.core", "MISPLACED", "only core coverage declares a tier and rate")
		}
		if p.EmployeeCost.Sign() != 0 || p.EmployerCost.Sign() != 0 {
			return electionReject("election.default.core", "MISPLACED", "only core coverage declares costs")
		}
		return nil
	}
	if !p.CoreTier.Valid() {
		return electionReject("election.default.tier", "UNDECLARED", "core tier is not declared")
	}
	if strings.TrimSpace(p.RateVersion) == "" {
		return electionReject("election.default.rate_version", "STALE", "core coverage names no rate version")
	}
	if err := p.EmployeeCost.Validate(); err != nil {
		return electionReject("election.default.employee_cost", "INVALID", "core employee cost is not a valid decimal")
	}
	if err := p.EmployerCost.Validate(); err != nil {
		return electionReject("election.default.employer_cost", "INVALID", "core employer cost is not a valid decimal")
	}
	if p.EmployeeCost.Sign() < 0 || p.EmployerCost.Sign() < 0 {
		return electionReject("election.default.cost", "NEGATIVE", "core costs must not be negative")
	}
	return nil
}

// Digest is the stable policy version recorded on every default it applies.
func (p DefaultElectionPolicy) Digest() string {
	w := canonicalbytes.New("hcmnext.domains.benefits.DefaultElectionPolicy", 1).
		String("plan_ref", p.PlanRef).
		String("plan_revision", p.PlanRevision).
		String("kind", string(p.Kind)).
		String("core_tier", string(p.CoreTier)).
		String("employee_cost", p.EmployeeCost.String()).
		String("employer_cost", p.EmployerCost.String()).
		String("rate_version", p.RateVersion)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// MissedWindow is the caller's proof that one worker's window closed with
// no election. Tenant and WorkerRef name the non-responder; the window
// itself must already be CLOSED — this operation applies defaults, it never
// decides that a window closed.
type MissedWindow struct {
	Tenant        string
	WorkerRef     string
	WindowID      string
	Kind          WindowKind
	Status        WindowStatus
	ClosedAt      time.Time
	EffectiveDate time.Time
}

// Validate refuses a window that is not a closed, dated, worker-bound miss.
func (w MissedWindow) Validate() error {
	if strings.TrimSpace(w.Tenant) == "" || strings.TrimSpace(w.WorkerRef) == "" {
		return electionReject("election.default.scope", "MISSING", "missed window names no tenant and worker")
	}
	if strings.TrimSpace(w.WindowID) == "" || !validWindowKind(w.Kind) {
		return electionReject("election.default.window", "UNDECLARED", "missed window is not identified")
	}
	if w.Status != WindowClosed {
		return electionReject("election.default.window", "NOT_CLOSED", "defaults apply only to a closed window")
	}
	if w.ClosedAt.IsZero() || w.EffectiveDate.IsZero() {
		return electionReject("election.default.window", "MISSING", "missed window names no closure and effective date")
	}
	return nil
}

// EvidenceRef cites the miss in one stable string, recorded as the waiver
// evidence of a WAIVE default.
func (w MissedWindow) EvidenceRef() string {
	return "missed-window:" + w.WindowID + ":" + w.ClosedAt.UTC().Format(time.RFC3339)
}

// Digest seals the miss for the result record.
func (w MissedWindow) Digest() string {
	digest, err := canonicalbytes.New("hcmnext.domains.benefits.MissedWindow", 1).
		String("tenant", w.Tenant).
		String("worker_ref", w.WorkerRef).
		String("window_id", w.WindowID).
		String("kind", string(w.Kind)).
		String("status", string(w.Status)).
		String("closed_at", w.ClosedAt.UTC().Format(time.RFC3339)).
		String("effective_date", w.EffectiveDate.UTC().Format(time.RFC3339)).
		Digest()
	if err != nil {
		return ""
	}
	return digest
}

// DefaultElectionResult is one applied default: the sealed revision beside
// the kind, the policy version and the missed-window evidence that prove it
// is passive.
type DefaultElectionResult struct {
	Revision     ElectionRevision
	Kind         DefaultKind
	PolicyDigest string
	Evidence     MissedWindow
}

// ApplyDefaultElection records the policy's default for a non-responder.
// head is the worker's accepted revision or nil when none exists: a head
// already in this window is a responder and is refused, and ROLLOVER
// without a head has nothing to carry. The revision is recorded through
// RecordElection, so it chains, validates and seals exactly like an active
// election.
func ApplyDefaultElection(head *ElectionRevision, policy DefaultElectionPolicy, window MissedWindow) (DefaultElectionResult, error) {
	if err := policy.Validate(); err != nil {
		return DefaultElectionResult{}, err
	}
	if err := window.Validate(); err != nil {
		return DefaultElectionResult{}, err
	}
	if head != nil {
		if err := head.Verify(); err != nil {
			return DefaultElectionResult{}, err
		}
		if head.Tenant != window.Tenant || head.WorkerRef != window.WorkerRef {
			return DefaultElectionResult{}, electionReject("election.default.scope", "MISMATCH", "prior election is outside the missed window scope")
		}
		if head.WindowID == window.WindowID {
			return DefaultElectionResult{}, electionReject("election.default.window", "ALREADY_ELECTED", "the worker elected in this window")
		}
	}
	var in ElectionInput
	switch policy.Kind {
	case DefaultRollover:
		if head == nil {
			return DefaultElectionResult{}, electionReject("election.default.rollover", "MISSING", "rollover has no prior election to carry")
		}
		if head.PlanRef != policy.PlanRef || head.PlanRevision != policy.PlanRevision {
			return DefaultElectionResult{}, electionReject("election.default.rollover", "MISMATCH", "rollover stays within its declared plan revision")
		}
		in = ElectionInput{
			Tenant: head.Tenant, WorkerRef: head.WorkerRef,
			PlanRef: head.PlanRef, PlanRevision: head.PlanRevision, RateVersion: head.RateVersion,
			WindowID: window.WindowID, Tier: head.Tier, Dependents: head.Dependents,
			EffectiveDate: window.EffectiveDate,
			EmployeeCost:  head.EmployeeCost, EmployerCost: head.EmployerCost,
			PriorDigest: head.CanonicalDigest, Supersedes: head.CanonicalDigest,
		}
	case DefaultWaive:
		// A waiver records the tier and rate it waives: the prior ones when
		// chaining, else the neutral tier and an explicit no-rate marker.
		tier, rate := TierEmployeeOnly, "rates:waived"
		var prior string
		if head != nil {
			tier, rate, prior = head.Tier, head.RateVersion, head.CanonicalDigest
		}
		in = ElectionInput{
			Tenant: window.Tenant, WorkerRef: window.WorkerRef,
			PlanRef: policy.PlanRef, PlanRevision: policy.PlanRevision,
			RateVersion: rate, WindowID: window.WindowID, Tier: tier,
			EffectiveDate: window.EffectiveDate,
			EmployeeCost:  values.MustDecimal("0.00", 2, values.RoundingHalfUp),
			EmployerCost:  values.MustDecimal("0.00", 2, values.RoundingHalfUp),
			Waiver:        true, WaiverEvidence: window.EvidenceRef(), PriorDigest: prior,
		}
	case DefaultCoreCoverage:
		var prior string
		if head != nil {
			prior = head.CanonicalDigest
		}
		in = ElectionInput{
			Tenant: window.Tenant, WorkerRef: window.WorkerRef,
			PlanRef: policy.PlanRef, PlanRevision: policy.PlanRevision, RateVersion: policy.RateVersion,
			WindowID: window.WindowID, Tier: policy.CoreTier,
			EffectiveDate: window.EffectiveDate,
			EmployeeCost:  policy.EmployeeCost, EmployerCost: policy.EmployerCost,
			PriorDigest: prior,
		}
	default:
		return DefaultElectionResult{}, electionReject("election.default.kind", "UNDECLARED", "default kind is not declared")
	}
	rev, err := RecordElection(head, in)
	if err != nil {
		return DefaultElectionResult{}, err
	}
	return DefaultElectionResult{Revision: rev, Kind: policy.Kind, PolicyDigest: policy.Digest(), Evidence: window}, nil
}
