package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_AGENTUX_002(t *testing.T) {
	view := ApplyLocale(NewView(PageAgentOperations, "tenant-1", "owner-1", ""), ResolveProductLocale("en-US"))
	view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
	view.EffectivePermissions = []RolePagePermission{{Page: PageAdmin, View: true}, {Page: PageChatSettings, View: true, Update: true}, {Page: PagePersonaAdmin, View: true}}
	markup := renderAgentOperationsTest(t, BuildAgentOperationsPage(view))
	for _, want := range []string{
		"Agent operations", "Activity", "Rollout", "Move an agent between workspaces",
		"Ask", "Setup", "Operations", `href="/workspace/app/admin/personas?locale=en-US"`, `id="agent-controls"`, `id="agent-rollout-portable"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("agent operations page missing %q:\n%s", want, markup)
		}
	}
	if strings.Count(markup, "<h1") != 1 || strings.Contains(markup, "Agent owner controls") || strings.Contains(markup, "Agent administration") {
		t.Fatalf("agent operations page has the wrong heading hierarchy:\n%s", markup)
	}
	definition, ok := LookupPage(PageAgentOperations)
	if !ok || definition.Route != "/workspace/app/admin/agents" || definition.ParentNav != PageAdmin || definition.RenderOrder != 178 {
		t.Fatalf("agent operations registration = %+v, present=%t", definition, ok)
	}
}

func TestTodo_AGENTUX_002_Browser(t *testing.T) {
	snapshot := AgentControlsSnapshot{
		Available: true,
		Schedules: []AgentControlSchedule{{
			ID: "policy-helper-v4", Name: "Policy Helper", Revision: 4,
			State: "PAUSED", Actions: []string{"resume", "retire"},
		}},
	}
	markup := renderAgentOperationsTest(t, RenderAgentControls(ResolveProductLocale("en-US"), snapshot, "done"))
	for _, want := range []string{
		"Policy Helper", "Paused", "Resume Policy Helper", "Resume Policy Helper for everyone?", "Run details",
		`aria-live="polite"`, `data-busy-label="Working…"`, `class="persona-admin-editor-field"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("owner control missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "<h4>policy-helper-v4</h4>") || strings.Contains(markup, ">Resume policy-helper-v4<") || strings.Contains(markup, "Refresh controls") || strings.Count(markup, `data-owner-refresh="true"`) > 1 {
		t.Fatalf("owner control exposed an identifier or duplicate/technical refresh label:\n%s", markup)
	}
	portable := renderAgentOperationsTest(t, AgentRolloutPortableMount(ResolveProductLocale("en-US"), AgentRolloutSnapshot{
		Available: true, CanPreview: true, CanApprove: true, PersonaID: "policy-helper",
		Personas:      []AgentRolloutPersona{{ID: "policy-helper", Name: "Policy Helper"}},
		Versions:      []AgentRolloutVersion{{PersonaID: "policy-helper", Version: 4}},
		Installations: []AgentRolloutInstallation{{ID: "install-opaque", PersonaID: "policy-helper", Name: "Benefits team chat", Visible: true, ConversationKind: "PRIVATE_CHANNEL", MemberCount: 4}},
		Active:        &AgentRolloutPlan{ID: "rollout-opaque", Version: 4}, Progress: &AgentRolloutProgress{Revision: 2, Stage: "PREVIEWED"},
	}, AgentPortableSnapshot{Available: true, CanExport: true, CanImport: true, Manifests: []AgentPortableManifest{{ID: "manifest-opaque", Version: "4", Name: "Policy Helper"}}}))
	for _, want := range []string{"Rollout", "Move an agent between workspaces", "Benefits team chat", ">Approve<", "Policy Helper", "version 4", "1. Export", "Download agent file", "2. Import", "Choose an agent file", "Import as draft", "Import this agent as a draft?", "3. Review imported drafts", `data-rollout-id="rollout-opaque"`} {
		if !strings.Contains(portable, want) {
			t.Errorf("rollout/portable region missing %q:\n%s", want, portable)
		}
	}
	if strings.Contains(portable, ">install-opaque<") || strings.Contains(portable, ">manifest-opaque<") || strings.Count(portable, `data-rollout-action="REFRESH"`) > 1 {
		t.Fatalf("rollout/portable labels exposed identifiers or duplicate refresh controls:\n%s", portable)
	}
	states := renderAgentOperationsTest(t, AgentRolloutPortableMount(ResolveProductLocale("en-US"), AgentRolloutSnapshot{Available: true, CanPreview: true}, AgentPortableSnapshot{Available: true}))
	for _, want := range []string{"Review and publish an agent in Agent setup", "No portable agent files are available"} {
		if !strings.Contains(states, want) {
			t.Errorf("empty region does not explain the next step %q:\n%s", want, states)
		}
	}
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		view := NewView(PageAgentOperations, "tenant-1", "owner-1", "")
		view.Locale = ResolveProductLocale(localeID)
		view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
		view.EffectivePermissions = []RolePagePermission{{Page: PagePersonaAdmin, View: true}}
		localized := renderAgentOperationsTest(t, BuildAgentOperationsPage(view))
		if strings.Contains(localized, "⟦") || !strings.Contains(localized, `dir="`+string(view.Locale.Direction)+`"`) {
			t.Fatalf("%s page is untranslated or lost direction:\n%s", localeID, localized)
		}
	}
}

