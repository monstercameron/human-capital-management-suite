// WF-REV-008: route effects that cannot be undone to a human decision
// block. Pay already released or a notice already sent can only be
// corrected or accepted; this fragment presents each remaining effect with
// its options and records the decision and any open obligation.
package repair

import (
	"errors"
	"fmt"
	"sort"
)

// Decision errors. All are matchable with errors.Is.
var (
	// ErrFragmentInvalid is returned for a malformed decision fragment.
	ErrFragmentInvalid = errors.New("repair: decision fragment is malformed")
	// ErrUnknownEffect is returned when deciding an effect the fragment
	// does not present.
	ErrUnknownEffect = errors.New("repair: decision fragment presents no such effect")
	// ErrAlreadyDecided is returned when deciding an effect twice. A
	// recorded human decision is not edited in place; a new fragment
	// supersedes it.
	ErrAlreadyDecided = errors.New("repair: effect already decided")
	// ErrDecisionInvalid is returned for a malformed decision.
	ErrDecisionInvalid = errors.New("repair: decision is malformed")
	// ErrAuthorityRequired is returned for a waiver with no authority. A
	// waiver without a named authority is an opinion, not a decision.
	ErrAuthorityRequired = errors.New("repair: waiver requires an authority")
)

// IrreversibleReason names why an effect cannot be undone.
type IrreversibleReason uint8

// Irreversible reasons.
const (
	// ReasonUnspecified is the zero value and is never legal.
	ReasonUnspecified IrreversibleReason = iota
	// ReasonIrreversible means the effect has no reverse path at all.
	ReasonIrreversible
	// ReasonOutOfWindow means the reversal window has closed.
	ReasonOutOfWindow
)

// String renders the reason for receipts and fragments.
func (r IrreversibleReason) String() string {
	switch r {
	case ReasonIrreversible:
		return "IRREVERSIBLE"
	case ReasonOutOfWindow:
		return "OUT_OF_WINDOW"
	default:
		return "UNSPECIFIED"
	}
}

// DecisionOption is one of the three human options for a remaining effect.
type DecisionOption uint8

// Decision options.
const (
	// OptionUnspecified is the zero value and is never legal.
	OptionUnspecified DecisionOption = iota
	// OptionCorrect starts a correction intent for the effect.
	OptionCorrect
	// OptionWaive records a waiver with authority.
	OptionWaive
	// OptionAcceptResidual accepts the effect as a residual.
	OptionAcceptResidual
)

// String renders the option for receipts and fragments.
func (o DecisionOption) String() string {
	switch o {
	case OptionCorrect:
		return "CORRECT"
	case OptionWaive:
		return "WAIVE"
	case OptionAcceptResidual:
		return "ACCEPT_RESIDUAL"
	default:
		return "UNSPECIFIED"
	}
}

// AllDecisionOptions is the closed option list every remaining effect is
// presented with.
var AllDecisionOptions = []DecisionOption{OptionCorrect, OptionWaive, OptionAcceptResidual}

// ObligationKind names what a recorded decision leaves behind.
type ObligationKind uint8

// Obligation kinds.
const (
	// ObligationUnspecified is the zero value and is never legal.
	ObligationUnspecified ObligationKind = iota
	// ObligationCorrectionIntent is an open correction to run.
	ObligationCorrectionIntent
	// ObligationWaiverRecord is a recorded waiver.
	ObligationWaiverRecord
	// ObligationResidualAcceptance is a recorded accepted residual.
	ObligationResidualAcceptance
)

// String renders the obligation kind.
func (k ObligationKind) String() string {
	switch k {
	case ObligationCorrectionIntent:
		return "CORRECTION_INTENT"
	case ObligationWaiverRecord:
		return "WAIVER_RECORD"
	case ObligationResidualAcceptance:
		return "RESIDUAL_ACCEPTANCE"
	default:
		return "UNSPECIFIED"
	}
}

// ObligationStatus is the lifecycle of a decision obligation.
type ObligationStatus uint8

