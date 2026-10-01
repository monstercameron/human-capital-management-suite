package productclient

import (
	"context"
	"errors"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	notificationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/notification/v1"
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

func TestNotificationReadHandlerResolvesVersionAndDoesNotBlindRetry(t *testing.T) {
	var marked *notificationv1.MarkNotificationReadRequest
	service := Service{
		ListNotifications: func(context.Context, *notificationv1.ListNotificationsRequest) (*notificationv1.ListNotificationsResponse, error) {
			return &notificationv1.ListNotificationsResponse{Notifications: []*notificationv1.Notification{{Id: "notice-1", ReadState: "UNREAD", Version: 7}}}, nil
		},
		MarkNotificationRead: func(_ context.Context, request *notificationv1.MarkNotificationReadRequest) (*notificationv1.MarkNotificationReadResponse, error) {
			marked = request
			return &notificationv1.MarkNotificationReadResponse{}, nil
		},
	}
	done := make(chan error, 1)
	notificationReadHandler(context.Background(), service)("notice-1", func(err error) { done <- err })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if marked == nil || marked.GetId() != "notice-1" || marked.GetExpectedVersion() != 7 {
		t.Fatalf("mark request = %+v, want notice-1 at version 7", marked)
	}

	marked = nil
	service.ListNotifications = func(context.Context, *notificationv1.ListNotificationsRequest) (*notificationv1.ListNotificationsResponse, error) {
		return &notificationv1.ListNotificationsResponse{Notifications: []*notificationv1.Notification{{Id: "notice-1", ReadState: "READ", Version: 8}}}, nil
	}
	done = make(chan error, 1)
	notificationReadHandler(context.Background(), service)("notice-1", func(err error) { done <- err })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if marked != nil {
		t.Fatal("already-read notification triggered a blind mutation")
	}
}

func TestNotificationReadHandlerPagesBeforeMarking(t *testing.T) {
	var calls int
	var marked *notificationv1.MarkNotificationReadRequest
	service := Service{
		ListNotifications: func(_ context.Context, request *notificationv1.ListNotificationsRequest) (*notificationv1.ListNotificationsResponse, error) {
			calls++
			if request.GetCursor() == "" {
				return &notificationv1.ListNotificationsResponse{Notifications: []*notificationv1.Notification{{Id: "first", ReadState: "UNREAD", Version: 1}}, NextCursor: "next"}, nil
			}
			return &notificationv1.ListNotificationsResponse{Notifications: []*notificationv1.Notification{{Id: "target", ReadState: "UNREAD", Version: 9}}}, nil
		},
		MarkNotificationRead: func(_ context.Context, request *notificationv1.MarkNotificationReadRequest) (*notificationv1.MarkNotificationReadResponse, error) {
			marked = request
			return &notificationv1.MarkNotificationReadResponse{}, nil
		},
	}
	done := make(chan error, 1)
	notificationReadHandler(context.Background(), service)("target", func(err error) { done <- err })
	if err := <-done; err != nil || calls != 2 {
		t.Fatalf("completion=%v list calls=%d, want two pages and success", err, calls)
	}
	if marked == nil || marked.GetExpectedVersion() != 9 {
		t.Fatalf("mark request=%+v, want target version 9", marked)
	}
}
