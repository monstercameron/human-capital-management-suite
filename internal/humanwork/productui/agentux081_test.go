package productui

import (
	"strings"
	"testing"
)

// TestTodo_AGENTUX_081_Activity states each agent's real state in words: only
// a published agent placed in a conversation and working right now is
// Answering. A quiet placed agent is Ready, a published agent in no
// conversation is Not placed, and a suspended one stays Paused.
func TestTodo_AGENTUX_081_Activity(t *testing.T) {
	placed := []PersonaAdminInstallation{{InstallationID: "i1", ConversationID: "general", Conversation: "general", Kind: "channel"}}
	snapshot := AgentControlsSnapshot{Available: true, AllowedCommands: []string{"SUSPEND", "PUBLISH"}, Agents: []PersonaAdminPersona{
		{ID: "working", Name: "Working", Version: "1", Lifecycle: PersonaPublished, Installations: placed,
			RecentRuns: []AgentControlRun{{ID: "r1", State: "running"}}},
		{ID: "quiet", Name: "Quiet", Version: "1", Lifecycle: PersonaPublished, Installations: placed,
			RecentRuns: []AgentControlRun{{ID: "r2", State: "COMPLETED"}}},
		{ID: "nowhere", Name: "Nowhere", Version: "1", Lifecycle: PersonaPublished},
		{ID: "paused", Name: "Paused", Version: "1", Lifecycle: PersonaSuspended, Installations: placed},
	}}
	locale := ResolveProductLocale("en-US")
	page := agentUXR7Render(t, RenderAgentControls(locale, snapshot, "done"))
	want := map[string]string{"working": "Answering", "quiet": "Ready", "nowhere": "Not placed", "paused": "Paused"}
	for id, state := range want {
		start := strings.Index(page, `data-persona-id="`+id+`"`)
		if start < 0 {
			t.Fatalf("no row for agent %q in page", id)
		}
		rest := page[start:]
		if next := strings.Index(rest[len(`data-persona-id="`+id+`"`):], "data-persona-id="); next >= 0 {
			rest = rest[:len(`data-persona-id="`+id+`"`)+next]
		}
		if !strings.Contains(rest, ">"+state+"<") {
			t.Fatalf("agent %q: want state %q in row, row has:\n%s", id, state, rest)
		}
	}
}

// TestTodo_AGENTUX_081 pins the three Agents-page reads: a failed Cost, Cost
// limits, Connections or access read ends in the shared placeholder with a
// working Try again, never in endless loading or a dead end.
func TestTodo_AGENTUX_081(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	cost := agentUXR7Render(t, AgentCostPanel(AgentCostPanelProps{I18nProps: I18nProps{Locale: locale}, State: AgentAccessStateUnavailable}))
	for _, want := range []string{`data-agent-cost-state="failed"`, `data-agent-cost-action="retry"`, ">Try again<"} {
		if !strings.Contains(cost, want) {
			t.Fatalf("failed Cost read is missing %q: %s", want, cost)
		}
	}
	admin := agentUXR7Render(t, AgentAdminAccessPage(AgentAdminAccessPageProps{I18nProps: I18nProps{Locale: locale}, State: AgentAccessStateUnavailable, Embedded: true}))
	for _, want := range []string{`data-agent-access-state="unavailable"`, `data-agent-admin-action="retry"`, ">Try again<"} {
		if !strings.Contains(admin, want) {
			t.Fatalf("failed Connections read is missing %q: %s", want, admin)
		}
	}
	if strings.Contains(admin, "Agent administration is unavailable</") {
		t.Fatalf("Connections still prints the dead-end sentence: %s", admin)
	}
	access := agentUXR7Render(t, AgentAccessPage(AgentAccessPageProps{I18nProps: I18nProps{Locale: locale}, State: AgentAccessStateUnavailable, Embedded: true}))
	for _, want := range []string{`data-agent-access-state="unavailable"`, `data-agent-access-action="retry"`, ">Try again<"} {
		if !strings.Contains(access, want) {
			t.Fatalf("failed access read is missing %q: %s", want, access)
		}
	}
}

// TestTodo_AGENTUX_081_EditWiring pins the Edit contract: the card's Edit names
// its form, the form starts hidden, and the first field a person can type in
// is the display name, so Edit can bring the form into view with focus there.
func TestTodo_AGENTUX_081_EditWiring(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	persona := PersonaAdminPersona{ID: "policy-helper", Name: "Policy Helper", Version: "3", Lifecycle: PersonaPublished,
		StarterID: "hcmnext.persona_template.policy_helper", StarterVersion: 1, ChannelClasses: []string{"ANY_INTERNAL"}}
	snapshot := PersonaAdminSnapshot{}
	editorID := personaAdminVersionEditorID(persona)
	opener := agentUXR7Render(t, personaAdminHeaderActions(locale, nil, persona, snapshot))
	for _, want := range []string{`aria-controls="` + editorID + `"`, `data-persona-editor-toggle="` + editorID + `"`, `aria-expanded="false"`} {
		if !strings.Contains(opener, want) {
			t.Fatalf("Edit does not name its form %q: %s", want, opener)
		}
	}
	form := agentUXR7Render(t, personaAdminVersionEditor(locale, nil, persona, false))
	if !strings.Contains(form, `id="`+editorID+`"`) || !strings.Contains(form, "hidden") {
		t.Fatalf("Edit form %q does not start hidden: %s", editorID, form)
	}
	handle := strings.Index(form, `id="`+editorID+`-handle"`)
	name := strings.Index(form, `name="display_name"`)
	if handle < 0 || name < 0 || name < handle {
		t.Fatalf("display name is not the first editable field: %s", form)
	}
	if tag := form[handle : strings.Index(form[handle:], ">")+handle]; !strings.Contains(strings.ToLower(tag), "readonly") {
		t.Fatalf("handle field is editable, so focus would land on it: %s", form)
	}
}

// TestTodo_AGENTUX_081_CheckboxLines pins the reaction checkbox: its label and
// its help are separate elements, and the shipped stylesheet stacks them on
// separate lines.
func TestTodo_AGENTUX_081_CheckboxLines(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	persona := PersonaAdminPersona{ID: "policy-helper", Name: "Policy Helper", Version: "3", Lifecycle: PersonaPublished}
	control := agentUXR7Render(t, personaAdminReactionsControl(locale, persona, PersonaAdminSnapshot{}))
	label := strings.Index(control, "React to questions with an emoji")
	hint := strings.Index(control, "adds an emoji to the question")
	if label < 0 || hint < 0 || hint < label {
		t.Fatalf("label and help are not separate, ordered elements: %s", control)
	}
	if !strings.Contains(control, "</label>") || !strings.Contains(control[:hint], "</label>") {
		t.Fatalf("label and help run in one element: %s", control)
	}
	if sheet := agentUX073Stylesheet(); !strings.Contains(sheet, ".persona-admin-reactions{display:grid") {
		t.Fatal("the shipped stylesheet no longer stacks the checkbox label above its help")
	}
}
