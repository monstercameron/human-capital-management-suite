package productui

import (
	"fmt"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	stdhtml "html"
	"strings"
	"testing"
	"time"
)

func agentUXR7Render(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}
func agentUXR7Contains(t *testing.T, markup string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(markup, want) {
			t.Fatalf("render missing %q", want)
		}
	}
}

// L1-L6, H6, G26, K2: used, unknown and no-document answers retain truthful sources and one row time.
func TestAgentUXR7_AnswerSourcesAndFailure(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		t.Run(language, func(t *testing.T) {
			locale := ResolveProductLocale(language).WithTimeZone("America/New_York")
			view := ApplyLocale(NewView(PageAgents, "tenant", "viewer", ""), locale)
			at := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
			task := AgentTask{ID: "answer", Title: "What is my leave allowance?", State: AgentTaskCompleted, AnsweringAgentDisplayName: "Policy Helper", AnswerText: "Ten days.", CreatedAt: at, UpdatedAt: at.Add(time.Second), Documents: []AgentTaskDocumentReference{{DocumentID: "leave", Label: "Leave policy"}}, UsedDocuments: []AgentTaskDocumentReference{{DocumentID: "leave", Label: "Leave policy", SectionAnchor: "allowance"}}, DocumentUsageState: AgentDocumentUsageUsed}
			markup := agentUXR7Render(t, RenderAgentTasksRegion(view, locale, AgentSnapshot{Tasks: []AgentTask{task}, SelectedTask: &task}))
			agentUXR7Contains(t, markup, `<bdi>What is my leave allowance?</bdi>`, agentUXR7Text(locale, "sources"), `docs?document=leave#allowance`, "Ten days.", `class="agents-detail-close-desktop"`, `class="agents-detail-back-mobile"`)
			times := agentUXR7Render(t, agentTaskRowTimes(locale, task, at.Add(time.Hour)))
			if strings.Count(times, "<time") != 1 {
				t.Fatal("row repeats its time")
			}
			if strings.Contains(times, ">13:00") {
				t.Fatal("time ignored viewer zone")
			}
			task.DocumentUsageState = AgentDocumentUsageUnknown
			markup = agentUXR7Render(t, agentTaskDocumentLinks(view, locale, task, true))
			agentUXR7Contains(t, markup, agentUXR7Text(locale, "attached"), "Leave policy", "docs?document=leave")
			task.DocumentUsageState = AgentDocumentUsageNone
			markup = agentUXR7Render(t, agentTaskDocumentLinks(view, locale, task, false))
			agentUXR7Contains(t, markup, locale.Text("agents.no_documents_used"))
			if strings.Contains(markup, "Leave policy") {
				t.Fatal("unused source claimed as used")
			}
			task.State = AgentTaskFailed
			task.FailureReason = "service unavailable"
			markup = agentUXR7Render(t, RenderAgentTasksRegion(view, locale, AgentSnapshot{Tasks: []AgentTask{task}, SelectedTask: &task}))
			agentUXR7Contains(t, markup, locale.Text("agents.task_failed_reassurance"), locale.Text("agents.failure_service"), `data-agent-ask-again="true"`, `data-agent-retry-prompt="What is my leave allowance?"`)
			markup = agentUXR7Render(t, RenderAgentTasksRegion(view, locale, AgentSnapshot{TasksLoadFailed: true}))
			agentUXR7Contains(t, markup, locale.Text("agents.tasks_load_failed"), `data-agent-tasks-retry="true"`)
			if strings.Contains(markup, locale.Text("agents.no_tasks")) {
				t.Fatal("load failure presented as empty")
			}
		})
	}
}

