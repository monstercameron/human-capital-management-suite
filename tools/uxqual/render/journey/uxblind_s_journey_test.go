package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_062(t *testing.T) {
	markup, err := ui.RenderToString(timelineSectionLocale("en-US", []TimelineEvent{
		{At: "12:00", Actor: "Loretta", Title: "Finance review: approved", Detail: "Reason: budget confirmed"},
		{At: "12:01", Actor: "Morgan", Title: "Manager review: assigned", Detail: "Assigned to Morgan"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Loretta", "Finance review: approved", "Reason: budget confirmed", "Assigned to Morgan"} {
		if !strings.Contains(markup, want) {
			t.Errorf("history omitted %q: %s", want, markup)
		}
	}
}

func TestTodo_UXBLIND_062_Browser(t *testing.T) {
	markup, err := ui.RenderToString(timelineSectionLocale("de-DE", []TimelineEvent{{
		Actor: "Loretta", Title: "Finanzprüfung: genehmigt", Detail: "Begründung: Budget bestätigt",
	}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Begründung: Budget bestätigt") || !strings.Contains(markup, "Loretta") {
		t.Fatalf("localized browser history = %s", markup)
	}
}

func TestTodo_UXBLIND_062_Golden(t *testing.T) {
	markup, err := ui.RenderToString(timelineSectionLocale("en-US", []TimelineEvent{{
		At: "2026-09-28T12:00:00Z", Actor: "Loretta", Title: "Manager review: rejected", Detail: "Reason: evidence incomplete",
	}}))
	if err != nil {
		t.Fatal(err)
	}
	want := `class="jn-tltitle">Manager review: rejected</p>`
	if !strings.Contains(markup, want) {
		t.Fatalf("golden timeline missing %q: %s", want, markup)
	}
}

func TestTodo_UXBLIND_063(t *testing.T) {
	view := DetailView{
		Diagnostics: true,
		Journey:     JourneyCard{DiagnosticsAuthorized: true},
		Nodes:       []NodeRow{{NodeID: "approve_finance", StepType: "approval", Status: "RUNNING", Attempt: "1", Started: "12:00", Completed: "12:03", Tone: "info"}},
	}
	markup, err := ui.RenderToString(diagnosticsSectionLocale("en-US", view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"authorized support staff", "12:00", "12:03", "In progress"} {
		if !strings.Contains(markup, want) {
			t.Errorf("diagnostics omitted %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, ">RUNNING<") {
		t.Fatal("raw runtime status leaked into diagnostics")
	}
}

func TestTodo_UXBLIND_063_Browser(t *testing.T) {
	view := DetailView{Diagnostics: true, Journey: JourneyCard{DiagnosticsAuthorized: false}, Nodes: []NodeRow{{NodeID: "secret", Status: "RUNNING"}}}
	node := diagnosticsSectionLocale("en-US", view)
	if node != nil {
		t.Fatal("unauthorized viewer received technical diagnostics")
	}
}
