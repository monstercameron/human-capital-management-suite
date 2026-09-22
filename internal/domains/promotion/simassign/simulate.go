package simassign

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Simulation errors. Each names the contract that was broken; a business
// refusal is never one of these, it is a [Refusal] on the result.
var (
	// ErrRequestInvalid is returned for a malformed [Request].
	ErrRequestInvalid = errors.New("promotion/simassign: request is invalid")
	// ErrSnapshotUntrusted is returned when the supplied snapshot's digest
	// does not recompute from its own material inputs -- the shape a
	// hand-built snapshot carrying caller-chosen current values takes.
	ErrSnapshotUntrusted = errors.New("promotion/simassign: snapshot digest does not match its material inputs")
)

// Target is the placement the promotion proposes. It is the caller's
// intention, and it is the only part of a simulation request that is not read
// from the snapshot.
type Target struct {
	JobCode string
	Grade   string
	OrgUnit string
	PayZone string
}

// Validate reports whether the target is fully stated.
func (t Target) Validate() error {
	for _, field := range []struct{ name, value string }{
		{"job code", t.JobCode},
		{"grade", t.Grade},
		{"organizational unit", t.OrgUnit},
		{"pay zone", t.PayZone},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: target %s is required", ErrRequestInvalid, field.name)
		}
	}
	return nil
}

// Occupancy is the capacity the promotion would consume in the target
// position. POSITION-002 takes both as declared inputs rather than inferring
// them, and so does the reservation the transition depends on.
type Occupancy struct {
	// FTE is the fraction of the position's capacity the subject would take.
	FTE values.Decimal
	// Heads is the head count the subject would occupy.
	Heads int64
}

// Request is one Assignment-and-organization simulation: the immutable input
// snapshot, and the placement and manager the caller intends.
//
// There is deliberately no field on it that carries a current fact. Current
// job, grade, organizational unit, pay zone, position and manager are all read
// from the snapshot; the caller can state only what it wants to be true.
type Request struct {
	// Snapshot is the PROMO-001 input snapshot every effect derives from.
	Snapshot promosnapshot.PromotionInputSnapshot
	// Target is the proposed placement.
	Target Target
	// ProposedManager is the worker the subject would report to.
	ProposedManager values.EntityRef
	// ChainDepthBound is the maximum management-chain depth the proposal is
	// allowed to produce.
	ChainDepthBound int

	// ProposalRevisionID and ProposalDigest identify the proposal revision the
	// position reservation would be bound to.
	ProposalRevisionID string
	ProposalDigest     string

	// Occupancy is the capacity the subject would consume.
	Occupancy Occupancy
	// ReservationExpiry bounds the position hold. A reservation that never
	// expires is a leak, so POSITION-003 refuses one and so does this.
	ReservationExpiry values.Instant
	// AuthorityDigest pins the authority decision the reservation is taken
	// under.
	AuthorityDigest string

	// AuthorityDecision is the per-field source-authority decision every
	// planned write is proposed under. A write with no authority decision is
	// not a proposal, it is a guess, so the kernel refuses one and this
	// refuses to build one.
	AuthorityDecision string
}

// Validate reports whether the request is well formed and its snapshot is
// trustworthy.
//
// The digest check is the security boundary. PromotionInputSnapshot keeps its
// bound inputs unexported, so a caller cannot hand one a value; but the struct
// itself is copyable and its digest field is settable, so a caller could still
// present a hand-built snapshot with a plausible digest. Recomputing the
// digest from the snapshot's own material projection closes that: only a
// snapshot whose inputs actually hash to its digest is simulated.
func (r Request) Validate() error {
	snap := r.Snapshot
	if err := snap.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: snapshot tenant: %w", ErrRequestInvalid, err)
	}
	if len(snap.Inputs()) != len(promosnapshot.InputNames()) {
		return fmt.Errorf("%w: snapshot binds %d of %d declared inputs",
			ErrSnapshotUntrusted, len(snap.Inputs()), len(promosnapshot.InputNames()))
	}
	if got := materialDigestOf(snap); got != snap.Digest {
		return fmt.Errorf("%w: recorded %q, computed %q", ErrSnapshotUntrusted, snap.Digest, got)
	}
	if err := snap.EffectiveTime.Validate(); err != nil {
		return fmt.Errorf("%w: snapshot effective time: %w", ErrRequestInvalid, err)
	}
	if err := r.Target.Validate(); err != nil {
		return err
	}
	if err := r.ProposedManager.Validate(); err != nil {
		return fmt.Errorf("%w: proposed manager: %w", ErrRequestInvalid, err)
	}
	if r.ProposedManager.Tenant != snap.Tenant || r.ProposedManager.Kind != people.KindWorker {
		return fmt.Errorf("%w: proposed manager must be a worker in the snapshot's tenant", ErrRequestInvalid)
	}
	if r.ChainDepthBound < 1 {
		return fmt.Errorf("%w: chain depth bound must be positive", ErrRequestInvalid)
	}
	if strings.TrimSpace(r.ProposalRevisionID) == "" || strings.TrimSpace(r.ProposalDigest) == "" {
		return fmt.Errorf("%w: proposal revision id and digest are required", ErrRequestInvalid)
	}
	if err := r.Occupancy.FTE.Validate(); err != nil || r.Occupancy.FTE.Sign() <= 0 {
		return fmt.Errorf("%w: occupancy fte must be a positive exact decimal", ErrRequestInvalid)
	}
	if r.Occupancy.Heads <= 0 {
		return fmt.Errorf("%w: occupancy heads must be positive", ErrRequestInvalid)
	}
	if err := r.ReservationExpiry.Validate(); err != nil {
		return fmt.Errorf("%w: reservation expiry: %w", ErrRequestInvalid, err)
	}
	if !r.ReservationExpiry.After(snap.KnownAt.Instant()) {
		return fmt.Errorf("%w: reservation expiry is not after the snapshot's known-at horizon", ErrRequestInvalid)
	}
	if strings.TrimSpace(r.AuthorityDigest) == "" {
		return fmt.Errorf("%w: reservation authority digest is required", ErrRequestInvalid)
	}
	if strings.TrimSpace(r.AuthorityDecision) == "" {
		return fmt.Errorf("%w: source-authority decision is required", ErrRequestInvalid)
	}
	return nil
}