// L8-L10, L12-L19, K19, K29: owner actions, version history and explicit access results.
func TestAgentUXR7_SetupOwnerActionsHistoryAndAccess(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		t.Run(language, func(t *testing.T) {
			locale := ResolveProductLocale(language)
			persona := agentUXSetup2Persona()
			persona.Lifecycle = PersonaPublished
			persona.VersionHistory = []PersonaAdminVersionHistory{{Version: "4", Lifecycle: PersonaPublished, PublishedAt: "2026-10-01T13:00:00Z", PublishedBy: "Curtis Bell", Instructions: "New instruction", ConversationCount: 2}, {Version: "2", Lifecycle: PersonaPublished, PublishedAt: "2026-09-30T13:00:00Z", PublishedBy: "Curtis Bell", Instructions: "Old instruction", Guidance: "Old instruction", ConversationCount: 0}}
			snapshot := agentUXSetup2Snapshot(persona)
			markup := agentUXR7Render(t, personaAdminCard(locale, &personaAdminTestClient{}, persona, snapshot))
			agentUXR7Contains(t, markup, `data-persona-command="SUSPEND"`, `data-persona-confirm=`, agentUXR7Text(locale, "pause"), agentUXR7Text(locale, "ask_link"), agentUXR7Text(locale, "history"), "Curtis Bell", `version=2`, "Old instruction", `anchor-name:`)
			if strings.Contains(markup, `data-persona-command="ROLLBACK"`) {
				t.Fatal("unscoped rollback bypasses reviewed conversation preview")
			}
			editor := agentUXR7Render(t, personaAdminVersionEditor(locale, &personaAdminTestClient{}, persona, true))
			agentUXR7Contains(t, editor, agentUXR7Text(locale, "direct_always"), `data-agent-purpose-autosize`, `rows="3"`)
			persona.Lifecycle = PersonaSuspended
			markup = agentUXR7Render(t, personaAdminPauseButton(locale, persona, snapshot))
			agentUXR7Contains(t, markup, `data-persona-command="PUBLISH"`, agentUXR7Text(locale, "resume"))
			snapshot.AllowedCommands = nil
			markup = agentUXR7Render(t, personaAdminPauseButton(locale, persona, snapshot))
			agentUXR7Contains(t, markup, "disabled", agentUXR7Text(locale, "agent_administrator"))
			if strings.Contains(markup, persona.OwnerName) {
				t.Fatal("business owner falsely described as able to pause")
			}
			snapshot.Preview = PersonaAdminPreview{Subject: "ir-001-walt-brennan", Conversation: "general", OfficialDocumentTitles: []string{"Leave policy"}, EffectiveSkills: []PersonaAdminSkill{{ID: "knowledge_search_with_citations"}}}
			markup = agentUXR7Render(t, personaAdminPreview(locale, &personaAdminTestClient{}, snapshot))
			agentUXR7Contains(t, markup, `data-persona-preview-result="true"`, "Leave policy", persona.Name, "Walt Brennan")
			snapshot.Preview = PersonaAdminPreview{}
			snapshot.PreviewConversationID = ""
			snapshot.PreviewValidationFields = []string{"conversation"}
			markup = agentUXR7Render(t, personaAdminPreview(locale, &personaAdminTestClient{}, snapshot))
			agentUXR7Contains(t, markup, `aria-invalid="true"`)
			if strings.Count(markup, personaAdminPreviewValidationText(locale, "conversation")) != 1 {
				t.Fatal("access validation duplicated")
			}
		})
	}
}

