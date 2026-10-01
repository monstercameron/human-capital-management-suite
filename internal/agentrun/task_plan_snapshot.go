package agentrun

import (
	"fmt"
	"strings"
)

// HistoricalPlanSnapshot preserves the immutable inputs of one plan revision.
type HistoricalPlanSnapshot struct {
	Revision uint64             `json:"revision"`
	Digest   string             `json:"digest"`
	Steps    []PlanStepIdentity `json:"steps"`
}

// SnapshotPlan returns the digest-bound identity of a valid plan, excluding mutable execution state.
func SnapshotPlan(plan AgentPlan) (HistoricalPlanSnapshot, error) {
	if !plan.validDigest() {
		return HistoricalPlanSnapshot{}, fmt.Errorf("%w: cannot snapshot invalid plan", ErrInvalid)
	}
	snapshot := HistoricalPlanSnapshot{Revision: plan.Revision, Digest: plan.Digest, Steps: make([]PlanStepIdentity, len(plan.Steps))}
	for i, step := range plan.Steps {
		identity := identityOf(step)
		identity.Inputs = append([]InputRef(nil), identity.Inputs...)
		for j := range identity.Inputs {
			identity.Inputs[j].Taint = append([]string(nil), identity.Inputs[j].Taint...)
		}
		identity.Wait = cloneWake(identity.Wait)
		snapshot.Steps[i] = identity
	}
	return snapshot, nil
}

// Verify checks that the snapshot still hashes to its recorded revision digest.
func (s HistoricalPlanSnapshot) Verify() error {
	if s.Revision == 0 || s.Revision > maxPersistedTaskRevision || strings.TrimSpace(s.Digest) == "" || len(s.Steps) == 0 || len(s.Steps) > MaxPlanSteps {
		return fmt.Errorf("%w: historical plan snapshot is incomplete", ErrInvalid)
	}
	seen := make(map[string]struct{}, len(s.Steps))
	for _, step := range s.Steps {
		planStep := PlanStep{ID: step.ID, Type: step.Type, SkillID: step.SkillID, SkillVersion: step.SkillVersion, ConnectionID: step.ConnectionID,
			Inputs: step.Inputs, ExpectedOutput: step.ExpectedOutput, Tier: step.Tier, Destination: step.Destination,
			DestinationConfirmed: step.DestinationConfirmed, Wait: step.Wait}
		if err := validateStep(planStep); err != nil {
			return fmt.Errorf("%w: historical plan step identity is invalid", ErrInvalid)
		}
		if _, ok := seen[step.ID]; ok {
			return fmt.Errorf("%w: duplicate historical plan step %q", ErrInvalid, step.ID)
		}
		seen[step.ID] = struct{}{}
	}
	if digestPlanIdentity(s.Revision, s.Steps) != s.Digest {
		return fmt.Errorf("%w: historical plan snapshot digest mismatch", ErrInvalid)
	}
	return nil
}
