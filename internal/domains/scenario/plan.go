// Plan approval: SCENARIO-005 freezes a selected scenario revision into
// an immutable Plan.
//
// The Plan binds the exact revision digest, assumptions, baseline,
// scope, decision and validity window. Approval refuses a revision that
// moved since review, a mismatched baseline, a lapsed approver authority
// and a self-approval as SCENARIO_005_REJECTED. The clock is injected by
// the caller, so the decision is pure: no database, no wall clock, no
// network, no persistence — only the returned Plan.
package scenario

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PlanRejectedCode is the stable machine-readable refusal code.
const PlanRejectedCode = "SCENARIO_005_REJECTED"

// ErrPlanRejected is the sentinel for refused approvals. Match it with
// errors.Is rather than parsing the reason.
var ErrPlanRejected = errors.New("scenario: plan approval rejected")

// PlanRejectedError names the offending field and version.
type PlanRejectedError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *PlanRejectedError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// Is reports ErrPlanRejected without parsing the reason.
func (e *PlanRejectedError) Is(target error) bool {
	return target == ErrPlanRejected
}

// AsPlanRejected unwraps a SCENARIO_005_REJECTED refusal.
func AsPlanRejected(err error) (*PlanRejectedError, bool) {
	var rejected *PlanRejectedError
	if errors.As(err, &rejected) && rejected.Code == PlanRejectedCode {
		return rejected, true
	}
	return nil, false
}

func planRejected(field, state string) *PlanRejectedError {
	return &PlanRejectedError{Code: PlanRejectedCode, Field: field, State: state, Version: schemaVersion}
}

// PlanDecision is the closed decision vocabulary a Plan can record.
type PlanDecision string

const (
	// PlanApproved records an approval to proceed under the Plan bounds.
	PlanApproved PlanDecision = "APPROVED"
	// PlanRejected records a refusal; the Plan still freezes what was
	// decided so the refusal itself is auditable.
	PlanRejected PlanDecision = "REJECTED"
)

// Valid reports whether the decision is declared.
func (d PlanDecision) Valid() bool {
	return d == PlanApproved || d == PlanRejected
}

// PlanApproval is the fully bound input to one approval decision. The
// approver cites the exact revision digest and baseline they reviewed,
// plus the authority that empowers them and the instant they decided.
type PlanApproval struct {
	Approver              string
	AuthorityRef          string
	AuthorityVersion      string
	AuthorityValidThrough values.Instant
	ExpectedDigest        string
	ExpectedBaseline      string
	DecidedAt             values.Instant
	Decision              PlanDecision
	Statement             string
}

// Plan is the immutable record of one scenario decision. It carries the
// exact revision it froze, the decision taken and the validity window it
// was taken for — the scenario horizon, never wider.
type Plan struct {
	ScenarioID          string
	Revision            uint64
	RevisionDigest      string
	BaselineSnapshotRef string
	Scope               string
	Horizon             values.EffectiveInterval
	Assumptions         []Assumption
	Approver            string
	AuthorityRef        string
	AuthorityVersion    string
	Decision            PlanDecision
	DecidedAt           values.Instant
	Validity            values.EffectiveInterval
	Statement           string
	CanonicalDigest     string
}

// Validate implements validation.
func (p Plan) Validate() error {
	if strings.TrimSpace(p.ScenarioID) == "" || p.Revision == 0 {
		return fmt.Errorf("%w: scenario id and non-zero revision are required", ErrInvalidScenario)
	}
	if strings.TrimSpace(p.RevisionDigest) == "" {
		return fmt.Errorf("%w: revision digest is required", ErrInvalidScenario)
	}
	for name, field := range map[string]string{"baseline snapshot ref": p.BaselineSnapshotRef, "scope": p.Scope} {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidScenario, name)
		}
	}
	if err := p.Horizon.Validate(); err != nil {
		return fmt.Errorf("%w: horizon: %v", ErrInvalidScenario, err)
	}
	if len(p.Assumptions) == 0 {
		return fmt.Errorf("%w: at least one assumption is required", ErrInvalidScenario)
	}
	seen := make(map[string]struct{}, len(p.Assumptions))
	for _, assumption := range p.Assumptions {
		if err := assumption.Validate(); err != nil {
			return err
		}
		if _, ok := seen[assumption.Key]; ok {
			return fmt.Errorf("%w: duplicate assumption key %q", ErrInvalidScenario, assumption.Key)
		}
		seen[assumption.Key] = struct{}{}
	}
	for name, field := range map[string]string{"approver": p.Approver, "authority ref": p.AuthorityRef, "authority version": p.AuthorityVersion, "statement": p.Statement} {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidScenario, name)
		}
	}
	if !p.Decision.Valid() {
		return fmt.Errorf("%w: decision %q is not declared", ErrInvalidScenario, p.Decision)
	}
	if err := p.DecidedAt.Validate(); err != nil {
		return fmt.Errorf("%w: decided-at: %v", ErrInvalidScenario, err)
	}
	if err := p.Validity.Validate(); err != nil {
		return fmt.Errorf("%w: validity: %v", ErrInvalidScenario, err)
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidScenario)
	}
	return nil
}

