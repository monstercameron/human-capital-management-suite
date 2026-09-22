package workflow

import (
	"encoding/json"
	"fmt"
)

// DecodeCanonicalPlan rehydrates a compiled plan from the deterministic JSON
// bytes a version record stores (version.CanonicalPlanBytes). The digest is
// recomputed from the decoded content so the returned plan verifies and pins
// exactly like the plan it was rendered from. It is the only way a value this
// package did not compile in-process gets a digest.
func DecodeCanonicalPlan(data []byte) (*CompiledWorkflow, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("workflow: decode canonical plan: no bytes supplied")
	}
	var plan CompiledWorkflow
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("workflow: decode canonical plan: %w", err)
	}
	if plan.WorkflowID == "" {
		return nil, fmt.Errorf("workflow: decode canonical plan: decoded plan names no workflow")
	}
	plan.digest = computePlanDigest(&plan)
	return &plan, nil
}