// L20-L25: newest-first history, 25 rows per page, scoped streak and human location.
func TestAgentUXR7_ActivityFiltersPagingAndFailureWarning(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	runs := []AgentControlRun{}
	for i := 0; i < 38; i++ {
		state, version := "COMPLETED", "4"
		if i >= 33 {
			state, version = "FAILED", "6"
		}
		runs = append(runs, AgentControlRun{ID: fmt.Sprint(i), AgentID: "policy", Name: "Policy Helper", Version: version, State: state, FailureGate: "MODEL", RequestedBy: "Walt Brennan", Location: "#general", ConversationID: "general", Started: at.Add(time.Duration(i) * time.Minute).Format(time.RFC3339), Duration: "0s"})
	}
	page := FilterAgentRunHistory(runs, AgentRunHistoryFilter{Page: 1})
	if page.Total != 38 || len(page.Runs) != 25 || page.Runs[0].ID != "37" || page.Pages != 2 {
		t.Fatalf("first page %+v", page)
	}
	page = FilterAgentRunHistory(runs, AgentRunHistoryFilter{Page: 2})
	if len(page.Runs) != 13 || page.Runs[0].ID != "12" {
		t.Fatalf("second page %+v", page)
	}
	page = FilterAgentRunHistory(runs, AgentRunHistoryFilter{Outcome: "FAILED", Version: "6"})
	if page.Total != 5 {
		t.Fatalf("filter %+v", page)
	}
	foreign := append([]AgentControlRun(nil), runs...)
	foreign = append(foreign, AgentControlRun{AgentID: "another-policy", Name: "Policy Helper", Version: "6", State: "COMPLETED", Started: at.Add(time.Hour).Format(time.RFC3339)})
	if _, ok := AgentFailureStreak(foreign, "policy", "Policy Helper"); !ok {
		t.Fatal("same-named agent polluted failure history")
	}
	interleaved := append([]AgentControlRun(nil), runs...)
	interleaved = append(interleaved, AgentControlRun{AgentID: "policy", Name: "Policy Helper", Version: "4", State: "COMPLETED", Started: at.Add(time.Hour).Format(time.RFC3339)})
	if warning, ok := AgentFailureStreak(interleaved, "policy", "Policy Helper"); !ok || warning.Version != "6" || warning.Since != runs[33].Started {
		t.Fatal("another version's successful run hid the failing version")
	}
	warning, ok := AgentFailureStreak(runs, "policy", "Policy Helper")
	if !ok || warning.Version != "6" || warning.RollbackVersion != "4" || warning.Since != runs[33].Started {
		t.Fatalf("warning %+v %v", warning, ok)
	}
	runs[37].State = "COMPLETED"
	if _, ok = AgentFailureStreak(runs, "policy", "Policy Helper"); ok {
		t.Fatal("stale failure warning after success")
	}
	runs[37].State = "FAILED"
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		markup := agentUXR7Render(t, RenderAgentRunHistory(locale, runs, AgentRunHistoryFilter{Page: 1}))
		agentUXR7Contains(t, markup, `class="agent-history-table"`, `data-agent-history-filter="agent"`, `data-agent-history-filter="version"`, `data-agent-history-filter="outcome"`, `data-agent-history-page="2"`, `<bdi dir="ltr">#general</bdi>`, `href="`+personaAdminConversationHref("general")+`"`, `data-tone="danger"`)
		markup = agentUXR7Render(t, agentFailureWarning(locale, warning, "4", personaAdminRollbackHref("policy", "4"), agentSetupHref(locale)))
		agentUXR7Contains(t, markup, agentUXR7Text(locale, "pause"), `version=4`, `role="alert"`)
	}
}

// L22 and K40: current is the default; earlier reviewed versions remain actionable.
func TestAgentUXR7_RolloutCurrentAndEarlierReviewedVersion(t *testing.T) {
	snapshot := AgentRolloutSnapshot{Available: true, CanPreview: true, Personas: []AgentRolloutPersona{{ID: "policy", Name: "Policy Helper"}}, Versions: []AgentRolloutVersion{{PersonaID: "policy", Version: 4}, {PersonaID: "policy", Version: 6, Current: true}}, Installations: []AgentRolloutInstallation{{ID: "general", PersonaID: "policy", Version: 6, Visible: true, Name: "general", ConversationKind: "PUBLIC_CHANNEL", CanaryEligible: true}}}
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		markup := agentUXR7Render(t, RenderAgentRolloutForm(locale, snapshot))
		agentUXR7Contains(t, markup, `selected value="6"`, `<optgroup`, agentUXR7Text(locale, "rollback_group"), agentUXR7Text(locale, "all_current", "{count}", locale.FormatNumber("1", 0), "{version}", locale.FormatNumber("6", 0)))
		older := snapshot
		older.PersonaID = "policy"
		older.TargetVersion = 4
		markup = agentUXR7Render(t, RenderAgentRolloutForm(locale, older))
		agentUXR7Contains(t, markup, `selected value="4"`, agentUXR7Text(locale, "rollback_note", "{current}", locale.FormatNumber("6", 0), "{target}", locale.FormatNumber("4", 0)), `data-rollout-installation="general"`)
		if agentRolloutAllCurrent(older) {
			t.Fatal("earlier version falsely marked current")
		}
	}
}

