package productclient

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_REV_075_02_ClientProjection(t *testing.T) {
	session := Session{Tenant: "tenant-a", Principal: "subject-a", Scope: "scope-a", Roles: []string{"manager"}}
	state := State{Page: productui.PageReviewParticipants}
	baseline := LoadingView(session, state)
	calls := 0
	service := Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return &journeyv1.ListJourneysResponse{}, nil
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return &journeyv1.ListWorkersResponse{}, nil
		},
		GetReviewParticipants: func(context.Context) (*productui.ReviewParticipantsProjection, error) {
			calls++
			return &productui.ReviewParticipantsProjection{Cycles: []productui.ReviewParticipantsCycleProjection{{
				CycleID: "cycle-a", CycleRevision: 2, GraphRevision: 1, GraphDigest: "digest",
				Assignments: []productui.ReviewParticipantAssignmentProjection{{ParticipantID: "worker-a", ReviewerID: "worker-b", Relationship: "MANAGER"}},
			}}}, nil
		},
	}
	view, err := LoadWithBaseline(context.Background(), service, session, state, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || view.ReviewParticipants == nil || len(view.ReviewParticipants.Cycles) != 1 || view.ReviewParticipants.Cycles[0].Assignments[0].ReviewerID != "worker-b" {
		t.Fatalf("read calls=%d projection=%+v", calls, view.ReviewParticipants)
	}
	otherPage := State{Page: productui.PageHome}
	view, err = LoadWithBaseline(context.Background(), service, session, otherPage, view)
	if err != nil {
		t.Fatal(err)
	}
	if view.ReviewParticipants != nil {
		t.Fatal("review-participant data leaked to another page")
	}
}
