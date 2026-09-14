package application

import (
	"context"
	"slices"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// align029Frontier reads the durable frontier and runtime status of the
// workflow instance started for an intent.
func (h *promoux015Harness) align029Frontier(intentID string) (string, []string) {
	h.t.Helper()
	var status string
	var frontier []string
	err := h.pool.QueryRow(context.Background(), `
		SELECT wi.runtime_status, wi.current_node_ids
		FROM workflow_instance wi
		WHERE wi.instance_id = (SELECT instance_id FROM workflow_instance ORDER BY created_at DESC LIMIT 1)`).Scan(&status, &frontier)
	if err != nil {
		h.t.Fatalf("read frontier for %s: %v", intentID, err)
	}
	return status, frontier
}

// TestTodo_ALIGN_029_Integration drives a real promotion through the composed
// cell and proves the product stage every surface reads is the projection of
// the durable workflow frontier at each step, through to the effective-date
// wait.
func TestTodo_ALIGN_029_Integration(t *testing.T) {
	h := promoux015Compose(t)
	req, _ := h.discoverPromotion("hiring-manager")
	proposed, err := h.client.ProposeJourney(h.rpc("hiring-manager"), req)
	if err != nil {
		t.Fatal(err)
	}
	id := proposed.GetJourney().GetIntentId()
	stage := func() journeyv1.JourneyStage {
		detail, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: id})
		if err != nil {
			t.Fatalf("InspectJourney: %v", err)
		}
		return detail.GetDetail().GetJourney().GetStage()
	}
	if got := stage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED {
		t.Fatalf("before execution stage = %s", got)
	}
	steps := []struct {
		act      func()
		node     string
		expected journeyv1.JourneyStage
	}{
		{func() {
			if _, err := h.client.ExecuteJourney(h.rpc("admin"), &journeyv1.ExecuteJourneyRequest{IntentId: id}); err != nil {
				t.Fatal(err)
			}
		}, "approve_finance", journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL},
		{func() {
			if _, err := h.client.DecideJourney(h.rpc("finance-partner"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "finance"}); err != nil {
				t.Fatal(err)
			}
		}, "approve_manager", journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL},
		{func() {
			if _, err := h.client.DecideJourney(h.rpc("admin"), &journeyv1.DecideJourneyRequest{IntentId: id, Approve: true, Reason: "manager"}); err != nil {
				t.Fatal(err)
			}
		}, "wait_effective_date", journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE},
	}
	for _, s := range steps {
		s.act()
		status, frontier := h.align029Frontier(id)
		if !slices.Contains(frontier, s.node) {
			t.Fatalf("durable frontier = %s %v, want it at %s", status, frontier, s.node)
		}
		if got := stage(); got != s.expected {
			t.Fatalf("with the durable frontier at %s the product stage = %s, want %s", s.node, got, s.expected)
		}
	}
}

// TestTodo_ALIGN_029_Security proves the lifecycle projection is disclosed
// only through authorized reads: a persona that may not inspect the journey
// gets no stage, and an unknown intent reveals nothing.
func TestTodo_ALIGN_029_Security(t *testing.T) {
	h := promoux015Compose(t)
	id := h.proposeAndExecute()
	if _, err := h.client.InspectJourney(h.rpc("admin"), &journeyv1.InspectJourneyRequest{IntentId: "00000000-0000-7000-8000-000000000002"}); err == nil {
		t.Error("an unknown journey was inspectable")
	}
	disclosed := 0
	for persona := range h.tokens {
		detail, err := h.client.InspectJourney(h.rpc(persona), &journeyv1.InspectJourneyRequest{IntentId: id})
		if err != nil {
			continue
		}
		disclosed++
		if detail.GetDetail().GetJourney().GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL {
			t.Errorf("%s sees stage %s, want the one durable projection", persona, detail.GetDetail().GetJourney().GetStage())
		}
	}
	if disclosed == 0 || disclosed == len(h.tokens) {
		t.Fatalf("stage disclosed to %d of %d personas; want some but not all", disclosed, len(h.tokens))
	}
}
