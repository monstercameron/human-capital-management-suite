package cancellation

import (
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// SettlementState is the operator-facing state of one effect after folding
// the durable cancellation decisions for a run.
type SettlementState string

const (
	SettlementReversed   SettlementState = "REVERSED"
	SettlementCorrected  SettlementState = "CORRECTED"
	SettlementKept       SettlementState = "KEPT"
	SettlementUnresolved SettlementState = "UNRESOLVED"
)

// EffectSettlement is a read model for one effect. DecisionID and Evidence
// identify the append-only decision row and its sealed outcome; Reason is the
// recorded decision reason associated with the effect, when one exists.
type EffectSettlement struct {
	EffectID   string          `json:"effect_id"`
	State      SettlementState `json:"state"`
	Reason     string          `json:"reason"`
	DecisionID uuid.UUID       `json:"decision_id"`
	Evidence   string          `json:"evidence_digest"`
}

// Settlement is the projection of durable per-effect cancellation outcomes.
// It intentionally adds no lifecycle dimension: callers continue to render
// the existing five dimensions, while this projection explains the effects
// behind them.
type Settlement struct {
	Effects []EffectSettlement `json:"effects"`
	// BusinessState is NOT_ACHIEVED when cancellation leaves any effect in
	// any state. It is derived here from the settlement rows, not decision text.
	BusinessState string `json:"business_state"`
	// ConsistencyState is DEGRADED when at least one effect is unresolved;
	// otherwise the effects are settled with this cancellation.
	ConsistencyState string `json:"consistency_state"`
}

// ProjectSettlement folds a run's append-only cancellation rows into one
// latest disposition per effect. The caller must supply the rows from the
// tenant-scoped Decisions read. Evidence and reason are taken from persisted
// outcomes; no operator-facing status is inferred from a reason string alone.
func ProjectSettlement(records []Record) Settlement {
	latest := make(map[string]EffectSettlement)
	for _, rec := range records {
		if rec.Evidence.Digest == "" {
			continue
		}
		for _, effect := range rec.Evidence.Effects {
			state, ok := settlementState(effect)
			if !ok {
				continue
			}
			reason := settlementReason(rec.Reasons, effect.ID)
			latest[effect.ID] = EffectSettlement{
				EffectID: effect.ID, State: state, Reason: reason,
				DecisionID: rec.DecisionID, Evidence: rec.Evidence.Digest,
			}
		}
	}
	out := Settlement{Effects: make([]EffectSettlement, 0, len(latest)),
		BusinessState: "NOT_ACHIEVED", ConsistencyState: "CONSISTENT"}
	for _, effect := range latest {
		out.Effects = append(out.Effects, effect)
		if effect.State == SettlementUnresolved {
			out.ConsistencyState = "DEGRADED"
		}
	}
	sort.Slice(out.Effects, func(i, j int) bool { return out.Effects[i].EffectID < out.Effects[j].EffectID })
	return out
}

func settlementState(effect workflow.EffectDisposition) (SettlementState, bool) {
	disposition := strings.TrimSpace(effect.Disposition)
	switch {
	case disposition == "REVERTED", disposition == EffectCompensated:
		return SettlementReversed, true
	case disposition == "CORRECTED":
		return SettlementCorrected, true
	case disposition == "IRREVERSIBLE":
		return SettlementKept, true
	case disposition == "AMBIGUOUS", strings.HasPrefix(disposition, EffectRemaining+":"),
		strings.HasPrefix(disposition, "COMPENSATE:"), strings.HasPrefix(disposition, "CORRECT:"):
		// CORRECT names a forward correction contract, not evidence that the
		// correction ran. Keep it unresolved until a durable completion record
		// can prove the corrected outcome.
		return SettlementUnresolved, true
	default:
		// NOT_PRODUCED records no effect and therefore is not part of settlement.
		return "", false
	}
}

func settlementReason(reasons []Reason, effectID string) string {
	for _, reason := range reasons {
		if reason.Ref == effectID || strings.HasSuffix(reason.Ref, "/"+effectID) {
			return reason.Code
		}
	}
	return ""
}
