// Gap explanation: DEMAND-004 traces every coverage shortage and
// excess back to its signals, provenance, qualification dimension and
// supply attributions.
//
// Attribution never credits one supply twice: assignments are consumed
// in deterministic order across the sorted signals, so overlapping
// demand apportions shared supply instead of double-counting it.
// Workers are never cited — attributions name supply references only —
// so population and count privacy holds by construction. Malformed
// requests reject with DEMAND_004_REJECTED naming field, state and
// version, persisting nothing: explanation is pure.
package demand

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// GapRejectedCode is the stable machine-readable refusal code.
const GapRejectedCode = "DEMAND_004_REJECTED"

// ErrInvalidExplanation marks a malformed gap or explanation.
var ErrInvalidExplanation = errors.New("demand: invalid gap explanation")

// GapRejectedError names the offending field and version.
type GapRejectedError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *GapRejectedError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// AsGapRejected unwraps a DEMAND_004_REJECTED refusal.
func AsGapRejected(err error) (*GapRejectedError, bool) {
	if err == nil {
		return nil, false
	}
	var rejected *GapRejectedError
	if errors.As(err, &rejected) && rejected.Code == GapRejectedCode {
		return rejected, true
	}
	return nil, false
}

func gapReject(field, state string) *GapRejectedError {
	return &GapRejectedError{Code: GapRejectedCode, Field: field, State: state, Version: schemaVersion}
}

// Attribution credits part of one supply assignment to one signal.
// The worker behind the assignment is deliberately absent.
type Attribution struct {
	SupplyRef  string
	Attributed values.Quantity
}

// Validate implements validation.
func (a Attribution) Validate() error {
	if strings.TrimSpace(a.SupplyRef) == "" {
		return fmt.Errorf("%w: attribution supply ref is required", ErrInvalidExplanation)
	}
	if err := a.Attributed.Validate(); err != nil {
		return fmt.Errorf("%w: attributed quantity: %v", ErrInvalidExplanation, err)
	}
	return nil
}

// Canonical returns the canonical bytes of a valid attribution.
func (a Attribution) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.demand.Attribution", schemaVersion).
		String("supply_ref", a.SupplyRef).Value("attributed", a.Attributed).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// SignalGap explains one non-covered cell. Attributed plus Shortfall
// always equals Required; Surplus carries supply beyond need.
type SignalGap struct {
	SignalID     string
	Outcome      CellOutcome
	Required     values.Quantity
	Attributed   values.Quantity
	Shortfall    values.Quantity
	Surplus      values.Quantity
	Reason       string
	SourceRef    string
	Scenario     string
	Version      string
	Target       string
	Attributions []Attribution
}

// Validate implements validation.
func (g SignalGap) Validate() error {
	if strings.TrimSpace(g.SignalID) == "" {
		return fmt.Errorf("%w: gap signal id is required", ErrInvalidExplanation)
	}
	switch g.Outcome {
	case CellShort, CellExcess, CellUnknown:
	default:
		return fmt.Errorf("%w: gap outcome %q explains nothing", ErrInvalidExplanation, g.Outcome)
	}
	for name, quantity := range map[string]values.Quantity{
		"required": g.Required, "attributed": g.Attributed, "shortfall": g.Shortfall, "surplus": g.Surplus,
	} {
		if err := quantity.Validate(); err != nil {
			return fmt.Errorf("%w: gap %s: %v", ErrInvalidExplanation, name, err)
		}
	}
	if g.Outcome == CellUnknown {
		// Unknown signals explain nothing: crediting a shortfall
		// would fabricate a shortage from an uncredible signal.
		if g.Attributed.Value().Sign() != 0 || g.Shortfall.Value().Sign() != 0 || len(g.Attributions) != 0 {
			return fmt.Errorf("%w: unknown gap attributes quantities", ErrInvalidExplanation)
		}
	} else if recomposed, err := g.Attributed.Add(g.Shortfall); err != nil || recomposed.Value().Cmp(g.Required.Value()) != 0 {
		return fmt.Errorf("%w: attributed plus shortfall does not equal required", ErrInvalidExplanation)
	}
	if g.Surplus.Value().Sign() < 0 {
		return fmt.Errorf("%w: negative surplus", ErrInvalidExplanation)
	}
	sum, err := zeroLike(g.Required)
	if err != nil {
		return err
	}
	for _, attribution := range g.Attributions {
		if err := attribution.Validate(); err != nil {
			return err
		}
		sum, err = sum.Add(attribution.Attributed)
		if err != nil || sum.Value().Cmp(g.Attributed.Value()) > 0 {
			return fmt.Errorf("%w: attributions exceed attributed total", ErrInvalidExplanation)
		}
	}
	if sum.Value().Cmp(g.Attributed.Value()) != 0 {
		return fmt.Errorf("%w: attributions do not sum to attributed total", ErrInvalidExplanation)
	}
	if strings.TrimSpace(g.SourceRef) == "" || strings.TrimSpace(g.Scenario) == "" ||
		strings.TrimSpace(g.Version) == "" || strings.TrimSpace(g.Target) == "" {
		return fmt.Errorf("%w: gap provenance is required", ErrInvalidExplanation)
	}
	if g.Outcome == CellUnknown && strings.TrimSpace(g.Reason) == "" {
		return fmt.Errorf("%w: unknown gap needs a reason", ErrInvalidExplanation)
	}
	return nil
}

