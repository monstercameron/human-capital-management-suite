package humanwork

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	requirementSchema = "hcmnext.humanwork.ApprovalRequirement"
	requirementSetSch = "hcmnext.humanwork.ApprovalRequirementSet"
)

// Clock supplies the recording time. It is a parameter so that derivation and
// resolution replay to identical evidence.
type Clock func() values.Instant

// Quorum is the cardinality of a requirement: how many approvals satisfy it and
// whether they must come from different people.
type Quorum struct {
	MinApprovals              uint32
	RequireDistinctPrincipals bool
}

// Validate rejects a quorum of zero. "Nobody has to approve" is expressed by
// not stating the requirement, never by a requirement that needs no approvals.
func (q Quorum) Validate() error {
	if q.MinApprovals == 0 {
		return newError("Validate", "quorum.min_approvals", ErrInvalidRequirement,
			"a requirement that needs zero approvals is not a requirement")
	}
	if q.MinApprovals > 1 && !q.RequireDistinctPrincipals {
		return newError("Validate", "quorum.require_distinct_principals", ErrInvalidRequirement,
			"a quorum of %d must say whether the approvers are distinct", q.MinApprovals)
	}
	return nil
}

// Deadline is when a decision is due and how long a recorded one stays fresh.
// The two are separate because a decision made in time can still go stale, and
// conflating them is how an approval from last quarter ends up authorizing a
// commit today.
type Deadline struct {
	// DecideBy is the instant by which a decision must be recorded.
	DecideBy values.Instant
	// Expiry is the instant after which a recorded decision is no longer
	// current and must be revalidated or re-taken.
	Expiry values.Instant
}

// Validate rejects an unset or inverted deadline.
func (d Deadline) Validate() error {
	if !d.DecideBy.IsSet() {
		return newError("Validate", "deadline.decide_by", ErrInvalidRequirement,
			"requirement states no decision deadline")
	}
	if !d.Expiry.IsSet() {
		return newError("Validate", "deadline.expiry", ErrInvalidRequirement,
			"requirement states no decision expiry")
	}
	if d.Expiry.Before(d.DecideBy) {
		return newError("Validate", "deadline.expiry", ErrInvalidRequirement,
			"decision expiry %s precedes the decision deadline %s", d.Expiry, d.DecideBy)
	}
	return nil
}

// EscalationAction is what happens when a deadline passes. There is
// deliberately no action that records a decision: escalation changes attention,
// routing or eligibility, and never approves work on a human's behalf.
type EscalationAction string

// Escalation actions.
const (
	// EscalationUnspecified is the zero value and is never legal.
	EscalationUnspecified EscalationAction = ""
	// EscalationNotify raises attention and changes nothing else.
	EscalationNotify EscalationAction = "NOTIFY"
	// EscalationReassignToFallback widens routing to the requirement's declared
	// fallback expression. Fallback candidates are held to the same authority
	// floor and the same separation constraints as the primary ones.
	EscalationReassignToFallback EscalationAction = "REASSIGN_TO_FALLBACK"
	// EscalationBlock stops the proposal and requires human intervention.
	EscalationBlock EscalationAction = "BLOCK"
)

var escalationWire = map[EscalationAction]bool{
	EscalationNotify:             true,
	EscalationReassignToFallback: true,
	EscalationBlock:              true,
}

// Valid reports whether a is a declared escalation action.
func (a EscalationAction) Valid() bool { return escalationWire[a] }

// EscalationPolicy is the requirement's deadline behaviour.
type EscalationPolicy struct {
	OnDeadline EscalationAction
	// Fallback is required when OnDeadline is REASSIGN_TO_FALLBACK and must be
	// absent otherwise.
	Fallback *Expression
	RuleID   string
}

// Validate rejects an unstated or self-contradictory escalation policy.
func (p EscalationPolicy) Validate() error {
	if !p.OnDeadline.Valid() {
		return newError("Validate", "escalation.on_deadline", ErrInvalidRequirement,
			"escalation action %q is not declared", string(p.OnDeadline))
	}
	if p.RuleID == "" {
		return newError("Validate", "escalation.rule_id", ErrInvalidRequirement,
			"escalation policy cites no rule id")
	}
	switch p.OnDeadline {
	case EscalationReassignToFallback:
		if p.Fallback == nil {
			return newError("Validate", "escalation.fallback", ErrInvalidRequirement,
				"REASSIGN_TO_FALLBACK names no fallback expression")
		}
		return p.Fallback.Validate()
	default:
		if p.Fallback != nil {
			return newError("Validate", "escalation.fallback", ErrInvalidRequirement,
				"%s carries a fallback expression it never uses", string(p.OnDeadline))
		}
	}
	return nil
}

