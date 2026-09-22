package workflow_test

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestDecodeCanonicalPlanRoundTrip(t *testing.T) {
	plan := mustCompilePromotion(t)
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	raw = append(raw, '\n')
	decoded, err := workflow.DecodeCanonicalPlan(raw)
	if err != nil {
		t.Fatalf("DecodeCanonicalPlan: %v", err)
	}
	if decoded.Digest() != plan.Digest() {
		t.Fatalf("decoded digest = %s, want %s", decoded.Digest(), plan.Digest())
	}
	if decoded.WorkflowID != plan.WorkflowID || decoded.Version != plan.Version {
		t.Fatalf("decoded identity = %s v%d, want %s v%d",
			decoded.WorkflowID, decoded.Version, plan.WorkflowID, plan.Version)
	}
	if err := decoded.Verify(); err != nil {
		t.Fatalf("decoded plan does not verify: %v", err)
	}
}

func TestDecodeCanonicalPlanRefusals(t *testing.T) {
	if _, err := workflow.DecodeCanonicalPlan(nil); err == nil {
		t.Fatal("empty bytes decoded without error")
	}
	if _, err := workflow.DecodeCanonicalPlan([]byte("{not json")); err == nil {
		t.Fatal("malformed JSON decoded without error")
	}
	if _, err := workflow.DecodeCanonicalPlan([]byte(`{"workflow_id":"","version":1}`)); err == nil {
		t.Fatal("plan with no workflow id decoded without error")
	}
}
