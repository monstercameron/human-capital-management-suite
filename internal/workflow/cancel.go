package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

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
// irreversible effect means no release exists. Correction names the forward
// correction path of an otherwise-irreversible effect the run keeps; empty
// means no correction exists and the effect is kept as-is. Ambiguous marks
// an effect whose outcome is not durably known (in flight, or a failed
// attempt that recorded an effect reference): it is observed through the
// request's observer before anything is decided about it, and only an
// effect still unknown after that bounded observation needs repair.
type EffectRecord struct {
	ID           string `json:"id"`
	Reversible   bool   `json:"reversible"`
	Compensation string `json:"compensation"`
	Correction   string `json:"correction,omitempty"`
	Ambiguous    bool   `json:"ambiguous,omitempty"`
}

// NodeEffectScope is the semantic idempotency scope a served node declares.
// It lives in this lower workflow package so recovery and cancellation derive
// the same coordinates without depending on one another.
func NodeEffectScope(nodeID string) string { return "workflow-node:" + nodeID }

// StepActivationKey is the idempotency key of one node activation attempt.
func StepActivationKey(instanceID uuid.UUID, nodeID string, attempt int) string {
	return "wf-step-effect:" + instanceID.String() + ":" + nodeID + ":" + strconv.Itoa(attempt)
}

// EffectVerdict is one effect's cancellation verdict. Every committed
// effect receives exactly one: what cancellation does with that effect,
// decided independently of its siblings so a reversible effect beside an
// irreversible or unconfirmed one is still undone.
type EffectVerdict string

// Per-effect verdicts.
const (
	// EffectCompensate undoes the effect: by a superseding revision for a
	// reversible effect (disposition REVERTED) or by its published
	// compensation otherwise (disposition COMPENSATE:*).
	EffectCompensate EffectVerdict = "COMPENSATE"
	// EffectKeepAndCorrect keeps the effect and records its forward
	// correction path (disposition CORRECT:*): cancellation cannot undo it,
	// but the effect is not abandoned.
	EffectKeepAndCorrect EffectVerdict = "KEEP_AND_CORRECT"
	// EffectKeepIrreversible keeps the effect with no undo and no
	// correction path (disposition IRREVERSIBLE).
	EffectKeepIrreversible EffectVerdict = "KEEP_IRREVERSIBLE"
	// EffectUnresolved marks an effect still unknown after bounded
	// observation (disposition AMBIGUOUS): only this effect, never its
	// siblings, is left for repair.
	EffectUnresolved EffectVerdict = "UNRESOLVED"
)

// EffectObservation is what bounded observation of one unconfirmed effect
// found. Produced reports the effect was confirmed produced (judge it by
// its declared undo contract) or confirmed never produced (it needs no
// verdict: nothing committed).
type EffectObservation struct {
	Produced bool
}

// ObserveEffect resolves one unconfirmed effect by id. Any error,
// including a timeout, leaves exactly that effect unresolved; the observer
// bounds itself and is called at most once per unconfirmed effect.
type ObserveEffect func(effectID string) (EffectObservation, error)

// CancellationRequest asks for one governed cancellation decision. The
// function decides; it manages no goroutine, deletes no history and
// declares nothing complete while a child or effect is unresolved.
// Observe, when non-nil, resolves every unconfirmed effect before the
// verdict: without it an ambiguous effect stays unresolved, as before.
type CancellationRequest struct {
	RunID    string            `json:"run_id"`
	Revision string            `json:"revision"`
	Phase    string            `json:"phase"`
	Children []CancellableNode `json:"children"`
	Effects  []EffectRecord    `json:"effects"`
	Observe  ObserveEffect     `json:"-"`
}

// ChildCancellation is one recorded child report.
type ChildCancellation struct {
	Ref    subworkflow.ChildRef    `json:"ref"`
	Report subworkflow.ChildReport `json:"report"`
}

