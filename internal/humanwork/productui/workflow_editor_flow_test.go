package productui

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// A step added from the library continues from the selected step only where
// that costs nothing: the first usual result that leads nowhere. It must never
// pick an exception result, a result that already goes somewhere, or an exit.
func TestWorkflowInsertAnchorNeverReroutesWorkingResults(t *testing.T) {
	draft := workflowEditorFixture()
	if from, route, ok := workflowInsertAnchor(draft, "collect"); ok {
		t.Fatalf("collect's only open result is an exception, yet it anchors at %s/%s", from, route)
	}
	draft.Nodes[0].Outcomes[0].TargetNodeIDs = nil
	if from, route, ok := workflowInsertAnchor(draft, "collect"); !ok || from != "collect" || route != "SUCCEEDED" {
		t.Fatalf("anchor = %s/%s/%t, want collect/SUCCEEDED", from, route, ok)
	}
	if _, _, ok := workflowInsertAnchor(draft, "end_complete"); ok {
		t.Fatal("an exit was offered as something to continue from")
	}
	if _, _, ok := workflowInsertAnchor(draft, "missing"); ok {
		t.Fatal("an unknown selection produced an anchor")
	}
}

func TestWorkflowConnectSourcesOfferOnlyResultsThatLeadNowhere(t *testing.T) {
	draft := workflowEditorFixture()
	draft.Nodes = append(draft.Nodes, WorkflowDraftNode{ID: "notify", StepType: "TASK", Outcomes: []WorkflowDraftOutcome{{RouteKey: "SUCCEEDED"}}})
	got := workflowConnectSources(draft, draft.Nodes[len(draft.Nodes)-1])
	if want := []workflowConnectSource{{NodeID: "collect", RouteKey: "CANCELLED"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("connect sources = %+v, want %+v", got, want)
	}
}

func TestWorkflowInspectorPutsTheUsualRouteFirstAndOffersOneActionForTheRest(t *testing.T) {
	draft := workflowEditorFixture()
	draft.Nodes[1].Outcomes = []WorkflowDraftOutcome{{RouteKey: "CANCELLED"}, {RouteKey: "EXPIRED"}, {RouteKey: "REJECTED"}, {RouteKey: "APPROVED"}}
	draft.Edges = draft.Edges[:1]
	props := allWorkflowEditorCommands(WorkflowEditorProps{Draft: draft, SelectedNodeID: "review"})
	props.OnSetOutcomes = func([]WorkflowOutcomeChange) {}
	markup := renderWorkflowEditor(t, props)

	usual, rare := strings.Index(markup, "When this step ends"), strings.Index(markup, "If something goes wrong")
	approved, cancelled := strings.Index(markup, `for="workflow-outcome-review-APPROVED"`), strings.Index(markup, `for="workflow-outcome-review-CANCELLED"`)
	if usual < 0 || rare < 0 || !(usual < approved && approved < rare && rare < cancelled) {
		t.Fatalf("the approved route is not listed ahead of the exception routes (%d %d %d %d)", usual, approved, rare, cancelled)
	}
	if !strings.Contains(markup, "Send all 3 unset results to") || !regexp.MustCompile(`<select[^>]*name="bulk_exception_target"`).MatchString(markup) {
		t.Fatalf("three unset exception results offer no single action:\n%s", markup)
	}
	// The usual result is flagged on its own row; the three exceptions are
	// counted once on their folded heading, so the pane shows the same two
	// things to finish the step's badge counts. Every unset select is still
	// exposed as invalid.
	if got := strings.Count(markup, "Not set"); got != 1 {
		t.Fatalf("%d rows say Not set, want only the usual result's", got)
	}
	if !strings.Contains(markup, ">3 not set<") {
		t.Fatal("the folded exceptions do not say how many are unset")
	}
	if got := strings.Count(markup, `aria-invalid="true"`); got != 4 {
		t.Fatalf("%d of 4 unset results are exposed as invalid", got)
	}

	// With no exit to send them to, the control says what to do first.
	draft.Nodes = draft.Nodes[:2]
	props.Draft = draft
	if noExit := renderWorkflowEditor(t, props); !strings.Contains(noExit, "Add an End step") {
		t.Fatal("bulk routing with no exit gives no next action")
	}
}

func TestWorkflowLooseStepOffersToConnectItself(t *testing.T) {
	draft := workflowEditorFixture()
	draft.Nodes = append(draft.Nodes, WorkflowDraftNode{ID: "notify", StepType: "TASK", Outcomes: []WorkflowDraftOutcome{{RouteKey: "SUCCEEDED"}}})
	markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: draft, SelectedNodeID: "notify"}))
	for _, want := range []string{"Not connected yet", "Connect this step", `name="connect_from"`, "Collect details", "Cancelled"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("loose step editor missing %q", want)
		}
	}
	connected := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: draft, SelectedNodeID: "review"}))
	if strings.Contains(connected, `name="connect_from"`) {
		t.Fatal("a step already on the path offers to connect itself")
	}
}

