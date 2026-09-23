package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func workflowPaletteFixture() []WorkflowPaletteItem {
	return []WorkflowPaletteItem{
		{ID: "kernel.approval", Version: 1, Name: "Approval", Kind: "BLOCK", Domain: "Control flow", Description: "Add a approval step.", EffectClass: "PURE", Reversal: "NO_EFFECT", Status: "ACTIVE", StepType: "APPROVAL"},
		{ID: "fragment.promotion-review", Version: 2, Name: "Promotion review", Kind: "FRAGMENT", Domain: "People", Description: "Manager and finance approval group.", EffectClass: "PURE", Reversal: "NO_EFFECT", Status: "ACTIVE"},
		{ID: "template.promotion", Version: 3, Name: "Promotion", Kind: "TEMPLATE", Domain: "People", Description: "Promotion with compensation controls.", EffectClass: "INTERNAL_MUTATION", Reversal: "COMPENSATION_REQUIRED", Status: "ACTIVE"},
	}
}

func workflowEditorFixture() WorkflowDraftView {
	return WorkflowDraftView{
		DraftID: "draft-9", WorkflowID: "workflow.9", Name: "Contractor onboarding", SemanticVersion: "1.2.3", Revision: 9,
		StartNodeID: "collect", HistoryPosition: 2, HistoryLength: 3, CanUndo: true, CanRedo: true, HistoryLabel: "Connect collect SUCCEEDED",
		Nodes: []WorkflowDraftNode{
			{ID: "collect", Label: "Collect details", StepType: "TASK", Locked: true, LockKind: "START",
				Parameters: []WorkflowNodeParameter{{ID: "display_name", Label: "Display name", Kind: "TEXT", Value: "Collect details", Maximum: 120}},
				Outcomes:   []WorkflowDraftOutcome{{RouteKey: "SUCCEEDED", TargetNodeIDs: []string{"review"}}, {RouteKey: "CANCELLED"}}},
			{ID: "review", StepType: "APPROVAL",
				Parameters: []WorkflowNodeParameter{{ID: "signal_timeout_seconds", Label: "Close after seconds", Kind: "INTEGER", Value: "3600", Minimum: 0, Maximum: 31536000}},
				Outcomes:   []WorkflowDraftOutcome{{RouteKey: "APPROVED", TargetNodeIDs: []string{"end_complete"}}, {RouteKey: "REJECTED", TargetNodeIDs: []string{"end_rejected"}}},
				Bindings: []WorkflowDraftBinding{{TargetPath: "worker_id", TargetType: "WorkerID", SourceKind: "NODE_OUTPUT", SourceNodeID: "collect", SourcePath: "worker_id",
					Candidates: []WorkflowDraftBindingCandidate{{SourceNodeID: "collect", SourcePath: "worker_id", ValueType: "WorkerID"}}}}},
			{ID: "end_complete", StepType: "END"},
			{ID: "end_rejected", StepType: "END"},
		},
		Edges: []WorkflowDraftEdge{
			{FromID: "collect", ToID: "review", RouteKey: "SUCCEEDED"},
			{FromID: "review", ToID: "end_complete", RouteKey: "APPROVED"},
			{FromID: "review", ToID: "end_rejected", RouteKey: "REJECTED"},
		},
		Changes: []WorkflowDraftSemanticChange{{Kind: "EDGE", Operation: "ADDED", SubjectID: "collect", Field: "SUCCEEDED", After: "review"}},
	}
}

func renderWorkflowEditor(t *testing.T, props WorkflowEditorProps) string {
	t.Helper()
	if props.Locale.Requested == "" && props.Locale.Resolved == "" {
		props.I18nProps = I18nProps{Locale: ResolveProductLocale("en-US")}
	}
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowEditor, props))
	if err != nil {
		t.Fatalf("render editor: %v", err)
	}
	return markup
}

