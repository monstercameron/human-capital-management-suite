package productui

import (
	"strings"
	"testing"
)

func agentUXSetup2Snapshot(persona PersonaAdminPersona) PersonaAdminSnapshot {
	return PersonaAdminSnapshot{
		Available: true, DocumentServiceAvailable: true, CommandPermissionsAvailable: true,
		AllowedCommands:  []string{"CREATE_VERSION", "REQUEST_REVIEW", "REVIEW", "RUN_EVALUATION", "PUBLISH", "ROLLBACK", "INSTALL", "UNINSTALL", "SUSPEND", "RETIRE"},
		Personas:         []PersonaAdminPersona{persona},
		SubjectOptions:   []PersonaAdminTarget{{ID: "ir-001-walt-brennan", Label: "Walt Brennan", Role: "hcm_admin"}},
		Conversations:    []PersonaAdminTarget{{ID: "general", Label: "general", Kind: "CHANNEL"}, {ID: "direct-walt", Label: "Walt Brennan", Kind: "DIRECT_MESSAGE"}, {ID: "benefits", Label: "benefits", Kind: "CHANNEL"}},
		PreviewPersonaID: persona.ID, PreviewSubjectID: "ir-001-walt-brennan", PreviewConversationID: "general",
	}
}

func agentUXSetup2Persona() PersonaAdminPersona {
	return PersonaAdminPersona{
		ID: "policy-helper", StarterID: "hcmnext.persona_template.policy_helper", StarterVersion: 1,
		Handle: "policy-helper", Name: "Policy Helper", Purpose: "Answer policy questions with citations.", PurposeLanguage: "en",
		Lifecycle: PersonaInReview, Version: "4", ChannelClasses: []string{"PRIVATE", "PUBLIC"},
		Owner: "ir-001-walt-brennan", OwnerName: "Walt Brennan", Steward: "ir-003-loretta-haynes", StewardName: "Loretta Haynes",
		ReviewRequired: true, Reviewer: "ir-008-curtis-bell", ReviewerName: "Curtis Bell",
	}
}

func TestAgentUXSetup2_LifecycleEvaluationAndPublication(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PersonaAdminPersona)
		wants  []string
		absent []string
	}{
		{name: "draft", mutate: func(p *PersonaAdminPersona) { p.Lifecycle = PersonaDraft; p.ReviewApproved = false }, wants: []string{"Step 1 of 4: Draft", ">Request review</button>"}},
		{name: "in review", mutate: func(p *PersonaAdminPersona) { p.Lifecycle = PersonaInReview; p.ReviewApproved = false }, wants: []string{"Step 2 of 4: Waiting for review", "Waiting for Curtis Bell to review version 4."}},
		{name: "reviewed awaiting evaluation", mutate: func(p *PersonaAdminPersona) { p.ReviewApproved = true }, wants: []string{"Run evaluation", "usually takes a few minutes"}},
		{name: "evaluation running", mutate: func(p *PersonaAdminPersona) { p.ReviewApproved = true; p.EvaluationStatus = "RUNNING" }, wants: []string{"Evaluation is running. Results will appear here."}, absent: []string{"Run evaluation"}},
		{name: "evaluation failed", mutate: func(p *PersonaAdminPersona) {
			p.ReviewApproved = true
			p.EvaluationStatus = "FAILED"
			p.EvaluationPassed = 7
			p.EvaluationFailed = 2
			p.EvaluationFailureNames = []string{"missing-citation", "unsafe-answer"}
		}, wants: []string{"7 passed, 2 failed", "Missing citation", "Unsafe answer", "Run evaluation"}},
		{name: "evaluated", mutate: func(p *PersonaAdminPersona) {
			p.ReviewApproved = true
			p.EvaluationRef = "eval-4"
			p.EvaluationStatus = "PASSED"
			p.EvaluationPassed = 9
		}, wants: []string{"Step 3 of 4: Evaluated", "<h4>Ready to publish</h4>", ">Publish version 4</button>"}},
		{name: "published", mutate: func(p *PersonaAdminPersona) {
			p.Lifecycle = PersonaPublished
			p.ReviewApproved = true
			p.EvaluationRef = "eval-4"
			p.ReviewApprovedAt = "2026-09-29"
			p.EvaluationPassedAt = "2026-09-30"
			p.PublishedAt = "2026-10-01"
		}, wants: []string{"Step 4 of 4: Published", "Reviewed by", "Curtis Bell", "Sep 29", "Evaluation passed", "Sep 30", "Version history"}, absent: []string{"Run evaluation", "Evaluation is required before publication"}},
		{name: "suspended", mutate: func(p *PersonaAdminPersona) {
			p.Lifecycle = PersonaSuspended
			p.ReviewApproved = true
			p.EvaluationRef = "eval-4"
		}, wants: []string{"Paused", "Step 4 of 4: Paused", "Resume agent"}, absent: []string{"Publish version 4"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			persona := agentUXSetup2Persona()
			tc.mutate(&persona)
			snapshot := agentUXSetup2Snapshot(persona)
			markup := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, snapshot))
			for _, want := range tc.wants {
				if !strings.Contains(markup, want) {
					t.Errorf("missing %q: %s", want, markup)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(markup, absent) {
					t.Errorf("published state retained %q: %s", absent, markup)
				}
			}
		})
	}
}