func (p Plan) body() []byte {
	assumptions := append([]Assumption(nil), p.Assumptions...)
	sort.Slice(assumptions, func(i, j int) bool { return assumptions[i].Key < assumptions[j].Key })
	w := canonicalbytes.New("hcmnext.domains.scenario.Plan", schemaVersion).
		String("scenario_id", p.ScenarioID).Int("revision", int64(p.Revision)).
		String("revision_digest", p.RevisionDigest).String("baseline_snapshot_ref", p.BaselineSnapshotRef).
		String("scope", p.Scope).Value("horizon", p.Horizon).
		String("approver", p.Approver).String("authority_ref", p.AuthorityRef).
		String("authority_version", p.AuthorityVersion).String("decision", string(p.Decision)).
		Value("decided_at", p.DecidedAt).Value("validity", p.Validity).
		String("statement", p.Statement).Count("assumptions", len(assumptions))
	for _, assumption := range assumptions {
		w.Value("assumption", assumption)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (p Plan) computedDigest() string {
	b := p.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// Canonical returns the canonical encoding of a valid Plan.
func (p Plan) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return p.body()
}

// Digest returns the stable digest of a valid Plan.
func (p Plan) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p.computedDigest(), nil
}

// AssumptionsCopy returns detached assumptions for read-only consumers.
func (p Plan) AssumptionsCopy() []Assumption { return cloneAssumptions(p.Assumptions) }

// Explain returns a stable summary for audit and presentation consumers.
func (p Plan) Explain() string {
	return fmt.Sprintf("plan %s revision %d decision %s approver %s digest %s",
		p.ScenarioID, p.Revision, p.Decision, p.Approver, p.CanonicalDigest)
}

// ApprovePlan freezes one scenario revision into an immutable Plan. now
// is the injected clock: the decision cannot come from its future. Only
// the returned Plan is recorded; the inputs are never mutated and
// nothing is persisted.
func ApprovePlan(revision ScenarioRevision, approval PlanApproval, now values.Instant) (Plan, error) {
	if err := revision.Validate(); err != nil {
		return Plan{}, planRejected("revision", "invalid")
	}
	if err := now.Validate(); err != nil {
		return Plan{}, planRejected("clock", "invalid")
	}
	if strings.TrimSpace(approval.Approver) == "" {
		return Plan{}, planRejected("approval.approver", "missing")
	}
	if strings.TrimSpace(approval.AuthorityRef) == "" || strings.TrimSpace(approval.AuthorityVersion) == "" {
		return Plan{}, planRejected("approval.authority", "missing")
	}
	if err := approval.AuthorityValidThrough.Validate(); err != nil {
		return Plan{}, planRejected("approval.authority", "invalid")
	}
	if strings.TrimSpace(approval.ExpectedDigest) == "" || strings.TrimSpace(approval.ExpectedBaseline) == "" {
		return Plan{}, planRejected("approval.expected", "missing")
	}
	if err := approval.DecidedAt.Validate(); err != nil {
		return Plan{}, planRejected("approval.decision", "invalid")
	}
	if !approval.Decision.Valid() {
		return Plan{}, planRejected("approval.decision", "invalid")
	}
	if strings.TrimSpace(approval.Statement) == "" {
		return Plan{}, planRejected("approval.statement", "missing")
	}
	if approval.Approver == revision.Author {
		return Plan{}, planRejected("approval.approver", "self-approval")
	}
	if approval.ExpectedDigest != revision.computedDigest() {
		return Plan{}, planRejected("revision", "changed-since-review")
	}
	if approval.ExpectedBaseline != revision.BaselineSnapshotRef {
		return Plan{}, planRejected("baseline", "mismatch")
	}
	if approval.DecidedAt.After(approval.AuthorityValidThrough) {
		return Plan{}, planRejected("approval.authority", "stale")
	}
	if approval.DecidedAt.After(now) {
		return Plan{}, planRejected("approval.decision", "future-decision")
	}
	plan := Plan{
		ScenarioID: revision.ScenarioID, Revision: revision.Revision,
		RevisionDigest: revision.computedDigest(), BaselineSnapshotRef: revision.BaselineSnapshotRef,
		Scope: revision.Scope, Horizon: revision.Horizon, Assumptions: cloneAssumptions(revision.Assumptions),
		Approver: approval.Approver, AuthorityRef: approval.AuthorityRef,
		AuthorityVersion: approval.AuthorityVersion, Decision: approval.Decision,
		DecidedAt: approval.DecidedAt, Validity: revision.Horizon, Statement: approval.Statement,
	}
	plan.CanonicalDigest = plan.computedDigest()
	if err := plan.Validate(); err != nil {
		return Plan{}, planRejected("plan", "invalid")
	}
	return plan, nil
}
