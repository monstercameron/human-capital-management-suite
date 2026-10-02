package productui

import (
	"strings"
	"testing"
)

type agentUXR5ReviewClient struct{ *personaAdminTestClient }

func (*agentUXR5ReviewClient) ReviewPersona(string, string) error { return nil }

func TestAgentUXR5Setup_LifecycleSingleSource(t *testing.T) {
	tests := []struct {
		name  string
		phase personaAdminLifecyclePhase
		make  func() PersonaAdminPersona
	}{
		{"draft", personaAdminPhaseDraft, func() PersonaAdminPersona { p := agentUXSetup2Persona(); p.Lifecycle = PersonaDraft; return p }},
		{"waiting review", personaAdminPhaseWaitingReview, func() PersonaAdminPersona { return agentUXSetup2Persona() }},
		{"ready evaluation", personaAdminPhaseReadyEvaluation, func() PersonaAdminPersona { p := agentUXSetup2Persona(); p.ReviewApproved = true; return p }},
		{"evaluation running", personaAdminPhaseEvaluationRunning, func() PersonaAdminPersona {
			p := agentUXSetup2Persona()
			p.ReviewApproved = true
			p.EvaluationStatus = "RUNNING"
			return p
		}},
		{"evaluation failed", personaAdminPhaseEvaluationFailed, func() PersonaAdminPersona {
			p := agentUXSetup2Persona()
			p.ReviewApproved = true
			p.EvaluationStatus = "FAILED"
			p.EvaluationFailed = 1
			return p
		}},
		{"ready publication", personaAdminPhaseReadyPublication, func() PersonaAdminPersona {
			p := agentUXSetup2Persona()
			p.ReviewApproved = true
			p.EvaluationStatus = "PASSED"
			p.EvaluationRef = "evaluation-6"
			return p
		}},
		{"published no placement", personaAdminPhasePublished, func() PersonaAdminPersona { p := agentUXSetup2Persona(); p.Lifecycle = PersonaPublished; return p }},
		{"published current placement", personaAdminPhasePublished, func() PersonaAdminPersona {
			p := agentUXSetup2Persona()
			p.Lifecycle = PersonaPublished
			p.Installations = []PersonaAdminInstallation{{Conversation: "general", Version: "4"}}
			return p
		}},
		{"published outdated placement", personaAdminPhasePublished, func() PersonaAdminPersona {
			p := agentUXSetup2Persona()
			p.Lifecycle = PersonaPublished
			p.Version = "6"
			p.Installations = []PersonaAdminInstallation{{Conversation: "general", Kind: "PUBLIC_CHANNEL", Version: "4"}}
			return p
		}},
	}
	for _, localeName := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(localeName)
		for _, tc := range tests {
			t.Run(localeName+"/"+tc.name, func(t *testing.T) {
				persona := tc.make()
				state := personaAdminLifecycleState(locale, persona)
				if state.Phase != tc.phase || state.StepLabel == "" || state.Badge != state.StepLabel || state.Heading == "" {
					t.Fatalf("inconsistent lifecycle presentation: %#v", state)
				}
				markup := personaAdminRender(t, personaAdminLifecycleBadges(locale, persona, state)) + personaAdminRender(t, personaAdminLifecycleProgress(locale, persona, state)) + personaAdminRender(t, personaAdminLifecycleSentence(locale, persona, state)) + personaAdminRender(t, personaAdminLifecycleBlock(locale, persona, agentUXSetup2Snapshot(persona)))
				for _, want := range []string{state.StepLabel, state.Heading, `data-lifecycle-phase="` + string(state.Phase) + `"`, `data-lifecycle-sentence="` + state.SentenceKey + `"`} {
					if !strings.Contains(markup, want) {
						t.Errorf("%s lifecycle output missing %q: %s", localeName, want, markup)
					}
				}
				if strings.Contains(markup, "{version}") || strings.Contains(markup, "version .") {
					t.Errorf("%s lifecycle output has unresolved copy: %s", localeName, markup)
				}
			})
		}
	}
}