// Obligation statuses.
const (
	// ObligationStatusUnspecified is the zero value and is never legal.
	ObligationStatusUnspecified ObligationStatus = iota
	// ObligationOpen means the obligation still needs work.
	ObligationOpen
	// ObligationRecorded means the record is complete at decision time.
	ObligationRecorded
)

// String renders the obligation status.
func (s ObligationStatus) String() string {
	switch s {
	case ObligationOpen:
		return "OPEN"
	case ObligationRecorded:
		return "RECORDED"
	default:
		return "UNSPECIFIED"
	}
}

// RemainingEffect is one effect the cancel could not undo.
type RemainingEffect struct {
	EffectID    string
	Kind        string
	Description string
	Reason      IrreversibleReason
}

// CommittedEffect is one committed run effect for cancel analysis.
type CommittedEffect struct {
	EffectID     string
	Kind         string
	Description  string
	Reversible   bool
	WindowClosed bool
}

// RemainingAfterCancel returns the committed effects a cancel cannot undo:
// irreversible effects and effects whose reversal window has closed.
// Reversible in-window effects are omitted; the cancel path unwinds them.
func RemainingAfterCancel(committed []CommittedEffect) []RemainingEffect {
	var out []RemainingEffect
	for _, c := range committed {
		if c.Reversible && !c.WindowClosed {
			continue
		}
		reason := ReasonIrreversible
		if c.Reversible && c.WindowClosed {
			reason = ReasonOutOfWindow
		}
		out = append(out, RemainingEffect{
			EffectID:    c.EffectID,
			Kind:        c.Kind,
			Description: c.Description,
			Reason:      reason,
		})
	}
	return out
}

// Actor is the human deciding, with the authority they decide under.
type Actor struct {
	ID        string
	Authority string
}

// EffectDecision is the recorded human decision for one remaining effect.
type EffectDecision struct {
	EffectID         string
	Option           DecisionOption
	DecidedBy        string
	Authority        string
	Rationale        string
	OpenObligationID string
}

// Obligation is the record a decision leaves behind.
type Obligation struct {
	ID       string
	EffectID string
	Kind     ObligationKind
	Status   ObligationStatus
	Owner    string
	Detail   string
}

// DecisionFragment is the published human decision block for the effects a
// cancel could not undo. It presents each remaining effect with the same
// closed option list and records one single-shot decision per effect.
type DecisionFragment struct {
	FragmentID  string
	RunID       string
	effects     []RemainingEffect
	byID        map[string]RemainingEffect
	decisions   map[string]EffectDecision
	obligations map[string]Obligation
	seq         int
}

// PublishDecisionFragment publishes a decision block presenting every
// remaining effect. It refuses an empty run, an empty list, blank or
// duplicate effect ids, and effects with no reason.
func PublishDecisionFragment(runID string, effects []RemainingEffect) (*DecisionFragment, error) {
	if runID == "" || len(effects) == 0 {
		return nil, fmt.Errorf("%w: run %q with %d effects", ErrFragmentInvalid, runID, len(effects))
	}
	byID := make(map[string]RemainingEffect, len(effects))
	ordered := make([]RemainingEffect, 0, len(effects))
	for _, e := range effects {
		if e.EffectID == "" || e.Kind == "" || e.Description == "" {
			return nil, fmt.Errorf("%w: effect %q names no id, kind or description", ErrFragmentInvalid, e.EffectID)
		}
		if e.Reason != ReasonIrreversible && e.Reason != ReasonOutOfWindow {
			return nil, fmt.Errorf("%w: effect %q names no reason", ErrFragmentInvalid, e.EffectID)
		}
		if _, dup := byID[e.EffectID]; dup {
			return nil, fmt.Errorf("%w: duplicate effect %q", ErrFragmentInvalid, e.EffectID)
		}
		byID[e.EffectID] = e
		ordered = append(ordered, e)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].EffectID < ordered[j].EffectID })
	return &DecisionFragment{
		FragmentID:  "frag-" + runID,
		RunID:       runID,
		effects:     ordered,
		byID:        byID,
		decisions:   make(map[string]EffectDecision, len(effects)),
		obligations: make(map[string]Obligation),
	}, nil
}

