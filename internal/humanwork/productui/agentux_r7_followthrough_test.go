package productui

import (
	"strings"
	"testing"
)

func TestAgentUXR7_L24_ActivityTabNamesFinishedWork(t *testing.T) {
	for index, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		view := ApplyLocale(NewView(PageAgentOperations, "tenant", "owner", ""), locale)
		markup := agentUXR7Render(t, agentOperationsTabs(view, locale))
		want := []string{"Activity", "Aktivität", "النشاط"}[index]
		agentUXR7Contains(t, markup, want, `data-agent-operations-tab="running"`)
		if strings.Contains(markup, []string{">Running<", ">Laufend<", ">قيد التشغيل<"}[index]) {
			t.Fatal("finished runs still live under a running-only label")
		}
		if language == "de-DE" {
			agentUXR7Contains(t, markup, "Versionswechsel")
		}
	}
}

func TestAgentUXR7_L9_PausedCardAndCommandFeedback(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		persona := agentUXSetup2Persona()
		persona.Lifecycle = PersonaSuspended
		snapshot := agentUXSetup2Snapshot(persona)
		markup := agentUXR7Render(t, personaAdminCard(locale, &personaAdminTestClient{}, persona, snapshot))
		agentUXR7Contains(t, markup, personaAdminR5Text(locale, "phase_suspended"), `data-persona-next-step="resume"`, `data-persona-command="PUBLISH"`, agentUXR7Text(locale, "resume"))
		if strings.Contains(markup, `data-status-tone="published"`) || strings.Contains(markup, `data-persona-next-step="next_published"`) {
			t.Fatal("paused agent is shown as healthy published work")
		}
		for _, action := range []struct{ action, key string }{{"SUSPEND", "pause_saved"}, {"RESUME", "resume_saved"}} {
			snapshot.CommandStatus, snapshot.CommandAction, snapshot.CommandPersonaID = "success", action.action, persona.ID
			markup = agentUXR7Render(t, personaAdminCard(locale, &personaAdminTestClient{}, persona, snapshot))
			agentUXR7Contains(t, markup, agentUXR7Text(locale, action.key, "{agent}", persona.Name))
		}
		persona.Lifecycle = PersonaPublished
		markup = agentUXR7Render(t, personaAdminPauseButton(locale, persona, snapshot))
		consequence := map[string]string{"en-US": "Tasks already running may stop before answering.", "de-DE": "Laufende Aufgaben können vor der Antwort anhalten.", "ar": "قد تتوقف المهام الجارية قبل الإجابة."}[language]
		agentUXR7Contains(t, markup, consequence, `data-persona-confirm=`)
	}
}

func TestAgentUXR7_K19_DocumentMetadataSeparatorsAndWarning(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		markup := agentUXR7Render(t, personaAdminDocumentReferences(locale, "policy", []PersonaAdminDocumentReference{{DocumentID: "leave", Title: "Leave policy", Location: "Walt Brennan", PinnedVersion: 1}}))
		agentUXR7Contains(t, markup, "Leave policy", personaAdminInlineSeparator+"Walt Brennan", `persona-admin-document-warning`, personaAdminR5SetupText(locale, "cannot_open_document"))
		if strings.Contains(markup, " ? ") {
			t.Fatal("document owner uses a corrupted separator")
		}
		agentUXR7Contains(t, agentUXR7Stylesheet(), ".persona-admin-document-warning{flex-basis:100%}")
	}
}

func TestAgentUXR7_L10_RollbackActionsOnlyNameEarlierVersions(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		persona := agentUXSetup2Persona()
		persona.Version = "6"
		persona.VersionHistory = []PersonaAdminVersionHistory{{Version: "7", Lifecycle: PersonaPublished}, {Version: "6", Lifecycle: PersonaPublished}, {Version: "4", Lifecycle: PersonaPublished}, {Version: "3", Lifecycle: PersonaDraft}}
		markup := agentUXR7Render(t, personaAdminVersionHistory(locale, persona))
		agentUXR7Contains(t, markup, `version=4`)
		for _, version := range []string{"7", "6", "3"} {
			if strings.Contains(markup, "version="+version) {
				t.Fatalf("rollback offered an ineligible version %s", version)
			}
		}
	}
}