func TestWorkflowBlankDraftOffersAFirstStepWhereTheAuthorIsLooking(t *testing.T) {
	palette := append(workflowPaletteFixture(), WorkflowPaletteItem{ID: "kernel.task", Version: 1, Name: "Task", Kind: "BLOCK", Domain: "Control flow", StepType: "TASK"})
	markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: WorkflowDraftView{DraftID: "d", WorkflowID: "w", Name: "Untitled workflow", SemanticVersion: "0.1.0", Revision: 1}, Palette: palette}))
	if got := len(regexp.MustCompile(`class="[^"]*workflow-path-starter"`).FindAllString(markup, -1)); got != 2 {
		t.Fatalf("blank draft offers %d starter steps, want the task and the approval", got)
	}
	if strings.Contains(markup, "Change 1 of 1") {
		t.Fatal("an unedited draft reports a history position")
	}
}

func TestWorkflowExitMarksIdentifyExactlyOneExit(t *testing.T) {
	draft := workflowPathFixture()
	draft.Nodes = append(draft.Nodes, WorkflowDraftNode{ID: "end_blocked", StepType: "END"})
	draft.Edges = append(draft.Edges, WorkflowDraftEdge{FromID: "manager", ToID: "end_blocked", RouteKey: "EXPIRED"}, WorkflowDraftEdge{FromID: "finance", ToID: "end_blocked", RouteKey: "BLOCKED"})
	path := buildWorkflowPath(draft, "")
	seen := map[string]string{}
	for _, exit := range path.Exits {
		key := exit.Tone + "/" + string(rune('0'+exit.Shape))
		if other, clash := seen[key]; clash {
			t.Fatalf("exits %s and %s share the mark %s", other, exit.Node.ID, key)
		}
		seen[key] = exit.Node.ID
	}
	// Two routes from one step into one exit are one mark.
	draft.Edges = append(draft.Edges, WorkflowDraftEdge{FromID: "snapshot", ToID: "end_rejected", RouteKey: "AMBIGUOUS"})
	snapshot := buildWorkflowPath(draft, "").Steps[0]
	if len(snapshot.Exits) != 1 || len(snapshot.Exits[0].RouteKeys) != 2 {
		t.Fatalf("snapshot exits = %+v, want one mark carrying two routes", snapshot.Exits)
	}
}

