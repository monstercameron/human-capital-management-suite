package productui

import (
	"fmt"
	"github.com/monstercameron/GoWebComponents/v5/html"
	"strings"
	"testing"
	"time"
)

func TestAgentUXR7_EmployeeAudienceAndOwnerDenials(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		view := ApplyLocale(NewView(PageAgents, "tenant", "employee", ""), locale)
		snapshot := AgentSnapshot{Availability: AgentsAvailable, StartAvailable: true}
		markup := agentUXR7Render(t, renderAgentsPage(view, locale, snapshot))
		agentUXR7Contains(t, markup, locale.Text("agents.general_agent_purpose"), agentUXR7Text(locale, "other_agents"), `aria-current="page"`)
		if strings.Contains(markup, `href="/workspace/app/admin/`) {
			t.Fatal("employee page advertises owner navigation")
		}
		for _, page := range []PageID{PageAgentOperations, PagePersonaAdmin} {
			view.Page = page
			denied := agentUXR7Render(t, agentOperationsDenied(view, locale))
			agentUXR7Contains(t, denied, agentUXR7Text(locale, "owner_page"), `href="`+Path(PageAgents))
			if strings.Count(denied, `class="button`) != 1 || strings.Contains(denied, `id="agent-controls"`) {
				t.Fatal("owner denial has duplicate recovery or mounts private data")
			}
		}
	}
}

func TestAgentUXR7_ActiveTaskPlanAndApproval(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), locale)
		task := AgentTask{ID: "plan", Title: "Compare the policies", State: AgentTaskAwaitingPlanConfirmation, Version: 3, Actions: AgentTaskActionPolicy{ConfirmPlan: true}, Steps: []AgentTaskStep{{Name: "Read the policies", State: "pending"}, {Name: "Compare the changes", State: "pending"}}}
		markup := agentUXR7Render(t, RenderAgentTaskDetail(view, locale, task, true))
		agentUXR7Contains(t, markup, locale.Text("agents.proposed_plan"), "Read the policies", "Compare the changes", `data-task-action="confirm-plan"`, `data-task-version="3"`)
		task.State = AgentTaskRunning
		task.LiveStep = "Read the policies"
		task.Actions = AgentTaskActionPolicy{Pause: true, Cancel: true}
		markup = agentUXR7Render(t, RenderAgentTaskDetail(view, locale, task, true))
		agentUXR7Contains(t, markup, "Read the policies", `data-task-action="pause"`, `data-task-action="cancel"`)
		if strings.Contains(markup, `data-task-action="confirm-plan"`) {
			t.Fatal("running task still offers plan approval")
		}
	}
}

func TestAgentUXR7_ReviewRejectionAndSavedDraftFeedback(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		persona := agentUXSetup2Persona()
		persona.Version, persona.Lifecycle = "7", PersonaDraft
		snapshot := agentUXSetup2Snapshot(persona)
		snapshot.CommandStatus, snapshot.CommandPersonaID, snapshot.CommandAction = "success", persona.ID, "REJECT"
		markup := agentUXR7Render(t, personaAdminCard(locale, &personaAdminTestClient{}, persona, snapshot))
		agentUXR7Contains(t, markup, agentUXR7Text(locale, "review_rejected", "{version}", locale.FormatNumber("7", 0)), `aria-live="polite"`)
		if strings.Contains(markup, personaAdminCommandOutcomeText(locale, "REVIEW", persona)) {
			t.Fatal("rejected review is reported as approved")
		}
		snapshot.CommandAction = "CREATE_VERSION"
		markup = agentUXR7Render(t, personaAdminCard(locale, &personaAdminTestClient{}, persona, snapshot))
		agentUXR7Contains(t, markup, personaAdminCommandOutcomeText(locale, "CREATE_VERSION", persona), locale.FormatNumber("7", 0))
		persona.Lifecycle = PersonaPublished
		markup = agentUXR7Render(t, personaAdminHeaderActions(locale, &personaAdminTestClient{}, persona, snapshot))
		if strings.Contains(markup, `button primary`) {
			t.Fatal("owner header makes asking or editing the filled action")
		}
	}
}

