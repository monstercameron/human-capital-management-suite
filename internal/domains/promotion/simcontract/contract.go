package simcontract

import (
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simassign"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/decision"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
)

// SimulationResult is the immutable WorkflowSimulationContract PROMO-004
// assembles: everything a Promotion or Manager Change simulation decided,
// bound to the exact intent and snapshot it was computed from, digested once,
// and never executable by anything in this package.
type SimulationResult struct {
	Intent   IntentRef
	Snapshot SnapshotRef

	// ProposalCandidateDigest identifies the exact candidate proposal (the
	// union of what PROMO-002's simassign and PROMO-003's simcomp -- or an
	// equivalent single-domain computation for a Manager Change -- decided)
	// this contract describes. It is a caller-supplied binding, the same way
	// SnapshotRef is: this package does not recompute it, it cites it.
	ProposalCandidateDigest string

	Reads            []intent.PlannedRead
	Writes           []intent.PlannedWrite
	Streams          []intent.PlanParticipant
	Conflicts        []conflict.Candidate
	Approvals        []decision.ApprovalRequirement
	Authority        []evidence.SourceAuthority
	LegalObligations []decision.Obligation
	SideEffects      []SideEffect
	Repair           []intent.CompensationBinding

	Cost         Cost
	Completion   Completion
	Revalidation Revalidation

	// Findings are the business-rule findings the candidate's preflight
	// produced, in the same vocabulary
	// [github.com/monstercameron/human-capital-management-suite/internal/domains/promotion.PreflightResult]
	// uses -- canonicalized by [Assemble]: findings sharing a
	// [github.com/monstercameron/human-capital-management-suite/internal/domains/promotion.FindingIdentity]
	// are deduplicated per
	// [github.com/monstercameron/human-capital-management-suite/internal/domains/promotion.DeduplicateFindings]
	// before this contract is digested, so this slice is always the
	// deterministic, deduplicated set (PROMOUX-009).
	Findings []promotion.Finding
	// Refusals are the typed effect refusals PROMO-002/003 produced, if any.
	Refusals []simassign.Refusal

	// Status is always derived by [Assemble] from Findings and Refusals; there
	// is no way to assert an EXECUTABLE_AS_SIMULATED status over a blocked
	// finding or a refusal.
	Status ResultStatus

	// Digest is "sha256:<hex>" over the canonical encoding of every field
	// above. It is minted once, by [Assemble].
	Digest string
}

// AssembleInput is everything [Assemble] consumes. It carries no Status and
// no Digest field: both are derived, never asserted, which is what makes a
// caller-forged status or digest unrepresentable rather than merely rejected.
type AssembleInput struct {
	Intent                  IntentRef
	Snapshot                SnapshotRef
	ProposalCandidateDigest string

	Reads            []intent.PlannedRead
	Writes           []intent.PlannedWrite
	Streams          []intent.PlanParticipant
	Conflicts        []conflict.Candidate
	Approvals        []decision.ApprovalRequirement
	Authority        []evidence.SourceAuthority
	LegalObligations []decision.Obligation
	SideEffects      []SideEffect
	Repair           []intent.CompensationBinding

	Cost         Cost
	Completion   Completion
	Revalidation Revalidation

	Findings []promotion.Finding
	Refusals []simassign.Refusal
}

// clone returns a copy that preserves nilness: nil stays nil (the section was
// never stated), and a non-nil, possibly empty, slice stays non-nil (the
// section was stated as empty). The distinction is what [SimulationResult.Validate]
// keys its section-presence check on.
func clone[T any](s []T) []T {
	if s == nil {
		return nil
	}
	return append([]T{}, s...)
}

// deriveStatus computes the contract's overall verdict from its findings and
// refusals. It is never caller-supplied.
func deriveStatus(findings []promotion.Finding, refusals []simassign.Refusal) ResultStatus {
	if len(refusals) > 0 {
		return ResultBlocked
	}
	for _, f := range findings {
		switch f.Severity {
		case promotion.SeverityBlocking, promotion.SeverityNeedsData, promotion.SeverityDenied:
			return ResultBlocked
		}
	}
	return ResultExecutableAsSimulated
}

