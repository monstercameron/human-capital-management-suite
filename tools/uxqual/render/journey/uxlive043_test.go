package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXLIVE_043(t *testing.T) {
	markup, err := ui.RenderToString(detailView(Page{Locale: "en-US"}, DetailView{
		Journey:       JourneyCard{Stage: "OBSERVING_EFFECTS", StageLabel: "Checking downstream effects"},
		OutcomeReason: &OutcomeReason{Stage: "Rejected", Summary: "A typed reason", NextStep: "A safe next step"},
		Progress:      &Progress{Phase: "Checking downstream effects", Summary: "The workflow is running", NextAction: "It continues automatically"},
		Actions:       []Action{{ID: "approve", Label: "Approve", Variant: "primary"}, {ID: "edit", Label: "Edit proposal", Variant: "secondary"}},
		Steps:         []Step{{ID: "proposal", Label: "Proposal", State: stepDone}, {ID: "recorded", Label: "Recorded", State: stepActive}},
	}))
	if err != nil {
		t.Fatalf("render status-first detail: %v", err)
	}
	for _, want := range []string{"outcome-reason", "jn-progress-summary", "jn-actions", "jn-stepper", "jn-interventions"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("detail omitted %q:\n%s", want, markup)
		}
	}
	if !(strings.Index(markup, "outcome-reason") < strings.Index(markup, "jn-progress-summary") && strings.Index(markup, "jn-progress-summary") < strings.Index(markup, "jn-actions") && strings.Index(markup, "jn-actions") < strings.Index(markup, "jn-stepper")) {
		t.Fatalf("visual and DOM order is not status-first:\n%s", markup)
	}
}

func TestTodo_UXLIVE_043_Golden(t *testing.T) {
	markup, err := ui.RenderToString(progressSummarySectionLocale("en-US", &Progress{Phase: "Final checks", Summary: "The workflow is running", NextAction: "It continues automatically"}))
	if err != nil || !strings.Contains(markup, `id="progress-heading"`) || !strings.Contains(markup, "Final checks") {
		t.Fatalf("progress golden composition drifted: %v\n%s", err, markup)
	}
}

func TestTodo_UXLIVE_043_Browser(t *testing.T) {
	markup, err := ui.RenderToString(detailView(Page{Locale: "de-DE"}, DetailView{Journey: JourneyCard{Stage: "RECORDED", Closed: true}, Steps: []Step{{ID: "recorded", Label: "Erfasst", State: stepDone}}}))
	if err != nil || !strings.Contains(markup, "Erfasst") || strings.Contains(markup, "jn-interventions") {
		t.Fatalf("terminal browser composition retained irrelevant interventions: %v\n%s", err, markup)
	}
}

func TestTodo_UXLIVE_043_Accessibility(t *testing.T) {
	markup, err := ui.RenderToString(progressSummarySectionLocale("ar", &Progress{Phase: "الفحوص", Summary: "جارٍ التحقق", NextAction: "سيستمر تلقائيًا"}))
	if err != nil || !strings.Contains(markup, `aria-live="polite"`) || !strings.Contains(markup, `id="progress-heading"`) {
		t.Fatalf("progress announcement is not accessible: %v\n%s", err, markup)
	}
}

func TestTodo_UXLIVE_043_I18N(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(outcomeReasonSectionLocale(locale, DetailView{OutcomeReason: &OutcomeReason{Stage: "Blocked", Summary: "reason", NextStep: "next"}}))
		if err != nil || !strings.Contains(markup, "reason") || !strings.Contains(markup, "next") {
			t.Fatalf("%s outcome reason did not render: %v\n%s", locale, err, markup)
		}
	}
}

func TestTodo_UXLIVE_043_Regression(t *testing.T) {
	markup, err := ui.RenderToString(detailView(Page{Locale: "en-US"}, DetailView{Journey: JourneyCard{Closed: true}, Actions: []Action{{ID: "edit", Label: "Edit", Variant: "secondary", Disabled: true}}}))
	if err != nil {
		t.Fatalf("render terminal detail: %v", err)
	}
	if strings.Contains(markup, "Edit") {
		t.Fatalf("terminal detail exposed a disabled rare intervention:\n%s", markup)
	}
}
