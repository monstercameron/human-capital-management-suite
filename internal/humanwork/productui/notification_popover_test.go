package productui

import (
	"regexp"
	"strings"
	"testing"
)

func TestNotificationMenuPublishesItsTransientPopoverBehavior(t *testing.T) {
	doc, err := Render(testView(PageWork))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="popover-root notifications network-slot network-slot-ready"`,
		`data-hcm-transient-popover="notification"`,
		`data-hcm-popover-grace-ms="180"`,
		// UXBLIND-056: the bell is a notification panel that announces its unread count.
		`aria-label="Notifications, 0 unread notifications"`,
		`class="popover-surface popover notification-popover"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("notification menu missing %q", want)
		}
	}
	for _, want := range []string{
		`class="popover-root locale-menu"`,
		`data-hcm-transient-popover="locale"`,
		`class="popover-surface popover locale-popover"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("locale menu did not use shared popover contract %q", want)
		}
	}
}

func TestUnreadWorkflowNotificationsWithOverridesUpdatesBellCount(t *testing.T) {
	items := []WorkflowNotification{{ID: "notice-1"}, {ID: "notice-2", Read: true}}
	if got := unreadWorkflowNotificationsWithOverrides(items, map[string]bool{"notice-1": true}); got != 0 {
		t.Fatalf("unread count with local read override = %d, want 0", got)
	}
	if got := unreadWorkflowNotificationsWithOverrides(items, map[string]bool{}); got != 1 {
		t.Fatalf("unread count without override = %d, want 1", got)
	}
}

func TestTodo_REV_064_02(t *testing.T) {
	view := testView(PageWork)
	actionable := view.Work[0]
	tracked := actionable
	tracked.ID = "tracked"
	tracked.ViewerResponsibility = "TRACKING"
	completed := actionable
	completed.ID = "completed"
	completed.Terminal = true
	view.Work = []WorkItem{actionable, tracked, completed}

	// Open lifecycle summaries still partition the stream, while the live
	// notification surface counts only work the viewer must act on.
	if len(OpenWorkItems(view.Work))+len(RecentWork(view.Work)) != len(view.Work) {
		t.Fatal("open and recent work no longer partition the admitted stream")
	}
	if got := len(ActionableWorkItems(view.Work)); got != 1 {
		t.Fatalf("actionable work = %d, want 1", got)
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	// UXBLIND-056: the bell announces unread notifications; the actionable
	// work count asserted above no longer feeds it.
	want := `aria-label="` + view.Locale.Text("shell.notifications") + ", " + view.Locale.Plural("notifications.unread_count", 0) + `"`
	if !strings.Contains(doc, want) {
		t.Fatalf("notification summary missing unread count %q", want)
	}
}

func TestTodo_NAAS_001_NotificationDisclosureAccessibility(t *testing.T) {
	css := PopoverStylesheet()
	rule := regexp.MustCompile(`\.popover-root\[open\]::details-content\{[^}]*\}`).FindString(css)
	if !strings.Contains(rule, "display:contents") || !strings.Contains(rule, "content-visibility:visible") {
		t.Fatal("floating notification text can disappear from the accessibility tree")
	}
	if strings.Contains(css, `.popover-root::details-content{display:contents`) {
		t.Fatal("closed notifications must retain native hiding")
	}
}