// Assemble builds and validates a [SimulationResult]. It refuses -- with a
// typed [Refusal] naming the exact section -- a contract missing any of the
// twelve mandatory sections, and it never touches a domain revision store, an
// external operation port, the outbox, a MessageIntent sink or a WorkItem
// store: every field here is a value already computed elsewhere.
func Assemble(in AssembleInput) (SimulationResult, error) {
	if err := in.Intent.Validate(); err != nil {
		return SimulationResult{}, err
	}
	if err := in.Snapshot.Validate(); err != nil {
		return SimulationResult{}, err
	}
	if strings.TrimSpace(in.ProposalCandidateDigest) == "" {
		return SimulationResult{}, fmt.Errorf("%w: proposal candidate digest is required", ErrInvalidInput)
	}

	r := SimulationResult{
		Intent:                  in.Intent,
		Snapshot:                in.Snapshot,
		ProposalCandidateDigest: in.ProposalCandidateDigest,
		Reads:                   clone(in.Reads),
		Writes:                  clone(in.Writes),
		Streams:                 clone(in.Streams),
		Conflicts:               clone(in.Conflicts),
		Approvals:               clone(in.Approvals),
		Authority:               clone(in.Authority),
		LegalObligations:        clone(in.LegalObligations),
		SideEffects:             clone(in.SideEffects),
		Repair:                  clone(in.Repair),
		Cost:                    in.Cost,
		Completion:              in.Completion,
		Revalidation: Revalidation{
			Rules:                 clone(in.Revalidation.Rules),
			ControlSnapshotDigest: in.Revalidation.ControlSnapshotDigest,
		},
		// Findings are canonicalized here, before Status is derived and
		// before the digest is computed, which is deliberate: this is the
		// simulation-contract boundary PROMOUX-009 canonicalizes at, and it
		// sits strictly before both. Deduplicating after digesting (or
		// worse, after [Persist.Store]) would let a caller mint a digest
		// over undeduplicated findings -- exactly the "digest that changes
		// depending on how many times a rule fired" bug this todo exists to
		// close -- and [Persist] never re-derives anything from what it is
		// handed, so it must already be canonical when it arrives.
		Findings: promotion.DeduplicateFindings(clone(in.Findings)),
		Refusals: clone(in.Refusals),
	}
	r.Status = deriveStatus(r.Findings, r.Refusals)

	if err := r.Validate(); err != nil {
		return SimulationResult{}, err
	}
	r.Digest = r.computeDigest()
	return r, nil
}

