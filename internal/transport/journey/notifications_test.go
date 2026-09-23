package journey_test

import (
	"context"
	"errors"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

type notificationEngine struct {
	*fakeEngine
	fail bool
	seen int
}

func (e *notificationEngine) WorkflowNotifications(_ context.Context, visible []workspace.JourneySummary) ([]workspace.WorkflowNotification, error) {
	e.seen = len(visible)
	if e.fail {
		return nil, errors.New("inbox unavailable")
	}
	return []workspace.WorkflowNotification{{ID: "notice", JourneyID: visible[0].IntentID, WorkItemID: "item", WorkerName: "Employee", Purpose: "APPROVAL", Status: "ASSIGNED", CreatedAt: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}}, nil
}

func TestTodo_NAAS_001(t *testing.T) {
	for _, fail := range []bool{false, true} {
		engine := &notificationEngine{fakeEngine: newFakeEngine(), fail: fail}
		client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
		resp, err := client.ListJourneys(testContext(t), &journeyv1.ListJourneysRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Journeys) != 1 || engine.seen != 1 {
			t.Fatal("inbox lost authorized journey projection")
		}
		if resp.NotificationsUnavailable != fail {
			t.Fatal("inbox availability was misrepresented")
		}
		if !fail && (len(resp.Notifications) != 1 || resp.Notifications[0].Status != "ASSIGNED" || resp.Notifications[0].CreatedAt == nil) {
			t.Fatalf("wire notice=%+v", resp.Notifications)
		}
		if fail && len(resp.Notifications) != 0 {
			t.Fatal("failed inbox produced notices")
		}
	}
}