func TestTodo_AGENTUX_015(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	snapshot := AgentControlsSnapshot{Available: true, Runs: []AgentControlRun{{
		ID: "run-secret", Name: "Policy Helper", Version: "2", State: "RUNNING", Since: "9:42 AM", Revision: 7, Actions: []string{"pause", "resume", "stop"},
	}}}
	markup := renderAgentOperationsTest(t, RenderAgentControls(locale, snapshot, "done"))
	for _, want := range []string{"Policy Helper, version 2", "Running", "Since", "9:42 AM", "Pause task Policy Helper, version 2", "Resume task Policy Helper, version 2", "Cancel task Policy Helper, version 2", "Pause task Policy Helper, version 2 for everyone?", "Refresh running agents"} {
		if !strings.Contains(markup, want) {
			t.Errorf("running-agent list missing %q:\n%s", want, markup)
		}
	}
	empty := renderAgentOperationsTest(t, RenderAgentControls(locale, AgentControlsSnapshot{Available: true}, "done"))
	for _, want := range []string{"No agents are running right now.", "Recent finished runs are listed below."} {
		if !strings.Contains(empty, want) {
			t.Errorf("running-agent empty state missing %q: %s", want, empty)
		}
	}
}

func TestTodo_AGENTUX_015_Browser(t *testing.T) {
	markup := renderAgentOperationsTest(t, AgentRolloutPortableMount(ResolveProductLocale("en-US"), AgentRolloutSnapshot{Available: true}, AgentPortableSnapshot{Available: true, CanExport: true, CanImport: true, Manifests: []AgentPortableManifest{{ID: "policy-helper", Version: "2", Name: "Policy Helper"}}, Definition: `{"purpose":"policy"}`, Drafts: []AgentPortableReviewDraft{{ID: "opaque-draft", Name: "Policy Helper", ImportedAt: "Sep 30, 2026"}}}))
	for _, want := range []string{"1. Export", "Policy Helper · File version 2", "Download agent file", "Copy", "2. Import", `type="file"`, "Import preview", "3. Review imported drafts", "Policy Helper", "Sep 30, 2026", `data-definition-id="opaque-draft"`, ">Open<"} {
		if !strings.Contains(markup, want) {
			t.Errorf("portable workflow missing %q:\n%s", want, markup)
		}
	}
	for _, forbidden := range []string{"Definition version", "Imported draft identifier", `id="agent-portable-draft-id"`, "Destination mappings</legend>"} {
		if strings.Contains(markup, forbidden) {
			t.Errorf("portable workflow retained internal or empty control %q:\n%s", forbidden, markup)
		}
	}
	incomplete := renderAgentOperationsTest(t, AgentRolloutPortableMount(ResolveProductLocale("en-US"), AgentRolloutSnapshot{}, AgentPortableSnapshot{Available: true, CanImport: true, Manifests: []AgentPortableManifest{{ID: "policy-helper", Version: "2", Name: "Policy Helper"}}}))
	if !strings.Contains(incomplete, `class="button primary" data-busy-label="Working…" type="submit">Import as draft</button>`) || strings.Contains(incomplete, `class="button primary" data-busy-label="Working…" disabled`) {
		t.Fatalf("invalid import inputs presented a filled primary action: %s", incomplete)
	}
	view := ApplyLocale(NewView(PageAgentOperations, "tenant-1", "owner-1", ""), ResolveProductLocale("en-US"))
	view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
	view.EffectivePermissions = []RolePagePermission{{Page: PageAdmin, View: true}, {Page: PagePersonaAdmin, View: true}, {Page: PageAgentOperations, View: true}}
	shell := renderAgentOperationsTest(t, BuildShell(view, BuildAgentOperationsPage(view), false))
	current := strings.Index(shell, `aria-current="page"`)
	if current < 0 || !strings.Contains(shell[current:min(len(shell), current+400)], `/workspace/app/admin/agents`) {
		t.Fatalf("Agent operations is not current in the Admin navigation: %s", shell)
	}
}

