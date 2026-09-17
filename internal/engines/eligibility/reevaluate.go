// ELIG-006: reevaluate eligibility when governed inputs change.
//
// A stored Result is evidence for exactly one pinned input context (see
// ValidateBinding). When a governed input moves — fact, rule, program,
// population or time — the old result must stop being consumable as the
// current answer. Reevaluate refuses to bless a stale request (a typed
// ELIG_006_REJECTED naming field/state/version, with no value emitted) and,
// once the caller pins the declared change into a new request, emits exactly
// one bounded Reevaluation comparing the prior and current outcomes. A status
// flip into or out of ELIGIBLE always carries participation obligations: this
// package never silently changes participation.
package eligibility

import (
	"context"
	"fmt"
)

// MaxReevaluationChanges bounds one reevaluation: a change set larger than
// this is not a reevaluation but an undeclared re-baselining.
const MaxReevaluationChanges = 32

// ErrReevaluationRejected identifies an ELIG-006 refusal. Match with
// errors.Is; the *ReevaluationRejection carries field/state/version.
var ErrReevaluationRejected = fmt.Errorf("ELIG_006_REJECTED")

// ReevaluationRejection identifies the exact contract member that prevented
// reevaluation. Version is the program revision the caller asked about.
type ReevaluationRejection struct {
	Code    string
	Field   string
	State   string
	Version string
	Reason  string
}

func (e *ReevaluationRejection) Error() string {
	return fmt.Sprintf("%s field=%s state=%s version=%s: %s", e.Code, e.Field, e.State, e.Version, e.Reason)
}

// Unwrap lets errors.Is(err, ErrReevaluationRejected) match.
func (e *ReevaluationRejection) Unwrap() error { return ErrReevaluationRejected }

func rejectReevaluation(field, state, version, reason string) error {
	return &ReevaluationRejection{Code: "ELIG_006_REJECTED", Field: field, State: state, Version: version, Reason: reason}
}

// ChangeKind names which governed input moved.
type ChangeKind uint8

// Governed input kinds.
const (
	ChangeUnspecified ChangeKind = iota
	ChangeFact
	ChangeRule
	ChangeProgram
	ChangePopulation
	ChangeTime
)

var changeKindWire = map[ChangeKind]string{
	ChangeFact:       "FACT",
	ChangeRule:       "RULE",
	ChangeProgram:    "PROGRAM",
	ChangePopulation: "POPULATION",
	ChangeTime:       "TIME",
}

// Valid reports whether k names a governed input kind.
func (k ChangeKind) Valid() bool { return changeKindWire[k] != "" }

// String returns the wire token.
func (k ChangeKind) String() string {
	if s, ok := changeKindWire[k]; ok {
		return s
	}
	return "CHANGE_UNSPECIFIED"
}

// InputChange declares one governed input move from one pinned version to
// another. Ref names the field, rule, program, pool or interval that moved.
type InputChange struct {
	Kind        ChangeKind
	Ref         string
	FromVersion string
	ToVersion   string
}

func (c InputChange) validate() error {
	if !c.Kind.Valid() {
		return fmt.Errorf("kind %d is not governed", uint8(c.Kind))
	}
	if c.Ref == "" {
		return fmt.Errorf("ref is required")
	}
	if c.FromVersion == "" || c.ToVersion == "" {
		return fmt.Errorf("from/to versions are required")
	}
	if c.FromVersion == c.ToVersion {
		return fmt.Errorf("from/to versions are identical")
	}
	return nil
}

// priorVersionOf reads the prior result's pinned version for the input kind
// the change claims moved.
func priorVersionOf(prior Result, c InputChange) string {
	switch c.Kind {
	case ChangeFact:
		return prior.FactSnapshotRef
	case ChangeRule:
		return prior.RuleSnapshotRef
	case ChangePopulation:
		return prior.PopulationSnapshotRef
	case ChangeProgram:
		return prior.ProgramVersion
	}
	return ""
}

// Reevaluation is the single bounded comparison of one prior result against
// the current evaluation under the declared changes. Obligations always
// carries one entry per declared change plus a participation-review entry
// whenever participation flips; a flip is therefore never silent.
type Reevaluation struct {
	PriorStatus          Status
	CurrentStatus        Status
	Changed              bool
	ParticipationChanged bool
	Obligations          []string
	PriorDigest          string
	CurrentDigest        string
	Changes              []InputChange
}