// G6/G60/H5/H13/H14/H26/H31/K24/K31/K32/K41 and L3/L11/L14/L15/L17/L23/L28/L29.
func TestAgentUXR7_ResponsiveTokenStylesAndPopovers(t *testing.T) {
	css := agentUXR7Stylesheet()
	for _, width := range []int{1440, 800, 390, 320} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {

			rules := map[int]string{1440: "@media(min-width:1024px){.agents-tasks[data-has-task-detail=true]{grid-template-columns:420px minmax(0,1fr)}", 800: "@media(min-width:761px) and (max-width:800px)", 390: "@media(max-width:390px){.agents-task-row-heading", 320: "@media(max-width:359px){.persona-admin-page .persona-admin-card-controls{grid-template-columns:minmax(0,1fr)}"}
			agentUXR7Contains(t, css, rules[width])
			agentUXR7Contains(t, css, "min-width:0", "max-width:100%", "width:100%", "text-align:start", "[dir=rtl]", "grid-template-columns:420px minmax(0,1fr)", "@media(max-width:800px)", "@media(max-width:390px)", "@media(max-width:359px)", "overflow-x:auto", "position-try-fallbacks:flip-inline,flip-block", "inset-block-start:anchor(bottom)", "[popover]:not(:popover-open){display:none}", "field-sizing:content", "var(--hcm-color-danger)", "height:44px", "grid-template-columns:repeat(2,minmax(0,1fr))")
		})
	}
	if strings.Contains(css, "#fff") || strings.Contains(css, "font-family") {
		t.Fatal("page bypasses brand/theme tokens")
	}
}

// G28, K14-K23/K28/K31: all unpublished lifecycle states are observable and coherent in three locales.
func TestAgentUXR7_LifecycleStatesAndReviewerDecisions(t *testing.T) {
	states := []struct {
		name string
		edit func(*PersonaAdminPersona)
	}{{"draft", func(p *PersonaAdminPersona) { p.Lifecycle = PersonaDraft }}, {"review", func(p *PersonaAdminPersona) { p.Lifecycle = PersonaInReview }}, {"reviewed", func(p *PersonaAdminPersona) { p.ReviewApproved = true }}, {"evaluating", func(p *PersonaAdminPersona) { p.ReviewApproved = true; p.EvaluationStatus = "RUNNING" }}, {"failed evaluation", func(p *PersonaAdminPersona) {
		p.ReviewApproved = true
		p.EvaluationStatus = "FAILED"
		p.EvaluationFailed = 1
		p.EvaluationPassed = 7
		p.EvaluationFailureNames = []string{"missing-citation"}
	}}, {"evaluated", func(p *PersonaAdminPersona) {
		p.ReviewApproved = true
		p.EvaluationStatus = "PASSED"
		p.EvaluationRef = "eval"
		p.EvaluationPassed = 8
	}}, {"published", func(p *PersonaAdminPersona) {
		p.Lifecycle = PersonaPublished
		p.ReviewApproved = true
		p.EvaluationRef = "eval"
		p.EvaluationCaseCount = 8
	}}}
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		for _, test := range states {
			t.Run(language+"/"+test.name, func(t *testing.T) {
				locale := ResolveProductLocale(language)
				persona := agentUXSetup2Persona()
				test.edit(&persona)
				snapshot := agentUXSetup2Snapshot(persona)
				client := &agentUXR5ReviewClient{&personaAdminTestClient{}}
				markup := agentUXR7Render(t, personaAdminCard(locale, client, persona, snapshot))
				state := personaAdminLifecycleState(locale, persona)
				agentUXR7Contains(t, markup, `data-lifecycle-phase="`+string(state.Phase)+`"`, `data-lifecycle-heading="`+string(state.Phase)+`"`, `data-step-number="4"`)
				if strings.Count(markup, `data-persona-next-step=`) != 1 {
					t.Fatal("lifecycle tells the owner conflicting next steps")
				}
				if persona.Lifecycle == PersonaInReview && !persona.ReviewApproved {
					agentUXR7Contains(t, markup, `data-persona-review-decision="APPROVE"`, `data-review-decision-wrapper="REJECT"`)
				}
				if test.name == "failed evaluation" {
					agentUXR7Contains(t, markup, locale.FormatNumber("7", 0), locale.FormatNumber("1", 0))
				}
				if test.name == "published" {
					agentUXR7Contains(t, markup, locale.FormatNumber("8", 0), personaAdminR5SetupText(locale, "view_results"))
				}
			})
		}
	}
}

