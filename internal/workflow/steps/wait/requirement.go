package wait

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CalculationEvidence records how a TimerRequirement's fire-at instant was
// derived, so the decision can be defended without recomputing it. Inputs is
// a flat string map (encoding/json sorts map keys, so its digest is
// deterministic); Steps is an ordered narrative of what the calculation did.
type CalculationEvidence struct {
	WakeKind WakeKind          `json:"wake_kind"`
	Inputs   map[string]string `json:"inputs,omitempty"`
	Steps    []string          `json:"steps,omitempty"`
}

// TimerRequirement is the durable, typed continuation record WF-STEP-005
// produces in place of a scheduled sleep: the fire-at instant, the
// calendar/timezone/tzdb identity it was computed against, the calculation
// evidence, and a content digest that identifies this exact requirement.
//
// ReviewRequired marks a requirement whose wake condition could not be
// resolved to an instant at all (a DST-ambiguous or non-existent local time
// under a REJECT_GAP policy). Such a requirement is still minted — it is the
// typed record of the refusal — but FireAt is unset and Resolve always
// returns TIMER_REVIEW_REQUIRED for it, never a guess.
type TimerRequirement struct {
	WorkflowID      string `json:"workflow_id"`
	WorkflowVersion uint32 `json:"workflow_version"`
	NodeID          string `json:"node_id"`

	// FireAt is the resolved wake instant. It is unset when ReviewRequired.
	FireAt values.Instant `json:"-"`

	Reference values.TimerReference  `json:"-"`
	Dataset   values.DatasetVersions `json:"-"`

	ReviewRequired bool   `json:"review_required"`
	ReviewReason   string `json:"review_reason,omitempty"`

	Evidence CalculationEvidence `json:"evidence"`

	// Digest is the requirement's content identity, minted by
	// ComputeTimerRequirement. A zero-value TimerRequirement (never through
	// ComputeTimerRequirement) has no digest and Resolve refuses it.
	Digest string `json:"digest"`
}

// FireAtText returns the canonical RFC 3339 text of FireAt, or "" when unset.
func (r TimerRequirement) FireAtText() string {
	if !r.FireAt.IsSet() {
		return ""
	}
	return r.FireAt.String()
}

// isAmbiguousLocalTime reports whether err is the specific "refuse rather
// than guess" refusal a REJECT_GAP disambiguation produces for a DST gap or
// fold. Any other resolver error (an unloadable zone, an invalid calendar
// date) is a hard construction failure, not a reviewable timer state.
func isAmbiguousLocalTime(err error) bool {
	return errors.Is(err, values.ErrDSTGap) || errors.Is(err, values.ErrDSTAmbiguous)
}

// ComputeTimerRequirement derives a TimerRequirement purely from a compiled
// WAIT node's declared wake condition and the dataset versions in force. It
// reads no ambient clock: the dataset is caller-supplied, exactly like the
// "now" Resolve later takes.
//
// A missing or unspecified reference-update policy, an unloadable zone, an
// invalid calendar date, or malformed dataset versions are refused outright
// (a non-nil error, no requirement minted) — "a dataset/policy change without
// a declared policy cannot advance" is exactly this path, since
// TimerReference.Validate rejects ReferenceUpdateUnspecified. A DST-ambiguous
// or non-existent local wall-clock time under REJECT_GAP is different: it is
// a legitimate business state, not a caller error, so it produces a minted
// requirement with ReviewRequired set rather than an error. A time-expression
// wake is refused here for want of its anchor; use
// ComputeTimerRequirementAtAnchor.
func ComputeTimerRequirement(node CompiledWaitNode, dataset values.DatasetVersions) (TimerRequirement, error) {
	return computeRequirement(node, dataset, values.Instant{}, node.resolver())
}