// SeparationConstraints is the separation-of-duties contract for one
// requirement. Every flag is an exclusion rule applied during resolution, and
// each one that fires is reported with RuleID so a candidate set can explain
// every person it left out.
type SeparationConstraints struct {
	// RequesterMayNotApprove excludes the principal who raised the proposal,
	// and any delegate acting on that principal's own authority.
	RequesterMayNotApprove bool
	// SubjectMayNotApprove excludes the worker the proposal is about.
	SubjectMayNotApprove bool
	// OneRequirementPerPrincipal excludes a principal already filling another
	// requirement in the same set, so one person cannot be two distinct roles.
	OneRequirementPerPrincipal bool
	// ForbidRequesterManagerChain excludes anyone on the requester's manager
	// chain in either direction: a manager approving their own report's request
	// and a report approving their manager's are the same conflict.
	ForbidRequesterManagerChain bool
	RuleID                      string
}

// Validate rejects a separation contract that constrains nothing. A stated
// requirement with no separation constraint is the APPROVAL-001 RED clause's
// "missing SoD", so it fails compilation rather than compiling into an
// unconstrained approval.
func (c SeparationConstraints) Validate() error {
	if c.RuleID == "" {
		return newError("Validate", "separation.rule_id", ErrInvalidRequirement,
			"separation constraints cite no rule id")
	}
	if !c.RequesterMayNotApprove && !c.SubjectMayNotApprove &&
		!c.OneRequirementPerPrincipal && !c.ForbidRequesterManagerChain {
		return newError("Validate", "separation", ErrInvalidRequirement,
			"requirement declares separation of duties but constrains nobody")
	}
	return nil
}

// InvalidatorKind is an event that invalidates decisions bound to a
// requirement.
type InvalidatorKind string

// Invalidator kinds.
const (
	// InvalidatorMaterialProposalChange is the mandatory one: a material change
	// to the proposal invalidates every decision bound to the old digest.
	InvalidatorMaterialProposalChange InvalidatorKind = "MATERIAL_PROPOSAL_CHANGE"
	// InvalidatorAuthorityRevoked covers a role, relationship or entitlement
	// withdrawn from a recorded approver.
	InvalidatorAuthorityRevoked InvalidatorKind = "AUTHORITY_REVOKED"
	// InvalidatorDelegationExpired covers a delegated decision whose delegation
	// has since expired.
	InvalidatorDelegationExpired InvalidatorKind = "DELEGATION_EXPIRED"
	// InvalidatorDeadlineExpired covers a decision past its validity expiry.
	InvalidatorDeadlineExpired InvalidatorKind = "DEADLINE_EXPIRED"
	// InvalidatorMandatoryDeny covers a mandatory deny appearing during
	// control-snapshot revalidation.
	InvalidatorMandatoryDeny InvalidatorKind = "MANDATORY_DENY"
)

var invalidatorWire = map[InvalidatorKind]bool{
	InvalidatorMaterialProposalChange: true,
	InvalidatorAuthorityRevoked:       true,
	InvalidatorDelegationExpired:      true,
	InvalidatorDeadlineExpired:        true,
	InvalidatorMandatoryDeny:          true,
}

// Valid reports whether k is a declared invalidator kind.
func (k InvalidatorKind) Valid() bool { return invalidatorWire[k] }

// Invalidator is one declared invalidation trigger plus the rule that decides
// it.
type Invalidator struct {
	Kind   InvalidatorKind
	RuleID string
}

// RequirementSource cites the rules-engine decision the requirement was derived
// from, so a stored requirement remains auditable after the threshold table is
// republished under a new version.
type RequirementSource struct {
	Tier         rules.ApprovalTier
	TableID      string
	TableVersion string
	TableDigest  string
	MatchedRowID string
	// GovernancePolicyRef is the governance configuration that turned the tier
	// into deadlines, quorum, separation and escalation.
	GovernancePolicyRef string
}