// H8/H9/H10/K25 and the missing editor/picker states: metadata, removal, actionable failures and references.
func TestAgentUXR7_DocumentPickerAndEditorStates(t *testing.T) {
	for _, language := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(language)
		chip := agentUXR7Render(t, AgentRequestDocumentChip(locale, "leave", "Leave policy", "allowance"))
		agentUXR7Contains(t, chip, `docs?document=leave#allowance`, locale.Text("agents.document_remove_named", map[string]string{"title": "Leave policy"}))
		suggestion := agentUXR7Render(t, AgentRequestDocumentResult(locale, AgentRequestDocumentSuggestion{DocumentID: "leave", Title: "Leave policy", Owner: "Walt Brennan", Folder: "People", Updated: "Oct 1", Snippet: "Ten days of leave."}, 0))
		agentUXR7Contains(t, suggestion, "Walt Brennan", "People", "Ten days of leave.")
		for _, state := range []AgentDocumentPickerState{AgentDocumentPickerReady, AgentDocumentPickerEmpty, AgentDocumentPickerFailed, AgentDocumentPickerLoading} {
			markup := agentUXR7Render(t, AgentDocumentReferencePicker(locale, AgentDocumentReferencePickerModel{ID: "r7-docs", Available: true, State: state, Suggestions: []AgentDocumentSuggestion{{DocumentID: "leave", Title: "Leave policy", Location: "Walt Brennan", Updated: "Oct 1"}}}))
			agentUXR7Contains(t, markup, `data-agentdoc-state="`+string(state)+`"`)
			if state == AgentDocumentPickerFailed {
				agentUXR7Contains(t, markup, `role="alert"`, stdhtml.EscapeString(agentDocumentPickerText(locale, "failed")), agentDocumentPickerText(locale, "retry"))
				if strings.Contains(markup, `type="search"`) {
					t.Fatal("failed search left an unusable field")
				}
			} else {
				agentUXR7Contains(t, markup, agentDocumentPickerStateText(locale, state, ""), `role="combobox"`)
			}
		}
		persona := agentUXSetup2Persona()
		persona.Guidance = "Follow the leave policy."
		persona.DocumentReferences = []PersonaAdminDocumentReference{{DocumentID: "leave", Title: "Leave policy", Readable: true}}
		markup := agentUXR7Render(t, personaAdminVersionEditor(locale, &personaAdminTestClient{}, persona, true))
		agentUXR7Contains(t, markup, `data-agentdoc-instructions="true"`, `data-agentdoc-mention-menu="true"`, `data-agentdoc-instruction-validation="true"`, personaAdminVersionText(locale, "cancel"))
		if strings.Contains(markup, `name="handle" type="text"`) {
			t.Fatal("version editor changed the live address")
		}
	}
}