func TestAgentUXR7_L3_TitleFitsBesideTimeInDetailColumn(t *testing.T) {
	css := agentUXR7Stylesheet()
	// A former 240px title minimum plus time, icons and gaps overfilled the
	// 420px detail-list column. The title must be allowed to shrink and wrap.
	start := strings.Index(css, ".agents-task-request{")
	if start < 0 {
		t.Fatal("title has no layout rule")
	}
	end := strings.Index(css[start:], "}")
	if end < 0 {
		t.Fatal("title layout rule is incomplete")
	}
	declaration := css[start : start+end]
	agentUXR7Contains(t, declaration, "min-inline-size:0", "overflow-wrap:anywhere", "text-align:start")
}

func TestAgentUXR7_L20_DraftDoesNotBecomeRollbackTarget(t *testing.T) {
	persona := agentUXSetup2Persona()
	persona.Version = "7"
	persona.Lifecycle = PersonaDraft
	persona.VersionHistory = []PersonaAdminVersionHistory{{Version: "7", Lifecycle: PersonaDraft}, {Version: "6", Lifecycle: PersonaPublished}, {Version: "4", Lifecycle: PersonaPublished}}
	for _, run := range []string{"1", "2", "3", "4", "5"} {
		persona.RecentRuns = append(persona.RecentRuns, AgentControlRun{ID: run, AgentID: persona.ID, Name: persona.Name, Version: "6", State: "FAILED", Started: "2026-10-01T13:00:00Z"})
	}
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		markup := agentUXR7Render(t, personaAdminFailureWarning(ResolveProductLocale(language), persona))
		agentUXR7Contains(t, markup, "version=4")
		if strings.Contains(markup, "version=6") {
			t.Fatal("rollback points at the same failing live version")
		}
		personaWithoutHistory := persona
		personaWithoutHistory.VersionHistory = nil
		markup = agentUXR7Render(t, personaAdminFailureWarning(ResolveProductLocale(language), personaWithoutHistory))
		if strings.Contains(markup, "version=4") || strings.Contains(markup, "version=6") {
			t.Fatal("run history alone invented a reviewed rollback target")
		}
	}
}

func TestAgentUXR7_G94_BuiltInRunNamesAreLocalized(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		markup := agentUXR7Render(t, RenderAgentRunHistory(locale, []AgentControlRun{{ID: "private-run", Name: "General agent", State: "COMPLETED", Version: "1"}}, AgentRunHistoryFilter{}))
		agentUXR7Contains(t, markup, locale.Text("agents.general_agent"))
		if language != "en-US" && strings.Contains(markup, ">General agent,") {
			t.Fatal("built-in agent uses English in localized history")
		}
		if got := agentControlDisplayName(locale, "run", "", "internal.worker-run-id"); strings.Contains(got, "worker") {
			t.Fatal("missing run name exposes its internal identifier")
		}
	}
}

func TestAgentUXR7_K31_ReadableCurrentAndNextStepOnPhones(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		persona := agentUXSetup2Persona()
		persona.Lifecycle = PersonaInReview
		persona.ReviewApproved = true
		persona.EvaluationRef = ""
		state := personaAdminLifecycleState(locale, persona)
		markup := agentUXR7Render(t, personaAdminLifecycleProgress(locale, persona, state))
		if strings.Count(markup, `data-step-number=`) != 4 {
			t.Fatal("phone progress lost a segment")
		}
		agentUXR7Contains(t, markup, `data-mobile-progress="true"`, state.StepLabel, state.NextStepLabel)
		for _, width := range []int{1440, 800, 390, 320} {
			css := agentUXR7Stylesheet()
			agentUXR7Contains(t, css, "font-size:.875rem", "font-weight:600")
			if width <= 480 {
				agentUXR7Contains(t, css, "@media(max-width:480px)", ".persona-admin-mobile-progress{display:block;")
			}
			if strings.Contains(css, "font-size:.7rem") {
				t.Fatal("progress text was reduced below 14px to fit")
			}
		}
	}
}

func TestAgentUXR7_L21_MobileRunDetailsAreCollapsed(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		run := AgentControlRun{ID: "run", Name: "Policy Helper", Version: "6", State: "FAILED", FailureGate: "MODEL_CALL", RequestedBy: "Walt Brennan", ConversationID: "general", Location: "#general"}
		markup := agentUXR7Render(t, agentUXR7RecentRunCard(locale, run))
		position := strings.Index(markup, `<details class="agent-operations-technical">`)
		if position < 0 {
			t.Fatal("mobile run repeats all expanded details")
		}
		reason, action := agentOpsFailureCopy(locale, run.FailureGate)
		if strings.Count(markup[:position], reason) != 1 {
			t.Fatal("collapsed mobile run duplicates its failure cause")
		}
		agentUXR7Contains(t, markup[:position], action, `data-tone="danger"`, `<bdi dir="ltr">#general</bdi>`)
		agentUXR7Contains(t, markup[position:], agentControlsText(locale, "technical"), agentUXR7Text(locale, "cost_unavailable"))
	}
}