func TestTodo_AGENTUX_046(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	persona := agentUXSetup2Persona()
	persona.Lifecycle, persona.Version = PersonaPublished, "6"
	persona.Installations = []PersonaAdminInstallation{{Conversation: "general", Kind: "PUBLIC_CHANNEL", Version: "4"}}
	state := personaAdminLifecycleState(locale, persona)
	markup := personaAdminRender(t, personaAdminLifecycleSentence(locale, persona, state))
	want := `Version 6 is published. #general still runs version 4. Move conversations to version 6 in <a href="/workspace/app/admin/agents?tab=rollout">Operations › Rollout</a>.`
	if !strings.Contains(markup, want) {
		t.Fatalf("published rollout copy is not exact: %s", markup)
	}
	persona.Installations[0].Version = "6"
	state = personaAdminLifecycleState(locale, persona)
	if current := personaAdminRender(t, personaAdminLifecycleSentence(locale, persona, state)); !strings.Contains(current, "Every conversation runs version 6.") {
		t.Fatalf("current rollout copy is unclear: %s", current)
	}
}

func TestTodo_AGENTUX_046_Browser(t *testing.T) {
	persona := agentUXSetup2Persona()
	persona.Lifecycle, persona.Version = PersonaPublished, "6"
	persona.Installations = []PersonaAdminInstallation{{Conversation: "general", Version: "4"}}
	markup := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, agentUXSetup2Snapshot(persona)))
	for _, want := range []string{`data-lifecycle-badge="published"`, "Version 6 · Published", "Step 4 of 4: Published", "<h4>Published</h4>", `href="/workspace/app/admin/agents?tab=rollout"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("published card missing %q: %s", want, markup)
		}
	}
}

func TestTodo_AGENTUX_046_Accessibility(t *testing.T) {
	persona := agentUXSetup2Persona()
	state := personaAdminLifecycleState(ResolveProductLocale("en-US"), persona)
	markup := personaAdminRender(t, personaAdminLifecycleProgress(ResolveProductLocale("en-US"), persona, state))
	for _, want := range []string{`aria-label="Publication progress"`, `aria-current="step"`, `data-step-number="1"`, `data-step-number="4"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("accessible lifecycle missing %q: %s", want, markup)
		}
	}
}