// Validate rejects a requirement whose provenance cannot be reconstructed.
func (s RequirementSource) Validate() error {
	switch s.Tier {
	case rules.ApprovalTierStandard, rules.ApprovalTierFinanceRequired, rules.ApprovalTierExecutiveRequired:
	case rules.ApprovalTierUnknownBlocked:
		return newError("Validate", "source.tier", ErrTierBlocked,
			"UNKNOWN_BLOCKED states no requirement")
	default:
		return newError("Validate", "source.tier", ErrInvalidRequirement,
			"approval tier %q is not a declared tier", string(s.Tier))
	}
	for _, f := range []struct{ field, value string }{
		{"source.table_id", s.TableID},
		{"source.table_version", s.TableVersion},
		{"source.table_digest", s.TableDigest},
		{"source.matched_row_id", s.MatchedRowID},
		{"source.governance_policy_ref", s.GovernancePolicyRef},
	} {
		if f.value == "" {
			return newError("Validate", f.field, ErrInvalidRequirement,
				"requirement provenance omits %s", f.field)
		}
	}
	return nil
}

// RequirementSpec is the compilation request for one approval requirement. It
// carries no digest field: the compiler mints ExpressionDigest, so a caller can
// never assert a candidate set it did not compile.
type RequirementSpec struct {
	RequirementID string
	Revision      uint64

	// Stage orders requirements against each other. Requirements sharing a
	// stage may be decided in parallel; a lower stage decides first.
	Stage uint32

	// Candidates is the resolution expression for the primary candidate set.
	Candidates Expression

	// AuthorityFloor is the set of roles a decision on this requirement
	// actually needs. It is what makes "fallback may not broaden authority"
	// checkable rather than aspirational: every candidate reached by fallback,
	// and every delegation reached through a holder, must carry the whole floor.
	AuthorityFloor []string

	Quorum   Quorum
	Deadline Deadline
	// DeadlineExpr, when set, is the time expression the decision deadline
	// resolves from at open time. DecideBy stays unset in the spec and
	// Expiry stays absolute; ResolveDeadline merges them per instance.
	DeadlineExpr *values.TimeExpr
	Escalation   EscalationPolicy
	Separation   SeparationConstraints
	Invalidators []Invalidator
	Source       RequirementSource
}

// ApprovalRequirement is a compiled requirement. It is produced only by
// [Compile] and is immutable in practice: the slices are cloned on the way in.
type ApprovalRequirement struct {
	RequirementID string
	Revision      uint64
	Stage         uint32

	Candidates     Expression
	AuthorityFloor []string

	Quorum       Quorum
	Deadline     Deadline
	DeadlineExpr *values.TimeExpr
	Escalation   EscalationPolicy
	Separation   SeparationConstraints
	Invalidators []Invalidator
	Source       RequirementSource

	// ExpressionDigest is minted by Compile over the candidate expression.
	ExpressionDigest string
}

// ResolveDeadline returns the absolute deadline for one workflow instance:
// the declared deadline when the requirement carries no expression, else
// the expression resolved against the evaluated anchor and pinned calendar
// dataset merged with the declared absolute expiry. The merged deadline is
// validated, so an expression resolving past expiry is refused.
func (r ApprovalRequirement) ResolveDeadline(anchor values.Instant, cals values.CalendarDataset, dataset values.DatasetVersions) (Deadline, error) {
	if r.DeadlineExpr == nil {
		return r.Deadline, nil
	}
	res, err := values.ResolveTimeExpr(*r.DeadlineExpr, anchor, cals, dataset)
	if err != nil {
		return Deadline{}, err
	}
	merged := Deadline{DecideBy: res.FireAt, Expiry: r.Deadline.Expiry}
	if err := merged.Validate(); err != nil {
		return Deadline{}, err
	}
	return merged, nil
}

