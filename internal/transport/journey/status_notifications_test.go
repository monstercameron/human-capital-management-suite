package journey_test

import (
	"context"
	"errors"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/notifyplan"
)

type statusNotificationEngine struct {
	*notificationEngine
	failStatus bool
}

func (e *statusNotificationEngine) WorkflowStatusNotifications(_ context.Context, visible []workspace.JourneySummary) ([]workspace.WorkflowNotification, error) {
	if e.failStatus {
		return nil, errors.New("inbox unavailable")
	}
	// Newer than the approval notice the embedded engine returns.
	return []workspace.WorkflowNotification{{ID: "status", JourneyID: visible[0].IntentID, WorkerName: "Employee",
		Purpose: notifyplan.PurposeUpdate, Status: notifyplan.StatusFinishedApproved, CreatedAt: time.Date(2026, 9, 21, 13, 0, 0, 0, time.UTC)}}, nil
}

// TestTodo_WF_NOTIFY_001_Transport proves status notices ride the existing message,
// newest first, and that a failed status read never serves a partial list.
func TestTodo_WF_NOTIFY_001_Transport(t *testing.T) {
	for _, fail := range []bool{false, true} {
		engine := &statusNotificationEngine{notificationEngine: &notificationEngine{fakeEngine: newFakeEngine()}, failStatus: fail}
		client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
		resp, err := client.ListJourneys(testContext(t), &journeyv1.ListJourneysRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if fail {
			if !resp.NotificationsUnavailable || len(resp.Notifications) != 0 {
				t.Fatalf("failed status read served a partial list: %+v", resp.Notifications)
			}
			continue
		}
		if resp.NotificationsUnavailable || len(resp.Notifications) != 2 {
			t.Fatalf("notifications=%+v unavailable=%v", resp.Notifications, resp.NotificationsUnavailable)
		}
		first, second := resp.Notifications[0], resp.Notifications[1]
		if first.Purpose != notifyplan.PurposeUpdate || first.Status != notifyplan.StatusFinishedApproved || first.WorkItemId != "" || second.Purpose != "APPROVAL" {
			t.Fatalf("order or shape: %+v then %+v", first, second)
		}
	}
}