func TestAgentUXR5Setup_EditorAndMoreAreStableSiblings(t *testing.T) {
	persona := agentUXSetup2Persona()
	persona.Installations = []PersonaAdminInstallation{{ConversationID: "general", Conversation: "general", Kind: "PUBLIC_CHANNEL", Version: "4"}}
	markup := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, agentUXSetup2Snapshot(persona)))
	for _, want := range []string{`</div><form aria-label="Create a new agent version: Policy Helper"`, `</form><div class="persona-admin-secondary-actions"`, `popover="auto"`, `data-persona-editor-toggle="persona-admin-version-policy-helper"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("card sibling contract missing %q: %s", want, markup)
		}
	}
	css := personaAdminStylesheet() + agentUXR7Stylesheet()
	widths := []struct{ name, rule string }{{"800px", "@media(max-width:50rem)"}, {"390px", "@media(max-width:24.375rem)"}, {"320px", "@media(max-width:20rem)"}}
	for _, width := range widths {
		t.Run(width.name, func(t *testing.T) {
			if !strings.Contains(css, width.rule) || !strings.Contains(css, "width:100%") || !strings.Contains(css, `.persona-admin-lifecycle-progress li:not([data-step-state="current"]){display:flex`) {
				t.Fatalf("stable editor, menu, controls, placement and step layout missing at %s", width.name)
			}
			if !strings.Contains(markup, `class="persona-admin-version-editor"`) || !strings.Contains(markup, `popover="auto"`) {
				t.Fatalf("responsive panels missing from rendered tree at %s", width.name)
			}
		})
	}
}

func TestAgentUXR5Setup_NoStarterHasARealNextStep(t *testing.T) {
	for _, localeName := range []string{"en-US", "de-DE", "ar"} {
		snapshot := PersonaAdminSnapshot{Available: true, StarterCatalogAvailable: true}
		markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale(localeName)}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{}}))
		if strings.Contains(markup, `data-persona-new-agent=`) || !strings.Contains(markup, `id="persona-admin-new-agent-note"`) {
			t.Fatalf("%s left a dead new-agent action: %s", localeName, markup)
		}
	}
	unnamed := personaAdminRender(t, personaAdminNoStarterNotice(ResolveProductLocale("en-US"), nil))
	if !strings.Contains(unnamed, "Ask the person who manages this workspace.") || strings.Contains(unnamed, "<a ") {
		t.Fatalf("unnamed installer guidance is wrong: %s", unnamed)
	}
	named := personaAdminRender(t, personaAdminNoStarterNotice(ResolveProductLocale("en-US"), []PersonaAdminTarget{{ID: "ir-admin", Label: "Alex Morgan", Role: "platform_administrator"}}))
	if !strings.Contains(named, "Alex Morgan") || !strings.Contains(named, "Message Alex Morgan") {
		t.Fatalf("known installer is not actionable: %s", named)
	}
}

func TestAgentUXR5Setup_ReviewDocumentsAndEvidence(t *testing.T) {
	persona := agentUXSetup2Persona()
	persona.DocumentReferences = []PersonaAdminDocumentReference{{Title: "Readable", Readable: true}, {Label: "Restricted", Readable: false}}
	snapshot := agentUXSetup2Snapshot(persona)
	client := &agentUXR5ReviewClient{&personaAdminTestClient{}}
	markup := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), client, persona, snapshot))
	for _, want := range []string{"You cannot open 1 of the 2 documents", "Ask Walt Brennan for access", "Approve without reading the documents", `data-review-unreadable-submit=`, `disabled`} {
		if !strings.Contains(markup, want) {
			t.Errorf("review safeguard missing %q: %s", want, markup)
		}
	}
	owner := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, snapshot))
	if strings.Contains(owner, ">Approve</button>") || strings.Contains(owner, ">Remove</button>") == false {
		t.Fatalf("owner/reviewer authorization controls are contradictory: %s", owner)
	}
}

func TestAgentUXR5Setup_LifecycleActorsAndOutcomes(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	persona := agentUXSetup2Persona()
	for action, want := range map[string]string{
		"REQUEST_REVIEW": "Review requested for version 4.",
		"REVIEW":         "You approved version 4.",
		"RUN_EVALUATION": "Evaluation passed: 8 of 8 cases.",
		"PUBLISH":        "Version 4 published.",
	} {
		persona.EvaluationPassed, persona.EvaluationFailed = 8, 0
		if got := personaAdminCommandOutcomeText(locale, action, persona); got != want {
			t.Errorf("%s outcome = %q, want %q", action, got, want)
		}
	}
	authorSnapshot := agentUXSetup2Snapshot(persona)
	authorSnapshot.AllowedCommands = []string{"REQUEST_REVIEW"}
	author := personaAdminRender(t, personaAdminLifecycleBlock(locale, persona, authorSnapshot))
	for _, want := range []string{"Waiting for Curtis Bell", "You wrote version 4, so someone else must review it.", "Message Curtis Bell"} {
		if !strings.Contains(author, want) {
			t.Errorf("author waiting state missing %q: %s", want, author)
		}
	}
	persona.ReviewApproved = true
	evaluatorSnapshot := agentUXSetup2Snapshot(persona)
	evaluatorSnapshot.AllowedCommands = []string{"PUBLISH"}
	evaluator := personaAdminRender(t, personaAdminLifecycleBlock(locale, persona, evaluatorSnapshot))
	for _, want := range []string{"Next: Loretta Haynes runs the evaluation.", "Message Loretta Haynes"} {
		if !strings.Contains(evaluator, want) {
			t.Errorf("reviewer handoff missing %q: %s", want, evaluator)
		}
	}
	persona.EvaluationRef, persona.EvaluationStatus = "evaluation-4", "PASSED"
	persona.EvaluationPassedAt = "2026-10-01"
	persona.EvaluationRunnerName = "Loretta Haynes"
	ready := personaAdminRender(t, personaAdminLifecycleBlock(locale, persona, agentUXSetup2Snapshot(persona)))
	for _, want := range []string{"<h4>Ready to publish</h4>", `datetime="2026-10-01"`, "8 of 8 test questions", "run by Loretta Haynes", "View results", `href="/workspace/app/admin/agents?tab=rollout"`} {
		if !strings.Contains(ready, want) {
			t.Errorf("ready-to-publish evidence missing %q: %s", want, ready)
		}
	}
}

func TestAgentUXR5Setup_AccessCheckValidationAndResult(t *testing.T) {
	snapshot := agentUXSetup2Snapshot(agentUXSetup2Persona())
	snapshot.PreviewValidationFields = []string{"persona", "subject", "conversation"}
	snapshot.PreviewPersonaID = ""
	snapshot.PreviewSubjectID = ""
	snapshot.PreviewConversationID = ""
	invalid := personaAdminRender(t, personaAdminPreview(ResolveProductLocale("en-US"), &personaAdminTestClient{}, snapshot))
	if strings.Count(invalid, `aria-invalid="true"`) != 3 || !strings.Contains(invalid, `role="alert"`) || !strings.Contains(invalid, "Choose an agent.") {
		t.Fatalf("missing access-check fields are not marked: %s", invalid)
	}
	snapshot.PreviewValidationFields = nil
	snapshot.PreviewPersonaID = "policy-helper"
	snapshot.PreviewSubjectID = "ir-001-walt-brennan"
	snapshot.PreviewConversationID = "general"
	snapshot.Preview = PersonaAdminPreview{Subject: "Walt Brennan", Conversation: "general", DerivedData: []string{"POLICY_DOCUMENT"}, EffectiveSkills: []PersonaAdminSkill{{ID: "hcmnext.skill.knowledge_search_with_citations"}, {ID: "persona.chat_reply"}}, UnreadableDocumentCount: 2}
	result := personaAdminRender(t, personaAdminPreview(ResolveProductLocale("en-US"), &personaAdminTestClient{}, snapshot))
	for _, want := range []string{`data-persona-preview-result="true"`, "Policy documents", "search and cite", "reply in the conversation", "2 documents Walt cannot open"} {
		if !strings.Contains(result, want) {
			t.Errorf("access result missing %q: %s", want, result)
		}
	}
}

func TestAgentUXR5Setup_EditorPickerPlacementAndRTL(t *testing.T) {
	persona := agentUXSetup2Persona()
	persona.Instructions = "Follow @[Benefits policy]  and cite it."
	persona.DocumentReferences = []PersonaAdminDocumentReference{{Title: "سياسة الإجازات", Label: "Benefits policy", Readable: false, PinnedVersion: 3}}
	markup := personaAdminRender(t, personaAdminCard(ResolveProductLocale("ar"), &personaAdminTestClient{}, persona, agentUXSetup2Snapshot(persona)))
	for _, want := range []string{"حفظ كإصدار", `data-persona-channel-display="DIRECT_MESSAGE"`, "@Benefits policy", `dir="auto"`, "الإصدارات", "لا يمكنك فتح هذا المستند."} {
		if !strings.Contains(markup, want) {
			t.Errorf("localized editor/RTL contract missing %q: %s", want, markup)
		}
	}
	failed := personaAdminRender(t, AgentDocumentReferencePicker(ResolveProductLocale("en-US"), AgentDocumentReferencePickerModel{ID: "failed", Available: true, State: AgentDocumentPickerFailed}))
	if !strings.Contains(failed, "Couldn&#39;t load documents.") || !strings.Contains(failed, ">Try again</button>") || strings.Contains(failed, `role="combobox"`) {
		t.Fatalf("failed picker retains a dead search field: %s", failed)
	}
	placement := personaAdminRender(t, personaAdminAddPlacement(ResolveProductLocale("en-US"), persona, agentUXSetup2Snapshot(persona), nil))
	inReview := agentUXSetup2Persona()
	inReview.Version, inReview.LiveVersion = "6", "4"
	editor := personaAdminRender(t, personaAdminVersionEditor(ResolveProductLocale("en-US"), &personaAdminTestClient{}, inReview, true))
	for _, want := range []string{"Version 6 stays in review.", "Version 4 stays live.", "This card will show version 7; version 6 remains under Version history"} {
		if !strings.Contains(editor, want) {
			t.Errorf("in-review editor leaves the live version unclear: missing %q in %s", want, editor)
		}
	}
	arabic := ResolveProductLocale("ar")
	pinned := personaAdminRender(t, AgentDocumentReferencePicker(arabic, AgentDocumentReferencePickerModel{ID: "arabic", Available: true, References: []PersonaAdminDocumentReference{{DocumentID: "policy", Title: "Policy", Label: "Policy", VersionMode: "PINNED", PinnedVersion: 1, Readable: true}}}))
	if strings.Contains(pinned, "version 1") || !strings.Contains(pinned, arabic.FormatNumber("1", 0)) {
		t.Errorf("Arabic pinned version was not localized: %s", pinned)
	}
	for _, want := range []string{"#general · Channel", ">Add</button>", ">Cancel</button>", `popover="auto"`} {
		if !strings.Contains(placement, want) {
			t.Errorf("placement popover missing %q: %s", want, placement)
		}
	}
}