func TestAgentUXSetup2_PlacementsAndOfficialDocuments(t *testing.T) {
	three, zero := 3, 0
	persona := agentUXSetup2Persona()
	persona.Lifecycle = PersonaPublished
	persona.Installations = []PersonaAdminInstallation{
		{InstallationID: "install-general", ConversationID: "general", Conversation: "general", Kind: "CHANNEL", Version: "4", OfficialDocumentCount: &three, OfficialDocumentTitles: []string{"Paid time off policy", "Benefits guide", "Leave policy"}},
		{InstallationID: "install-direct", ConversationID: "direct-walt", Conversation: "Walt Brennan", Kind: "DIRECT_MESSAGE", Version: "4", OfficialDocumentCount: &zero},
	}
	snapshot := agentUXSetup2Snapshot(persona)
	markup := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, snapshot))
	for _, want := range []string{
		"#general", "Public channel", "running version 4", `href="/workspace/app/chat#channel=general"`,
		"Direct conversation with Walt Brennan", "Direct message", "3 documents in this conversation", "Paid time off policy", "Benefits guide", "Leave policy",
		"No official documents are in this conversation. The agent will not find anything to cite.", "Place a document", "Add to a conversation", `data-persona-admin-command-form="INSTALL"`, `value="benefits"`,
		`data-persona-admin-command-form="UNINSTALL"`, `data-installation-id="install-general"`, ">Remove</button>", "Ask it a question",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("placement contract missing %q: %s", want, markup)
		}
	}
}

func TestTodo_AGENTUX_034(t *testing.T) {
	three, zero := 3, 0
	persona := agentUXSetup2Persona()
	persona.Installations = []PersonaAdminInstallation{
		{ConversationID: "general", Conversation: "general", Kind: "CHANNEL", Version: "4", OfficialDocumentCount: &three, OfficialDocumentTitles: []string{"Benefits guide", "Leave policy", "Paid time off policy"}},
		{ConversationID: "direct-walt", Conversation: "Walt Brennan", Kind: "DIRECT_MESSAGE", Version: "4", OfficialDocumentCount: &zero},
	}
	markup := personaAdminRender(t, personaAdminPlacements(ResolveProductLocale("en-US"), persona, agentUXSetup2Snapshot(persona)))
	for _, want := range []string{"3 documents in this conversation", "Benefits guide", "Leave policy", "Paid time off policy", "No official documents are in this conversation. The agent will not find anything to cite.", "Place a document", `/workspace/app/chat#channel=general&amp;tab=docs`} {
		if !strings.Contains(markup, want) {
			t.Errorf("AGENTUX-034 placement evidence missing %q: %s", want, markup)
		}
	}
}

func TestTodo_AGENTUX_034_Security(t *testing.T) {
	persona := agentUXSetup2Persona()
	persona.Installations = []PersonaAdminInstallation{{ConversationID: "restricted", Conversation: "restricted", Kind: "PRIVATE_CHANNEL", OfficialDocumentTitles: []string{"Executive succession plan"}}}
	markup := personaAdminRender(t, personaAdminPlacements(ResolveProductLocale("en-US"), persona, agentUXSetup2Snapshot(persona)))
	if strings.Contains(markup, "Executive succession plan") {
		t.Fatalf("document title rendered without an authorized count projection: %s", markup)
	}
	if !strings.Contains(markup, "Open this conversation&#39;s Documents") || !strings.Contains(markup, `channel=restricted&amp;tab=docs`) {
		t.Fatalf("safe unavailable state did not preserve the conversation-scoped route: %s", markup)
	}
}

func TestTodo_AGENTUX_034_Browser(t *testing.T) {
	one := 1
	persona := agentUXSetup2Persona()
	persona.Installations = []PersonaAdminInstallation{{ConversationID: "general", Conversation: "general", Kind: "CHANNEL", Version: "4", OfficialDocumentCount: &one, OfficialDocumentTitles: []string{"سياسة الإجازات"}}}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := personaAdminRender(t, personaAdminPlacements(ResolveProductLocale(locale), persona, agentUXSetup2Snapshot(persona)))
		for _, want := range []string{`<bdi dir="ltr">#`, `tab=docs`, `data-persona-admin-command-form="UNINSTALL"`} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s responsive placement contract missing %q: %s", locale, want, markup)
			}
		}
	}
	css := personaAdminStylesheet() + agentUXR7Stylesheet()
	if !strings.Contains(css, `@media(max-width:50rem)`) || !strings.Contains(css, `.persona-admin-installation{grid-template-columns:minmax(0,1fr)}`) || !strings.Contains(css, `overflow-wrap:anywhere`) {
		t.Fatalf("placement rows do not have a narrow-width, overflow-safe layout: %s", css)
	}
}

