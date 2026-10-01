package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/notifyplan"
)

func TestTodo_UXBLIND_056(t *testing.T) {
	view := testView(PageWork)
	view.WorkflowNotifications = []WorkflowNotification{{
		ID: "notice-1", JourneyID: "journey-1", WorkerName: "Ana", Purpose: notifyplan.PurposeUpdate,
		Status: notifyplan.StatusSentForReview, CreatedAt: time.Date(2026, 9, 28, 13, 45, 0, 0, time.UTC),
	}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		">Notifications<", ">Ana<", "28 Sep 2026, 13:45 UTC", `datetime="2026-09-28T13:45:00Z"`,
		"Sent to a reviewer", "Unread", `notification-unread-count`,
		`aria-label="Request update from Ana, 28 Sep 2026, 13:45 UTC, Sent to a reviewer, Unread"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("notification panel missing %q", want)
		}
	}
	if got := unreadWorkflowNotifications(view.WorkflowNotifications); got != 1 {
		t.Fatalf("unread count = %d, want 1", got)
	}
	view.WorkflowNotifications[0].Read = true
	if got := unreadWorkflowNotifications(view.WorkflowNotifications); got != 0 {
		t.Fatalf("read notification remained unread: %d", got)
	}
	readDoc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readDoc, ">Read<") || strings.Contains(readDoc, "workflow-notification-row is-unread") {
		t.Fatal("read notification did not render its read state")
	}
}

func TestTodo_UXBLIND_056_Browser(t *testing.T) {
	view := testView(PageWork)
	view.WorkflowNotifications = []WorkflowNotification{{
		ID: "notice-browser", JourneyID: "journey-browser", WorkerName: "Ana", Purpose: notifyplan.PurposeUpdate,
		Status: notifyplan.StatusSentForReview, CreatedAt: time.Date(2026, 9, 28, 13, 45, 0, 0, time.UTC),
	}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-hcm-transient-popover="notification"`, `class="workflow-notification-row is-unread"`,
		`class="workflow-notification"`, JourneyDetailHref(view, "journey-browser"),
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("browser component contract missing %q", want)
		}
	}
	if strings.Contains(doc, `Work overview`) {
		t.Fatal("notification panel retained the old work-overview title")
	}
}

func TestTodo_UXBLIND_056_Accessibility(t *testing.T) {
	view := testView(PageWork)
	view.WorkflowNotifications = []WorkflowNotification{{
		ID: "notice-accessibility", JourneyID: "journey-accessibility", WorkerName: "Ana", Purpose: notifyplan.PurposeUpdate,
		Status: notifyplan.StatusSentForReview, CreatedAt: time.Date(2026, 9, 28, 13, 45, 0, 0, time.UTC),
	}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`aria-label="Notifications, 1 unread notification"`,
		`aria-label="Request update from Ana, 28 Sep 2026, 13:45 UTC, Sent to a reviewer, Unread"`,
		`aria-label="Workflow notifications"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("notification accessibility contract missing %q", want)
		}
	}
}

func TestTodo_UXBLIND_064(t *testing.T) {
	view := testView(PageHome)
	view.Viewer = ViewerProfile{Name: "Taylor Morgan", Initials: "TM", Role: "People partner"}
	view.Tenant = "Harborcare"
	view.LogoutHref = "/workspace/logout"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-hcm-transient-popover="account"`, ">Taylor Morgan<", ">Harborcare<",
		">Myself<", ">Settings<", ">Sign out<", `aria-label="Account menu"`,
		`href="/workspace/app/myself`, `href="/workspace/app/settings`, `href="/workspace/logout"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("account menu missing %q", want)
		}
	}
}

func TestTodo_UXBLIND_064_Browser(t *testing.T) {
	view := testView(PageHome)
	view.Viewer = ViewerProfile{Name: "Taylor Morgan", Initials: "TM"}
	view.LogoutHref = "/workspace/logout"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `class="popover-root account-menu network-slot network-slot-ready"`) ||
		!strings.Contains(doc, `class="viewer-profile-link"`) {
		t.Fatal("avatar is not the keyboard-operable account disclosure trigger")
	}
	if !strings.Contains(doc, `data-hcm-popover-grace-ms="180"`) {
		t.Fatal("account menu does not use the shared transient popover controller")
	}
}

func TestTodo_UXBLIND_064_Accessibility(t *testing.T) {
	view := testView(PageHome)
	view.Viewer = ViewerProfile{Name: "Taylor Morgan", Initials: "TM"}
	view.LogoutHref = "/workspace/logout"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `<nav aria-label="Account menu">`) {
		t.Fatal("account menu links have no named navigation region")
	}
	if strings.Contains(doc, `class="viewer-profile-link" href=`) {
		t.Fatal("avatar still renders as a direct profile link instead of a disclosure")
	}
}
