// Operational intent proposals: DEMAND-006 turns coverage gaps into
// governed, human-approved intent descriptions.
//
// A proposal never mutates schedules, headcount, assignments or any
// other authoritative state: it names an intent with scope, risk,
// snapshot, cost basis and a human approval gate, and stops there.
// Execution belongs to the intent and transaction planes, which are
// outside this package. Unknown gaps refuse: there is no credible
// basis to propose from.
package demand

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// IntentKind is the closed proposal vocabulary.
type IntentKind string

// The intent kinds.
const (
	IntentCloseShortfall IntentKind = "CLOSE_SHORTFALL"
	IntentAbsorbExcess   IntentKind = "ABSORB_EXCESS"
)

// RiskTier is the closed risk vocabulary. Risk derives
// deterministically from the gap share, never from judgment.
type RiskTier string

// The risk tiers.
const (
	RiskLow    RiskTier = "LOW"
	RiskMedium RiskTier = "MEDIUM"
	RiskHigh   RiskTier = "HIGH"
)

// IntentParams are the caller-bound proposal inputs.
type IntentParams struct {
	IntentName   string
	Scope        string
	Snapshot     string
	Cost         values.Quantity
	HasCost      bool
	CostBasis    string
	ApprovalRole string
	Quorum       int
}

// Validate implements validation.
func (p IntentParams) Validate() error {
	if strings.TrimSpace(p.IntentName) == "" || strings.TrimSpace(p.Scope) == "" || strings.TrimSpace(p.Snapshot) == "" {
		return fmt.Errorf("%w: intent name, scope and snapshot are required", ErrInvalidExplanation)
	}
	if strings.TrimSpace(p.ApprovalRole) == "" || p.Quorum < 1 {
		return fmt.Errorf("%w: a human approval role and positive quorum are required", ErrInvalidExplanation)
	}
	if p.HasCost {
		if err := p.Cost.Validate(); err != nil {
			return fmt.Errorf("%w: cost: %v", ErrInvalidExplanation, err)
		}
		if strings.TrimSpace(p.CostBasis) == "" {
			return fmt.Errorf("%w: cost without a basis ref", ErrInvalidExplanation)
		}
	} else if strings.TrimSpace(p.CostBasis) != "" {
		return fmt.Errorf("%w: cost basis without a cost", ErrInvalidExplanation)
	}
	return nil
}

// IntentProposal is one governed operational intent description.
type IntentProposal struct {
	IntentName      string
	Kind            IntentKind
	Scope           string
	SignalID        string
	Shortfall       values.Quantity
	Surplus         values.Quantity
	Risk            RiskTier
	Snapshot        string
	CostKnown       bool
	Cost            values.Quantity
	CostBasis       string
	ApprovalRole    string
	Quorum          int
	SourceRef       string
	Scenario        string
	Version         string
	Target          string
	CanonicalDigest string
}

// Validate implements validation.
func (p IntentProposal) Validate() error {
	if strings.TrimSpace(p.IntentName) == "" || strings.TrimSpace(p.Scope) == "" || strings.TrimSpace(p.SignalID) == "" {
		return fmt.Errorf("%w: proposal binding is required", ErrInvalidExplanation)
	}
	switch p.Kind {
	case IntentCloseShortfall, IntentAbsorbExcess:
	default:
		return fmt.Errorf("%w: intent kind %q is not declared", ErrInvalidExplanation, p.Kind)
	}
	switch p.Risk {
	case RiskLow, RiskMedium, RiskHigh:
	default:
		return fmt.Errorf("%w: risk %q is not declared", ErrInvalidExplanation, p.Risk)
	}
	for name, quantity := range map[string]values.Quantity{"shortfall": p.Shortfall, "surplus": p.Surplus} {
		if err := quantity.Validate(); err != nil {
			return fmt.Errorf("%w: proposal %s: %v", ErrInvalidExplanation, name, err)
		}
	}
	if p.Kind == IntentCloseShortfall && p.Shortfall.Value().Sign() <= 0 {
		return fmt.Errorf("%w: close-shortfall intent needs a positive shortfall", ErrInvalidExplanation)
	}
	if p.Kind == IntentAbsorbExcess && p.Surplus.Value().Sign() <= 0 {
		return fmt.Errorf("%w: absorb-excess intent needs a positive surplus", ErrInvalidExplanation)
	}
	if strings.TrimSpace(p.Snapshot) == "" || strings.TrimSpace(p.ApprovalRole) == "" || p.Quorum < 1 {
		return fmt.Errorf("%w: snapshot and human approval are required", ErrInvalidExplanation)
	}
	if p.CostKnown {
		if err := p.Cost.Validate(); err != nil {
			return fmt.Errorf("%w: cost: %v", ErrInvalidExplanation, err)
		}
		if strings.TrimSpace(p.CostBasis) == "" {
			return fmt.Errorf("%w: cost without a basis ref", ErrInvalidExplanation)
		}
	}
	if strings.TrimSpace(p.SourceRef) == "" || strings.TrimSpace(p.Scenario) == "" ||
		strings.TrimSpace(p.Version) == "" || strings.TrimSpace(p.Target) == "" {
		return fmt.Errorf("%w: proposal provenance is required", ErrInvalidExplanation)
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidExplanation)
	}
	return nil
}