// materialDigestOf recomputes the snapshot's digest from its own material
// projection, using the kernel's material encoding -- the same definition
// PROMO-001 mints it with, reached through exported API only.
func materialDigestOf(s promosnapshot.PromotionInputSnapshot) string {
	sum := sha256.Sum256(s.MaterialInputs().MaterialPayload().WireBytes)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ChainVerdict is what the simulation can say about the proposed management
// chain.
type ChainVerdict string

// Chain verdicts.
const (
	// ChainWithinBound means the proposed chain resolves and is no deeper
	// than the declared bound.
	ChainWithinBound ChainVerdict = "WITHIN_BOUND"
	// ChainExceedsBound means it resolves and is deeper than the bound.
	ChainExceedsBound ChainVerdict = "EXCEEDS_BOUND"
	// ChainCycle means the proposed relationship closes a cycle.
	ChainCycle ChainVerdict = "CYCLE"
	// ChainTailNotEstablished means the proposed manager does not appear in
	// the subject's resolved chain, so this snapshot does not carry the chain
	// above them. The depth is reported as unknown rather than assumed.
	ChainTailNotEstablished ChainVerdict = "TAIL_NOT_ESTABLISHED"
	// ChainNotEvaluated means the manager-chain input was not disclosed, so
	// nothing about the chain was decided.
	ChainNotEvaluated ChainVerdict = "NOT_EVALUATED"
)

// ChainHop is one disclosed level of the subject's resolved chain.
type ChainHop struct {
	Level          int
	RelationshipID string
	Type           string
	ManagerID      string
	// Withheld marks a hop whose manager identity authorization denied. The
	// level is still reported, so the depth of the chain stays visible without
	// the identity being disclosed.
	Withheld bool
}

// ChainProjection is what the manager-relationship change does to the
// management chain.
type ChainProjection struct {
	// CurrentRelationshipID and CurrentManagerID are the subject's direct
	// relationship today, empty when there is none or it was withheld.
	CurrentRelationshipID string
	CurrentManagerID      string
	// ProposedRelationshipID is the deterministic, proposal-scoped identity
	// the new relationship is proposed under. The org domain mints the real
	// identity at execution; what a simulation can offer is an identity the
	// same inputs always produce, which is what makes the effect idempotent.
	ProposedRelationshipID string
	// ProposedManagerID is the worker the subject would report to.
	ProposedManagerID string
	// Changed reports whether the promotion actually moves the relationship.
	Changed bool
	// CurrentDepth is the number of disclosed hops the snapshot resolved.
	CurrentDepth int
	// ProposedDepth is the depth of the chain above the subject after the
	// change, or -1 when the snapshot does not establish it.
	ProposedDepth int
	// DepthBound is the bound the proposal declared.
	DepthBound int
	// Verdict is the typed finding.
	Verdict ChainVerdict
	// Hops are the disclosed hops of the current chain, in level order.
	Hops []ChainHop
}

// OccupancyVerdict is what the simulation can say about the target position's
// occupancy transition.
type OccupancyVerdict string

// Occupancy verdicts.
const (
	// OccupancyHeadAvailable means the position has a head free at the
	// effective date.
	OccupancyHeadAvailable OccupancyVerdict = "HEAD_AVAILABLE"
	// OccupancyAtCapacity means the position is full at the effective date.
	OccupancyAtCapacity OccupancyVerdict = "AT_CAPACITY"
	// OccupancyNotEvaluated means the capacity input was not disclosed.
	OccupancyNotEvaluated OccupancyVerdict = "NOT_EVALUATED"
)

// OccupancyProjection is the target position's capacity picture at the
// effective date.
type OccupancyProjection struct {
	// Capacity is the snapshot's own capacity summary token.
	Capacity string
	// VacantAfter is the date the position is known to sit vacant, when one
	// was disclosed.
	VacantAfter    values.LocalDate
	HasVacantAfter bool
	// Verdict is the typed finding.
	Verdict OccupancyVerdict
}

// Result is the deterministic, zero-effect simulation of the Assignment and
// organization changes a promotion implies.
type Result struct {
	Tenant         values.TenantId
	Subject        values.EntityRef
	TargetPosition values.EntityRef

	// EffectiveOn is the promotion's business date, and PriorAssignmentEnd is
	// the last day the prior assignment stands: the new revision is
	// effective-dated with the prior assignment's end, never overlapping it.
	EffectiveOn        values.LocalDate
	PriorAssignmentEnd values.LocalDate

	// SnapshotDigest binds the result to the exact inputs it was computed
	// from.
	SnapshotDigest string

	// Effects are the proposed effects, in declaration order.
	Effects []ProposedEffect
	// Refusals are the typed reasons an effect was not produced.
	Refusals []Refusal

	Chain     ChainProjection
	Occupancy OccupancyProjection

	// Reservation is the POSITION-003 hold the occupancy transition depends
	// on, built but never taken. ReservationPlanned is false when the
	// transition itself was refused.
	Reservation        position.PositionReservationRequest
	ReservationPlanned bool

	// Digest is "sha256:<hex>" over the canonical encoding of everything
	// above.
	Digest string
}

// Executable reports whether the simulated changes could proceed as
// simulated: true only when nothing was refused.
func (r Result) Executable() bool { return len(r.Refusals) == 0 }

// Err returns the first refusal as an error, or nil. Every returned error
// matches [ErrRefused], and matches the [Refusal] itself with errors.As.
func (r Result) Err() error {
	if len(r.Refusals) == 0 {
		return nil
	}
	return r.Refusals[0]
}

// Lookup returns the proposed effect of one kind, if there is one.
func (r Result) Lookup(kind EffectKind) (ProposedEffect, bool) {
	for _, e := range r.Effects {
		if e.Kind == kind {
			return e, true
		}
	}
	return ProposedEffect{}, false
}

// PlannedWrites returns every planned write the proposed effects imply, in
// effect order.
func (r Result) PlannedWrites() []intent.PlannedWrite {
	out := make([]intent.PlannedWrite, 0, len(r.Effects)*4)
	for _, e := range r.Effects {
		out = append(out, e.PlannedWrites()...)
	}
	return out
}

// PlannedEffects projects every proposed effect onto the kernel's declared
// effect shape.
func (r Result) PlannedEffects() []intent.PlannedEffect {
	out := make([]intent.PlannedEffect, 0, len(r.Effects))
	for _, e := range r.Effects {
		out = append(out, e.PlannedEffect())
	}
	return out
}

// OutboxEffects projects the non-local effects onto the plan's outbox shape.
// A local effect is part of the ACID commit and is never an outbox record, so
// it is deliberately absent here.
func (r Result) OutboxEffects() []intent.OutboxEffect {
	out := make([]intent.OutboxEffect, 0, len(r.Effects))
	for _, e := range r.Effects {
		if e.Local {
			continue
		}
		out = append(out, e.OutboxEffect())
	}
	return out
}

// Compensations returns the compensating action for every non-local effect,
// which is the set intent.CompilePlan validates against the outbox set.
func (r Result) Compensations() []intent.CompensationBinding {
	out := make([]intent.CompensationBinding, 0, len(r.Effects))
	for _, e := range r.Effects {
		if e.Local {
			continue
		}
		out = append(out, e.Compensation())
	}
	return out
}

// Observations returns the post-commit observation for every non-local
// effect, at the caller's declared deadline.
func (r Result) Observations(deadline values.Instant) []intent.PostCommitObservation {
	out := make([]intent.PostCommitObservation, 0, len(r.Effects))
	for _, e := range r.Effects {
		if e.Local {
			continue
		}
		out = append(out, e.Observation(deadline))
	}
	return out
}

// Participants returns the plan participants the effects touch.
func (r Result) Participants() []intent.PlanParticipant {
	out := make([]intent.PlanParticipant, 0, len(r.Effects))
	for _, e := range r.Effects {
		out = append(out, e.PlanParticipant())
	}
	return out
}

// Reads returns the baseline reads the effects assume, one per effect.
func (r Result) Reads() []intent.PlannedRead {
	out := make([]intent.PlannedRead, 0, len(r.Effects))
	for _, e := range r.Effects {
		out = append(out, intent.PlannedRead{ResourceKey: e.ResourceKey, ExpectedRevision: e.ExpectedRevision})
	}
	return out
}

// EffectSubjects returns the deduplicated material subjects the proposed
// effects are about, sorted, so a proposal revision built from this simulation
// names the same subjects the snapshot named and in the same order.
func (r Result) EffectSubjects() []intent.SubjectReference {
	seen := make(map[intent.SubjectReference]struct{}, len(r.Effects))
	out := make([]intent.SubjectReference, 0, len(r.Effects))
	for _, e := range r.Effects {
		if _, dup := seen[e.Subject]; dup {
			continue
		}
		seen[e.Subject] = struct{}{}
		out = append(out, e.Subject)
	}
	slices.SortFunc(out, func(a, b intent.SubjectReference) int {
		if a.AuthorityDomain != b.AuthorityDomain {
			return strings.Compare(a.AuthorityDomain, b.AuthorityDomain)
		}
		if a.Kind != b.Kind {
			return strings.Compare(a.Kind, b.Kind)
		}
		return strings.Compare(a.SubjectID, b.SubjectID)
	})
	return out
}

// Explain reports what the simulation decided, in effect order, carrying no
// input value beyond the canonical texts the snapshot already disclosed to
// this caller. It is safe to place in a refusal, an evidence record or a log.
func (r Result) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "promotion assignment simulation %s over snapshot %s: subject %s into position %s, effective %s (prior assignment ends %s), %d effect(s), %d refusal(s), executable=%t",
		r.Digest, r.SnapshotDigest, r.Subject, r.TargetPosition, r.EffectiveOn, r.PriorAssignmentEnd,
		len(r.Effects), len(r.Refusals), r.Executable())
	fmt.Fprintf(&b, "\n- manager chain: %s (current depth %d, proposed depth %s, bound %d, changed=%t)",
		r.Chain.Verdict, r.Chain.CurrentDepth, depthText(r.Chain.ProposedDepth), r.Chain.DepthBound, r.Chain.Changed)
	fmt.Fprintf(&b, "\n- position occupancy: %s (capacity %s)", r.Occupancy.Verdict, orNone(r.Occupancy.Capacity))
	if r.ReservationPlanned {
		fmt.Fprintf(&b, "\n- position reservation: planned for %s heads=%d fte=%s expiring %s",
			r.Reservation.Position, r.Reservation.Heads, r.Reservation.FTE, r.Reservation.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"))
	} else {
		b.WriteString("\n- position reservation: not planned")
	}
	for _, e := range r.Effects {
		fmt.Fprintf(&b, "\n- effect %s (%s): %s, local=%t, compensation %s, observation %s, derived from %s",
			e.EffectID, e.Kind, e.Reversibility, e.Local, e.CompensationRef, e.ObservationRef,
			strings.Join(e.DerivedFrom, ","))
		for _, c := range e.Changes {
			if !c.Changed {
				continue
			}
			fmt.Fprintf(&b, "\n    %s: %s -> %s", c.Field, orNone(c.Before), orNone(c.After))
		}
	}
	for _, refusal := range r.Refusals {
		fmt.Fprintf(&b, "\n- refused %s: %s", refusal.Kind, refusal.Reason)
		if refusal.InputName != "" {
			fmt.Fprintf(&b, " on %s (%s)", refusal.InputName, refusal.Availability)
		}
	}
	return b.String()
}