// Compile validates a requirement specification and mints its expression
// digest. Every RED clause of APPROVAL-001 is a rejection here: an unnamed or
// unscoped role, a pinned person with no policy, and any requirement missing
// scope, cardinality, quorum, separation of duties, expiry or invalidators.
func Compile(spec RequirementSpec) (ApprovalRequirement, error) {
	if spec.RequirementID == "" {
		return ApprovalRequirement{}, newError("Compile", "requirement_id", ErrInvalidRequirement,
			"requirement has no id")
	}
	if spec.Revision == 0 {
		return ApprovalRequirement{}, newError("Compile", "revision", ErrInvalidRequirement,
			"requirement revisions are numbered from 1")
	}
	if spec.Stage == 0 {
		return ApprovalRequirement{}, newError("Compile", "stage", ErrInvalidRequirement,
			"requirement %q declares no ordering stage", spec.RequirementID)
	}
	if err := spec.Candidates.Validate(); err != nil {
		return ApprovalRequirement{}, err
	}
	if len(spec.AuthorityFloor) == 0 {
		return ApprovalRequirement{}, newError("Compile", "authority_floor", ErrInvalidRequirement,
			"requirement %q names no required authority", spec.RequirementID)
	}
	for _, role := range spec.AuthorityFloor {
		if role == "" {
			return ApprovalRequirement{}, newError("Compile", "authority_floor", ErrInvalidRequirement,
				"requirement %q lists an unnamed authority", spec.RequirementID)
		}
	}
	if err := spec.Quorum.Validate(); err != nil {
		return ApprovalRequirement{}, err
	}
	if spec.DeadlineExpr != nil {
		if err := spec.DeadlineExpr.Validate(); err != nil {
			return ApprovalRequirement{}, newError("Compile", "deadline_expr", ErrInvalidRequirement,
				"requirement %q carries an invalid deadline expression: %v", spec.RequirementID, err)
		}
		if spec.Deadline.DecideBy.IsSet() {
			return ApprovalRequirement{}, newError("Compile", "deadline.decide_by", ErrInvalidRequirement,
				"requirement %q names both a decision deadline and a deadline expression; exactly one is legal", spec.RequirementID)
		}
		if !spec.Deadline.Expiry.IsSet() {
			return ApprovalRequirement{}, newError("Compile", "deadline.expiry", ErrInvalidRequirement,
				"requirement %q carries a deadline expression but states no absolute decision expiry", spec.RequirementID)
		}
	} else if err := spec.Deadline.Validate(); err != nil {
		return ApprovalRequirement{}, err
	}
	if err := spec.Escalation.Validate(); err != nil {
		return ApprovalRequirement{}, err
	}
	if err := spec.Separation.Validate(); err != nil {
		return ApprovalRequirement{}, err
	}
	if err := validateInvalidators(spec.RequirementID, spec.Invalidators); err != nil {
		return ApprovalRequirement{}, err
	}
	if err := spec.Source.Validate(); err != nil {
		return ApprovalRequirement{}, err
	}

	floor := append([]string(nil), spec.AuthorityFloor...)
	sort.Strings(floor)
	inv := append([]Invalidator(nil), spec.Invalidators...)
	sort.Slice(inv, func(i, j int) bool {
		if inv[i].Kind != inv[j].Kind {
			return inv[i].Kind < inv[j].Kind
		}
		return inv[i].RuleID < inv[j].RuleID
	})

	req := ApprovalRequirement{
		RequirementID:    spec.RequirementID,
		Revision:         spec.Revision,
		Stage:            spec.Stage,
		Candidates:       spec.Candidates,
		AuthorityFloor:   floor,
		Quorum:           spec.Quorum,
		Deadline:         spec.Deadline,
		DeadlineExpr:     copyTimeExpr(spec.DeadlineExpr),
		Escalation:       spec.Escalation,
		Separation:       spec.Separation,
		Invalidators:     inv,
		Source:           spec.Source,
		ExpressionDigest: spec.Candidates.Digest(),
	}
	if req.ExpressionDigest == "" {
		return ApprovalRequirement{}, newError("Compile", "expression_digest", ErrInvalidExpression,
			"candidate expression for %q could not be canonicalized", spec.RequirementID)
	}
	return req, nil
}

// copyTimeExpr deep-copies a deadline expression so a compiled requirement
// never aliases its spec.
func copyTimeExpr(e *values.TimeExpr) *values.TimeExpr {
	if e == nil {
		return nil
	}
	dup := *e
	if e.Statute != nil {
		statute := *e.Statute
		dup.Statute = &statute
	}
	return &dup
}

func validateInvalidators(requirementID string, inv []Invalidator) error {
	if len(inv) == 0 {
		return newError("Compile", "invalidators", ErrInvalidRequirement,
			"requirement %q declares no invalidator", requirementID)
	}
	material := false
	for _, i := range inv {
		if !i.Kind.Valid() {
			return newError("Compile", "invalidators.kind", ErrInvalidRequirement,
				"invalidator %q is not declared", string(i.Kind))
		}
		if i.RuleID == "" {
			return newError("Compile", "invalidators.rule_id", ErrInvalidRequirement,
				"invalidator %s cites no rule id", string(i.Kind))
		}
		if i.Kind == InvalidatorMaterialProposalChange {
			material = true
		}
	}
	if !material {
		return newError("Compile", "invalidators", ErrInvalidRequirement,
			"requirement %q does not declare %s, which is never optional",
			requirementID, string(InvalidatorMaterialProposalChange))
	}
	return nil
}