// ComputeTimerRequirementAtAnchor is ComputeTimerRequirement for a
// time-expression wake: anchor is the evaluated AnchorPath instant for this
// workflow instance and cals is the pinned calendar dataset. The anchor is
// recorded in the requirement evidence so the fire-at instant stays
// defensible per instance. For a fixed or wall-clock wake the anchor and
// dataset are ignored and this is ComputeTimerRequirement.
func ComputeTimerRequirementAtAnchor(node CompiledWaitNode, anchor values.Instant, cals values.CalendarDataset, dataset values.DatasetVersions) (TimerRequirement, error) {
	return computeRequirement(node, dataset, anchor, node.resolverAtAnchor(anchor, cals))
}

func computeRequirement(node CompiledWaitNode, dataset values.DatasetVersions, anchor values.Instant, resolve values.TimerResolver) (TimerRequirement, error) {
	if err := node.Validate(); err != nil {
		return TimerRequirement{}, err
	}
	ref := node.reference()
	if err := ref.Validate(); err != nil {
		return TimerRequirement{}, err
	}
	if err := dataset.Validate(); err != nil {
		return TimerRequirement{}, err
	}

	kind := node.Kind()
	evidence := CalculationEvidence{
		WakeKind: kind,
		Inputs: map[string]string{
			"zone":             node.Zone.String(),
			"calendar":         node.Calendar.String(),
			"policy":           node.Policy.String(),
			"dataset_tzdb":     dataset.TzdbVersion,
			"dataset_calendar": dataset.CalendarVersion,
		},
	}
	switch kind {
	case WakeAtInstant:
		evidence.Inputs["fixed_instant"] = node.WakeInstant.String()
		evidence.Steps = append(evidence.Steps, "wake condition is a fixed instant; dataset does not affect it")
	case WakeAtLocalDate:
		evidence.Inputs["local_date"] = node.WakeLocalDate.String()
		evidence.Inputs["disambiguation"] = node.Disambiguation.String()
		evidence.Steps = append(evidence.Steps, "resolved start-of-day for the local date against the zone")
	case WakeAtLocalDateTime:
		evidence.Inputs["local_date"] = node.WakeLocalDate.String()
		evidence.Inputs["local_time"] = node.WakeLocalTime.String()
		evidence.Inputs["disambiguation"] = node.Disambiguation.String()
		evidence.Steps = append(evidence.Steps, "resolved the local date/time against the zone")
	case WakeAtTimeExpr:
		evidence.Inputs["anchor_path"] = node.WakeTimeExpr.AnchorPath
		evidence.Inputs["offset_kind"] = string(node.WakeTimeExpr.OffsetKind)
		evidence.Inputs["offset_days"] = fmt.Sprintf("%d", node.WakeTimeExpr.OffsetDays)
		evidence.Inputs["offset_seconds"] = fmt.Sprintf("%d", node.WakeTimeExpr.OffsetSeconds)
		if node.WakeTimeExpr.Statute != nil {
			evidence.Inputs["statute"] = node.WakeTimeExpr.Statute.String()
		}
		if anchor.IsSet() {
			evidence.Inputs["anchor"] = anchor.String()
		}
		evidence.Steps = append(evidence.Steps, "resolved the time expression against the instance anchor and pinned calendar")
	}

	fireAt, err := resolve(dataset)
	req := TimerRequirement{
		WorkflowID:      node.WorkflowID,
		WorkflowVersion: node.WorkflowVersion,
		NodeID:          node.NodeID,
		Reference:       ref,
		Dataset:         dataset,
		Evidence:        evidence,
	}
	switch {
	case err == nil:
		req.FireAt = fireAt
		req.Evidence.Inputs["fire_at"] = fireAt.String()
		req.Evidence.Steps = append(req.Evidence.Steps, fmt.Sprintf("fire_at resolved to %s", fireAt))
	case isAmbiguousLocalTime(err):
		req.ReviewRequired = true
		req.ReviewReason = err.Error()
		req.Evidence.Steps = append(req.Evidence.Steps, "refused to guess a DST-ambiguous or non-existent local time: "+err.Error())
	default:
		return TimerRequirement{}, err
	}

	req.Digest = computeRequirementDigest(req)
	return req, nil
}