// Canonical returns the canonical bytes of a valid gap.
func (g SignalGap) Canonical() []byte {
	if g.Validate() != nil {
		return nil
	}
	attributions := append([]Attribution(nil), g.Attributions...)
	sort.Slice(attributions, func(i, j int) bool { return attributions[i].SupplyRef < attributions[j].SupplyRef })
	w := canonicalbytes.New("hcmnext.domains.demand.SignalGap", schemaVersion).
		String("signal_id", g.SignalID).String("outcome", string(g.Outcome)).
		Value("required", g.Required).Value("attributed", g.Attributed).
		Value("shortfall", g.Shortfall).Value("surplus", g.Surplus).
		String("reason", g.Reason).String("source_ref", g.SourceRef).
		String("scenario", g.Scenario).String("version", g.Version).
		String("target", g.Target).Count("attributions", len(attributions))
	for _, attribution := range attributions {
		w.Value("attribution", attribution)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// GapExplanation is the deterministic, digested account of every
// non-covered cell in one partition.
type GapExplanation struct {
	RequirementID   string
	Snapshot        string
	Gaps            []SignalGap
	CanonicalDigest string
}

// Validate implements validation.
func (e GapExplanation) Validate() error {
	if strings.TrimSpace(e.RequirementID) == "" || strings.TrimSpace(e.Snapshot) == "" {
		return fmt.Errorf("%w: explanation binding is required", ErrInvalidExplanation)
	}
	seen := make(map[string]struct{}, len(e.Gaps))
	for _, gap := range e.Gaps {
		if err := gap.Validate(); err != nil {
			return err
		}
		if _, ok := seen[gap.SignalID]; ok {
			return fmt.Errorf("%w: duplicate gap for %q", ErrInvalidExplanation, gap.SignalID)
		}
		seen[gap.SignalID] = struct{}{}
	}
	if e.CanonicalDigest != "" && e.CanonicalDigest != e.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidExplanation)
	}
	return nil
}

func (e GapExplanation) body() []byte {
	gaps := append([]SignalGap(nil), e.Gaps...)
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].SignalID < gaps[j].SignalID })
	w := canonicalbytes.New("hcmnext.domains.demand.GapExplanation", schemaVersion).
		String("requirement_id", e.RequirementID).String("snapshot", e.Snapshot).
		Count("gaps", len(gaps))
	for _, gap := range gaps {
		w.Value("gap", gap)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (e GapExplanation) computedDigest() string {
	raw := e.body()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Canonical returns the canonical bytes of a valid explanation.
func (e GapExplanation) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	return e.body()
}

func zeroLike(like values.Quantity) (values.Quantity, error) {
	zero, err := values.NewQuantity("0", like.Unit(), like.Value().Scale(), values.RoundingHalfEven)
	if err != nil {
		return values.Quantity{}, fmt.Errorf("%w: zero quantity: %v", ErrInvalidExplanation, err)
	}
	return zero, nil
}

// supplyApplies reports whether one assignment can satisfy a signal.
func supplyApplies(signal DemandSignal, assignment SupplyAssignment) bool {
	if !assignment.Qualified || !assignment.Available {
		return false
	}
	supply := assignment.Supply
	if supplyScope(supply) != demandScope(signal.Location, signal.OrgUnit) || supplyTarget(supply) != demandTarget(signal) {
		return false
	}
	overlaps, err := supply.Work.Overlaps(signal.Work)
	return err == nil && overlaps
}

// ExplainGaps traces every non-covered partition cell to its signal,
// provenance, qualification dimension and apportioned supply. It is
// pure: inputs are never mutated and nothing is persisted.
func ExplainGaps(partition DemandPartition, requirement CoverageRequirement, assignments []SupplyAssignment) (GapExplanation, error) {
	if err := requirement.Validate(); err != nil {
		return GapExplanation{}, gapReject("requirement", "invalid")
	}
	if partition.RequirementID != requirement.RequirementID {
		return GapExplanation{}, gapReject("requirement", "mismatch")
	}
	if strings.TrimSpace(partition.Snapshot) == "" {
		return GapExplanation{}, gapReject("partition", "missing-snapshot")
	}
	for _, assignment := range assignments {
		if err := assignment.Supply.Validate(); err != nil {
			return GapExplanation{}, gapReject("assignments", "invalid")
		}
		if strings.TrimSpace(assignment.WorkerRef) == "" {
			return GapExplanation{}, gapReject("assignments", "invalid")
		}
	}
	signals := make(map[string]DemandSignal, len(requirement.Signals))
	for _, signal := range requirement.Signals {
		signals[signal.SignalID] = signal
	}
	order := make([]int, len(assignments))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := assignments[order[i]], assignments[order[j]]
		if a.Supply.Ref != b.Supply.Ref {
			return a.Supply.Ref < b.Supply.Ref
		}
		return a.WorkerRef < b.WorkerRef
	})
	remaining := make([]values.Quantity, len(assignments))
	for _, i := range order {
		remaining[i] = assignments[i].Supply.Quantity
	}
	explanation := GapExplanation{RequirementID: requirement.RequirementID, Snapshot: partition.Snapshot}
	for _, cell := range partition.Cells {
		signal, ok := signals[cell.SignalID]
		if !ok {
			return GapExplanation{}, gapReject("signals", "unknown-signal")
		}
		switch cell.Outcome {
		case CellCovered:
			continue
		case CellShort, CellExcess, CellUnknown:
		default:
			return GapExplanation{}, gapReject("signals", "invalid-outcome")
		}
		gap, err := explainCell(signal, cell.Outcome, assignments, order, remaining)
		if err != nil {
			return GapExplanation{}, err
		}
		explanation.Gaps = append(explanation.Gaps, gap)
	}
	sort.Slice(explanation.Gaps, func(i, j int) bool { return explanation.Gaps[i].SignalID < explanation.Gaps[j].SignalID })
	explanation.CanonicalDigest = explanation.computedDigest()
	if err := explanation.Validate(); err != nil {
		return GapExplanation{}, gapReject("explanation", "invalid")
	}
	return explanation, nil
}