func depthText(depth int) string {
	if depth < 0 {
		return "UNKNOWN"
	}
	return fmt.Sprintf("%d", depth)
}

func orNone(v string) string {
	if v == "" {
		return "(none)"
	}
	return v
}

// Simulate produces the Assignment and organization changes one promotion
// implies, as a pure function of the input snapshot and the caller's stated
// intention.
//
// It reads nothing, writes nothing, reserves nothing, takes no clock and
// makes no network call. An effect whose input the snapshot could not disclose
// is refused by name rather than computed from a substitute, and the refusal
// carries no value.
func Simulate(req Request) (Result, error) {
	if err := req.Validate(); err != nil {
		return Result{}, err
	}
	snap := req.Snapshot
	s := &simulation{req: req, snap: snap}

	out := Result{
		Tenant:             snap.Tenant,
		Subject:            snap.Subject,
		TargetPosition:     snap.TargetPosition,
		EffectiveOn:        snap.EffectiveOn,
		PriorAssignmentEnd: snap.EffectiveOn.AddDays(-1),
		SnapshotDigest:     snap.Digest,
	}

	chain, chainRefusals := s.projectChain()
	out.Chain = chain
	out.Refusals = append(out.Refusals, chainRefusals...)

	occupancy, occupancyRefusals := s.projectOccupancy()
	out.Occupancy = occupancy
	out.Refusals = append(out.Refusals, occupancyRefusals...)

	assignment, assignmentRefusals, err := s.assignmentEffect(chain)
	if err != nil {
		return Result{}, err
	}
	out.Refusals = append(out.Refusals, assignmentRefusals...)
	if assignmentRefusals == nil {
		out.Effects = append(out.Effects, assignment)
	}

	if chain.Verdict != ChainCycle && chain.Verdict != ChainExceedsBound && chain.Verdict != ChainNotEvaluated {
		managerEffect, err := s.managerEffect(chain)
		if err != nil {
			return Result{}, err
		}
		out.Effects = append(out.Effects, managerEffect)
	}

	if occupancy.Verdict == OccupancyHeadAvailable {
		occupancyEffect, err := s.occupancyEffect(occupancy)
		if err != nil {
			return Result{}, err
		}
		out.Effects = append(out.Effects, occupancyEffect)
		reservation, err := s.reservationPlan()
		if err != nil {
			return Result{}, err
		}
		out.Reservation, out.ReservationPlanned = reservation, true
	}

	for _, e := range out.Effects {
		if err := e.Validate(); err != nil {
			return Result{}, err
		}
	}
	out.Digest = digestOf(out)
	return out, nil
}

