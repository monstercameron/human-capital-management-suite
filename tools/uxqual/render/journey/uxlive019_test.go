package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UXLIVE-019's RED was measured on the running server: the stepper's status
// words live in span.jn-visually-hidden, so assistive technology heard
// "Finance review - Did not complete" while a sighted reader saw a numbered
// disc whose only difference from the next one was its hue. Completed steps
// already carried a check glyph; the stopped step did not.
//
// The fix gives the stopped step its own glyph and states its status in
// visible text, and keeps exactly one copy of that status in the
// accessibility tree so a screen reader does not hear it twice.

func uxlive019Step(t *testing.T, s Step, index int) string {
	t.Helper()
	markup, err := ui.RenderToString(stepNodeLocale("en-US", index, s))
	if err != nil {
		t.Fatalf("render step: %v", err)
	}
	return markup
}

// TestTodo_UXLIVE_019 is the primary red/green test: a stopped step is
// distinguishable without colour.
func TestTodo_UXLIVE_019(t *testing.T) {
	stopped := uxlive019Step(t, Step{ID: "effective-date", Label: "Effective date", State: stepFailed, Detail: "The effective-date checks need attention."}, 3)
	upcoming := uxlive019Step(t, Step{ID: "recorded", Label: "Recorded", State: stepUpcoming, Detail: "The promotion is recorded after the effective date."}, 4)

	if !strings.Contains(stopped, "jn-stepstate") {
		t.Fatalf("a stopped step states its status only in colour:\n%s", stopped)
	}
	if strings.Contains(upcoming, "jn-stepstate") {
		t.Fatalf("an ordinary upcoming step should not carry a status badge:\n%s", upcoming)
	}
	if !strings.Contains(stopped, "jn-stepdanger") {
		t.Fatalf("a stopped step keeps the same numbered disc as an unstarted one:\n%s", stopped)
	}

	// Measured live: naming a second danger colour here rendered the badge
	// at 1.11:1 against the card in dark mode. It inherits the step label's
	// own themed colour instead.
	rule := cssRule(t, Stylesheet(), ".jn-stepstate")
	if !strings.Contains(rule, "color:inherit") && !strings.Contains(rule, "color: inherit") {
		t.Fatalf(".jn-stepstate names its own colour instead of the step's: %q", rule)
	}

	word := stepStateWordLocale("en-US", stepFailed)
	if strings.Count(stopped, word) != 1 {
		t.Fatalf("status word %q appears %d times; a screen reader hears it once, a reader sees it once:\n%s",
			word, strings.Count(stopped, word), stopped)
	}
	if strings.Contains(stopped, `class="jn-visually-hidden"`) && strings.Contains(stopped, "jn-visually-hidden\">"+word) {
		t.Fatalf("the visible status is still duplicated into the hidden one:\n%s", stopped)
	}
}

// TestTodo_UXLIVE_019_Browser keeps every other step's accessible status
// exactly where it was: the fix must not remove the words screen readers
// already relied on.
func TestTodo_UXLIVE_019_Browser(t *testing.T) {
	for _, state := range []string{stepDone, stepActive, stepUpcoming} {
		markup := uxlive019Step(t, Step{ID: "finance-review", Label: "Finance review", State: state}, 1)
		word := stepStateWordLocale("en-US", state)
		if !strings.Contains(markup, word) {
			t.Fatalf("%s step lost its accessible status %q:\n%s", state, word, markup)
		}
		if strings.Count(markup, word) != 1 {
			t.Fatalf("%s step announces %q %d times", state, word, strings.Count(markup, word))
		}
	}

	full, err := ui.RenderToString(stepperSectionLocale("en-US", []Step{
		{ID: "proposal", Label: "Proposal", State: stepDone},
		{ID: "finance-review", Label: "Finance review", State: stepDone},
		{ID: "manager-review", Label: "Manager review", State: stepDone},
		{ID: "effective-date", Label: "Effective date", State: stepFailed},
		{ID: "recorded", Label: "Recorded", State: stepUpcoming},
	}))
	if err != nil {
		t.Fatalf("render stepper: %v", err)
	}
	if strings.Count(full, "jn-stepstate") != 1 {
		t.Fatalf("the stepper marks %d stopped steps, a run stops once:\n%s", strings.Count(full, "jn-stepstate"), full)
	}
}