func explainCell(signal DemandSignal, outcome CellOutcome, assignments []SupplyAssignment, order []int, remaining []values.Quantity) (SignalGap, error) {
	need := signal.Quantity
	zero, err := zeroLike(need)
	if err != nil {
		return SignalGap{}, err
	}
	gap := SignalGap{
		SignalID: signal.SignalID, Outcome: outcome,
		Required: need, Attributed: zero, Shortfall: zero, Surplus: zero,
		SourceRef: signal.SourceRef, Scenario: signal.Scenario, Version: signal.Version,
		Target: demandTarget(signal),
	}
	if strings.TrimSpace(gap.SourceRef) == "" {
		gap.SourceRef = signal.Source
	}
	if outcome == CellUnknown {
		gap.Reason = "unknown confidence: no attribution without a credible signal"
		if signal.ConfidenceClass != ConfidenceUnknown {
			gap.Reason = "unattributable supply: no applicable qualified assignment"
		}
		return gap, nil
	}
	assigned, err := zeroLike(need)
	if err != nil {
		return SignalGap{}, err
	}
	for _, i := range order {
		assignment := assignments[i]
		if !supplyApplies(signal, assignment) || assignment.Supply.Quantity.Unit() != need.Unit() {
			continue
		}
		assigned, err = assigned.Add(assignment.Supply.Quantity)
		if err != nil {
			return SignalGap{}, gapReject("assignments", "incompatible-unit")
		}
		want, err := need.Sub(gap.Attributed)
		if err != nil {
			return SignalGap{}, gapReject("assignments", "incompatible-unit")
		}
		if want.Value().Sign() <= 0 {
			continue
		}
		take := remaining[i]
		if remaining[i].Value().Cmp(want.Value()) > 0 {
			take = want
		}
		if take.Value().Sign() <= 0 {
			continue
		}
		left, err := remaining[i].Sub(take)
		if err != nil {
			return SignalGap{}, gapReject("assignments", "incompatible-unit")
		}
		remaining[i] = left
		gap.Attributed, err = gap.Attributed.Add(take)
		if err != nil {
			return SignalGap{}, gapReject("assignments", "incompatible-unit")
		}
		gap.Attributions = append(gap.Attributions, Attribution{SupplyRef: assignment.Supply.Ref, Attributed: take})
	}
	if assigned.Value().Cmp(need.Value()) >= 0 {
		gap.Surplus, err = assigned.Sub(need)
	} else {
		gap.Shortfall, err = need.Sub(assigned)
	}
	if err != nil {
		return SignalGap{}, gapReject("assignments", "incompatible-unit")
	}
	// Attributed can never exceed need even when supply overflows: the
	// remainder is surplus, not double credit.
	if gap.Attributed.Value().Cmp(need.Value()) > 0 {
		return SignalGap{}, gapReject("explanation", "over-attributed")
	}
	short, err := need.Sub(gap.Attributed)
	if err != nil {
		return SignalGap{}, gapReject("assignments", "incompatible-unit")
	}
	gap.Shortfall = short
	return gap, nil
}
