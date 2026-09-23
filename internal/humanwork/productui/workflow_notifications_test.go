package productui

import (
	"strings"
	"testing"
)

func TestTodo_NAAS_001(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := testView(PageWork)
		view.Locale = ResolveProductLocale(locale)
		view.WorkflowNotifications = []WorkflowNotification{{ID: "notice-1", JourneyID: "journey-1", WorkerName: "<unsafe>", Purpose: "APPROVAL", Status: "ASSIGNED"}, {ID: "notice-2", JourneyID: "journey-2", WorkerName: "Employee two", Purpose: "TASK", Status: "COMPLETED"}}
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"workflow-notifications", "journey=journey-1", "journey=journey-2", "&lt;unsafe&gt;", view.Locale.Text("notifications.approval"), view.Locale.Text("notifications.completed")} {
			if !strings.Contains(doc, want) {
				t.Errorf("%s missing %q", locale, want)
			}
		}
		if strings.Contains(doc, "<unsafe>") {
			t.Fatal("notification content was not escaped")
		}
	}
}

func TestWorkflowNotificationStates(t *testing.T) {
	for _, tc := range []struct {
		name, status, want string
		unavailable, empty bool
	}{
		{name: "completed approval", status: "COMPLETED", want: "Your decision is recorded"},
		{name: "cancelled", status: "CANCELLED", want: "Cancelled"},
		{name: "expired", status: "EXPIRED", want: "Expired"},
		{name: "unknown", status: "FUTURE_STATE", want: "Status unavailable"},
		{name: "empty", empty: true, want: "No workflow notifications yet."},
		{name: "failed read", unavailable: true, want: "Notifications are temporarily unavailable."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := testView(PageWork)
			view.NotificationsUnavailable = tc.unavailable
			if !tc.empty {
				view.WorkflowNotifications = []WorkflowNotification{{ID: "notice-state", JourneyID: "state-journey", WorkerName: "Recipient-only worker", Purpose: "APPROVAL", Status: tc.status}}
			}
			doc := renderNotificationSubtree(t, view)
			if !strings.Contains(doc, tc.want) {
				t.Fatalf("missing %q in %s", tc.want, doc)
			}
			if tc.unavailable && strings.Contains(doc, "Recipient-only worker") {
				t.Fatal("unavailable inbox rendered stale recipient data")
			}
		})
	}
}