// simulation carries the request through the per-effect derivations.
type simulation struct {
	req  Request
	snap promosnapshot.PromotionInputSnapshot
}

// disclosed returns one input's canonical text, or the refusal its
// non-disclosure produces for the given effect kind.
func (s *simulation) disclosed(kind EffectKind, name string) (promosnapshot.Input, string, *Refusal) {
	in, ok := s.snap.Lookup(name)
	if !ok {
		refusal := Refusal{
			Kind: kind, Reason: ReasonInputUnknown, InputName: name,
			Availability: promosnapshot.AvailabilityUnknown,
			Detail:       "the snapshot binds no such input",
		}
		return promosnapshot.Input{}, "", &refusal
	}
	if in.Availability != promosnapshot.AvailabilityDisclosed {
		refusal := RefusalForInput(kind, in)
		return in, "", &refusal
	}
	return in, in.CanonicalText, nil
}

// -------------------------------------------------------------------------
// Manager chain
// -------------------------------------------------------------------------

// projectChain reads the ORG-002 chain the snapshot bound and decides what the
// proposed manager relationship does to it.
func (s *simulation) projectChain() (ChainProjection, []Refusal) {
	out := ChainProjection{
		ProposedManagerID: s.req.ProposedManager.Id,
		DepthBound:        s.req.ChainDepthBound,
		ProposedDepth:     -1,
		Verdict:           ChainNotEvaluated,
	}
	out.ProposedRelationshipID = s.proposedRelationshipID()

	in, text, refusal := s.disclosed(EffectManagerRelationship, promosnapshot.InputManagerChain)
	if refusal != nil {
		_ = in
		return out, []Refusal{*refusal}
	}

	out.Hops = parseChain(text)
	out.CurrentDepth = len(out.Hops)
	if len(out.Hops) > 0 && !out.Hops[0].Withheld {
		out.CurrentRelationshipID = out.Hops[0].RelationshipID
		out.CurrentManagerID = out.Hops[0].ManagerID
	}
	out.Changed = out.CurrentManagerID != out.ProposedManagerID

	// A worker who reports to themselves is a cycle of length one, and it is
	// the one cycle this snapshot can decide on its own: the chain above the
	// proposed manager is not among the eight inputs.
	if s.req.ProposedManager.Id == s.snap.Subject.Id {
		out.Verdict = ChainCycle
		return out, []Refusal{{
			Kind: EffectManagerRelationship, Reason: ReasonManagerCycle,
			InputName: promosnapshot.InputManagerChain, Availability: promosnapshot.AvailabilityDisclosed,
			Detail: "the proposed manager is the subject",
		}}
	}

	level, found := chainLevelOf(out.Hops, s.req.ProposedManager.Id)
	if !found {
		// The proposed manager is not in the subject's resolved chain, so this
		// snapshot does not carry the chain above them. Stating the depth
		// would be a guess; PROMO-004 resolves the proposed manager's own
		// chain and decides it there.
		out.Verdict = ChainTailNotEstablished
		return out, nil
	}
	out.ProposedDepth = len(out.Hops) - level
	if out.ProposedDepth > s.req.ChainDepthBound {
		out.Verdict = ChainExceedsBound
		return out, []Refusal{{
			Kind: EffectManagerRelationship, Reason: ReasonChainDepthExceeded,
			InputName: promosnapshot.InputManagerChain, Availability: promosnapshot.AvailabilityDisclosed,
			Detail: fmt.Sprintf("the proposed chain is %d deep against a bound of %d", out.ProposedDepth, s.req.ChainDepthBound),
		}}
	}
	out.Verdict = ChainWithinBound
	return out, nil
}

