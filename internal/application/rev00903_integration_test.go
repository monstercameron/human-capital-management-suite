package application

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// TestTodo_REV_009_03_Integration proves the served Journey inspector follows
// an executed instance through inspect.Load and returns its durable manifest.
func TestTodo_REV_009_03_Integration(t *testing.T) {
	h := promoux015Compose(t)
	intentID := h.proposeAndExecute()

	response, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: intentID})
	if err != nil {
		t.Fatalf("InspectJourney on executed instance: %v", err)
	}
	detail := response.GetDetail()
	if detail == nil || detail.GetInstance() == nil || detail.GetInstance().GetInstanceId() == "" {
		t.Fatalf("executed journey has no durable instance: %+v", detail)
	}

	manifest := make(map[string]*journeyv1.DurableRecordFamilyProfile, len(detail.GetDurableRecords()))
	for _, profile := range detail.GetDurableRecords() {
		manifest[profile.GetFamily()] = profile
	}
	instance := manifest["workflow_instance"]
	if instance == nil || instance.GetState() != "LOADED" || instance.GetCount() != 1 {
		t.Fatalf("instance family = %+v, want loaded instance row", instance)
	}
	nodes := manifest["workflow_node_execution"]
	if nodes == nil || nodes.GetState() != "LOADED" || nodes.GetCount() == 0 {
		t.Fatalf("node execution family = %+v, want loaded executions", nodes)
	}
	for _, family := range []string{
		"workflow_timer", "workflow_lease", "workflow_checkpoint", "outbox", "effect_reconciliation_job",
	} {
		profile := manifest[family]
		if profile == nil {
			t.Errorf("manifest omits traversed durable family %q", family)
			continue
		}
		switch profile.GetState() {
		case "LOADED", "NOT_RECORDED", "REDACTED", "INTEGRITY_FAILED", "UNAVAILABLE":
		default:
			t.Errorf("family %q has unknown state %q", family, profile.GetState())
		}
	}
}