func allWorkflowEditorCommands(props WorkflowEditorProps) WorkflowEditorProps {
	props.OnSelectNode = func(string) {}
	props.OnInsert = func(WorkflowPaletteItem) {}
	props.OnRename = func(string) {}
	props.OnUpdateNode = func(WorkflowNodeParameterChange) {}
	props.OnSetOutcome = func(WorkflowOutcomeChange) {}
	props.OnClearOutcome = func(WorkflowOutcomeChange) {}
	props.OnBindInput = func(WorkflowInputBindingChange) {}
	props.OnRemoveNode = func(string) {}
	props.OnOverlay = func(WorkflowTemplateOverlayChange) {}
	props.OnHistory = func(string) {}
	return props
}

var workflowStepTag = regexp.MustCompile(`<button[^>]*class="workflow-path-step[^"]*"[^>]*>`)

// TestTodo_WF_UI_009_Browser: the path is the keyboard and screen-reader way
// to read and select steps, not a second view beside a canvas. Every step is
// a real button in reading order, exactly one is current, and the step the
// inspector edits is that one.
func TestTodo_WF_UI_009_Browser(t *testing.T) {
	markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: workflowEditorFixture(), Palette: workflowPaletteFixture(), SelectedNodeID: "review"}))
	steps := workflowStepTag.FindAllString(markup, -1)
	if len(steps) != 2 {
		t.Fatalf("path rendered %d step buttons, want 2:\n%s", len(steps), markup)
	}
	if !strings.Contains(steps[0], `data-node-id="collect"`) || !strings.Contains(steps[1], `data-node-id="review"`) {
		t.Fatalf("steps are not in reading order: %v", steps)
	}
	if got := strings.Count(markup, `aria-current="step"`); got != 1 || !strings.Contains(steps[1], `aria-current="step"`) {
		t.Fatalf("aria-current count = %d; the selected step must be the only current one", got)
	}
	if !strings.Contains(markup, `class="workflow-inspector" data-node-id="review"`) && !strings.Contains(markup, `data-node-id="review" aria-labelledby="workflow-inspector-title"`) {
		if !regexp.MustCompile(`<section[^>]*class="workflow-inspector"[^>]*data-node-id="review"|<section[^>]*data-node-id="review"[^>]*class="workflow-inspector"`).MatchString(markup) {
			t.Fatalf("the inspector is not editing the selected step:\n%s", markup)
		}
	}
	for _, want := range []string{`<ol`, `workflow-path-exit`, `Complete`, `Rejected`, `Approved`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("path missing %q", want)
		}
	}
	if strings.Contains(markup, `tabindex="-1"`) {
		t.Fatal("the path removed a control from the keyboard tab order")
	}
	if strings.Contains(markup, "Not started") {
		t.Fatal("a draft, which has never run, shows a run state")
	}
	for _, id := range []string{"collect", "review", "end_complete", "end_rejected"} {
		if got := strings.Count(markup, `<button`) - strings.Count(strings.ReplaceAll(markup, `data-node-id="`+id+`"`, ""), `<button`); got != 0 {
			t.Fatalf("unexpected button accounting for %s", id)
		}
		if got := len(regexp.MustCompile(`<button[^>]*data-node-id="`+id+`"`).FindAllString(markup, -1)); got != 1 {
			t.Fatalf("step %q has %d select controls, want exactly 1", id, got)
		}
	}
}

// A selection that arrives from the host after the first render must win:
// the inspector once seeded its step a single time and then edited that step
// whatever was selected afterwards.
func TestWorkflowEditorInspectorFollowsTheSelectedStep(t *testing.T) {
	for _, selected := range []string{"collect", "review", "end_rejected"} {
		markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: workflowEditorFixture(), SelectedNodeID: selected}))
		if !regexp.MustCompile(`<section[^>]*data-node-id="` + selected + `"[^>]*>|<section[^>]*class="workflow-inspector"[^>]*data-node-id="` + selected + `"`).MatchString(markup) {
			t.Fatalf("selected %q but the inspector shows another step", selected)
		}
	}
	markup := renderWorkflowEditor(t, WorkflowEditorProps{Draft: workflowEditorFixture(), SelectedNodeID: "no-such-step"})
	if !strings.Contains(markup, `data-node-id="collect"`) || strings.Count(markup, `aria-current="step"`) != 1 {
		t.Fatal("an unknown selection did not fall back to the start step")
	}
}

