package journey

import (
	"strings"
	"testing"
)

// TestTodo_UXBLIND_078_Browser proves the short Journeys tracker keeps search
// and Status visible while the secondary controls use an accessible Filters
// disclosure, then leaves every filter visible once the list is larger.
func TestTodo_UXBLIND_078_Browser(t *testing.T) {
	short := uxlive031Render(t, ListView{Journeys: []JourneyCard{uxlive010Card("short")}, Filter: uxlive031Filter(false, 4, 4)})
	panel := strings.Index(short, `class="jn-journey-filter-panel"`)
	if panel < 0 {
		t.Fatalf("short tracker has no secondary filter panel:\n%s", short)
	}
	for _, visible := range []string{`name="journey_q"`, `name="journey_status"`} {
		if index := strings.Index(short, visible); index < 0 || index > panel {
			t.Fatalf("short tracker did not keep %s before Filters:\n%s", visible, short)
		}
	}
	if !strings.Contains(short, `class="jn-btn jn-journey-filter-toggle"`) || !strings.Contains(short, `aria-controls="journey-filter-panel"`) {
		t.Fatalf("short tracker has no accessible Filters control:\n%s", short)
	}
	for _, secondary := range []string{`name="journey_from"`, `name="journey_to"`, `name="journey_sort"`, `name="journey_group"`} {
		if index := strings.Index(short, secondary); index < panel {
			t.Fatalf("secondary control %s escaped the collapsed panel:\n%s", secondary, short)
		}
	}

	large := uxlive031Render(t, ListView{Journeys: []JourneyCard{uxlive010Card("large")}, Filter: uxlive031Filter(false, 6, 6)})
	if strings.Contains(large, "journey-filter-toggle") || !strings.Contains(large, `data-open="true"`) {
		t.Fatalf("large tracker did not expand its secondary filters:\n%s", large)
	}
}