func TestTodo_AGENTUX_015_Security(t *testing.T) {
	denied := renderAgentOperationsTest(t, RenderAgentControls(ResolveProductLocale("en-US"), AgentControlsSnapshot{OwnerName: "Walt Brennan"}, "denied"))
	for _, want := range []string{"Only the agent&#39;s owner or steward", "Owner: Walt Brennan"} {
		if !strings.Contains(denied, want) {
			t.Errorf("owner notice missing %q: %s", want, denied)
		}
	}
	for _, forbidden := range []string{"data-owner-refresh", "data-owner-action", "run-secret"} {
		if strings.Contains(denied, forbidden) {
			t.Errorf("denied projection exposed %q: %s", forbidden, denied)
		}
	}
}

func TestTodo_AGENTUX_018_Ops(t *testing.T) {
	for _, localeID := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeID)
		label := agentPortableManifestLabel(locale, AgentPortableManifest{ID: "policy-helper", Version: "2"})
		if label == "" || strings.Contains(strings.ToLower(label), "persona") || strings.Contains(strings.ToLower(label), "definition") || strings.Contains(strings.ToLower(label), "installation") {
			t.Fatalf("%s agent label uses internal vocabulary: %q", localeID, label)
		}
		markup := renderAgentOperationsTest(t, AgentRolloutPortableMount(locale, AgentRolloutSnapshot{Available: true, CanPreview: true}, AgentPortableSnapshot{Available: true}))
		for _, want := range []string{agentRPText(locale, "setup_link"), agentRPText(locale, "setup_suffix")} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s operations link missing %q: %s", localeID, want, markup)
			}
		}
	}
}

func TestTodo_AGENTUX_018_OpsNavigationVocabulary(t *testing.T) {
	wants := map[string][4]string{
		"en-US": {"Agent setup", "Agent setup", "Decide who each agent is, what it may read and do, and where people can use it.", "portable agent files"},
		"de-DE": {"Agenteneinrichtung", "Agenteneinrichtung", "Legen Sie fest, wer jeder Agent ist, was er lesen und tun darf und wo Menschen ihn verwenden können.", "portable Agentendateien"},
		"ar":    {"إعداد الوكلاء", "إعداد الوكلاء", "حدّد هوية كل وكيل وما يمكنه قراءته وفعله وأين يمكن للأشخاص استخدامه.", "ملفات الوكلاء المحمولة"},
	}
	for localeID, want := range wants {
		locale := ResolveProductLocale(localeID)
		got := [4]string{locale.Text("page.personas.label"), locale.Text("page.personas.title"), locale.Text("page.personas.subtitle"), locale.Text("page.agent_operations.subtitle")}
		for index := range want {
			if !strings.Contains(got[index], want[index]) {
				t.Errorf("%s copy %d = %q, want %q", localeID, index, got[index], want[index])
			}
		}
	}
}

func TestTodo_AGENTUX_015_Styles(t *testing.T) {
	css := Stylesheet()
	for _, selector := range []string{".agent-operations-region-header", ".agent-portable-step", ".agent-portable-draft-row"} {
		declarations := declarationsFor(css, selector)
		if !strings.Contains(declarations, "min-width:0") {
			t.Errorf("%s does not guard narrow-width overflow: %s", selector, declarations)
		}
	}
	if !strings.Contains(css, "@media (max-width:800px)") || !strings.Contains(css, "grid-template-columns:minmax(0,1fr)") {
		t.Errorf("operations regions do not stack below 800px: %s", css)
	}
	if !strings.Contains(css, "#agent-rollout-portable :is(input,select,textarea){background:var(--hcm-color-surface,var(--surface))") {
		t.Errorf("rollout inputs do not use the surface token")
	}
	if physical := agentsStylesheet(); strings.Contains(physical, "margin-left") || strings.Contains(physical, "margin-right") || strings.Contains(physical, "padding-left") || strings.Contains(physical, "padding-right") {
		t.Fatalf("operations styles use physical positioning and cannot mirror in RTL")
	}
}