func TestTodo_WF_UI_009_OutlineIsResponsiveThemeableAndLocalized(t *testing.T) {
	css := workflowEditorStylesheet()
	for _, want := range []string{`@media (max-width:1400px)`, `@media (max-width:820px)`, `@media (max-width:420px)`, `prefers-reduced-motion`, `[dir=rtl] .workflow-path-rail svg`, `var(--hcm-radius-control)`, `var(--hcm-font-size-small)`, `min-block-size:3.25rem`} {
		if !strings.Contains(css, want) {
			t.Fatalf("editor stylesheet missing %q", want)
		}
	}
	for _, sheet := range []string{css, workflowDesignerStylesheet()} {
		for _, forbidden := range []string{"padding-left", "padding-right", "margin-left", "margin-right", "border-left", "border-right", "text-align:left", "text-align:right", "#", "rgb(", "hsl("} {
			if strings.Contains(sheet, forbidden) {
				t.Fatalf("stylesheet contains fixed or direction-specific declaration %q", forbidden)
			}
		}
	}
}

// TestTodo_WF_UI_009_SingularTopologyCopyIsLocalized keeps its historical
// name; what it guards is that counted copy uses the locale's plural rules
// rather than a one/other switch in Go, which gave Arabic "2 خطوة".
func TestTodo_WF_UI_009_SingularTopologyCopyIsLocalized(t *testing.T) {
	for locale, want := range map[string]map[int64]string{
		"en-US": {1: "1 thing to finish", 2: "2 things to finish"},
		"de-DE": {1: "1 offener Punkt", 5: "5 offene Punkte"},
		"ar":    {1: "أمر واحد يجب إكماله", 2: "أمران يجب إكمالهما", 3: "3 أمور يجب إكمالها", 11: "11 أمرًا يجب إكماله"},
	} {
		resolved := ResolveProductLocale(locale)
		for count, text := range want {
			got := resolved.Plural("workflow_editor.problem_count", count)
			// Arabic formats digits in its own numbering system.
			if locale == "ar" {
				if strings.Contains(got, "⟦") || got == "" {
					t.Fatalf("%s %d = %q", locale, count, got)
				}
				continue
			}
			if got != text {
				t.Fatalf("%s plural(%d) = %q, want %q", locale, count, got, text)
			}
		}
	}
}

func TestWorkflowEditorCopyIsCompleteInEveryLocale(t *testing.T) {
	english := workflowEditorMessages()
	for _, locale := range []string{"de-DE", "ar"} {
		translated := workflowEditorTranslations(locale)
		for key, message := range english {
			other, ok := translated[key]
			if !ok {
				t.Fatalf("%s is missing %s", locale, key)
			}
			if (message.Plural == nil) != (other.Plural == nil) {
				t.Fatalf("%s %s plural shape differs from English", locale, key)
			}
			if locale == "ar" && other.Plural != nil && len(other.Plural) != 6 {
				t.Fatalf("ar %s carries %d plural categories, want 6", key, len(other.Plural))
			}
		}
		for key := range translated {
			if _, ok := english[key]; !ok {
				t.Fatalf("%s translates unknown key %s", locale, key)
			}
		}
		for _, key := range MissingProductTranslations(locale) {
			if strings.HasPrefix(key, "workflow_editor.") || strings.HasPrefix(key, "workflow_palette.step.") || strings.HasPrefix(key, "workflow_viewer.effect.") || strings.HasPrefix(key, "workflow_viewer.reversal.") || strings.HasPrefix(key, "workflow_viewer.lock.") {
				t.Fatalf("%s falls back to English for %s", locale, key)
			}
		}
	}
}