func TestAgentUXR7_RolloutStagesAndPortableDraft(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language).WithTimeZone("America/New_York")
		plan := AgentRolloutPlan{ID: "private-rollout", Digest: "private-digest", Version: 4, CanaryCount: 1, Candidates: []AgentRolloutCandidate{{InstallationID: "general"}}}
		snapshot := AgentRolloutSnapshot{CanApprove: true, CanAdvance: true, CanPromote: true, PersonaID: "policy", Personas: []AgentRolloutPersona{{ID: "policy", Name: "Policy Helper"}}, Installations: []AgentRolloutInstallation{{ID: "general", PersonaID: "policy", Name: "general", Visible: true, ConversationKind: "PUBLIC_CHANNEL"}}}
		for _, state := range []struct{ stage, action string }{{"PREVIEWED", "APPROVE"}, {"APPROVED", "ADVANCE"}, {"CANARY_COMPLETE", "PROMOTE"}, {"COMPLETE", ""}} {
			snapshot.Progress = &AgentRolloutProgress{Stage: state.stage, Revision: 2}
			markup := agentUXR7Render(t, agentRolloutPlan(locale, plan, snapshot))
			agentUXR7Contains(t, markup, "#general", agentRolloutStage(locale, state.stage))
			count := strings.Count(markup, `data-rollout-action=`)
			if state.action == "" && count != 0 || state.action != "" && count != 1 {
				t.Fatalf("%s exposes conflicting rollout actions: %s", state.stage, markup)
			}
			if state.action != "" {
				agentUXR7Contains(t, markup, `data-rollout-action="`+state.action+`"`, `data-confirm=`)
			}
			if strings.Contains(markup, `>private-rollout<`) || strings.Contains(markup, `>private-digest<`) {
				t.Fatal("rollout exposes internal authority identifiers")
			}
		}
		draft := AgentPortableReviewDraft{ID: "private-draft", Name: "Policy Helper", Version: 1, ImportedAt: "2026-10-01T13:00:00Z", ImportedBy: "Walt Brennan", Purpose: "Answer policy questions", Instructions: "Cite the policy."}
		portable := AgentPortableSnapshot{Available: true, CanExport: true, CanImport: true, Draft: &draft, Manifests: []AgentPortableManifest{{ID: "manifest", Name: "Policy Helper", Version: "2", PersonaVersion: "6", Live: true}}}
		markup := agentUXR7Render(t, agentPortablePanelForTab(locale, portable, "move"))
		agentUXR7Contains(t, markup, "Policy Helper", "Walt Brennan", "Cite the policy.", `data-definition-id="private-draft"`, agentUXR7Text(locale, "file_version"), agentUXR7Text(locale, "export_incomplete"))
		if strings.Contains(markup, `>private-draft<`) || strings.Contains(markup, "version 6 (live)") {
			t.Fatal("portable surface invents a current export or shows an internal draft identifier")
		}
	}
}

func TestAgentUXR7_ViewerDatesDurationsAndCompactRowTime(t *testing.T) {
	at := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language).WithTimeZone("America/New_York")
		instant := agentOperationsFormatInstant(locale, at.Format(time.RFC3339))
		expected := "09:00"
		if language == "en-US" {
			expected = "9:00 AM"
		}
		if !strings.Contains(instant, docsLocaleDigits(language, expected)) || strings.Contains(instant, "UTC") {
			t.Fatalf("%s ignores viewer clock: %s", language, instant)
		}
		if got := agentTaskRelativeTime(locale, at, at.Add(3*time.Hour)); got != agentUXR7Text(locale, "time_hours", "{count}", locale.FormatNumber("3", 0)) {
			t.Fatalf("row time is duplicated or not compact: %s", got)
		}
		if legacy := agentOperationsFormatInstant(locale, "1 Oct 2026, 13:00 UTC"); legacy != instant {
			t.Fatalf("legacy clock was not localized: %s", legacy)
		}
		expiry := agentUXR7Render(t, agentControlsFacts(locale, []string{"expires"}, []string{at.Format(time.RFC3339)}))
		agentUXR7Contains(t, expiry, instant)
		occurrence := agentUXR7Render(t, html.Div(html.Props{}, agentControlOccurrenceTimes(locale, []string{at.Format(time.RFC3339)})...))
		agentUXR7Contains(t, occurrence, instant, `datetime="2026-10-01T13:00:00Z"`)
		for _, duration := range []struct{ raw, key, count string }{{"0s", "under_second", ""}, {"12s", "duration_seconds", "12"}, {"2m", "duration_minutes", "2"}, {"1h", "duration_hours", "1"}} {
			if got := agentRunDurationLabel(locale, duration.raw); got != agentUXR7Text(locale, duration.key, "{count}", locale.FormatNumber(duration.count, 0)) {
				t.Fatalf("duration %s: %s", duration.raw, got)
			}
		}
	}
}

func TestAgentUXR7_FourOperationTabsAtEveryWidth(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		for _, width := range []int{1440, 800, 390, 320} {
			t.Run(language+"/"+fmt.Sprint(width), func(t *testing.T) {
				view := ApplyLocale(NewView(PageAgentOperations, "tenant", "owner", ""), ResolveProductLocale(language))
				view.AgentsProjection = &AgentsAvailabilityProjection{ViewerIsAdmin: true}
				view.Query = "tab=announcements"
				markup := agentUXR7Render(t, BuildAgentOperationsPage(view))
				if strings.Count(markup, `role="tab"`) != 4 {
					t.Fatal("operations omitted the announcements tab")
				}
				agentUXR7Contains(t, markup, `id="agent-announcements"`, `dir="`+string(view.Locale.Direction)+`"`, agentOperationsText(view.Locale, "tab_running"))
				agentUXR7Contains(t, agentUXR7Stylesheet(), ".agent-operations-tabs", "overflow-x:auto", "mask-image:linear-gradient", "min-width:0")
			})
		}
	}
}