func TestAgentUXSetup2_CapabilitiesPeopleAudienceAndDirection(t *testing.T) {
	persona := agentUXSetup2Persona()
	persona.Lifecycle = PersonaPublished
	persona.Purpose = "English administrator-written fallback"
	persona.PurposeLanguage = "en"
	persona.OwnerInitials, persona.StewardInitials = "WB", "LH"
	persona.Audience = "employees, hcm_admin, comp_admin, intent_author, promotion_operator, executive"
	persona.AudienceRoles = []PersonaAdminTarget{{ID: "employees"}, {ID: "hcm_admin"}, {ID: "comp_admin"}, {ID: "intent_author"}, {ID: "promotion_operator"}, {ID: "executive"}}
	persona.Skills = []PersonaAdminSkill{{ID: "persona.chat_reply", Tier: "T2"}, {ID: "hcmnext.skill.knowledge_search_with_citations", Tier: "T0"}}
	snapshot := agentUXSetup2Snapshot(persona)
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := personaAdminRender(t, personaAdminCard(ResolveProductLocale(locale), &personaAdminTestClient{}, persona, snapshot))
		for _, want := range []string{`dir="auto" lang="en"`, `class="avatar tiny"`, `href="/workspace/app/person?person=ir-001-walt-brennan"`, "Walt Brennan", "Loretta Haynes"} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s people/direction contract missing %q: %s", locale, want, markup)
			}
		}
		if strings.Contains(markup, "persona_admin.") {
			t.Fatalf("%s leaked an i18n key: %s", locale, markup)
		}
	}
	english := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, snapshot))
	for _, want := range []string{"Posts a reply", "Replies to the person who asked. Answers drawn from documents are sent to them privately.", "Read-only", "People in these 6 roles who are members of a conversation where this agent is added", "No limits set · ", "To change usage limits, contact", `data-status-tone="published"`} {
		if !strings.Contains(english, want) {
			t.Errorf("capability contract missing %q: %s", want, english)
		}
	}
	german := personaAdminRender(t, personaAdminCard(ResolveProductLocale("de-DE"), &personaAdminTestClient{}, persona, snapshot))
	for _, want := range []string{"Wer ihn verwenden kann", "Workflow-Autor:innen"} {
		if !strings.Contains(german, want) {
			t.Errorf("German contract missing %q: %s", want, german)
		}
	}
}

func TestAgentUXSetup2_AccessCheckHeaderEditAndResponsiveStyles(t *testing.T) {
	persona := agentUXSetup2Persona()
	persona.ReviewApproved = true
	persona.Lifecycle = PersonaPublished
	snapshot := agentUXSetup2Snapshot(persona)
	snapshot.Preview = PersonaAdminPreview{Subject: "Walt Brennan", Conversation: "general", EffectiveSkills: []PersonaAdminSkill{{ID: "persona.chat_reply"}}, DerivedData: []string{"POLICY_DOCUMENT"}, ReplyPlacement: "private", Audience: "employees"}
	markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{}}))
	for _, want := range []string{
		`href="/workspace/app/admin">Admin</a>`, `aria-current="page" class="muted">Agents</span>`, "conversations (channels and direct messages)", "Each card shows what the agent can do and how close it is to being available.", "Test what an agent can do for someone", "Pick a person and a conversation", `data-persona-preview-check="true"`, ">Check</button>",
		`In <bdi dir="ltr">#general</bdi>, for Walt Brennan, Policy Helper can read: Policy documents. It can: reply in the conversation.`, `data-persona-editor-toggle="persona-admin-version-policy-helper"`, "Creates version 5 as a draft. Version 4 stays live until you publish.", `popovertarget="persona-admin-more-policy-helper"`, "Version history", "Setup", "Operations", "Ask it a question",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("setup interaction contract missing %q: %s", want, markup)
		}
	}
	denied := snapshot
	denied.Preview.EffectiveSkills = nil
	denied.Preview.DerivedData = nil
	denied.Preview.ReplyPlacement = ""
	denied.Preview.AuthorizationReason = ""
	deniedMarkup := personaAdminRender(t, personaAdminPreview(ResolveProductLocale("en-US"), &personaAdminTestClient{}, denied))
	if !strings.Contains(deniedMarkup, `In <bdi dir="ltr">#general</bdi>, for Walt Brennan, Policy Helper can read: None set. It can: None set.`) {
		t.Fatalf("denied access result is not explained in words: %s", deniedMarkup)
	}
	css := personaAdminStylesheet() + agentUXR7Stylesheet()
	for _, want := range []string{"align-items:start", `data-status-tone="published"`, "var(--hcm-color-success-surface)", ".persona-admin-preview{padding:", "background:var(--hcm-color-surface)", "@media(max-width:30rem)", `li:not([data-step-state="current"]){display:flex`, "padding-inline:0"} {
		if !strings.Contains(css, want) {
			t.Errorf("responsive/tokenized setup CSS missing %q", want)
		}
	}
}