// proposedRelationshipID is the deterministic, proposal-scoped identity the
// new manager relationship is proposed under.
func (s *simulation) proposedRelationshipID() string {
	digest, err := canonicalbytes.New("hcmnext.domains.promotion.simassign.ProposedRelationship", schemaVersion).
		String("tenant", string(s.snap.Tenant)).
		Value("subject", s.snap.Subject).
		Value("manager", s.req.ProposedManager).
		Value("effective_on", s.snap.EffectiveOn).
		Digest()
	if err != nil {
		return ""
	}
	return "rel_proposed_" + strings.TrimPrefix(digest, "sha256:")[:16]
}

// parseChain reads the manager-chain input's canonical text: one hop per
// level, "<level>:<relationship>:<type>:<manager>", or "<level>:WITHHELD" for
// a hop whose manager identity was denied.
func parseChain(text string) []ChainHop {
	if text == "" {
		return nil
	}
	parts := strings.Split(text, ";")
	out := make([]ChainHop, 0, len(parts))
	for i, part := range parts {
		fields := strings.Split(part, ":")
		hop := ChainHop{Level: i}
		if len(fields) >= 1 {
			if level, err := parseLevel(fields[0]); err == nil {
				hop.Level = level
			}
		}
		switch {
		case len(fields) >= 4:
			hop.RelationshipID, hop.Type, hop.ManagerID = fields[1], fields[2], fields[3]
		default:
			hop.Withheld = true
		}
		out = append(out, hop)
	}
	slices.SortStableFunc(out, func(a, b ChainHop) int { return a.Level - b.Level })
	return out
}

