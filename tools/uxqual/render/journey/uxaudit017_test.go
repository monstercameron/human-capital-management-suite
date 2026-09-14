package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ---------------------------------------------------------------------
// TestTodo_UXAUDIT_017 -- this package's contribution to the PRIMARY
// contract.
//
// GREEN: "Journeys is a lifecycle tracker grouped by subject and status".
// The fixture below seeds Adrian with TWO journeys (one open, one
// completed) alongside Naomi and Samuel's single journeys each --
// deliberately, because a fixture where every subject appears exactly once
// cannot distinguish a grouped rendering from an ungrouped one: both would
// show three cards in some order. Only a repeated subject exposes whether
// the renderer actually clusters under one heading or silently repeats it.
// ---------------------------------------------------------------------

func TestTodo_UXAUDIT_017(t *testing.T) {
	view := ListView{
		Groups: []JourneySubjectGroup{
			{Subject: "Naomi Chen", Journeys: []JourneyCard{
				{IntentID: "int-naomi", WorkerRef: "worker-naomi", WorkerName: "Naomi Chen", Stage: "AWAITING_APPROVAL", StageLabel: "Awaiting approval", StageTone: "warning"},
			}, Statuses: []JourneyStatusChip{{Label: "Awaiting approval", Tone: "warning"}}},
			{Subject: "Adrian Fox", Journeys: []JourneyCard{
				{IntentID: "int-adrian-open", WorkerRef: "worker-adrian", WorkerName: "Adrian Fox", Stage: "BLOCKED", StageLabel: "Blocked", StageTone: "warning", EffectiveDate: "1 Nov 2026"},
				{IntentID: "int-adrian-done", WorkerRef: "worker-adrian", WorkerName: "Adrian Fox", Stage: "COMPLETED", StageLabel: "Completed", StageTone: "success", EffectiveDate: "1 Jun 2025"},
			}, Statuses: []JourneyStatusChip{{Label: "Blocked", Tone: "warning"}, {Label: "Completed", Tone: "success"}}},
			{Subject: "Samuel Ortiz", Journeys: []JourneyCard{
				{IntentID: "int-samuel", WorkerRef: "worker-samuel", WorkerName: "Samuel Ortiz", Stage: "MANAGER_APPROVAL", StageLabel: "Manager approval", StageTone: "warning"},
			}, Statuses: []JourneyStatusChip{{Label: "Manager approval", Tone: "warning"}}},
		},
		// Journeys still carries the flat projection too (ListPage always
		// sets both); the grouped rendering must win whenever Groups is
		// non-empty, regardless of what Journeys also contains.
		Journeys: []JourneyCard{
			{IntentID: "int-naomi", WorkerRef: "worker-naomi", WorkerName: "Naomi Chen"},
			{IntentID: "int-adrian-open", WorkerRef: "worker-adrian", WorkerName: "Adrian Fox"},
			{IntentID: "int-adrian-done", WorkerRef: "worker-adrian", WorkerName: "Adrian Fox"},
			{IntentID: "int-samuel", WorkerRef: "worker-samuel", WorkerName: "Samuel Ortiz"},
		},
	}

	markup, err := ui.RenderToString(journeysSection("en-US", view))
	if err != nil {
		t.Fatal(err)
	}

	// Three subjects, three group headings -- not four, and not one per
	// journey (which would be indistinguishable from the old flat list,
	// where every card already carries its own worker-name heading).
	if got := strings.Count(markup, `class="jn-journey-group-subject"`); got != 3 {
		t.Fatalf("subject group headings = %d, want 3 (one per subject, not one per journey): %s", got, markup)
	}
	if got := strings.Count(markup, `id="journey-group-1-heading">Adrian Fox<`); got != 1 {
		t.Fatalf(`Adrian's group heading must appear exactly once even though he has two journeys: got %d occurrences: %s`, got, markup)
	}

	// Adrian's two journeys are both present and both fall between his
	// heading and the next one (Samuel's), proving they are nested under
	// his single group rather than merely adjacent in a flat list.
	adrianHeading := strings.Index(markup, "Adrian Fox")
	samuelHeading := strings.Index(markup, "Samuel Ortiz")
	if adrianHeading < 0 || samuelHeading < 0 || samuelHeading < adrianHeading {
		t.Fatalf("expected Adrian's group before Samuel's: adrian=%d samuel=%d: %s", adrianHeading, samuelHeading, markup)
	}
	adrianSection := markup[adrianHeading:samuelHeading]
	if !strings.Contains(adrianSection, "1 Nov 2026") || !strings.Contains(adrianSection, "1 Jun 2025") {
		t.Fatalf("both of Adrian's journeys must be nested inside his single group section: %s", adrianSection)
	}

	// Status is a visible dimension of the group, not only of each card:
	// Adrian's group shows both statuses present across his journeys.
	if !strings.Contains(adrianSection, "Blocked") || !strings.Contains(adrianSection, "Completed") {
		t.Fatalf("Adrian's group must expose the distinct statuses across his journeys: %s", adrianSection)
	}

	// The shared next-step dimension renders on an open card only.
	open, err := ui.RenderToString(journeyCard(JourneyCard{IntentID: "int-open", WorkerName: "Zara Moll", StageLabel: "Manager approval", NextStep: "Manager decision"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(open, `class="jn-journey-next"`) || !strings.Contains(open, "Manager decision") {
		t.Fatalf("an open journey card must state its next step: %s", open)
	}

	// The tracker's empty state names the tracking task, in both the
	// standalone and the embedded product composition.
	empty, err := ui.RenderToString(journeysSection("en-US", ListView{Empty: "No promotion has been proposed in this tenant yet."}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(empty, ">"+journeysEmptyTitle+"<") {
		t.Fatalf("Journeys empty state lacks its lifecycle title %q: %s", journeysEmptyTitle, empty)
	}
	embedded, err := ui.RenderToString(embeddedListView(Page{}, ListView{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(embedded, journeysEmptyTitle) || !strings.Contains(embedded, "grouped by employee and status") {
		t.Fatalf("embedded Journeys empty state is not tracker-specific: %s", embedded)
	}
	if strings.Contains(embedded, "Nothing needs your action") {
		t.Fatalf("Journeys must not borrow My Work's action-queue empty state: %s", embedded)
	}
}

// TestTodo_UXAUDIT_017_Regression proves the grouped rendering is additive:
// a ListView that never sets Groups (every fixture and test that predates
// this todo) still renders the exact flat markup it always has, with no
// group heading class anywhere in the output.
func TestTodo_UXAUDIT_017_Regression(t *testing.T) {
	flat := ListView{Journeys: []JourneyCard{
		{IntentID: "int-1", WorkerName: "Priya Nair", StageLabel: "Recorded", StageTone: "success"},
		{IntentID: "int-2", WorkerName: "Sam Okafor", StageLabel: "Blocked", StageTone: "warning"},
	}}
	markup, err := ui.RenderToString(journeysSection("en-US", flat))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "jn-journey-group") {
		t.Fatalf("a ListView with no Groups must render the plain flat list, not the grouped structure: %s", markup)
	}
	if !strings.Contains(markup, "jn-grid") || !strings.Contains(markup, "Priya Nair") || !strings.Contains(markup, "Sam Okafor") {
		t.Fatalf("the flat fallback must still render both journeys: %s", markup)
	}
	// A card with no next step (terminal, or a fixture predating the field)
	// renders no next-step line, so pinned card goldens stay byte-identical.
	if strings.Contains(markup, "jn-journey-next") {
		t.Fatalf("a card without NextStep must not render a next-step line: %s", markup)
	}
}