// Canonical returns the deterministic byte encoding of the requirement.
func (r ApprovalRequirement) Canonical() []byte {
	w := canonicalbytes.New(requirementSchema, 1).
		String("requirement_id", r.RequirementID).
		Int("revision", int64(r.Revision)).
		Int("stage", int64(r.Stage)).
		String("expression_digest", r.ExpressionDigest).
		SortedStrings("authority_floor", r.AuthorityFloor).
		Int("quorum.min_approvals", int64(r.Quorum.MinApprovals)).
		Bool("quorum.distinct", r.Quorum.RequireDistinctPrincipals)
	if r.Deadline.DecideBy.IsSet() {
		w.Value("deadline.decide_by", r.Deadline.DecideBy)
	} else {
		// A deadline expression leaves DecideBy unresolved until open
		// time; the expression fields below carry the deadline instead.
		// An encoded instant is never empty, so this cannot collide.
		w.String("deadline.decide_by", "")
	}
	w = w.Value("deadline.expiry", r.Deadline.Expiry)
	// The expression encodes only when declared, so an absolute requirement
	// keeps its historical digest byte for byte.
	if expr := r.DeadlineExpr; expr != nil {
		statute := ""
		if expr.Statute != nil {
			statute = expr.Statute.String()
		}
		w.
			String("deadline_expr.anchor_path", expr.AnchorPath).
			String("deadline_expr.offset_kind", string(expr.OffsetKind)).
			Int("deadline_expr.offset_days", int64(expr.OffsetDays)).
			Int("deadline_expr.offset_seconds", expr.OffsetSeconds).
			String("deadline_expr.calendar", expr.Calendar.String()).
			String("deadline_expr.zone", expr.Zone.String()).
			String("deadline_expr.local_time", expr.WakeLocalTime.String()).
			String("deadline_expr.disambiguation", expr.Disambiguation.String()).
			String("deadline_expr.statute", statute)
	}
	w = w.
		String("escalation.on_deadline", string(r.Escalation.OnDeadline)).
		String("escalation.rule_id", r.Escalation.RuleID).
		Bool("sod.requester", r.Separation.RequesterMayNotApprove).
		Bool("sod.subject", r.Separation.SubjectMayNotApprove).
		Bool("sod.one_requirement", r.Separation.OneRequirementPerPrincipal).
		Bool("sod.manager_chain", r.Separation.ForbidRequesterManagerChain).
		String("sod.rule_id", r.Separation.RuleID).
		Count("invalidators", len(r.Invalidators))
	if r.Escalation.Fallback != nil {
		w.String("escalation.fallback_digest", r.Escalation.Fallback.Digest())
	} else {
		w.String("escalation.fallback_digest", "")
	}
	for _, i := range r.Invalidators {
		w.String("invalidator.kind", string(i.Kind)).String("invalidator.rule_id", i.RuleID)
	}
	raw, err := w.
		String("source.tier", string(r.Source.Tier)).
		String("source.table_id", r.Source.TableID).
		String("source.table_version", r.Source.TableVersion).
		String("source.table_digest", r.Source.TableDigest).
		String("source.matched_row_id", r.Source.MatchedRowID).
		String("source.governance_policy_ref", r.Source.GovernancePolicyRef).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the hex digest of the canonical requirement encoding.
func (r ApprovalRequirement) Digest() string {
	raw := r.Canonical()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// RequirementSet is the complete approval graph for one proposal, ordered by
// stage and then requirement id.
type RequirementSet struct {
	Requirements []ApprovalRequirement
	Tier         rules.ApprovalTier
	DerivedAt    values.Instant
	Source       RequirementSource
}

// Find returns the requirement with the given id.
func (s RequirementSet) Find(requirementID string) (ApprovalRequirement, bool) {
	for _, r := range s.Requirements {
		if r.RequirementID == requirementID {
			return r, true
		}
	}
	return ApprovalRequirement{}, false
}

// Stages groups the requirements by ordering stage, ascending. Requirements
// inside one group may be decided in parallel.
func (s RequirementSet) Stages() [][]ApprovalRequirement {
	var out [][]ApprovalRequirement
	var current []ApprovalRequirement
	var stage uint32
	for _, r := range s.Requirements {
		if current != nil && r.Stage != stage {
			out = append(out, current)
			current = nil
		}
		stage = r.Stage
		current = append(current, r)
	}
	if current != nil {
		out = append(out, current)
	}
	return out
}

// Digest returns the hex digest over the whole set, so a proposal can cite the
// exact approval graph it was simulated against.
func (s RequirementSet) Digest() string {
	w := canonicalbytes.New(requirementSetSch, 1).
		String("tier", string(s.Tier)).
		Value("derived_at", s.DerivedAt).
		Count("requirements", len(s.Requirements))
	for _, r := range s.Requirements {
		w.String("requirement", r.Digest())
	}
	raw, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}
