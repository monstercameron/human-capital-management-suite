package simcomp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/budget"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simassign"
	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Simulation errors. Each names the contract that was broken; a business
// refusal is never one of these, it is a [simassign.Refusal] on the result.
var (
	// ErrRequestInvalid is returned for a malformed [Request].
	ErrRequestInvalid = errors.New("promotion/simcomp: request is invalid")
	// ErrSnapshotUntrusted is returned when the supplied snapshot's digest does
	// not recompute from its own material inputs -- the shape a hand-built
	// snapshot carrying caller-chosen current values takes.
	ErrSnapshotUntrusted = errors.New("promotion/simcomp: snapshot digest does not match its material inputs")
	// ErrArithmetic wraps an exact-decimal failure. It is separate from an
	// invalid request because a rounding mode that cannot represent a result is
	// a declaration problem, not a typo.
	ErrArithmetic = errors.New("promotion/simcomp: exact-decimal arithmetic failed")
)

// Request is one compensation-and-budget simulation: the immutable input
// snapshot, and the declared money conventions and pay period the arithmetic is
// performed under.
//
// There is deliberately no amount field on it. Current and desired annualized
// pay are read from the snapshot's two COMP-003 band-position inputs; a caller
// can state how the arithmetic is done, never what the numbers are.
type Request struct {
	// Snapshot is the PROMO-001 input snapshot every effect derives from.
	Snapshot promosnapshot.PromotionInputSnapshot

	// ProposalRevisionID and ProposalDigest identify the proposal revision the
	// budget reservation would be bound to.
	ProposalRevisionID string
	ProposalDigest     string

	// MoneyScale and MoneyRounding are the declared money conventions every
	// amount is expressed and rounded at.
	MoneyScale    int32
	MoneyRounding values.RoundingMode
	// RateScale is the (higher) scale the intermediate daily rates carry, so a
	// daily rate is not rounded to money before it is multiplied by a day
	// count.
	RateScale int32
	// DaysPerYear is the declared divisor that turns an annualized amount into
	// a daily rate. It is required rather than conventional: a package that
	// reached for 365 would silently move every prorated number the day
	// somebody adopted a 360-day convention.
	DaysPerYear values.Decimal
	// PayPeriod is the pay period the effective date falls in.
	PayPeriod values.PayPeriod

	// BudgetID addresses the pool the reservation would be taken against.
	BudgetID string
	// AuthorityDigest pins the budget authority decision the reservation is
	// taken under.
	AuthorityDigest string
	// ReservationExpiry bounds the budget hold.
	ReservationExpiry values.Instant

	// AuthorityDecision is the per-field source-authority decision every
	// planned write is proposed under.
	AuthorityDecision string
}

// Validate reports whether the request is well formed and its snapshot is
// trustworthy. The digest check is the security boundary: only a snapshot whose
// inputs actually hash to its recorded digest is simulated, so a hand-built
// snapshot carrying caller-chosen current pay is refused rather than trusted.
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
	if strings.TrimSpace(r.ProposalRevisionID) == "" || strings.TrimSpace(r.ProposalDigest) == "" {
		return fmt.Errorf("%w: proposal revision id and digest are required", ErrRequestInvalid)
	}
	if r.MoneyScale < 0 || r.RateScale < r.MoneyScale {
		return fmt.Errorf("%w: rate scale must be at least the money scale", ErrRequestInvalid)
	}
	if !r.MoneyRounding.Valid() {
		return fmt.Errorf("%w: money rounding mode is unspecified", ErrRequestInvalid)
	}
	if err := r.DaysPerYear.Validate(); err != nil || r.DaysPerYear.Sign() <= 0 {
		return fmt.Errorf("%w: days per year must be a positive exact decimal", ErrRequestInvalid)
	}
	if err := r.PayPeriod.Validate(); err != nil {
		return fmt.Errorf("%w: pay period: %w", ErrRequestInvalid, err)
	}
	if strings.TrimSpace(r.BudgetID) == "" || strings.TrimSpace(r.AuthorityDigest) == "" {
		return fmt.Errorf("%w: budget id and authority digest are required", ErrRequestInvalid)
	}
	if err := r.ReservationExpiry.Validate(); err != nil {
		return fmt.Errorf("%w: reservation expiry: %w", ErrRequestInvalid, err)
	}
	if !r.ReservationExpiry.After(snap.KnownAt.Instant()) {
		return fmt.Errorf("%w: reservation expiry is not after the snapshot's known-at horizon", ErrRequestInvalid)
	}
	if strings.TrimSpace(r.AuthorityDecision) == "" {
		return fmt.Errorf("%w: source-authority decision is required", ErrRequestInvalid)
	}
	return nil
}

