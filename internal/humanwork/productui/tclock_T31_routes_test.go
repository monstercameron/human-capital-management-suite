package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTClockT31Routes(t *testing.T) {
	for _, key := range []string{"page.timecard.title", "page.time_schedule.title", "page.time_devices.title"} {
		if timeCopyEN[key] == "" || timeCopyDE[key] == "" || timeCopyAR[key] == "" {
			t.Fatalf("missing localized title %q", key)
		}
		if timeCopyDE[key] == timeCopyEN[key] || timeCopyAR[key] == timeCopyEN[key] {
			t.Fatalf("title %q was not translated", key)
		}
	}
	tests := []struct {
		page  PageID
		route string
		h1    string
	}{
		{PageTimecard, "/workspace/app/time/timecard", "My timecard"},
		{PageCrewSchedule, "/workspace/app/time/schedule", "Crew schedule"},
		{PageClockDevices, "/workspace/app/admin/time/devices", "Time clock devices"},
	}
	for _, test := range tests {
		t.Run(string(test.page), func(t *testing.T) {
			definition, ok := LookupPage(test.page)
			if !ok || definition.Route != test.route || !definition.OwnsHeading {
				t.Fatalf("page definition = %+v, found=%v", definition, ok)
			}
			doc, err := ui.RenderToString(BuildPageContent(testView(test.page)))
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if strings.Count(doc, "<h1") != 1 || !strings.Contains(doc, test.h1) {
				t.Fatalf("heading contract failed: %q", doc)
			}
		})
	}
}