func TestAgentUXR7_StylesOnlyLoadOnAgentPages(t *testing.T) {
	agent := agentUXR7Render(t, ProductPageFrame(ProductPageFrameProps{Class: "agent-page-frame", Title: "Agents"}))
	ordinary := agentUXR7Render(t, ProductPageFrame(ProductPageFrameProps{Title: "Home"}))
	if !strings.Contains(agent, agentUXR7Stylesheet()) {
		t.Fatal("inline CSS was escaped or truncated")
	}
	if strings.Count(agent, `data-agent-page-styles="true"`) != 1 || strings.Contains(ordinary, "data-agent-page-styles") {
		t.Fatal("agent CSS duplicated or loaded on another product page")
	}
}

func TestAgentUXR7_L20_SetupHistoryUnavailable(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		persona := agentUXSetup2Persona()
		persona.RecentRunsUnavailable = true
		markup := agentUXR7Render(t, personaAdminFailureWarning(locale, persona))
		agentUXR7Contains(t, markup, agentUXR7Text(locale, "history_unavailable"), `role="alert"`, `tab=running`)
	}
}

func TestAgentUXR7_G58_UnknownPortableNameDoesNotExposeIdentifier(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		label := agentPortableManifestLabel(locale, AgentPortableManifest{ID: "agent.starter.unlabelled", Version: "2"})
		if strings.Contains(label, "starter") || strings.Contains(label, "unlabelled") {
			t.Fatal("opaque manifest identity used as a visible name")
		}
	}
}

func TestAgentUXR7_LiveControlsAndRecovery(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		persona := agentUXSetup2Persona()
		persona.Lifecycle = PersonaPublished
		snapshot := AgentControlsSnapshot{Available: true, Agents: []PersonaAdminPersona{persona}, AllowedCommands: []string{"SUSPEND"}, Runs: []AgentControlRun{{ID: "private-run", Name: persona.Name, Version: "6", State: "RUNNING", Started: "2026-10-01T13:00:00Z", RequestedBy: "Walt Brennan", ConversationID: "general", Location: "#general", Duration: "12s", Revision: 3, Actions: []string{"pause", "resume", "stop"}, Installation: "private-installation", Spend: "123456", Cause: "internal-cause"}}}
		markup := agentUXR7Render(t, RenderAgentControls(locale, snapshot, ""))
		agentUXR7Contains(t, markup, `data-persona-command="SUSPEND"`, `data-persona-confirm=`, `data-owner-action="pause"`, `data-owner-action="resume"`, `data-owner-action="stop"`, `data-owner-confirm=`, `data-owner-revision="3"`, "Walt Brennan", `data-chevron=`, `<bdi dir="ltr">#general</bdi>`)
		agentUXR7Contains(t, markup, agentUXR7Text(locale, "pause_task"), agentUXR7Text(locale, "resume_task"), agentUXR7Text(locale, "cancel_task"))
		agentUXR7Contains(t, markup, agentUXR7Text(locale, "cost_unavailable"), agentUXR7Text(locale, "open_setup"))
		for _, internal := range []string{"private-installation", "123456", "internal-cause"} {
			if strings.Contains(markup, internal) {
				t.Fatalf("running details expose internal fact %s", internal)
			}
		}
		markup = agentUXR7Render(t, RenderAgentControls(locale, AgentControlsSnapshot{TechnicalContact: "Loretta Young"}, "timed_out"))
		agentUXR7Contains(t, markup, `role="alert"`, `data-owner-refresh="true"`, "Loretta Young", agentSetupHref(locale))
		snapshot.Runs = nil
		markup = agentUXR7Render(t, RenderAgentControls(locale, snapshot, ""))
		agentUXR7Contains(t, markup, agentControlsText(locale, "empty"), `data-persona-command="SUSPEND"`)
	}
}

func TestAgentUXR7_K37_UnknownCurrentVersionCannotBeLive(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		manifest := AgentPortableManifest{ID: "policy-helper", Name: "Policy Helper", Version: "2", Live: true}
		label := agentPortableManifestLabel(locale, manifest)
		if !strings.Contains(label, agentUXR7Text(locale, "file_version")) || strings.Contains(label, agentRPText(locale, "live_suffix")) {
			t.Fatalf("unverified file presented as live: %s", label)
		}
		markup := agentUXR7Render(t, agentPortablePanelForTab(locale, AgentPortableSnapshot{Available: true, CanExport: true, Manifests: []AgentPortableManifest{manifest}}, "move"))
		agentUXR7Contains(t, markup, agentUXR7Text(locale, "export_incomplete"))
		manifest.PersonaVersion = "2"
		if label = agentPortableManifestLabel(locale, manifest); strings.Contains(label, agentRPText(locale, "live_suffix")) {
			t.Fatalf("matching version numbers do not verify exported instructions: %s", label)
		}
	}
}