// materialDigestOf recomputes the snapshot's digest from its own material
// projection, using the kernel's material encoding.
func materialDigestOf(s promosnapshot.PromotionInputSnapshot) string {
	sum := sha256.Sum256(s.MaterialInputs().MaterialPayload().WireBytes)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Result is the deterministic, zero-effect simulation of the compensation and
// budget effects a promotion implies.
type Result struct {
	Tenant      values.TenantId
	Subject     values.EntityRef
	EffectiveOn values.LocalDate

	// SnapshotDigest binds the result to the exact inputs it was computed from.
	SnapshotDigest string

	// Effects are the proposed effects, in declaration order.
	Effects []simassign.ProposedEffect
	// Refusals are the typed reasons an effect was not produced.
	Refusals []simassign.Refusal

	BasePay   BasePayChange
	Band      BandFinding
	Proration Proration
	Budget    BudgetPlan

	// RequiredApprovals are the approval requirements the findings trigger,
	// in declaration order.
	RequiredApprovals []string

	// Digest is "sha256:<hex>" over the canonical encoding of everything above.
	Digest string
}

// Executable reports whether the simulated changes could proceed as simulated.
func (r Result) Executable() bool { return len(r.Refusals) == 0 }

// Err returns the first refusal as an error, or nil. Every returned error
// matches [simassign.ErrRefused].
func (r Result) Err() error {
	if len(r.Refusals) == 0 {
		return nil
	}
	return r.Refusals[0]
}

// Lookup returns the proposed effect of one kind, if there is one.
func (r Result) Lookup(kind simassign.EffectKind) (simassign.ProposedEffect, bool) {
	for _, e := range r.Effects {
		if e.Kind == kind {
			return e, true
		}
	}
	return simassign.ProposedEffect{}, false
}

// PlannedWrites returns every planned write the proposed effects imply.
func (r Result) PlannedWrites() []intent.PlannedWrite {
	out := make([]intent.PlannedWrite, 0, len(r.Effects)*3)
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
// The budget reservation is here whenever the pool it was weighed against is an
// external observation rather than locally authoritative state, which is
// exactly the distinction the reference workflow draws when it says finance
// participates in the local commit "only if locally authoritative".
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

// Compensations returns the compensating action for every non-local effect.
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

// Observations returns the post-commit observation for every non-local effect.
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

// Explain reports what the simulation decided, carrying no value the snapshot
// did not already disclose to this caller.
func (r Result) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "promotion compensation simulation %s over snapshot %s: subject %s, effective %s, %d effect(s), %d refusal(s), executable=%t",
		r.Digest, r.SnapshotDigest, r.Subject, r.EffectiveOn, len(r.Effects), len(r.Refusals), r.Executable())
	if r.BasePay.Evaluated {
		fmt.Fprintf(&b, "\n- base pay: %s -> %s (%s %s)",
			r.BasePay.CurrentAnnualized, r.BasePay.DesiredAnnualized, r.BasePay.Direction, r.BasePay.Delta)
	} else {
		b.WriteString("\n- base pay: not evaluated")
	}
	if r.Band.Evaluated {
		fmt.Fprintf(&b, "\n- pay band %s@%s: current %s, desired %s (boundary %s)",
			r.Band.BandID, r.Band.BandVersion, r.Band.CurrentClass, r.Band.DesiredClass, r.Band.DesiredBoundary)
		if r.Band.ApprovalRequirementID != "" {
			fmt.Fprintf(&b, ", requires %s", r.Band.ApprovalRequirementID)
		}
	} else {
		b.WriteString("\n- pay band: not evaluated")
	}
	if r.Proration.Evaluated {
		fmt.Fprintf(&b, "\n- proration over %s [%s,%s): %d day(s) at %s + %d day(s) at %s = %s (was %s, delta %s)",
			r.Proration.PeriodID, r.Proration.PeriodStart, r.Proration.PeriodEnd,
			r.Proration.DaysAtPriorRate, r.Proration.PriorDailyRate,
			r.Proration.DaysAtNewRate, r.Proration.NewDailyRate,
			r.Proration.PeriodAmount, r.Proration.UnproratedPeriodAmount, r.Proration.PeriodDelta)
	} else {
		b.WriteString("\n- proration: not evaluated")
	}
	fmt.Fprintf(&b, "\n- %s", r.Budget.Explain())
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

func orNone(v string) string {
	if v == "" {
		return "(none)"
	}
	return v
}

// Simulate produces the compensation and budget effects one promotion implies,
// as a pure function of the input snapshot and the declared conventions.
//
// It reads nothing, writes nothing, reserves nothing, takes no clock and makes
// no network call. A withheld compensation input refuses the effect that needed
// it by name rather than producing a number, and an insufficient pool refuses
// the reservation rather than planning one that would fail at execution.
func Simulate(req Request) (Result, error) {
	if err := req.Validate(); err != nil {
		return Result{}, err
	}
	snap := req.Snapshot
	s := &simulation{req: req, snap: snap}

	out := Result{
		Tenant:         snap.Tenant,
		Subject:        snap.Subject,
		EffectiveOn:    snap.EffectiveOn,
		SnapshotDigest: snap.Digest,
	}

	change, band, refusals, err := s.basePay()
	if err != nil {
		return Result{}, err
	}
	out.BasePay, out.Band = change, band
	out.Refusals = append(out.Refusals, refusals...)
	if band.ApprovalRequirementID != "" {
		out.RequiredApprovals = append(out.RequiredApprovals, band.ApprovalRequirementID)
	}

	if change.Evaluated {
		effect, err := s.compensationEffect(change, band)
		if err != nil {
			return Result{}, err
		}
		out.Effects = append(out.Effects, effect)

		proration, prorationRefusals, err := s.prorate(change)
		if err != nil {
			return Result{}, err
		}
		out.Proration = proration
		out.Refusals = append(out.Refusals, prorationRefusals...)
	}

	plan, budgetRefusals, err := s.budgetPlan(change)
	if err != nil {
		return Result{}, err
	}
	out.Budget = plan
	out.Refusals = append(out.Refusals, budgetRefusals...)
	if plan.Planned {
		effect, err := s.budgetEffect(plan)
		if err != nil {
			return Result{}, err
		}
		out.Effects = append(out.Effects, effect)
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
func (s *simulation) disclosed(kind simassign.EffectKind, name string) (promosnapshot.Input, string, *simassign.Refusal) {
	in, ok := s.snap.Lookup(name)
	if !ok {
		refusal := simassign.Refusal{
			Kind: kind, Reason: simassign.ReasonInputUnknown, InputName: name,
			Availability: promosnapshot.AvailabilityUnknown,
			Detail:       "the snapshot binds no such input",
		}
		return promosnapshot.Input{}, "", &refusal
	}
	if in.Availability != promosnapshot.AvailabilityDisclosed {
		refusal := simassign.RefusalForInput(kind, in)
		return in, "", &refusal
	}
	return in, in.CanonicalText, nil
}

// -------------------------------------------------------------------------
// Base pay and pay band
// -------------------------------------------------------------------------

// basePay reads both COMP-003 band-position inputs and derives the base-pay
// change and the band finding from them.
//
// Both amounts come from the snapshot's own annualized figures rather than
// being re-annualized here: the band placement was decided on those figures,
// and computing a second set would let the money and the band verdict disagree.
func (s *simulation) basePay() (BasePayChange, BandFinding, []simassign.Refusal, error) {
	var refusals []simassign.Refusal

	desiredIn, desiredText, desiredRefusal := s.disclosed(EffectCompensationRevision, promosnapshot.InputPayBandPositionDesired)
	if desiredRefusal != nil {
		return BasePayChange{Direction: DirectionUnchanged}, BandFinding{}, []simassign.Refusal{*desiredRefusal}, nil
	}
	_ = desiredIn
	desiredFields := parseFields(desiredText)

	band := BandFinding{Evaluated: true}
	band.BandID, band.BandVersion = splitBand(desiredFields["band"])
	band.DesiredClass = desiredFields["class"]
	band.DesiredBoundary = desiredFields["boundary"]
	band.ApprovalRequirementID = approvalFor(band.DesiredClass)

	currentIn, currentText, currentRefusal := s.disclosed(EffectCompensationRevision, promosnapshot.InputPayBandPositionCurrent)
	if currentRefusal != nil {
		// The desired position is known and the current one is not. Reporting
		// the desired band placement is honest -- it is a fact about the
		// caller's own intention against the catalog -- but a base-pay *change*
		// needs both ends, so it is refused rather than computed against zero.
		return BasePayChange{Direction: DirectionUnchanged}, band, []simassign.Refusal{*currentRefusal}, nil
	}
	_ = currentIn
	currentFields := parseFields(currentText)
	band.CurrentClass = currentFields["class"]

	current, err := s.parseMoney(currentFields["annualized"])
	if err != nil {
		refusals = append(refusals, simassign.Refusal{
			Kind: EffectCompensationRevision, Reason: simassign.ReasonInputUnparsable,
			InputName: promosnapshot.InputPayBandPositionCurrent, Availability: promosnapshot.AvailabilityDisclosed,
			Detail: "the band-position input carries no annualized amount",
		})
		return BasePayChange{Direction: DirectionUnchanged}, band, refusals, nil
	}
	desired, err := s.parseMoney(desiredFields["annualized"])
	if err != nil {
		refusals = append(refusals, simassign.Refusal{
			Kind: EffectCompensationRevision, Reason: simassign.ReasonInputUnparsable,
			InputName: promosnapshot.InputPayBandPositionDesired, Availability: promosnapshot.AvailabilityDisclosed,
			Detail: "the band-position input carries no annualized amount",
		})
		return BasePayChange{Direction: DirectionUnchanged}, band, refusals, nil
	}
	if current.Currency() != desired.Currency() {
		refusals = append(refusals, simassign.Refusal{
			Kind: EffectCompensationRevision, Reason: ReasonCurrencyMismatch,
			InputName: promosnapshot.InputPayBandPositionDesired, Availability: promosnapshot.AvailabilityDisclosed,
			Detail: "current and desired pay are denominated in different currencies",
		})
		return BasePayChange{Direction: DirectionUnchanged}, band, refusals, nil
	}

	delta, err := desired.Sub(current)
	if err != nil {
		return BasePayChange{}, band, nil, fmt.Errorf("%w: base pay delta: %w", ErrArithmetic, err)
	}
	change := BasePayChange{
		Evaluated:         true,
		CurrentAnnualized: current,
		DesiredAnnualized: desired,
		Delta:             delta,
		Direction:         directionOf(delta),
	}
	return change, band, refusals, nil
}

// directionOf names which way the change moves.
func directionOf(delta values.Money) string {
	switch delta.Amount().Sign() {
	case 1:
		return DirectionIncrease
	case -1:
		return DirectionDecrease
	default:
		return DirectionUnchanged
	}
}

// splitBand reads "<id>@<version>".
func splitBand(text string) (id, version string) {
	id, version, _ = strings.Cut(text, "@")
	return id, version
}

// parseMoney reads "<amount> <CURRENCY>" at the declared money scale and
// rounding mode.
func (s *simulation) parseMoney(text string) (values.Money, error) {
	amount, currency, ok := strings.Cut(text, " ")
	if !ok {
		return values.Money{}, fmt.Errorf("%w: %q is not an amount and a currency", ErrRequestInvalid, text)
	}
	return values.NewMoney(amount, currency, s.req.MoneyScale, s.req.MoneyRounding)
}

// -------------------------------------------------------------------------
// Proration
// -------------------------------------------------------------------------

// prorate splits the declared pay period at the effective date and prices both
// sides at their own daily rate.
func (s *simulation) prorate(change BasePayChange) (Proration, []simassign.Refusal, error) {
	interval := s.req.PayPeriod.Interval()
	start, startOK := interval.StartDate()
	end, endOK := interval.EndDate()
	if !startOK || !endOK {
		return Proration{}, nil, fmt.Errorf("%w: pay period is not a closed local-date interval", ErrRequestInvalid)
	}
	effective := s.snap.EffectiveOn
	if effective.Compare(start) < 0 || effective.Compare(end) >= 0 {
		return Proration{PeriodID: s.req.PayPeriod.ID()}, []simassign.Refusal{{
			Kind: EffectCompensationRevision, Reason: ReasonEffectiveDateOutsidePayPeriod,
			Detail: "the effective date does not fall inside the declared pay period",
		}}, nil
	}

	out := Proration{
		Evaluated:       true,
		PeriodID:        s.req.PayPeriod.ID(),
		PeriodStart:     start,
		PeriodEnd:       end,
		DaysInPeriod:    daysBetween(start, end),
		DaysAtPriorRate: daysBetween(start, effective),
		DaysAtNewRate:   daysBetween(effective, end),
		DaysPerYear:     s.req.DaysPerYear,
	}

	priorDaily, err := s.dailyRate(change.CurrentAnnualized)
	if err != nil {
		return Proration{}, nil, err
	}
	newDaily, err := s.dailyRate(change.DesiredAnnualized)
	if err != nil {
		return Proration{}, nil, err
	}
	out.PriorDailyRate, out.NewDailyRate = priorDaily, newDaily

	if out.PriorPortion, err = s.scaleBy(priorDaily, out.DaysAtPriorRate); err != nil {
		return Proration{}, nil, err
	}
	if out.NewPortion, err = s.scaleBy(newDaily, out.DaysAtNewRate); err != nil {
		return Proration{}, nil, err
	}
	if out.PeriodAmount, err = out.PriorPortion.Add(out.NewPortion); err != nil {
		return Proration{}, nil, fmt.Errorf("%w: period amount: %w", ErrArithmetic, err)
	}
	if out.UnproratedPeriodAmount, err = s.scaleBy(priorDaily, out.DaysInPeriod); err != nil {
		return Proration{}, nil, err
	}
	if out.PeriodDelta, err = out.PeriodAmount.Sub(out.UnproratedPeriodAmount); err != nil {
		return Proration{}, nil, fmt.Errorf("%w: period delta: %w", ErrArithmetic, err)
	}
	return out, nil, nil
}

// dailyRate divides an annualized amount by the declared days-per-year at the
// declared rate scale.
func (s *simulation) dailyRate(annual values.Money) (values.Money, error) {
	amount, err := annual.Amount().Div(s.req.DaysPerYear, s.req.RateScale, s.req.MoneyRounding)
	if err != nil {
		return values.Money{}, fmt.Errorf("%w: daily rate: %w", ErrArithmetic, err)
	}
	rate, err := values.NewMoneyFromDecimal(amount, annual.Currency())
	if err != nil {
		return values.Money{}, fmt.Errorf("%w: daily rate: %w", ErrArithmetic, err)
	}
	return rate, nil
}

// scaleBy multiplies a daily rate by a day count and quantizes to money scale.
func (s *simulation) scaleBy(rate values.Money, days int) (values.Money, error) {
	factor, err := values.NewDecimal(strconv.Itoa(days), 0, s.req.MoneyRounding)
	if err != nil {
		return values.Money{}, fmt.Errorf("%w: day count %d: %w", ErrArithmetic, days, err)
	}
	amount, err := rate.Amount().Mul(factor, s.req.MoneyScale, s.req.MoneyRounding)
	if err != nil {
		return values.Money{}, fmt.Errorf("%w: prorated portion: %w", ErrArithmetic, err)
	}
	out, err := values.NewMoneyFromDecimal(amount, rate.Currency())
	if err != nil {
		return values.Money{}, fmt.Errorf("%w: prorated portion: %w", ErrArithmetic, err)
	}
	return out, nil
}

// daysBetween returns the number of whole days in the half-open range
// [from, to). It converts through UTC midnights, which is exact for local
// dates and involves no clock.
func daysBetween(from, to values.LocalDate) int {
	a := time.Date(int(from.Year()), from.Month(), int(from.Day()), 0, 0, 0, 0, time.UTC)
	b := time.Date(int(to.Year()), to.Month(), int(to.Day()), 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a).Hours() / 24)
}

// -------------------------------------------------------------------------
// Budget
// -------------------------------------------------------------------------

// budgetPlan weighs the annualized cost delta against the observed pool and
// builds the reservation request that would be issued.
func (s *simulation) budgetPlan(change BasePayChange) (BudgetPlan, []simassign.Refusal, error) {
	in, text, refusal := s.disclosed(EffectBudgetReservation, promosnapshot.InputBudgetAvailability)
	if refusal != nil {
		return BudgetPlan{}, []simassign.Refusal{*refusal}, nil
	}
	fields := parseFields(text)
	plan := BudgetPlan{
		Evaluated:       true,
		Scope:           fields["scope"],
		Period:          fields["period"],
		BudgetType:      fields["type"],
		Unit:            fields["unit"],
		BaselineVersion: fields["baseline"],
	}
	if plan.Unit != string(budget.UnitMoney) {
		plan.Reason = "the observed pool is not measured in money"
		return plan, []simassign.Refusal{{
			Kind: EffectBudgetReservation, Reason: ReasonBudgetUnitMismatch,
			InputName: in.Name, Availability: promosnapshot.AvailabilityDisclosed,
			Detail: plan.Reason,
		}}, nil
	}
	available, err := s.parseMoney(fields["available"] + " " + fields["currency"])
	if err != nil {
		plan.Reason = "the pool observation carries no available amount"
		return plan, []simassign.Refusal{{
			Kind: EffectBudgetReservation, Reason: simassign.ReasonInputUnparsable,
			InputName: in.Name, Availability: promosnapshot.AvailabilityDisclosed,
			Detail: plan.Reason,
		}}, nil
	}
	plan.Available = available

	if !change.Evaluated {
		plan.Reason = "the base-pay change could not be established"
		return plan, nil, nil
	}
	if available.Currency() != change.Delta.Currency() {
		plan.Reason = "the pool and the pay change are in different currencies"
		return plan, []simassign.Refusal{{
			Kind: EffectBudgetReservation, Reason: ReasonCurrencyMismatch,
			InputName: in.Name, Availability: promosnapshot.AvailabilityDisclosed,
			Detail: plan.Reason,
		}}, nil
	}
	plan.Amount = change.Delta
	if change.Delta.Amount().Sign() <= 0 {
		// A promotion that does not raise annual cost consumes no pool. It is
		// not a refusal: there is simply nothing to reserve.
		plan.Reason = "the change does not increase annualized cost"
		plan.Sufficient = true
		plan.Remaining = available
		return plan, nil, nil
	}

	cmp, err := available.Cmp(change.Delta)
	if err != nil {
		return BudgetPlan{}, nil, fmt.Errorf("%w: budget comparison: %w", ErrArithmetic, err)
	}
	if cmp < 0 {
		plan.Reason = "the observed pool does not cover the reservation"
		return plan, []simassign.Refusal{{
			Kind: EffectBudgetReservation, Reason: ReasonInsufficientBudget,
			InputName: in.Name, Availability: promosnapshot.AvailabilityDisclosed,
			Detail: fmt.Sprintf("reservation %s exceeds available %s", change.Delta, available),
		}}, nil
	}
	plan.Sufficient = true
	if plan.Remaining, err = available.Sub(change.Delta); err != nil {
		return BudgetPlan{}, nil, fmt.Errorf("%w: remaining budget: %w", ErrArithmetic, err)
	}

	idempotency, err := canonicalbytes.New("hcmnext.domains.promotion.simcomp.ReservationIdempotency", schemaVersion).
		String("snapshot_digest", s.snap.Digest).
		String("proposal_revision_id", s.req.ProposalRevisionID).
		String("budget_id", s.req.BudgetID).
		Value("effective_on", s.snap.EffectiveOn).
		Digest()
	if err != nil {
		return BudgetPlan{}, nil, fmt.Errorf("%w: reservation idempotency key: %w", ErrArithmetic, err)
	}
	plan.Request = budget.CompensationReservationRequest{
		TenantID:        string(s.snap.Tenant),
		BudgetID:        s.req.BudgetID,
		ProposalDigest:  s.req.ProposalDigest,
		Amount:          change.Delta.Amount(),
		Currency:        change.Delta.Currency(),
		AuthorityDigest: s.req.AuthorityDigest,
		IdempotencyKey:  idempotency,
		ExpiresAt:       s.req.ReservationExpiry.Time(),
	}
	if err := plan.Request.Validate(s.snap.KnownAt.Instant().Time()); err != nil {
		return BudgetPlan{}, nil, fmt.Errorf("%w: budget reservation: %w", ErrRequestInvalid, err)
	}
	plan.Planned = true
	return plan, nil, nil
}

// -------------------------------------------------------------------------
// Effects
// -------------------------------------------------------------------------

// compensationEffect builds the proposed compensation revision.
func (s *simulation) compensationEffect(change BasePayChange, band BandFinding) (simassign.ProposedEffect, error) {
	in, _ := s.snap.Lookup(promosnapshot.InputPayBandPositionCurrent)
	changes := []simassign.FieldChange{
		{
			Field:       "rewards.compensation.annualized_base_pay",
			Before:      change.CurrentAnnualized.String(),
			After:       change.DesiredAnnualized.String(),
			Changed:     change.Direction != DirectionUnchanged,
			SourceInput: promosnapshot.InputPayBandPositionCurrent,
		},
		{
			Field:       "rewards.compensation.pay_band",
			Before:      band.BandID + "@" + band.BandVersion,
			After:       band.BandID + "@" + band.BandVersion,
			Changed:     false,
			SourceInput: promosnapshot.InputPayBandPositionDesired,
		},
		{
			Field:       "rewards.compensation.band_position_class",
			Before:      band.CurrentClass,
			After:       band.DesiredClass,
			Changed:     band.CurrentClass != band.DesiredClass,
			SourceInput: promosnapshot.InputPayBandPositionDesired,
		},
		{
			Field:       "rewards.compensation.effective_from",
			Before:      "",
			After:       s.snap.EffectiveOn.String(),
			Changed:     true,
			SourceInput: promosnapshot.InputPayBandPositionCurrent,
		},
	}
	return s.effect(EffectCompensationRevision, in, effectShape{
		participant:  "rewards.compensation",
		destination:  "rewards.compensation/" + s.snap.Subject.Id,
		storageClass: "LOCAL_EVENT_STREAM",
		local:        simassign.LocalAuthority(in.Entry.Authority),
		changes:      changes,
		sources: []string{
			promosnapshot.InputPayBandPositionCurrent,
			promosnapshot.InputPayBandPositionDesired,
		},
	})
}

// budgetEffect builds the proposed pool reservation.
func (s *simulation) budgetEffect(plan BudgetPlan) (simassign.ProposedEffect, error) {
	in, _ := s.snap.Lookup(promosnapshot.InputBudgetAvailability)
	changes := []simassign.FieldChange{
		{
			Field:       "budget.compensation_pool.available",
			Before:      plan.Available.String(),
			After:       plan.Remaining.String(),
			Changed:     true,
			SourceInput: promosnapshot.InputBudgetAvailability,
		},
		{
			Field:       "budget.compensation_pool.reservation",
			Before:      "",
			After:       s.req.ProposalRevisionID,
			Changed:     true,
			SourceInput: promosnapshot.InputBudgetAvailability,
		},
	}
	return s.effect(EffectBudgetReservation, in, effectShape{
		participant:  "budget.compensation_pool",
		destination:  "budget.compensation_pool/" + plan.Scope + "@" + plan.Period,
		storageClass: "EXTERNAL_AUTHORITY",
		local:        simassign.LocalAuthority(in.Entry.Authority),
		changes:      changes,
		sources: []string{
			promosnapshot.InputBudgetAvailability,
			promosnapshot.InputPayBandPositionCurrent,
			promosnapshot.InputPayBandPositionDesired,
		},
	})
}

// effectShape is the per-simulation remainder of an effect: everything the
// undo contract does not declare. The reversal (reversibility class,
// compensation and observation) comes from simassign's single declaration
// ([simassign.ReversalFor]), never from a per-effect literal, so the proposal
// and the cancellation verdict cannot disagree about how an effect is undone.
type effectShape struct {
	participant  string
	destination  string
	storageClass string
	local        bool
	changes      []simassign.FieldChange
	sources      []string
}

// effect builds one proposed effect, binding it to the input that supplied its
// baseline and minting its deterministic identity and idempotency key.
func (s *simulation) effect(
	kind simassign.EffectKind, baseline promosnapshot.Input, shape effectShape,
) (simassign.ProposedEffect, error) {
	rev, ok := simassign.ReversalFor(kind)
	if !ok {
		return simassign.ProposedEffect{}, fmt.Errorf("%w: effect kind %q declares no reversal", simassign.ErrEffectIncomplete, kind)
	}
	idempotency, err := canonicalbytes.New("hcmnext.domains.promotion.simcomp.Idempotency", schemaVersion).
		String("snapshot_digest", s.snap.Digest).
		String("kind", string(kind)).
		String("proposal_revision_id", s.req.ProposalRevisionID).
		Value("effective_on", s.snap.EffectiveOn).
		Digest()
	if err != nil {
		return simassign.ProposedEffect{}, fmt.Errorf("%w: idempotency key: %w", simassign.ErrEffectIncomplete, err)
	}
	return simassign.ProposedEffect{
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
		DerivedFrom:          simassign.DerivedFrom(shape.sources...),
	}, nil
}

// parseFields reads a "k=v;k=v" canonical text into a map. A malformed pair is
// skipped rather than guessed at.
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
	w := canonicalbytes.New("hcmnext.domains.promotion.simcomp.Result", schemaVersion).
		String("tenant", string(r.Tenant)).
		Value("subject", r.Subject).
		Value("effective_on", r.EffectiveOn).
		String("snapshot_digest", r.SnapshotDigest).
		Field("base_pay", r.BasePay.canonical()).
		Field("band", r.Band.canonical()).
		Field("proration", r.Proration.canonical()).
		Field("budget", r.Budget.canonical()).
		SortedStrings("required_approvals", r.RequiredApprovals).
		Count("effects", len(r.Effects))
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