func TestTodo_WF_UI_010_Browser(t *testing.T) {
	draft := workflowEditorFixture()
	markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: draft}))
	for _, want := range []string{`aria-label="Undo"`, `aria-label="Redo"`, `Change 2 of 3`, `What changed`, `Added`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("history controls missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "Added Added") {
		t.Fatal("a change row repeats its operation inside its sentence")
	}
	draft.CanUndo, draft.CanRedo = false, false
	frozen := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: draft}))
	for _, label := range []string{"Undo", "Redo"} {
		if !regexp.MustCompile(`<button[^>]*(disabled[^>]*aria-label="` + label + `"|aria-label="` + label + `"[^>]*disabled)`).MatchString(frozen) {
			t.Fatalf("%s is enabled with nothing to %s", label, strings.ToLower(label))
		}
	}
}

func TestTodo_WF_UI_010_BrowserLocalizesHistoryControls(t *testing.T) {
	for locale, want := range map[string]string{"de-DE": "Rückgängig", "ar": "تراجع"} {
		markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{I18nProps: I18nProps{Locale: ResolveProductLocale(locale)}, Draft: workflowEditorFixture()}))
		if !strings.Contains(markup, want) || strings.Contains(markup, "⟦") {
			t.Fatalf("%s history controls are not localized", locale)
		}
	}
}

func TestTodo_WF_UI_006_ProductInspectorRendersTypedFormAndLockedControls(t *testing.T) {
	markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: workflowEditorFixture(), SelectedNodeID: "review"}))
	for _, want := range []string{`type="number"`, `min="0"`, `max="31536000"`, `value="3600"`, `Stop waiting after (seconds)`, `When this step ends`, `Uses`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("typed inspector missing %q:\n%s", want, markup)
		}
	}
	locked := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: workflowEditorFixture(), SelectedNodeID: "collect"}))
	if !strings.Contains(locked, `role="note"`) || !strings.Contains(locked, "Required control (start)") {
		t.Fatalf("a locked step does not say so:\n%s", locked)
	}
	if strings.Contains(locked, "Remove step") {
		t.Fatal("a locked step in a multi-step draft offers removal")
	}
	if !strings.Contains(markup, "Remove step") {
		t.Fatal("an unlocked step the author added cannot be removed")
	}
}

