package productclient

import (
	"context"
	"errors"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestWorkflowNotificationsSurviveNavigationAndRefreshSafely(t *testing.T) {
	session := Session{Tenant: "harborcare", Principal: "worker-1", Scope: "employee"}
	baseline := productui.NewView(productui.PageHome, "Harborcare", "Worker 1", "Employee")
	baseline.WorkflowNotifications = []productui.WorkflowNotification{{ID: "old-notice", JourneyID: "old-journey"}}
	for _, tc := range []struct {
		name                     string
		page                     productui.PageID
		fail, unavailable, empty bool
		wantID                   string
		wantReads                int
	}{
		{name: "unrelated route retains shell", page: productui.PageSettings, wantID: "old-notice"},
		{name: "refresh replaces shell", page: productui.PageWork, wantID: "new-notice", wantReads: 1},
		{name: "empty replaces shell", page: productui.PageWork, empty: true, wantReads: 1},
		{name: "read failure clears shell", page: productui.PageWork, fail: true, wantReads: 1},
		{name: "inbox failure clears shell", page: productui.PageWork, unavailable: true, wantReads: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			service := Service{ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
				return &journeyv1.ListWorkersResponse{}, nil
			}, ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
				reads++
				if tc.fail {
					return nil, errors.New("read failed")
				}
				response := &journeyv1.ListJourneysResponse{NotificationsUnavailable: tc.unavailable}
				if !tc.empty && !tc.unavailable {
					response.Notifications = []*journeyv1.WorkflowNotification{nil, {NotificationId: "new-notice", JourneyId: "new-journey", WorkerName: "Employee", Purpose: "APPROVAL", Status: "COMPLETED"}}
				}
				return response, nil
			}}
			view, err := LoadWithBaseline(context.Background(), service, session, State{Page: tc.page, Request: productui.PageRequest{Page: tc.page}}, baseline)
			if (err != nil) != tc.fail || reads != tc.wantReads {
				t.Fatalf("error=%v reads=%d", err, reads)
			}
			if tc.wantID == "" {
				if len(view.WorkflowNotifications) != 0 {
					t.Fatalf("stale notices retained: %+v", view.WorkflowNotifications)
				}
			} else if len(view.WorkflowNotifications) != 1 || view.WorkflowNotifications[0].ID != tc.wantID {
				t.Fatalf("notices=%+v want %s", view.WorkflowNotifications, tc.wantID)
			}
			if view.NotificationsUnavailable != (tc.fail || tc.unavailable) {
				t.Fatal("incorrect inbox availability")
			}
		})
	}
}
