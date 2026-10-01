package journeyclient

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
)

// TestTodo_UXBLIND_082 proves the authorized worker projection repairs a
// short stored name consistently in the Journeys list and detail header.
func TestTodo_UXBLIND_082(t *testing.T) {
	h := newHarness(t)
	worker := &journeyv1.Worker{WorkerRef: "omar-reyes", WorkerId: "worker-omar", PreferredName: "Omar", LegalName: "Omar Reyes"}
	h.svc.workers = []*journeyv1.Worker{worker}
	listed := testJourney(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
	listed.WorkerRef, listed.WorkerName = worker.GetWorkerRef(), "Omar"
	h.svc.list = []*journeyv1.Journey{listed}
	h.app.Start(context.Background(), "")
	page := h.awaitPage(t, "the named journey list", listLoaded)
	if got := page.List.Journeys[0].WorkerName; got != "Omar Reyes" {
		t.Fatalf("journey list worker name = %q, want Omar Reyes", got)
	}

	h.svc.detail = &journeyv1.JourneyDetail{Journey: listed}
	h.app.OnHashChange(DetailHref(listed.GetIntentId()))
	page = h.awaitPage(t, "the named journey detail", detailShown)
	if got := page.Detail.Journey.WorkerName; got != "Omar Reyes" {
		t.Fatalf("journey detail worker name = %q, want Omar Reyes", got)
	}
}
