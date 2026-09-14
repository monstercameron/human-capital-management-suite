package productclient

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestPromotionJourneyWorkerAliasesShareDirectoryIdentity(t *testing.T) {
	workers := []*journeyv1.Worker{{WorkerRef: "hc-060-isaac-ward", WorkerId: "44444444-4444-4444-8444-444444444444"}}
	for _, ref := range []string{
		"hc-060-isaac-ward",
		"44444444-4444-4444-8444-444444444444",
		"eref:v1:harborcare-demo:worker:44444444-4444-4444-8444-444444444444",
	} {
		t.Run(ref, func(t *testing.T) {
			work := []productui.WorkItem{{PersonRef: ref, Terminal: false}}
			linkJourneyWorkers(work, workers, "harborcare-demo")
			if work[0].PersonRef != workers[0].WorkerRef {
				t.Fatalf("linked person = %q", work[0].PersonRef)
			}
			if !activePromotionConflicts(work)[workers[0].WorkerRef] {
				t.Fatal("blocked promotion was offered as a new start")
			}
		})
	}
	unknown := []productui.WorkItem{{PersonRef: "another-worker", Terminal: false}}
	linkJourneyWorkers(unknown, workers, "harborcare-demo")
	if unknown[0].PersonRef != "another-worker" {
		t.Fatalf("unadmitted journey rebound to %q", unknown[0].PersonRef)
	}
	foreign := []productui.WorkItem{{PersonRef: "eref:v1:other-tenant:worker:44444444-4444-4444-8444-444444444444", Terminal: false}}
	linkJourneyWorkers(foreign, workers, "harborcare-demo")
	if foreign[0].PersonRef != "eref:v1:other-tenant:worker:44444444-4444-4444-8444-444444444444" {
		t.Fatalf("foreign tenant journey rebound to %q", foreign[0].PersonRef)
	}
}

func TestPromotionBlockedAliasIsNotOfferedAsNewWorkflow(t *testing.T) {
	state, err := ParseState("/workspace/app/people", "")
	if err != nil {
		t.Fatal(err)
	}
	const workerRef = "hc-060-isaac-ward"
	const workerID = "44444444-4444-4444-8444-444444444444"
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{Journeys: []*journeyv1.Journey{{
				IntentId: "existing-blocked", WorkerRef: "eref:v1:harborcare-demo:worker:" + workerID,
				Stage: journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED,
			}}}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{Workers: []*journeyv1.Worker{{
				WorkerRef: workerRef, WorkerId: workerID, JobCode: "ENG1", Grade: "P1", PayZone: "US", Currency: "USD",
				ManagerRelationship: rootManagerRelationship(),
			}}, Options: &journeyv1.WorkforceOptions{Currency: "USD", Placements: []*journeyv1.WorkforcePlacementOption{{JobCode: "ENG2", Grade: "P2", PayZone: "US", Currency: "USD"}}, PromotionPaths: []*journeyv1.PromotionPathOption{{SourceJobCode: "ENG1", SourceGrade: "P1", TargetJobCode: "ENG2", TargetGrade: "P2"}}}}, nil
		},
	}
	view, err := Load(context.Background(), service, Session{Tenant: "harborcare-demo"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.People) != 1 || view.People[0].PromotionAvailability != productui.PromotionActiveConflict {
		t.Fatalf("blocked request did not govern promotion availability: %+v", view.People)
	}
	if len(view.Work) != 1 || view.Work[0].PersonRef != workerRef {
		t.Fatalf("existing journey not linked to admitted worker: %+v", view.Work)
	}
}
