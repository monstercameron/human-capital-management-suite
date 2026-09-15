package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/subworkflow"
)

// CancellationDecision is one governed cancellation outcome.
type CancellationDecision string

// Governed outcomes.
const (
	Cancelled            CancellationDecision = "CANCELLED"
	CompensationRequired CancellationDecision = "COMPENSATION_REQUIRED"
	CannotCancel         CancellationDecision = "CANNOT_CANCEL"
	RepairRequired       CancellationDecision = "REPAIR_REQUIRED"
)

// CancellableNode declares one child's cancellation semantics: its pinned
// ref, current state and whether it can stop. Every node declares; the
// policy never assumes.
type CancellableNode struct {
	Ref         subworkflow.ChildRef   `json:"ref"`
	State       subworkflow.ChildState `json:"state"`
	Cancellable bool                   `json:"cancellable"`
}

// EffectRecord declares one produced effect: whether it reverses and,
// when not, which compensation releases it. An empty Compensation on an
// irreversible effect means no release exists. Ambiguous marks an effect
// whose outcome is not durably known (in flight, or a failed attempt that
// recorded an effect reference): nothing can be decided about it, so the run
// needs repair.
type EffectRecord struct {
	ID           string `json:"id"`
	Reversible   bool   `json:"reversible"`
	Compensation string `json:"compensation"`
	Ambiguous    bool   `json:"ambiguous,omitempty"`
}

// CancellationRequest asks for one governed cancellation decision. The
// function decides; it manages no goroutine, deletes no history and
// declares nothing complete while a child or effect is unresolved.
type CancellationRequest struct {
	RunID    string            `json:"run_id"`
	Revision string            `json:"revision"`
	Phase    string            `json:"phase"`
	Children []CancellableNode `json:"children"`
	Effects  []EffectRecord    `json:"effects"`
}

// ChildCancellation is one recorded child report.
type ChildCancellation struct {
	Ref    subworkflow.ChildRef    `json:"ref"`
	Report subworkflow.ChildReport `json:"report"`
}

// EffectDisposition is one recorded effect outcome.
type EffectDisposition struct {
	ID          string `json:"id"`
	Disposition string `json:"disposition"`
}

// CancellationOutcome is one decision with its phase and effect evidence.
// History records every input child verbatim.
type CancellationOutcome struct {
	Decision     CancellationDecision `json:"decision"`
	Phase        string               `json:"phase"`
	ChildReports []ChildCancellation  `json:"child_reports"`
	Effects      []EffectDisposition  `json:"effects"`
	History      []CancellableNode    `json:"history"`
	Digest       string               `json:"digest"`
}

// DecideCancellation renders one governed cancellation decision.
func DecideCancellation(req CancellationRequest) (CancellationOutcome, error) {
	if strings.TrimSpace(req.RunID) == "" || strings.TrimSpace(req.Revision) == "" ||
		strings.TrimSpace(req.Phase) == "" {
		return CancellationOutcome{}, errors.New("workflow: DecideCancellation needs run, revision and phase")
	}
	outcome := CancellationOutcome{Phase: req.Phase}
	outcome.History = append([]CancellableNode(nil), req.Children...)
	repair := false
	for _, child := range req.Children {
		report, err := subworkflow.PropagateCancellation(subworkflow.CancellableChild{
			Ref: child.Ref, State: child.State, Cancellable: child.Cancellable,
		})
		if err != nil {
			repair = true
			continue
		}
		outcome.ChildReports = append(outcome.ChildReports, ChildCancellation{Ref: child.Ref, Report: report})
	}
	compensation := false
	refused := false
	for _, effect := range req.Effects {
		switch {
		case effect.Ambiguous:
			repair = true
			outcome.Effects = append(outcome.Effects, EffectDisposition{ID: effect.ID, Disposition: "AMBIGUOUS"})
		case effect.Reversible:
			outcome.Effects = append(outcome.Effects, EffectDisposition{ID: effect.ID, Disposition: "REVERTED"})
		case strings.TrimSpace(effect.Compensation) != "":
			compensation = true
			outcome.Effects = append(outcome.Effects, EffectDisposition{ID: effect.ID, Disposition: "COMPENSATE:" + effect.Compensation})
		default:
			refused = true
			outcome.Effects = append(outcome.Effects, EffectDisposition{ID: effect.ID, Disposition: "IRREVERSIBLE"})
		}
	}
	for _, report := range outcome.ChildReports {
		if report.Report == subworkflow.ReportCannotCancel {
			refused = true
		}
	}
	switch {
	case repair:
		outcome.Decision = RepairRequired
	case refused:
		outcome.Decision = CannotCancel
	case compensation:
		outcome.Decision = CompensationRequired
	default:
		outcome.Decision = Cancelled
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		req.RunID, req.Revision, req.Phase, string(outcome.Decision),
		fmt.Sprintf("children=%d", len(outcome.ChildReports)),
		fmt.Sprintf("effects=%d", len(outcome.Effects)),
	}, "\x00")))
	outcome.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return outcome, nil
}