func TestAgentUXR7_OwnerRoleListAndLimitContact(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		persona := agentUXSetup2Persona()
		persona.Audience = "employees,agent_administrator"
		markup := agentUXR7Render(t, personaAdminAudienceDefinition(locale, persona))
		agentUXR7Contains(t, markup, `persona-admin-audience-details`, `<summary>`, `<ul>`, `<li>`)
		persona.Steward, persona.StewardName = "loretta", "Loretta Young"
		markup = agentUXR7Render(t, personaAdminLimitsDefinition(locale, persona))
		agentUXR7Contains(t, markup, agentUXR7Text(locale, "limit_help"), "Loretta Young", `person=loretta`)
		persona.Steward, persona.StewardName = "", ""
		markup = agentUXR7Render(t, personaAdminLimitsDefinition(locale, persona))
		agentUXR7Contains(t, markup, personaAdminText(locale, "steward"))
		if strings.Contains(markup, `href=`) {
			t.Fatal("missing contact produced an empty person link")
		}
		markup = agentUXR7Render(t, personaAdminNoStarterNotice(locale, nil))
		if strings.Contains(strings.ToLower(markup), "platform") || strings.Contains(markup, "المنصة") {
			t.Fatal("setup contains platform vocabulary")
		}
	}
}

func TestAgentUXR7_AnnouncementsContentAndRecovery(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		for _, snapshot := range []AgentAnnouncementsSnapshot{{Loading: true}, {}, {Available: true}, {Available: true, CanCreate: true, Rows: []AgentAnnouncementRow{{ID: "announcement", AgentName: "Policy Helper", ConversationName: "#general", Instruction: "Share the coming holidays.", MessageHref: "/workspace/app/chat?message=answer", LastResult: "Posted", State: "ACTIVE", Revision: 1}}}} {
			markup := agentUXR7Render(t, RenderAgentAnnouncements(locale, snapshot))
			agentUXR7Contains(t, markup, agentAnnouncementText(locale, "help"), `data-announcement-new="true"`)
			if snapshot.Loading {
				agentUXR7Contains(t, markup, agentAnnouncementText(locale, "loading"), `role="status"`)
			} else if !snapshot.Available {
				agentUXR7Contains(t, markup, agentAnnouncementText(locale, "failed"), `data-announcement-retry="true"`, `role="alert"`)
			} else if len(snapshot.Rows) == 0 {
				agentUXR7Contains(t, markup, agentAnnouncementText(locale, "empty"))
			} else {
				agentUXR7Contains(t, markup, "Share the coming holidays.", `message=answer`, `data-announcement-action="pause"`, `data-announcement-action="post"`)
			}
		}
	}
}

func TestAgentUXR7_OwnerSchedulesUseAnnouncementEditor(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		markup := agentUXR7Render(t, RenderAgentControls(locale, AgentControlsSnapshot{Available: true, CanDraft: true, Schedules: []AgentControlSchedule{{ID: "private-schedule", Name: "Policy update", State: "ACTIVE", Version: "6", Installation: "private-installation", Destination: "private-destination", Recurrence: "internal-recurrence", Budget: "1234567"}}}, ""))
		agentUXR7Contains(t, markup, `tab=announcements`, agentUXR7Text(locale, "manage_announcements"), "Policy update")
		for _, value := range []string{"private-installation", "private-destination", "internal-recurrence", "1234567", "agent-schedule-max_input", "agent-schedule-revision"} {
			if strings.Contains(markup, value) {
				t.Fatalf("owner editor exposes internal setting %s", value)
			}
		}
	}
}

func TestAgentUXR7_L29_AccessResultIsolatesChannelName(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		markup := agentUXR7Render(t, personaAdminAccessCheckResult(locale, "Policy Helper", "Walt Brennan", "#general", PersonaAdminPreview{OfficialDocumentTitles: []string{"Leave policy"}}, ""))
		agentUXR7Contains(t, markup, `<bdi dir="ltr">#general</bdi>`, "Walt Brennan", "Policy Helper", "Leave policy", `data-persona-preview-result="true"`)
	}
}