func TestAgentUXR6Ops_DeepLinkSelectsRolloutOnServerRender(t *testing.T) {
	view := ApplyLocale(NewView(PageAgentOperations, "tenant-1", "owner-1", ""), ResolveProductLocale("en-US"))
	view.Query = "rollout"
	view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
	markup := renderAgentOperationsTest(t, BuildAgentOperationsPage(view))
	for _, want := range []string{`id="agent-operations-tab-rollout"`, `aria-selected="true"`, `id="agent-rollout-panel"`, `hidden id="agent-controls"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("rollout deep link missing %q:\n%s", want, markup)
		}
	}
}

func TestAgentUXR6Ops_LocalizedOperationsTimesAndStoppedResponding(t *testing.T) {
	snapshot := AgentControlsSnapshot{Available: true, UpdatedAt: "2026-10-01T10:58:37Z", Runs: []AgentControlRun{{
		ID: "run-a", Name: "Policy Helper", Version: "6", State: "STOPPED_RESPONDING", Started: "2026-10-01T05:41:09Z", Since: "2026-10-01T10:58:37Z", RequestedBy: "Walt Brennan", Location: "Your conversation with Policy Helper",
	}}}
	markup := renderAgentOperationsTest(t, RenderAgentControls(ResolveProductLocale("en-US"), snapshot, "done"))
	for _, want := range []string{"Stopped responding", "Walt Brennan", "Your conversation with Policy Helper"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("localized stopped run missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, ">2026-10-01T") || strings.Contains(markup, "Status: Running") {
		t.Fatalf("raw time or running state leaked:\n%s", markup)
	}
}

func TestAgentUXR6Ops_RolloutDefaultsLabelsAndStages(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	snapshot := AgentRolloutSnapshot{
		Available: true, CanPreview: true, CanApprove: true, PersonaID: "policy-helper",
		Personas: []AgentRolloutPersona{{ID: "policy-helper", Name: "Policy Helper"}},
		Versions: []AgentRolloutVersion{{PersonaID: "policy-helper", Version: 4, Current: true, PublishedAt: "2026-09-01"}, {PersonaID: "policy-helper", Version: 6, PublishedAt: "2026-10-01"}},
		Installations: []AgentRolloutInstallation{
			{ID: "inst-general", PersonaID: "policy-helper", Name: "general", ConversationKind: "PUBLIC_CHANNEL", MemberCount: 12, Visible: true, Version: 4, CanaryEligible: true},
			{ID: "inst-direct", PersonaID: "policy-helper", Name: "Walt Brennan", ConversationKind: "DIRECT", MemberCount: 2, Visible: true, Version: 4, CanaryEligible: true},
			{ID: "inst-private", PersonaID: "policy-helper", ConversationKind: "DIRECT", Visible: false, Version: 4, CanaryEligible: true},
		},
		Active:   &AgentRolloutPlan{ID: "rollout-a", Digest: "sha256:plan", Version: 6, CanaryCount: 1, Candidates: []AgentRolloutCandidate{{InstallationID: "inst-general"}, {InstallationID: "inst-direct"}}},
		Progress: &AgentRolloutProgress{Revision: 1, Stage: "PREVIEWED"},
	}
	markup := renderAgentOperationsTest(t, AgentRolloutPortableMountForTab(locale, snapshot, AgentPortableSnapshot{Available: true}, "rollout"))
	for _, want := range []string{`selected value="4"`, "Your conversation with Policy Helper", "A person&#39;s private conversation with Policy Helper", "At least one conversation is updated first.", ">Approve<"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("rollout render missing %q:\n%s", want, markup)
		}
	}
	snapshot.CanApprove, snapshot.CanAdvance, snapshot.CanPromote = false, true, false
	snapshot.Progress = &AgentRolloutProgress{Revision: 2, Stage: "APPROVED", Cursor: 0}
	first := renderAgentOperationsTest(t, AgentRolloutPortableMountForTab(locale, snapshot, AgentPortableSnapshot{Available: true}, "rollout"))
	if !strings.Contains(first, ">Update the first conversation<") {
		t.Fatalf("approved stage action unclear:\n%s", first)
	}
	snapshot.CanAdvance, snapshot.CanPromote = false, true
	snapshot.Progress = &AgentRolloutProgress{Revision: 3, Stage: "CANARY_COMPLETE", Cursor: 1}
	check := renderAgentOperationsTest(t, AgentRolloutPortableMountForTab(locale, snapshot, AgentPortableSnapshot{Available: true}, "rollout"))
	if !strings.Contains(check, ">Looks right, update the rest<") {
		t.Fatalf("canary check action unclear:\n%s", check)
	}
}

func TestTodo_AGENTUX_002_Security(t *testing.T) {
	view := NewView(PageAgentOperations, "tenant-1", "viewer-1", "")
	view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: false}
	view.EffectivePermissions = []RolePagePermission{{Page: PagePersonaAdmin, View: false}, {Page: PageAgentOperations, View: true}}
	markup := renderAgentOperationsTest(t, BuildAgentOperationsPage(view))
	if !strings.Contains(markup, `data-agent-operations-state="denied"`) || !strings.Contains(markup, "people who manage agents") {
		t.Fatalf("denied page did not explain the owner permission:\n%s", markup)
	}
	for _, forbidden := range []string{"agent-controls", "agent-rollout-portable", "data-owner-action", "data-portable-action", "/api/agents/"} {
		if strings.Contains(markup, forbidden) {
			t.Fatalf("denied page leaked owner controls %q:\n%s", forbidden, markup)
		}
	}
	if PageVisible(PageAgentOperations, []string{"manager"}) || PageVisible(PageAgentOperations, []string{"worker_self"}) || !PageVisible(PageAgentOperations, []string{RoleHCMAdmin}) {
		t.Fatal("agent operations route did not preserve the administrator-only audience")
	}
}

func renderAgentOperationsTest(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}