// parseLevel reads a non-negative level token.
func parseLevel(token string) (int, error) {
	level := 0
	if token == "" {
		return 0, fmt.Errorf("empty level")
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("non-numeric level %q", token)
		}
		level = level*10 + int(r-'0')
	}
	return level, nil
}

// chainLevelOf returns the level at which a manager appears in the chain.
func chainLevelOf(hops []ChainHop, managerID string) (int, bool) {
	for _, hop := range hops {
		if !hop.Withheld && hop.ManagerID == managerID {
			return hop.Level, true
		}
	}
	return 0, false
}

// -------------------------------------------------------------------------
// Position occupancy
// -------------------------------------------------------------------------

// projectOccupancy decides whether the target position can take the subject at
// the effective date, from the two POSITION-002 inputs.
//
// A position that still has a head is the only case that lets the transition
// through. When it does not, the conditional vacancy input is exactly the fact
// the promotion turns on, and the refusal distinguishes "nobody knows when it
// frees up" from "it frees up, but too late" -- two different conversations
// with the requester, and only the second has an obvious remedy.
func (s *simulation) projectOccupancy() (OccupancyProjection, []Refusal) {
	out := OccupancyProjection{Verdict: OccupancyNotEvaluated}

	_, capacity, refusal := s.disclosed(EffectPositionOccupancy, promosnapshot.InputTargetPositionCapacity)
	if refusal != nil {
		return out, []Refusal{*refusal}
	}
	out.Capacity = capacity
	if capacity == promosnapshot.CapacityAvailable {
		out.Verdict = OccupancyHeadAvailable
		return out, nil
	}
	out.Verdict = OccupancyAtCapacity

	vacancyIn, vacancyText, vacancyRefusal := s.disclosed(EffectPositionOccupancy, promosnapshot.InputTargetPositionVacancy)
	if vacancyRefusal != nil {
		_ = vacancyIn
		return out, []Refusal{{
			Kind: EffectPositionOccupancy, Reason: ReasonPositionAtCapacity,
			InputName: promosnapshot.InputTargetPositionVacancy, Availability: vacancyRefusal.Availability,
			Detail: "the position is at capacity and no date is known on which it frees up",
		}}
	}
	vacantAfter, err := values.ParseLocalDate(vacancyText)
	if err != nil {
		return out, []Refusal{{
			Kind: EffectPositionOccupancy, Reason: ReasonPositionAtCapacity,
			InputName: promosnapshot.InputTargetPositionVacancy, Availability: promosnapshot.AvailabilityDisclosed,
			Detail: "the position is at capacity and the vacancy input carries no business date",
		}}
	}
	out.VacantAfter, out.HasVacantAfter = vacantAfter, true
	return out, []Refusal{{
		Kind: EffectPositionOccupancy, Reason: ReasonVacancyAfterEffectiveDate,
		InputName: promosnapshot.InputTargetPositionVacancy, Availability: promosnapshot.AvailabilityDisclosed,
		Detail: "the position frees up on " + vacantAfter.String() + ", after the promotion's effective date",
	}}
}

// -------------------------------------------------------------------------
// Effects
// -------------------------------------------------------------------------

// assignmentEffect builds the new assignment revision from the current
// placement input and the caller's target.
func (s *simulation) assignmentEffect(chain ChainProjection) (ProposedEffect, []Refusal, error) {
	in, text, refusal := s.disclosed(EffectAssignmentRevision, promosnapshot.InputCurrentPlacement)
	if refusal != nil {
		return ProposedEffect{}, []Refusal{*refusal}, nil
	}
	current := parseFields(text)

	chainIn, chainOK := s.snap.Lookup(promosnapshot.InputManagerChain)
	changes := []FieldChange{
		change(people.FieldJobCode, current, s.req.Target.JobCode, in.Name),
		change(people.FieldGrade, current, s.req.Target.Grade, in.Name),
		change(people.FieldOrgUnit, current, s.req.Target.OrgUnit, in.Name),
		change(people.FieldPositionID, current, s.snap.TargetPosition.Id, in.Name),
		change(people.FieldPayZone, current, s.req.Target.PayZone, in.Name),
		{
			Field:       string(people.FieldManagerRelation),
			Before:      chain.CurrentRelationshipID,
			After:       chain.ProposedRelationshipID,
			Changed:     chain.CurrentRelationshipID != chain.ProposedRelationshipID,
			SourceInput: promosnapshot.InputManagerChain,
		},
	}

	sources := []string{promosnapshot.InputCurrentPlacement, promosnapshot.InputSubjectWorkerFacts}
	if chainOK {
		sources = append(sources, promosnapshot.InputManagerChain)
	}
	local := LocalAuthority(in.Entry.Authority)
	if chainOK {
		local = local && LocalAuthority(chainIn.Entry.Authority)
	}
	effect, err := s.effect(EffectAssignmentRevision, in, effectShape{
		participant:  "people.assignment",
		destination:  "people.assignment/" + s.snap.Subject.Id,
		storageClass: "LOCAL_EVENT_STREAM",
		local:        local,
		changes:      changes,
		sources:      sources,
	})
	return effect, nil, err
}

