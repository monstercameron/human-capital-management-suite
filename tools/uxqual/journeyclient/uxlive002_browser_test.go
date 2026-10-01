package journeyclient

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// TestTodo_UXLIVE_002_Browser renders the actual detail stepper contract. The
// native render path is the browser-facing component tree used by the live
// WASM client, so the completed approval states cannot be hidden behind a
// projection-only assertion.
func TestTodo_UXLIVE_002_Browser(t *testing.T) {
	steps := stepsLocale("en-US", stageBlocked, uxlive002BothApprovalsDone(), "2026-07-01")
	markup, err := journey.RenderToString(journey.Page{
		Locale: "en-US",
		Detail: &journey.DetailView{Steps: steps},
	})
	if err != nil {
		t.Fatalf("render blocked stepper: %v", err)
	}
	for _, label := range []string{"Proposal", "Finance review", "Manager review"} {
		if !strings.Contains(markup, label) {
			t.Fatalf("rendered stepper lost %q:\n%s", label, markup)
		}
	}
	for _, want := range []struct{ label, state string }{
		{label: "Proposal", state: "Completed"},
		{label: "Finance review", state: "Completed"},
		{label: "Manager review", state: "Completed"},
		{label: "Effective date", state: "Did not complete"},
	} {
		at := strings.Index(markup, want.label)
		end := strings.Index(markup[at:], "</li>")
		if at < 0 || end < 0 || !strings.Contains(markup[at:at+end], want.state) {
			t.Fatalf("rendered %q step does not show %q:\n%s", want.label, want.state, markup)
		}
	}
}
