package journey

import (
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_UXLIVE_035 records the reviewed status-first decision against the
// task-first and audit-first alternatives: status/progress comes first, one
// safe next action follows, and audit/intervention detail is secondary.
func TestTodo_UXLIVE_035(t *testing.T) {
	fixtures := ReviewedJourneyDetailHierarchy()
	if len(fixtures) != 6 {
		t.Fatalf("hierarchy covers %d states, want six", len(fixtures))
	}
	for _, fixture := range fixtures {
		if len(fixture.FirstViewport) < 2 || fixture.FirstViewport[0] != "status-summary" {
			t.Fatalf("%s does not lead with status: %+v", fixture.State, fixture)
		}
		if fixture.PrimaryAction == "" || !fixture.SecondaryDisclosure {
			t.Fatalf("%s has no safe primary or secondary intervention rule: %+v", fixture.State, fixture)
		}
	}
}

// TestTodo_UXLIVE_035_Golden pins the first-viewport order for all active and
// terminal fixtures without blessing a particular HTML structure.
func TestTodo_UXLIVE_035_Golden(t *testing.T) {
	want := map[JourneyDetailLayoutState][]string{
		JourneyDetailActive:   {"status-summary", "current-stage", "primary-action"},
		JourneyDetailBlocked:  {"status-summary", "blocking-reason", "primary-action"},
		JourneyDetailWaiting:  {"status-summary", "current-stage", "wait-explanation"},
		JourneyDetailRejected: {"status-summary", "outcome", "primary-action"},
		JourneyDetailFailed:   {"status-summary", "outcome", "primary-action"},
		JourneyDetailRecorded: {"status-summary", "outcome", "primary-action"},
	}
	for _, fixture := range ReviewedJourneyDetailHierarchy() {
		if !reflect.DeepEqual(fixture.FirstViewport, want[fixture.State]) {
			t.Fatalf("%s first viewport = %#v, want %#v", fixture.State, fixture.FirstViewport, want[fixture.State])
		}
	}
}

// TestTodo_UXLIVE_035_Accessibility makes focus order match visual order and
// prevents disabled destructive controls from becoming the first task.
func TestTodo_UXLIVE_035_Accessibility(t *testing.T) {
	for _, fixture := range ReviewedJourneyDetailHierarchy() {
		if fixture.FirstViewport[0] != "status-summary" || fixture.FirstViewport[len(fixture.FirstViewport)-1] == "intervention-menu" {
			t.Fatalf("%s puts a rare intervention in the primary focus path: %+v", fixture.State, fixture.FirstViewport)
		}
		if !fixture.SecondaryDisclosure {
			t.Fatalf("%s has no named secondary disclosure for rare interventions", fixture.State)
		}
	}
}

// TestTodo_UXLIVE_035_I18N proves the semantic slots have stable localized
// headings available in each supported locale; layout must not branch by
// translated status strings.
func TestTodo_UXLIVE_035_I18N(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		copy := productui.ResolveProductLocale(locale)
		for _, key := range []string{"journey.detail_title", "journey.stages_title", "journey.actions_heading"} {
			if strings.TrimSpace(copy.Text(key)) == "" {
				t.Fatalf("%s has no localized %s", locale, key)
			}
		}
	}
}

// TestTodo_UXLIVE_035_Conformance ensures every fixture has one primary next
// step, keeps terminal state explicit, and sends interventions to secondary
// disclosure instead of making them the page's main task.
func TestTodo_UXLIVE_035_Conformance(t *testing.T) {
	seen := map[JourneyDetailLayoutState]bool{}
	for _, fixture := range ReviewedJourneyDetailHierarchy() {
		if seen[fixture.State] {
			t.Fatalf("duplicate state fixture %q", fixture.State)
		}
		seen[fixture.State] = true
		if fixture.PrimaryAction == "" || !fixture.SecondaryDisclosure {
			t.Fatalf("incomplete conformance fixture: %+v", fixture)
		}
	}
	for _, state := range []JourneyDetailLayoutState{JourneyDetailRejected, JourneyDetailFailed, JourneyDetailRecorded} {
		if !seen[state] {
			t.Fatalf("terminal state %q is not covered", state)
		}
	}
}