// managerEffect builds the manager-relationship change.
func (s *simulation) managerEffect(chain ChainProjection) (ProposedEffect, error) {
	in, _ := s.snap.Lookup(promosnapshot.InputManagerChain)
	changes := []FieldChange{
		{
			Field:       "org.manager_relationship.relationship_id",
			Before:      chain.CurrentRelationshipID,
			After:       chain.ProposedRelationshipID,
			Changed:     chain.CurrentRelationshipID != chain.ProposedRelationshipID,
			SourceInput: promosnapshot.InputManagerChain,
		},
		{
			Field:       "org.manager_relationship.manager_id",
			Before:      chain.CurrentManagerID,
			After:       chain.ProposedManagerID,
			Changed:     chain.Changed,
			SourceInput: promosnapshot.InputManagerChain,
		},
		{
			Field:       "org.manager_relationship.type",
			Before:      currentHopType(chain),
			After:       string(org.RelationshipDirectManager),
			Changed:     currentHopType(chain) != string(org.RelationshipDirectManager),
			SourceInput: promosnapshot.InputManagerChain,
		},
	}
	return s.effect(EffectManagerRelationship, in, effectShape{
		participant:  "org.manager_relationship",
		destination:  "org.manager_relationship/" + s.snap.Subject.Id,
		storageClass: "LOCAL_EVENT_STREAM",
		local:        LocalAuthority(in.Entry.Authority),
		changes:      changes,
		sources:      []string{promosnapshot.InputManagerChain, promosnapshot.InputCurrentPlacement},
	})
}

// currentHopType returns the current direct relationship's type, or empty.
func currentHopType(chain ChainProjection) string {
	for _, hop := range chain.Hops {
		if !hop.Withheld && hop.RelationshipID == chain.CurrentRelationshipID {
			return hop.Type
		}
	}
	return ""
}

// occupancyEffect builds the target position's occupancy transition.
func (s *simulation) occupancyEffect(occupancy OccupancyProjection) (ProposedEffect, error) {
	in, _ := s.snap.Lookup(promosnapshot.InputTargetPositionCapacity)
	changes := []FieldChange{
		{
			Field:       "position.occupancy.capacity_state",
			Before:      occupancy.Capacity,
			After:       promosnapshot.CapacityExhausted,
			Changed:     occupancy.Capacity != promosnapshot.CapacityExhausted,
			SourceInput: promosnapshot.InputTargetPositionCapacity,
		},
		{
			Field:       "position.occupancy.occupant",
			Before:      "",
			After:       s.snap.Subject.Id,
			Changed:     true,
			SourceInput: promosnapshot.InputTargetPositionCapacity,
		},
		{
			Field:       "position.occupancy.reservation",
			Before:      "",
			After:       s.req.ProposalRevisionID,
			Changed:     true,
			SourceInput: promosnapshot.InputTargetPositionCapacity,
		},
	}
	sources := []string{promosnapshot.InputTargetPositionCapacity}
	if occupancy.HasVacantAfter {
		sources = append(sources, promosnapshot.InputTargetPositionVacancy)
	}
	return s.effect(EffectPositionOccupancy, in, effectShape{
		participant:  "position.occupancy",
		destination:  "position.occupancy/" + s.snap.TargetPosition.Id,
		storageClass: "LOCAL_EVENT_STREAM",
		local:        LocalAuthority(in.Entry.Authority),
		changes:      changes,
		sources:      sources,
	})
}

// effectShape is the per-simulation remainder of an effect: everything the
// undo contract does not declare. The reversal (reversibility class,
// compensation and observation) comes from the single declaration
// ([ReversalFor]), never from a per-effect literal, so the proposal and the
// cancellation verdict cannot disagree about how an effect is undone.
type effectShape struct {
	participant  string
	destination  string
	storageClass string
	local        bool
	changes      []FieldChange
	sources      []string
}

// effect builds one proposed effect, binding it to the input that supplied its
// baseline and minting its deterministic identity and idempotency key.
func (s *simulation) effect(kind EffectKind, baseline promosnapshot.Input, shape effectShape) (ProposedEffect, error) {
	rev, ok := ReversalFor(kind)
	if !ok {
		return ProposedEffect{}, fmt.Errorf("%w: effect kind %q declares no reversal", ErrEffectIncomplete, kind)
	}
	idempotency, err := canonicalbytes.New("hcmnext.domains.promotion.simassign.Idempotency", schemaVersion).
		String("snapshot_digest", s.snap.Digest).
		String("kind", string(kind)).
		String("proposal_revision_id", s.req.ProposalRevisionID).
		Value("effective_on", s.snap.EffectiveOn).
		Digest()
	if err != nil {
		return ProposedEffect{}, fmt.Errorf("%w: idempotency key: %w", ErrEffectIncomplete, err)
	}
	return ProposedEffect{
		EffectID:             string(kind) + "@" + s.snap.EffectiveOn.String() + "#" + s.req.ProposalRevisionID,
		Kind:                 kind,
		Participant:          shape.participant,
		DestinationRef:       shape.destination,
		StorageClass:         shape.storageClass,
		Local:                shape.local,
		Reversibility:        rev.Reversibility,
		CompensationRef:      rev.CompensationRef,
		CompensationStrategy: rev.CompensationStrategy,
		ObservationRef:       rev.ObservationRef,
		IdempotencyKey:       idempotency,
		Subject:              baseline.Subject,
		ResourceKey:          baseline.ResourceKey,
		Effective:            s.snap.EffectiveTime,
		ExpectedRevision:     baseline.Entry.Watermark,
		AuthorityDecision:    s.req.AuthorityDecision,
		Changes:              shape.changes,
		DerivedFrom:          DerivedFrom(shape.sources...),
	}, nil
}