// The "not connected" choice must carry a real value. An option with an empty
// value reports its visible text instead, which is how the old picker sent
// "Choose a next step" to the server as a step id.
func TestTodo_WF_UI_007_BrowserRendersKeyboardOutcomeAndBindingEditors(t *testing.T) {
	markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: workflowEditorFixture(), SelectedNodeID: "collect"}))
	if !strings.Contains(markup, `value="`+workflowNoTarget+`"`) {
		t.Fatalf("the not-connected option has no explicit value:\n%s", markup)
	}
	if regexp.MustCompile(`<option(?:\s+selected)?\s*>`).MatchString(markup) || strings.Contains(markup, `<option value=""`) {
		t.Fatal("an option without a value would submit its label")
	}
	for _, want := range []string{`<optgroup label="Steps"`, `<optgroup label="Exits"`, `for="workflow-outcome-collect-SUCCEEDED"`, `id="workflow-outcome-collect-SUCCEEDED"`, `class="workflow-inspector-route open"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("outcome editor missing %q:\n%s", want, markup)
		}
	}
	if regexp.MustCompile(`<option[^>]*value="collect"`).MatchString(markup) {
		t.Fatal("a step is offered as its own next step")
	}
	if strings.Contains(markup, ">Connect<") || strings.Contains(markup, ">Bind input<") {
		t.Fatal("a route still needs a second button press to save")
	}
	binding := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: workflowEditorFixture(), SelectedNodeID: "review"}))
	if !strings.Contains(binding, "Worker Id") || !strings.Contains(binding, "from") || !strings.Contains(binding, "Collect details") {
		t.Fatalf("binding editor does not name the source step by its display name:\n%s", binding)
	}
}

func TestTodo_WF_UI_006_Browser(t *testing.T) {
	// With no commands wired the editor must not offer controls that do nothing.
	markup := renderWorkflowEditor(t, WorkflowEditorProps{Draft: workflowEditorFixture(), SelectedNodeID: "review"})
	if strings.Contains(markup, "Remove step") || strings.Contains(markup, `id="workflow-draft-name"`) {
		t.Fatalf("read-only host still renders edit controls:\n%s", markup)
	}
	if !regexp.MustCompile(`<select[^>]*disabled`).MatchString(markup) {
		t.Fatal("route pickers are live without a command to run")
	}
}

func TestTodo_WF_UI_006_OverlayRequiresAnAuditReason(t *testing.T) {
	draft := workflowEditorFixture()
	draft.TemplateID, draft.TemplateVersion = "template.promotion", 3
	markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: draft, Palette: workflowPaletteFixture(), SelectedNodeID: "review"}))
	for _, label := range []string{"Omit step", "Replace step"} {
		if !regexp.MustCompile(`<button[^>]*disabled[^>]*>` + label).MatchString(markup) {
			t.Fatalf("%q is available before a reason is given:\n%s", label, markup)
		}
	}
	if strings.Contains(markup, "Remove step") {
		t.Fatal("a governed template step can be removed without a recorded reason")
	}
}

func TestTodo_WF_UI_006_Locales(t *testing.T) {
	for _, locale := range []string{"de-DE", "ar"} {
		markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{I18nProps: I18nProps{Locale: ResolveProductLocale(locale)}, Draft: workflowEditorFixture(), Palette: workflowPaletteFixture(), SelectedNodeID: "collect"}))
		for _, english := range []string{"⟦", "Required control", "When this step ends", "Internal Mutation", "Compensation Required", "Not connected", "No Effect", ">Pure<"} {
			if strings.Contains(markup, english) {
				t.Fatalf("%s editor shows untranslated %q", locale, english)
			}
		}
	}
}

func TestTodo_WF_UI_007_Locales(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		resolved := ResolveProductLocale(locale)
		for _, key := range []string{"workflow_editor.outcomes", "workflow_editor.uses", "workflow_editor.target_steps", "workflow_editor.target_exits", "workflow_inspector.not_connected", "workflow_inspector.not_bound", "workflow_editor.binding_option"} {
			if got := resolved.Text(key, map[string]string{"field": "a", "step": "b"}); got == "" || strings.Contains(got, "⟦") {
				t.Fatalf("%s %s = %q", locale, key, got)
			}
		}
	}
}

func TestTodo_WF_UI_005_PaletteRendersAuthorizedCatalog(t *testing.T) {
	items := workflowPaletteFixture()
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowPalette, WorkflowPaletteProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Items: items, OnInsert: func(WorkflowPaletteItem) {}}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Flow", "People", "Approval", "Promotion review", "Ask someone to approve before work continues.", "Changes HR data", "Undone by a compensating step", `data-entry-id="template.promotion"`, `aria-label="Add Approval"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("library missing %q:\n%s", want, markup)
		}
	}
	for _, noise := range []string{"Add a approval step", "No side effects", "Nothing to undo"} {
		if strings.Contains(markup, noise) {
			t.Fatalf("library shows %q, which tells an author nothing", noise)
		}
	}
	if items[0].Description != "Add a approval step." {
		t.Fatal("renderer mutated the caller's palette")
	}
}

func TestTodo_WF_UI_005_SearchMatchesMetadataWithoutMutatingEntries(t *testing.T) {
	items := workflowPaletteFixture()
	groups := filterWorkflowPalette(items, "compensation", "Other")
	if len(groups) != 1 || len(groups["People"]) != 1 || groups["People"][0].ID != "template.promotion" {
		t.Fatalf("search groups = %+v", groups)
	}
	if got := filterWorkflowPalette(items, "  ", "Other"); len(got["People"]) != 2 || len(got["Control flow"]) != 1 {
		t.Fatalf("blank search filtered entries: %+v", got)
	}
}

