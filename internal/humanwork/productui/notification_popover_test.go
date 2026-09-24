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
		`aria-label="Work overview, 1 promotion item needs your action."`,
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
	want := `aria-label="` + view.Locale.Text("shell.work_overview") + ", " + view.Locale.Plural("shell.work_count", 1) + `"`
	if !strings.Contains(doc, want) {
		t.Fatalf("notification summary missing live actionable count %q", want)
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
