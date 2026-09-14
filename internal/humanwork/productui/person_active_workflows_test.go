package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestPersonActiveWorkflowsOrderFallbackAndEmptyState pins the section's own
// behaviour beyond PROMOUX-012's matrix: open journeys for the person only,
// most urgent first, a journey with no projected href still links to its
// canonical workspace, the empty state is stated rather than blank, and a
// hidden section renders nothing.
func TestPersonActiveWorkflowsOrderFallbackAndEmptyState(t *testing.T) {
	view := testView(PagePerson)
	person := Person{ID: "worker-kim", Name: "Kim Lee"}
	view.People = append(view.People, person)
	view.Work = []WorkItem{
		{ID: "kim-wait", PersonRef: "worker-kim", Tone: "neutral", Status: "Waiting for effective date", NextStep: "await_effective_date", WaitingOn: "system"},
		{ID: "kim-repair", PersonRef: "worker-kim", Tone: "danger", Status: "Needs repair", NextStep: "repair", AwaitsPerson: true, Href: "/workspace/app/journeys?journey=kim-repair"},
		{ID: "kim-done", PersonRef: "worker-kim", Terminal: true, Status: "Completed"},
		{ID: "someone-else", PersonRef: "worker-other", Tone: "danger", Status: "Needs repair"},
	}
	props := personActiveWorkflowsProps(view, person, PagePerson)
	if len(props.Items) != 2 || props.Items[0].ID != "kim-repair" || props.Items[1].ID != "kim-wait" {
		t.Fatalf("items = %+v, want Kim's two open journeys, repair first", props.Items)
	}
	if want := JourneyDetailHref(view, "kim-wait"); props.Items[1].Href != want {
		t.Fatalf("fallback href = %q, want %q", props.Items[1].Href, want)
	}
	if props.Items[0].WaitingOn != "" || props.Items[1].WaitingOn == "" {
		t.Fatalf("owner lines = %q / %q, want none for repair and the workflow for the wait", props.Items[0].WaitingOn, props.Items[1].WaitingOn)
	}

	view.Work = nil
	empty, err := ui.RenderToString(ui.CreateElement(PersonActiveWorkflows, personActiveWorkflowsProps(view, person, PagePerson)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(empty, `class="muted person-active-empty">`+view.Locale.Text("person.active_workflows_empty")) || strings.Contains(empty, "<ul") {
		t.Fatalf("empty section = %s", empty)
	}
	hidden, err := ui.RenderToString(ui.CreateElement(PersonActiveWorkflows, PersonActiveWorkflowsProps{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hidden, "person-active-workflows") {
		t.Fatalf("a hidden section rendered: %s", hidden)
	}
	if len(personOpenWork(view, "")) != 0 {
		t.Fatal("an empty person id matched work")
	}
}