// The header popovers close on Escape, on focus or pointer leaving and on a
// click outside because they opt in to the product's shared popover policy,
// and opening one closes the other because they share a details name.
func TestWorkflowEditorPopoversUseTheSharedDismissalPolicy(t *testing.T) {
	markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: workflowEditorFixture(), Palette: workflowPaletteFixture()}))
	for _, want := range []string{`data-hcm-transient-popover="workflow-history"`, `data-hcm-transient-popover="workflow-problems"`, `data-hcm-popover-grace-ms="` + transientPopoverGraceMilliseconds + `"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Count(markup, `name="workflow-editor-popover"`) != 2 {
		t.Fatal("the two header popovers do not share one details group")
	}
}

// The header says how many things are left; the author must be able to find
// every one of them on the canvas. A draft with a loose step and no End used
// to count three and show two.
func TestWorkflowHeaderCountEqualsWhatTheCanvasShows(t *testing.T) {
	draft := WorkflowDraftView{DraftID: "d", WorkflowID: "w", Name: "Blank", SemanticVersion: "0.1.0", StartNodeID: "task_1", Nodes: []WorkflowDraftNode{
		{ID: "task_1", StepType: "TASK", Outcomes: []WorkflowDraftOutcome{{RouteKey: "SUCCEEDED"}, {RouteKey: "CANCELLED"}, {RouteKey: "EXPIRED"}}},
		{ID: "decision_1", StepType: "DECISION"},
	}}
	path := buildWorkflowPath(draft, "")
	problems := workflowPathProblems(path, nil)
	markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: draft, Palette: workflowPaletteFixture()}))
	i18n := I18nProps{Locale: ResolveProductLocale("en-US")}
	shown := 0
	for _, count := range []int64{1, 2, 3, 4} {
		shown += int(count) * strings.Count(markup, ">"+i18n.Locale.Plural("workflow_editor.todo_count", count)+"<")
	}
	if noExit := strings.Count(markup, "workflow-path-no-exit"); noExit != 1 {
		t.Fatalf("a draft with no End shows %d notes on the canvas, want 1", noExit)
	}
	if shown+1 != len(problems) {
		t.Fatalf("the header counts %d things; the canvas shows %d on steps plus the no-End note", len(problems), shown)
	}
}

func TestWorkflowFoldedLibraryOffersToShowEveryStep(t *testing.T) {
	palette := workflowPaletteFixture()
	for _, id := range []string{"task", "wait", "signal", "decision", "end"} {
		palette = append(palette, WorkflowPaletteItem{ID: "kernel." + id, Version: 1, Name: id, Kind: "BLOCK", Domain: "Control flow", Status: "ACTIVE", StepType: strings.ToUpper(id)})
	}
	markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: workflowEditorFixture(), Palette: palette}))
	for _, want := range []string{"workflow-palette-more", `aria-expanded="false"`, `aria-controls="workflow-palette-groups"`, `id="workflow-palette-groups"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %s", want)
		}
	}
	// Opened, it scrolls inside a bounded box so the path stays in view, and
	// on a phone the button sits in the header row, above the fold's clip.
	for _, want := range []string{".workflow-palette.expanded{max-block-size:min(60vh,34rem);overflow:auto}", ".workflow-palette-more{grid-row:2;grid-column:2;"} {
		if !strings.Contains(workflowEditorStylesheet(), want) {
			t.Fatalf("stylesheet lacks %s", want)
		}
	}
}

// A decision says what it chooses between and what it reads, beside the
// choices, and its note about rules comes before the folded exceptions.
func TestWorkflowDecisionInspectorSaysWhatItChoosesBetween(t *testing.T) {
	draft := workflowEditorFixture()
	draft.Nodes[1] = WorkflowDraftNode{ID: "review", StepType: "DECISION",
		Outcomes: []WorkflowDraftOutcome{{RouteKey: "ABOVE_THRESHOLD", TargetNodeIDs: []string{"end_complete"}}, {RouteKey: "WITHIN_THRESHOLD", TargetNodeIDs: []string{"end_rejected"}}, {RouteKey: "CANCELLED"}},
		Bindings: []WorkflowDraftBinding{{TargetPath: "raise_ratio", TargetType: "Decimal", SourceKind: "NODE_OUTPUT", SourceNodeID: "collect", SourcePath: "raise_ratio"}}}
	markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: draft, SelectedNodeID: "review"}))
	basis, note, rare := strings.Index(markup, "workflow-inspector-basis"), strings.Index(markup, "These choices come from the decision rule"), strings.Index(markup, "If something goes wrong")
	if basis < 0 || note < 0 || rare < 0 || !(basis < note && note < rare) {
		t.Fatalf("decision basis, rule note and exceptions are out of order (%d %d %d)", basis, note, rare)
	}
	for _, want := range []string{"Chooses between", "Above Threshold", "Within Threshold", "Raise Ratio"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("decision basis lacks %q", want)
		}
	}
}