// reservationPlan builds the POSITION-003 hold the occupancy transition
// depends on. It builds the request and validates it; it never takes the hold,
// because a simulation that reserved a head would be an effect.
func (s *simulation) reservationPlan() (position.PositionReservationRequest, error) {
	idempotency, err := canonicalbytes.New("hcmnext.domains.promotion.simassign.ReservationIdempotency", schemaVersion).
		String("snapshot_digest", s.snap.Digest).
		String("proposal_revision_id", s.req.ProposalRevisionID).
		Value("position", s.snap.TargetPosition).
		Value("effective_on", s.snap.EffectiveOn).
		Digest()
	if err != nil {
		return position.PositionReservationRequest{}, fmt.Errorf("%w: reservation idempotency key: %w", ErrRequestInvalid, err)
	}
	req := position.PositionReservationRequest{
		Tenant:             s.snap.Tenant,
		Position:           s.snap.TargetPosition,
		AsOf:               position.AsOf{EffectiveOn: s.snap.EffectiveOn, KnownAt: s.snap.KnownAt},
		ProposalRevisionID: s.req.ProposalRevisionID,
		ProposalDigest:     s.req.ProposalDigest,
		EffectiveDate:      s.snap.EffectiveOn,
		Effective:          s.snap.EffectiveTime,
		FTE:                s.req.Occupancy.FTE,
		Heads:              s.req.Occupancy.Heads,
		DesiredJobCode:     s.req.Target.JobCode,
		DesiredOrgUnit:     s.req.Target.OrgUnit,
		AuthorityDigest:    s.req.AuthorityDigest,
		IdempotencyKey:     idempotency,
		ExpiresAt:          s.req.ReservationExpiry.Time(),
	}
	if err := req.Validate(s.snap.KnownAt.Instant().Time()); err != nil {
		return position.PositionReservationRequest{}, fmt.Errorf("%w: position reservation: %w", ErrRequestInvalid, err)
	}
	return req, nil
}

// change builds one placement field's before/after from the current placement
// input.
func change(field people.FieldID, current map[string]string, after, sourceInput string) FieldChange {
	before := current[string(field)]
	return FieldChange{
		Field:       string(field),
		Before:      before,
		After:       after,
		Changed:     before != after,
		SourceInput: sourceInput,
	}
}

// parseFields reads a "k=v;k=v" canonical text into a map. A malformed pair is
// skipped rather than guessed at, which is what turns a missing field into a
// stated empty "before" instead of a fabricated one.
func parseFields(text string) map[string]string {
	out := make(map[string]string, 8)
	if text == "" {
		return out
	}
	for _, part := range strings.Split(text, ";") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[key] = value
	}
	return out
}

// digestOf returns "sha256:<hex>" over the canonical encoding of the whole
// result. The digest field itself is excluded: a digest never hashes itself.
func digestOf(r Result) string {
	w := canonicalbytes.New("hcmnext.domains.promotion.simassign.Result", schemaVersion).
		String("tenant", string(r.Tenant)).
		Value("subject", r.Subject).
		Value("target_position", r.TargetPosition).
		Value("effective_on", r.EffectiveOn).
		Value("prior_assignment_end", r.PriorAssignmentEnd).
		String("snapshot_digest", r.SnapshotDigest).
		String("chain.verdict", string(r.Chain.Verdict)).
		String("chain.current_relationship", r.Chain.CurrentRelationshipID).
		String("chain.current_manager", r.Chain.CurrentManagerID).
		String("chain.proposed_relationship", r.Chain.ProposedRelationshipID).
		String("chain.proposed_manager", r.Chain.ProposedManagerID).
		Bool("chain.changed", r.Chain.Changed).
		Int("chain.current_depth", int64(r.Chain.CurrentDepth)).
		Int("chain.proposed_depth", int64(r.Chain.ProposedDepth)).
		Int("chain.depth_bound", int64(r.Chain.DepthBound)).
		String("occupancy.capacity", r.Occupancy.Capacity).
		String("occupancy.verdict", string(r.Occupancy.Verdict)).
		Bool("occupancy.has_vacant_after", r.Occupancy.HasVacantAfter).
		Bool("reservation.planned", r.ReservationPlanned)
	if r.Occupancy.HasVacantAfter {
		w.Value("occupancy.vacant_after", r.Occupancy.VacantAfter)
	}
	if r.ReservationPlanned {
		w.Field("reservation", r.Reservation.Canonical())
	}
	w.Count("effects", len(r.Effects))
	for _, e := range r.Effects {
		w.Field("effect", e.Canonical())
	}
	w.Count("refusals", len(r.Refusals))
	for _, refusal := range r.Refusals {
		w.Field("refusal", refusal.Canonical())
	}
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}