func (p IntentProposal) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.demand.IntentProposal", schemaVersion).
		String("intent_name", p.IntentName).String("kind", string(p.Kind)).
		String("scope", p.Scope).String("signal_id", p.SignalID).
		Value("shortfall", p.Shortfall).Value("surplus", p.Surplus).
		String("risk", string(p.Risk)).String("snapshot", p.Snapshot).
		Bool("cost_known", p.CostKnown).Value("cost", p.Cost).String("cost_basis", p.CostBasis).
		String("approval_role", p.ApprovalRole).Int("quorum", int64(p.Quorum)).
		String("source_ref", p.SourceRef).String("scenario", p.Scenario).
		String("version", p.Version).String("target", p.Target)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (p IntentProposal) computedDigest() string {
	raw := p.body()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Canonical returns the canonical bytes of a valid proposal.
func (p IntentProposal) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return p.body()
}

// share computes part / whole at four decimal places.
func gapShare(part, whole values.Quantity) (values.Decimal, error) {
	share, err := part.Value().Div(whole.Value(), 4, values.RoundingHalfEven)
	if err != nil {
		return values.Decimal{}, fmt.Errorf("%w: gap share: %v", ErrInvalidExplanation, err)
	}
	return share, nil
}

// assessRisk derives the tier from the gap share: at least half the
// requirement short scores HIGH, at least a quarter MEDIUM, the rest
// LOW. Excess above half the requirement scores MEDIUM for
// overstaffing risk, else LOW.
func assessRisk(gap SignalGap) (RiskTier, error) {
	part := gap.Shortfall
	if gap.Outcome == CellExcess {
		part = gap.Surplus
	}
	share, err := gapShare(part, gap.Required)
	if err != nil {
		return "", err
	}
	half := values.MustDecimal("0.5", 1, values.RoundingHalfEven)
	quarter := values.MustDecimal("0.25", 2, values.RoundingHalfEven)
	switch {
	case share.Cmp(half) >= 0:
		if gap.Outcome == CellExcess {
			return RiskMedium, nil
		}
		return RiskHigh, nil
	case share.Cmp(quarter) >= 0:
		return RiskMedium, nil
	default:
		return RiskLow, nil
	}
}

// ProposeIntent describes one governed operational intent for a
// coverage gap. It is pure: nothing is reserved, assigned, scheduled
// or persisted — the human approval gate must fire elsewhere before
// anything happens.
func ProposeIntent(gap SignalGap, params IntentParams) (IntentProposal, error) {
	if err := gap.Validate(); err != nil {
		return IntentProposal{}, fmt.Errorf("%w: gap: %v", ErrInvalidExplanation, err)
	}
	if err := params.Validate(); err != nil {
		return IntentProposal{}, err
	}
	if gap.Outcome == CellUnknown {
		return IntentProposal{}, fmt.Errorf("%w: unknown gap has no credible proposal basis", ErrInvalidExplanation)
	}
	proposal := IntentProposal{
		IntentName: params.IntentName, Scope: params.Scope, SignalID: gap.SignalID,
		Shortfall: gap.Shortfall, Surplus: gap.Surplus,
		Snapshot:  params.Snapshot,
		CostKnown: params.HasCost, Cost: params.Cost, CostBasis: params.CostBasis,
		ApprovalRole: params.ApprovalRole, Quorum: params.Quorum,
		SourceRef: gap.SourceRef, Scenario: gap.Scenario, Version: gap.Version, Target: gap.Target,
	}
	switch gap.Outcome {
	case CellShort:
		proposal.Kind = IntentCloseShortfall
	case CellExcess:
		proposal.Kind = IntentAbsorbExcess
	default:
		return IntentProposal{}, fmt.Errorf("%w: outcome %q proposes nothing", ErrInvalidExplanation, gap.Outcome)
	}
	risk, err := assessRisk(gap)
	if err != nil {
		return IntentProposal{}, err
	}
	proposal.Risk = risk
	if !params.HasCost {
		zero, err := zeroLike(gap.Required)
		if err != nil {
			return IntentProposal{}, err
		}
		proposal.Cost = zero
	}
	proposal.CanonicalDigest = proposal.computedDigest()
	if err := proposal.Validate(); err != nil {
		return IntentProposal{}, err
	}
	return proposal, nil
}