// Pending returns the remaining effects with no recorded decision, in
// effect-id order.
func (f *DecisionFragment) Pending() []RemainingEffect {
	var out []RemainingEffect
	for _, e := range f.effects {
		if _, done := f.decisions[e.EffectID]; !done {
			out = append(out, e)
		}
	}
	return out
}

// Options returns the closed option list for an effect, or nil when the
// fragment presents no such effect.
func (f *DecisionFragment) Options(effectID string) []DecisionOption {
	if _, ok := f.byID[effectID]; !ok {
		return nil
	}
	return append([]DecisionOption(nil), AllDecisionOptions...)
}

// Decide records the human decision for one remaining effect. Waivers
// require an authority; every decision requires a decider and a rationale.
// A correction opens a correction obligation; a waiver or residual
// acceptance is recorded complete at decision time.
func (f *DecisionFragment) Decide(effectID string, opt DecisionOption, actor Actor, rationale string) (EffectDecision, error) {
	eff, ok := f.byID[effectID]
	if !ok {
		return EffectDecision{}, fmt.Errorf("%w: %q", ErrUnknownEffect, effectID)
	}
	if _, done := f.decisions[effectID]; done {
		return EffectDecision{}, fmt.Errorf("%w: %q", ErrAlreadyDecided, effectID)
	}
	if opt != OptionCorrect && opt != OptionWaive && opt != OptionAcceptResidual {
		return EffectDecision{}, fmt.Errorf("%w: option %s", ErrDecisionInvalid, opt)
	}
	if actor.ID == "" || rationale == "" {
		return EffectDecision{}, fmt.Errorf("%w: decider and rationale are required", ErrDecisionInvalid)
	}
	if opt == OptionWaive && actor.Authority == "" {
		return EffectDecision{}, fmt.Errorf("%w: waiving %q", ErrAuthorityRequired, effectID)
	}

	dec := EffectDecision{
		EffectID:  effectID,
		Option:    opt,
		DecidedBy: actor.ID,
		Authority: actor.Authority,
		Rationale: rationale,
	}
	switch opt {
	case OptionCorrect:
		f.seq++
		ob := Obligation{
			ID:       fmt.Sprintf("%s-obl-%03d", f.FragmentID, f.seq),
			EffectID: effectID,
			Kind:     ObligationCorrectionIntent,
			Status:   ObligationOpen,
			Owner:    actor.ID,
			Detail:   "correction intent for " + eff.Kind + ": " + rationale,
		}
		f.obligations[ob.ID] = ob
		dec.OpenObligationID = ob.ID
	case OptionWaive:
		f.seq++
		ob := Obligation{
			ID:       fmt.Sprintf("%s-obl-%03d", f.FragmentID, f.seq),
			EffectID: effectID,
			Kind:     ObligationWaiverRecord,
			Status:   ObligationRecorded,
			Owner:    actor.ID,
			Detail:   "waiver under " + actor.Authority + ": " + rationale,
		}
		f.obligations[ob.ID] = ob
	case OptionAcceptResidual:
		f.seq++
		ob := Obligation{
			ID:       fmt.Sprintf("%s-obl-%03d", f.FragmentID, f.seq),
			EffectID: effectID,
			Kind:     ObligationResidualAcceptance,
			Status:   ObligationRecorded,
			Owner:    actor.ID,
			Detail:   "accepted residual: " + rationale,
		}
		f.obligations[ob.ID] = ob
	}
	f.decisions[effectID] = dec
	return dec, nil
}

// Decision returns the recorded decision for an effect, or the zero
// decision when none is recorded.
func (f *DecisionFragment) Decision(effectID string) EffectDecision {
	return f.decisions[effectID]
}

// Obligation returns the obligation by id, or the zero obligation when
// none exists.
func (f *DecisionFragment) Obligation(id string) Obligation {
	return f.obligations[id]
}

// Complete reports whether every presented effect has a recorded decision.
func (f *DecisionFragment) Complete() bool {
	return len(f.decisions) == len(f.effects)
}