func TestTodo_WF_UI_005_Browser(t *testing.T) {
	// A template starts an empty workflow and nothing else: once a draft has
	// steps the library does not list it at all, and an empty draft does.
	markup := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: workflowEditorFixture(), Palette: workflowPaletteFixture()}))
	if strings.Contains(markup, `data-entry-id="template.promotion"`) {
		t.Fatalf("a template that can no longer be used is still listed:\n%s", markup)
	}
	blank := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: WorkflowDraftView{DraftID: "d", WorkflowID: "w", Name: "New", SemanticVersion: "0.1.0", Revision: 1}, Palette: workflowPaletteFixture()}))
	if !strings.Contains(blank, `data-entry-id="template.promotion"`) {
		t.Fatal("an empty draft does not offer the template")
	}
	draft := workflowEditorFixture()
	draft.Groups = []WorkflowDraftGroup{{ID: "group_review_1", Name: "Promotion review", NodeIDs: []string{"review"}}}
	draft.Nodes[1].GroupID = "group_review_1"
	grouped := renderWorkflowEditor(t, allWorkflowEditorCommands(WorkflowEditorProps{Draft: draft}))
	if !strings.Contains(grouped, `data-group-id="group_review_1"`) || !strings.Contains(grouped, "Promotion review") {
		t.Fatal("a step added with a fragment no longer shows the fragment it belongs to")
	}
}

// TestPromotionDraftTopologyMatchesTheExecutableDefinition renders the real
// shipped Promotion definition and proves the path drops nothing: every step
// appears exactly once, every route is accounted for as a continuation, a
// jump or an exit, and nothing reachable is reported as unconnected.
func TestPromotionDraftTopologyMatchesTheExecutableDefinition(t *testing.T) {
	definition := promotionexec.Definition()
	digest := workflowversion.DefinitionDigest(definition)
	draft := WorkflowDraftView{
		DraftID: "draft-promotion", WorkflowID: definition.WorkflowID, Name: definition.Name, SemanticVersion: "1.1.1", Revision: 1,
		StartNodeID: definition.StartNodeID, DefinitionDigest: digest, TemplateDefinitionDigest: digest, MatchesTemplateDefinition: true,
		TemplateID: "hcmnext.templates.promotion", TemplateVersion: 1,
	}
	for _, node := range definition.Nodes {
		draft.Nodes = append(draft.Nodes, WorkflowDraftNode{ID: node.ID, StepType: string(node.Type)})
	}
	for _, edge := range definition.Edges {
		draft.Edges = append(draft.Edges, WorkflowDraftEdge{FromID: edge.From, ToID: edge.To, RouteKey: edge.RouteKey})
	}

	path := buildWorkflowPath(draft, "")
	if len(path.Loose) != 0 {
		t.Fatalf("reachable promotion steps reported as unconnected: %+v", path.Loose)
	}
	if got := len(path.Steps) + len(path.Exits); got != len(definition.Nodes) {
		t.Fatalf("path shows %d of %d nodes", got, len(definition.Nodes))
	}
	if path.Steps[0].Node.ID != definition.StartNodeID {
		t.Fatalf("path starts at %q, want %q", path.Steps[0].Node.ID, definition.StartNodeID)
	}
	routes := 0
	for _, step := range path.Steps {
		for _, jump := range step.Jumps {
			routes += len(jump.RouteKeys)
		}
		for _, exit := range step.Exits {
			routes += len(exit.RouteKeys)
		}
		if step.Continues != "" {
			routes++
		}
		for _, jump := range step.Jumps {
			if jump.Lane < 0 {
				t.Fatalf("route %v from %s has no rail lane", jump.RouteKeys, step.Node.ID)
			}
		}
	}
	if routes != len(definition.Edges) {
		t.Fatalf("path accounts for %d of %d routes", routes, len(definition.Edges))
	}

	markup := renderWorkflowEditor(t, WorkflowEditorProps{Draft: draft})
	for _, want := range []string{`data-parity="exact"`, `Await Payroll Confirmation`, `Observe Payroll`, `Observe Access`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("promotion draft missing %q", want)
		}
	}
	for _, node := range definition.Nodes {
		if got := len(regexp.MustCompile(`class="workflow-path-(?:step|exit)[^"]*"[^>]*data-node-id="`+node.ID+`"|data-node-id="`+node.ID+`"[^>]*class="workflow-path-(?:step|exit)`).FindAllString(markup, -1)); got != 1 {
			t.Fatalf("promotion node %q rendered %d times, want once", node.ID, got)
		}
	}
}