// Validate reports whether the contract states all twelve mandatory sections
// and is internally coherent. It never mutates the receiver.
func (r SimulationResult) Validate() error {
	if err := r.Intent.Validate(); err != nil {
		return err
	}
	if err := r.Snapshot.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.ProposalCandidateDigest) == "" {
		return fmt.Errorf("%w: proposal candidate digest is required", ErrInvalidInput)
	}

	if r.Reads == nil {
		return refuse(SectionReads, "no planned reads were stated")
	}
	for i, read := range r.Reads {
		if err := read.ResourceKey.Validate(); err != nil {
			return refuse(SectionReads, "read %d: resource key: %v", i, err)
		}
		if !read.ExpectedRevision.IsSpecified() {
			return refuse(SectionReads, "read %d of %s pins no expected revision", i, read.ResourceKey)
		}
	}

	if r.Writes == nil {
		return refuse(SectionWrites, "no planned writes were stated")
	}
	for i, w := range r.Writes {
		if w.FieldPath == "" {
			return refuse(SectionWrites, "write %d names no field", i)
		}
		if w.SourceAuthorityDecision == "" {
			return refuse(SectionWrites, "write %d on %q records no source-authority decision", i, w.FieldPath)
		}
		if !w.ExpectedRevision.IsSpecified() {
			return refuse(SectionWrites, "write %d on %q pins no baseline revision", i, w.FieldPath)
		}
	}

	if r.Streams == nil {
		return refuse(SectionStreams, "no plan participants were stated")
	}
	for i, p := range r.Streams {
		if p.ParticipantID == "" || p.StreamID == "" || p.StorageClass == "" {
			return refuse(SectionStreams, "stream %d declares no participant, stream or storage class", i)
		}
	}

	if r.Conflicts == nil {
		return refuse(SectionConflicts, "no conflict candidates were stated")
	}
	for i, c := range r.Conflicts {
		if err := c.Validate(); err != nil {
			return refuse(SectionConflicts, "candidate %d: %v", i, err)
		}
	}

	if r.Approvals == nil {
		return refuse(SectionApprovals, "no approval requirements were stated")
	}
	for i, a := range r.Approvals {
		if a.ID == "" || a.Version == "" {
			return refuse(SectionApprovals, "approval %d has no id or version", i)
		}
		switch a.Satisfaction {
		case decision.ApprovalSatisfied, decision.ApprovalPending, decision.ApprovalRejected, decision.ApprovalUnknown:
		default:
			return refuse(SectionApprovals, "approval %d %q declares no satisfaction state", i, a.ID)
		}
	}

	if r.Authority == nil {
		return refuse(SectionAuthority, "no source authorities were stated")
	}
	for i, a := range r.Authority {
		if err := a.Validate(); err != nil {
			return refuse(SectionAuthority, "authority %d: %v", i, err)
		}
	}
	if len(r.Writes) > 0 && len(r.Authority) == 0 {
		return refuse(SectionAuthority, "writes are planned but no authority is bound")
	}

	if r.LegalObligations == nil {
		return refuse(SectionLegalObligations, "no legal obligations were stated")
	}
	for i, o := range r.LegalObligations {
		if o.ID == "" || o.Version == "" {
			return refuse(SectionLegalObligations, "obligation %d has no id or version", i)
		}
	}

	if r.SideEffects == nil {
		return refuse(SectionSideEffects, "no side effects were stated")
	}
	for i, e := range r.SideEffects {
		if err := e.Validate(); err != nil {
			return refuse(SectionSideEffects, "effect %d: %v", i, err)
		}
	}

	if r.Repair == nil {
		return refuse(SectionRepair, "no repair bindings were stated")
	}
	for i, rp := range r.Repair {
		if rp.EffectID == "" || rp.Strategy == "" {
			return refuse(SectionRepair, "repair binding %d has no effect id or strategy", i)
		}
	}
	for _, e := range r.SideEffects {
		if !slices.ContainsFunc(r.Repair, func(b intent.CompensationBinding) bool { return b.EffectID == e.EffectID }) {
			return refuse(SectionRepair, "effect %q declares no repair binding", e.EffectID)
		}
	}

	if r.Cost.State == CostStateUnspecified {
		return refuse(SectionCost, "cost was never evaluated")
	}
	if err := r.Cost.Validate(); err != nil {
		return refuse(SectionCost, "%v", err)
	}

	if r.Completion.State == CompletionStateUnspecified {
		return refuse(SectionCompletion, "completion was never evaluated")
	}
	if err := r.Completion.Validate(); err != nil {
		return refuse(SectionCompletion, "%v", err)
	}

	if r.Revalidation.Rules == nil {
		return refuse(SectionRevalidation, "no revalidation rules were stated")
	}
	if err := r.Revalidation.Validate(); err != nil {
		return refuse(SectionRevalidation, "%v", err)
	}

	if !r.Status.Valid() {
		return fmt.Errorf("%w: result status is unstated", ErrInvalidInput)
	}
	return nil
}