func touchesParticipation(a, b Status) bool {
	return a == StatusEligible || b == StatusEligible
}

// Reevaluate compares prior against a fresh evaluation of req under plan,
// gated on changes declaring exactly how the governed inputs moved. It is
// pure: readers only, no writes of any kind. A refusal returns the zero
// Reevaluation and an *ReevaluationRejection matching
// ErrReevaluationRejected.
func Reevaluate(ctx context.Context, facts FactReader, rules RuleReader, req Request, prior Result, plan CompiledPlan, changes []InputChange) (Reevaluation, error) {
	version := req.SubjectMatter.Revision
	if version == "" {
		version = prior.ProgramVersion
	}
	if err := prior.Validate(); err != nil {
		return Reevaluation{}, rejectReevaluation("prior", "INVALID", version, err.Error())
	}
	if err := req.Validate(); err != nil {
		return Reevaluation{}, rejectReevaluation("request", "INVALID", version, err.Error())
	}
	if len(changes) == 0 {
		return Reevaluation{}, rejectReevaluation("changes", "EMPTY", version, "a reevaluation without a declared input change would leave the stale result consumable")
	}
	if len(changes) > MaxReevaluationChanges {
		return Reevaluation{}, rejectReevaluation("changes", "EXCEEDS_BOUND", version, fmt.Sprintf("%d changes exceed bound %d", len(changes), MaxReevaluationChanges))
	}
	for i, c := range changes {
		if err := c.validate(); err != nil {
			return Reevaluation{}, rejectReevaluation("changes.kind", fmt.Sprintf("INVALID:%d", i), version, err.Error())
		}
		if c.Kind == ChangeTime {
			// Time versions are caller-declared cycle labels the engine
			// cannot resolve; what it verifies is that the effective
			// interval actually moved off the prior pin.
			if req.EffectiveInterval == prior.EffectiveInterval {
				return Reevaluation{}, rejectReevaluation("snapshots", fmt.Sprintf("STALE:%d", i), version, fmt.Sprintf("%s declares %q but the request still pins the prior interval", c.Ref, c.ToVersion))
			}
			continue
		}
		if got := priorVersionOf(prior, c); got != c.FromVersion {
			return Reevaluation{}, rejectReevaluation("changes.from", fmt.Sprintf("MISMATCH:%d", i), version, fmt.Sprintf("%s moved from %q, prior pins %q", c.Ref, c.FromVersion, got))
		}
		if pinned := requestVersionOf(req, prior, c); pinned != c.ToVersion {
			return Reevaluation{}, rejectReevaluation("snapshots", fmt.Sprintf("STALE:%d", i), version, fmt.Sprintf("%s declares %q but the request pins %q", c.Ref, c.ToVersion, pinned))
		}
	}
	current, err := Evaluate(ctx, facts, rules, req, plan)
	if err != nil {
		return Reevaluation{}, err
	}
	out := Reevaluation{
		PriorStatus:   prior.Status,
		CurrentStatus: current.Status,
		PriorDigest:   prior.Digest,
		CurrentDigest: current.Digest,
		Changes:       append([]InputChange(nil), changes...),
		Obligations:   make([]string, 0, len(changes)+1),
	}
	out.Changed = out.PriorStatus != out.CurrentStatus
	out.ParticipationChanged = out.Changed && touchesParticipation(out.PriorStatus, out.CurrentStatus)
	for _, c := range changes {
		out.Obligations = append(out.Obligations, "reconcile:"+c.Kind.String()+":"+c.Ref)
	}
	if out.ParticipationChanged {
		out.Obligations = append(out.Obligations, "participation-review:"+out.PriorStatus.String()+"->"+out.CurrentStatus.String())
	}
	return out, nil
}

// requestVersionOf reads the new request's pinned version for the input kind
// the change claims moved.
func requestVersionOf(req Request, prior Result, c InputChange) string {
	switch c.Kind {
	case ChangeFact:
		return req.Snapshots.FactSnapshotRef
	case ChangeRule:
		return req.Snapshots.RuleSnapshotRef
	case ChangePopulation:
		return req.Snapshots.PopulationSnapshotRef
	case ChangeProgram:
		return req.SubjectMatter.Revision
	}
	return ""
}
