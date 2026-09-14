package productui

import (
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