// EffectDisposition is one recorded effect outcome: the effect's verdict
// and the disposition that carries it. NOT_PRODUCED records an effect
// observed never produced; it carries no verdict because nothing committed.
type EffectDisposition struct {
	ID          string        `json:"id"`
	Disposition string        `json:"disposition"`
	Verdict     EffectVerdict `json:"verdict,omitempty"`
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
	childRepair := false
	childRefused := false
	for _, child := range req.Children {
		report, err := subworkflow.PropagateCancellation(subworkflow.CancellableChild{
			Ref: child.Ref, State: child.State, Cancellable: child.Cancellable,
		})
		if err != nil {
			childRepair = true
			continue
		}
		outcome.ChildReports = append(outcome.ChildReports, ChildCancellation{Ref: child.Ref, Report: report})
		if report == subworkflow.ReportCannotCancel {
			childRefused = true
		}
	}
	for _, effect := range req.Effects {
		rec := effect
		if rec.Ambiguous && req.Observe != nil {
			obs, err := req.Observe(rec.ID)
			switch {
			case err != nil:
				// Still unknown: only this effect is left unresolved.
			case !obs.Produced:
				outcome.Effects = append(outcome.Effects, EffectDisposition{ID: rec.ID, Disposition: "NOT_PRODUCED"})
				continue
			default:
				rec.Ambiguous = false
			}
		}
		verdict, disposition := judgeEffect(rec)
		outcome.Effects = append(outcome.Effects, EffectDisposition{ID: rec.ID, Disposition: disposition, Verdict: verdict})
	}
	outcome.Decision = SummarizeCancellation(outcome.Effects, childRefused, childRepair)
	sum := sha256.Sum256([]byte(strings.Join([]string{
		req.RunID, req.Revision, req.Phase, string(outcome.Decision),
		fmt.Sprintf("children=%d", len(outcome.ChildReports)),
		fmt.Sprintf("effects=%d", len(outcome.Effects)),
	}, "\x00")))
	outcome.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return outcome, nil
}

// judgeEffect renders one effect's verdict and its disposition, judging
// the effect alone: siblings never change what one committed effect needs.
func judgeEffect(effect EffectRecord) (EffectVerdict, string) {
	switch {
	case effect.Ambiguous:
		return EffectUnresolved, "AMBIGUOUS"
	case effect.Reversible:
		return EffectCompensate, "REVERTED"
	case strings.TrimSpace(effect.Compensation) != "":
		return EffectCompensate, "COMPENSATE:" + effect.Compensation
	case strings.TrimSpace(effect.Correction) != "":
		return EffectKeepAndCorrect, "CORRECT:" + effect.Correction
	default:
		return EffectKeepIrreversible, "IRREVERSIBLE"
	}
}

// SummarizeCancellation derives the run verdict as a pure summary of its
// per-effect verdicts and child reports: an unresolved effect or an
// unjudgeable child needs repair; a kept effect or a refusing child refuses
// the run; a compensated effect needs compensation; anything else cancels
// cleanly. Kept-and-correct effects refuse the run like kept-irreversible
// ones: the effect stays, so the run cannot cancel cleanly, and the
// recorded correction path is for the follow-up the effect's owner drives.
// A REVERTED compensation rides the cancel transition itself (the
// superseding revision is written as part of cancelling, with no discharge
// obligation), so only a COMPENSATE:* verdict needs the discharge driver:
// a run whose every undo reverts still cancels cleanly.
func SummarizeCancellation(dispositions []EffectDisposition, childRefused, childRepair bool) CancellationDecision {
	compensate := false
	keep := false
	for _, e := range dispositions {
		switch e.Verdict {
		case EffectUnresolved:
			return RepairRequired
		case EffectKeepIrreversible, EffectKeepAndCorrect:
			keep = true
		case EffectCompensate:
			if e.Disposition != "REVERTED" {
				compensate = true
			}
		}
	}
	switch {
	case childRepair:
		return RepairRequired
	case keep || childRefused:
		return CannotCancel
	case compensate:
		return CompensationRequired
	default:
		return Cancelled
	}
}