// canonicalBody encodes everything the digest covers.
func (r SimulationResult) canonicalBody() ([]byte, error) {
	w := canonicalbytes.New("hcmnext.domains.promotion.simcontract.SimulationResult", schemaVersion).
		Value("intent", r.Intent).
		Value("snapshot", r.Snapshot).
		String("proposal_candidate_digest", r.ProposalCandidateDigest)

	w.Count("reads", len(r.Reads))
	for _, read := range r.Reads {
		w.Value("read.resource_key", read.ResourceKey).Value("read.expected_revision", read.ExpectedRevision)
	}
	w.Count("writes", len(r.Writes))
	for _, wr := range r.Writes {
		w.String("write.subject.kind", wr.Subject.Kind).
			String("write.subject.id", wr.Subject.SubjectID).
			String("write.subject.authority", wr.Subject.AuthorityDomain).
			Value("write.resource_key", wr.ResourceKey).
			String("write.field_path", wr.FieldPath).
			String("write.current", wr.CurrentCanonicalText).
			String("write.proposed", wr.ProposedCanonicalText).
			String("write.authority_decision", wr.SourceAuthorityDecision).
			Value("write.expected_revision", wr.ExpectedRevision)
	}
	w.Count("streams", len(r.Streams))
	for _, p := range r.Streams {
		w.String("stream.participant_id", p.ParticipantID).
			String("stream.stream_id", p.StreamID).
			String("stream.storage_class", p.StorageClass).
			Bool("stream.local", p.Local)
	}
	w.Count("conflicts", len(r.Conflicts))
	for _, c := range r.Conflicts {
		w.String("conflict.proposal_revision_id", c.ProposalRevisionID).
			Value("conflict.footprint", c.Footprint).
			String("conflict.state", string(c.State)).
			Int("conflict.priority", int64(c.Priority)).
			Value("conflict.recorded_at", c.RecordedAt).
			String("conflict.supersedes", c.SupersedesRevisionID).
			String("conflict.depends_on", c.DependsOnRevisionID)
	}
	w.Count("approvals", len(r.Approvals))
	for _, a := range r.Approvals {
		w.String("approval.id", a.ID).
			String("approval.version", a.Version).
			String("approval.satisfaction", string(a.Satisfaction)).
			String("approval.rule_id", a.RuleID).
			String("approval.rule_version", a.RuleVersion)
	}
	w.Count("authority", len(r.Authority))
	for _, a := range r.Authority {
		w.Value("authority.entry", a)
	}
	w.Count("legal_obligations", len(r.LegalObligations))
	for _, o := range r.LegalObligations {
		w.String("obligation.id", o.ID).
			String("obligation.version", o.Version).
			String("obligation.scope", o.Scope).
			String("obligation.owner", o.Owner)
	}
	w.Count("side_effects", len(r.SideEffects))
	for _, e := range r.SideEffects {
		w.Value("side_effect", e)
	}
	w.Count("repair", len(r.Repair))
	for _, rp := range r.Repair {
		w.String("repair.effect_id", rp.EffectID).
			String("repair.strategy", rp.Strategy).
			String("repair.repair_plan_id", rp.RepairPlanID)
	}
	w.Value("cost", r.Cost)
	w.Value("completion", r.Completion)
	w.Value("revalidation", r.Revalidation)

	w.Count("findings", len(r.Findings))
	for _, f := range r.Findings {
		w.String("finding.code", f.Code).
			String("finding.severity", f.Severity.String()).
			String("finding.field", f.Field).
			String("finding.message", f.Message).
			String("finding.owner", f.Owner).
			SortedStrings("finding.corroborated_by", f.CorroboratedBy)
	}
	w.Count("refusals", len(r.Refusals))
	for _, ref := range r.Refusals {
		w.Value("refusal", ref)
	}
	w.String("status", string(r.Status))
	return w.Bytes()
}

// Canonical returns the canonical byte encoding of the whole contract, or nil
// when it is invalid. It excludes Digest: a digest never hashes itself.
func (r SimulationResult) Canonical() []byte {
	raw, err := r.canonicalBody()
	if err != nil {
		return nil
	}
	return raw
}

// computeDigest returns "sha256:<hex>" over the canonical body.
func (r SimulationResult) computeDigest() string {
	body, err := r.canonicalBody()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(body)
}

// VerifyDigest recomputes the contract's digest from its current content and
// reports whether it still matches the digest [Assemble] minted.
func (r SimulationResult) VerifyDigest() error {
	if got := r.computeDigest(); got != r.Digest {
		return fmt.Errorf("%w: contract records digest %q but its content hashes to %q", ErrInvalidInput, r.Digest, got)
	}
	return nil
}

// Executable reports whether the contract's candidate could proceed as
// simulated. It never grants execution rights; it only reports whether
// anything found a reason it could not.
func (r SimulationResult) Executable() bool { return r.Status == ResultExecutableAsSimulated }
