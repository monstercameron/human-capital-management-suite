package productui

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type personaAdminTestClient struct {
	snapshotCalls int
	actions       []string
	snapshot      PersonaAdminSnapshot
}

func (c *personaAdminTestClient) Snapshot(context.Context, PersonaAdminSnapshotRequest) (PersonaAdminSnapshot, error) {
	c.snapshotCalls++
	return c.snapshot, nil
}
func (c *personaAdminTestClient) Preview(context.Context, PersonaAdminPreviewRequest) (PersonaAdminPreview, error) {
	return c.snapshot.Preview, nil
}
func (c *personaAdminTestClient) RequestReview(id string) error {
	c.actions = append(c.actions, "review:"+id)
	return nil
}
func (c *personaAdminTestClient) PublishPersona(id string) error {
	c.actions = append(c.actions, "publish:"+id)
	return nil
}
func (c *personaAdminTestClient) RollbackPersona(id string) error {
	c.actions = append(c.actions, "rollback:"+id)
	return nil
}
func (c *personaAdminTestClient) SuspendPersona(id string) error {
	c.actions = append(c.actions, "suspend:"+id)
	return nil
}
func (c *personaAdminTestClient) RetirePersona(id string) error {
	c.actions = append(c.actions, "retire:"+id)
	return nil
}

func personaAdminRender(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func personaAdminSnapshot() PersonaAdminSnapshot {
	return PersonaAdminSnapshot{
		Available: true,
		Personas: []PersonaAdminPersona{
			{
				ID: "comp-analyst", Handle: "@comp-analyst", Name: "Comp Analyst", Purpose: "Answer compensation policy questions.", Lifecycle: PersonaPublished,
				Owner: "People Operations", Steward: "Maya Chen", Version: "3", Audience: "Compensation partners", DerivedData: []string{"WORKER_PROFILE", "COMPENSATION"},
				Skills:        []PersonaAdminSkill{{ID: "skill-pay-read", Name: "Read compensation", Tier: "T0", DataClasses: []string{"COMPENSATION"}}, {ID: "skill-policy", Name: "Read policy", Tier: "T0", DataClasses: []string{"POLICY"}}},
				Installations: []PersonaAdminInstallation{{ConversationID: "dm-maya", Conversation: "Maya Chen · 1:1", Kind: "DM", Audience: "Maya Chen", ReplyPlacement: "Private thread"}, {ConversationID: "hr-channel", Conversation: "#hr-ops", Kind: "Channel", Audience: "HR operations", ReplyPlacement: "Thread"}},
				Limits:        PersonaAdminLimits{InvocationsPerHour: "30 / user / hour", ConcurrentTasks: "3 concurrent tasks", DailySpend: "$250 / day"}, ReviewRequired: true, ReviewApproved: true, Reviewer: "Jordan Lee", EvaluationRef: "eval-persona-3",
				VersionHistory: []PersonaAdminVersionHistory{{Version: "3", Lifecycle: PersonaPublished}, {Version: "2", Lifecycle: PersonaPublished}},
			},
			{ID: "onboarding", Handle: "@onboarding", Name: "Onboarding Coordinator", Purpose: "Guide approved onboarding steps.", Lifecycle: PersonaInReview, Owner: "People Operations", Steward: "Ari Singh", Version: "1", Audience: "New hires", ReviewRequired: true},
		},
		SubjectOptions: []PersonaAdminTarget{{ID: "maya-chen", Label: "Maya Chen", Role: "comp_admin"}},
		Conversations:  []PersonaAdminTarget{{ID: "hr-channel", Label: "#hr-ops", Kind: "CHANNEL"}},
		Preview: PersonaAdminPreview{
			Subject: "Maya Chen", Conversation: "#hr-ops", ConversationKind: "CHANNEL", Audience: "HR operations", ReplyPlacement: "Thread",
			EffectiveSkills: []PersonaAdminSkill{{ID: "skill-pay-read", Name: "Read compensation", Tier: "T0"}}, DerivedData: []string{"COMPENSATION"},
			Warnings: []string{"Compensation is visible only when the selected user's current access permits it."},
		},
	}
}

func TestTodo_AGENTP_018(t *testing.T) {
	client := &personaAdminTestClient{snapshot: personaAdminSnapshot()}
	client.snapshot.Personas = append(client.snapshot.Personas, PersonaAdminPersona{ID: "policy-draft", Name: "Policy Helper", Lifecycle: PersonaDraft})
	markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: client.snapshot, Client: client}))
	for _, want := range []string{"Agent setup", "Comp Analyst", "Published", "Business owner", "Maya Chen", "Read compensation", "Read-only", "Compensation", "#hr-ops", "30 / user / hour", "independent reviewer", "Test what an agent can do for someone", "Request review", "More", "Roll back to version 2", "Pause agent", "Retire"} {
		if !strings.Contains(markup, want) {
			t.Errorf("persona admin missing %q:\n%s", want, markup)
		}
	}
	if !strings.Contains(markup, `data-lifecycle="PUBLISHED"`) || !strings.Contains(markup, `data-preview-subject="Maya Chen"`) {
		t.Fatalf("lifecycle or selected preview projection missing:\n%s", markup)
	}
	if !strings.Contains(markup, "disabled") {
		t.Fatal("unreviewed agent did not leave review gated")
	}
	if strings.Contains(markup, "task-secret") || strings.Contains(markup, "prompt") {
		t.Fatalf("persona catalogue rendered task content:\n%s", markup)
	}
	if _, ok := LookupPage(PagePersonaAdmin); !ok || Path(PagePersonaAdmin) != "/workspace/app/admin/personas" {
		t.Fatal("persona admin page is not registered at its canonical route")
	}
}

func TestTodo_AGENTP_018_Browser(t *testing.T) {
	snapshot := personaAdminSnapshot()
	markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{snapshot: snapshot}}))
	if strings.Count(markup, "<h1") != 1 {
		t.Fatalf("persona page must expose exactly one h1:\n%s", markup)
	}
	if strings.Contains(markup, "<main") {
		t.Fatalf("persona page must remain a shell-owned section, not html.Main:\n%s", markup)
	}
	for _, want := range []string{`type="button"`, `for="persona-admin-preview-subject"`, `for="persona-admin-preview-conversation"`, `aria-labelledby="persona-admin-title"`, `aria-labelledby="persona-admin-preview-title"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("keyboard/screen-reader contract missing %q:\n%s", want, markup)
		}
	}
}

func TestTodo_AGENTP_018_Accessibility(t *testing.T) {
	snapshot := personaAdminSnapshot()
	markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{snapshot: snapshot}}))
	if !strings.Contains(markup, `role="status"`) || !strings.Contains(markup, `data-review-approved`) {
		t.Fatalf("review and status semantics are not exposed:\n%s", markup)
	}
	for _, id := range []string{"persona-admin-title", "persona-admin-preview-title", "persona-admin-preview-subject", "persona-admin-preview-conversation"} {
		if !strings.Contains(markup, `id="`+id+`"`) {
			t.Errorf("accessible id %q missing:\n%s", id, markup)
		}
	}
}

func TestTodo_AGENTP_018_I18n(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale(locale)}, State: PersonaAdminReady, Snapshot: personaAdminSnapshot()}))
		if strings.Contains(markup, "persona_admin.") || strings.Contains(markup, "⟦") {
			t.Fatalf("%s rendered an untranslated persona key:\n%s", locale, markup)
		}
	}
	if got := ResolveProductLocale("de-DE").Text("persona_admin.title"); got != "Agenten einrichten" {
		t.Fatalf("German persona title is not translated: %q", got)
	}
	if got := ResolveProductLocale("ar").Text("persona_admin.title"); got != "إعداد الوكلاء" {
		t.Fatalf("Arabic persona title is not translated: %q", got)
	}
}

func TestTodo_AGENTP_018_RendererUsesServerBoundClient(t *testing.T) {
	client := &personaAdminTestClient{snapshot: personaAdminSnapshot()}
	view := NewView(PagePersonaAdmin, "harborcare", "maya", "")
	view.EffectivePermissions = []RolePagePermission{{Page: PagePersonaAdmin, View: true}}
	view.PersonaAdminClient = client
	markup := personaAdminRender(t, (personaAdminPageModuleRenderer{}).Render(view))
	if client.snapshotCalls != 1 || !strings.Contains(markup, "Comp Analyst") {
		t.Fatalf("renderer did not use its server-bound catalog client: calls=%d markup=%s", client.snapshotCalls, markup)
	}

	view.PersonaAdminClient = nil
	unavailable := personaAdminRender(t, (personaAdminPageModuleRenderer{}).Render(view))
	if strings.Contains(unavailable, "Comp Analyst") || !strings.Contains(unavailable, `data-persona-admin-state="unavailable"`) {
		t.Fatalf("nil server binding must remain unavailable: %s", unavailable)
	}
}
func TestTodo_AGENTP_018_Security(t *testing.T) {
	client := &personaAdminTestClient{snapshot: personaAdminSnapshot()}
	view := NewView(PagePersonaAdmin, "harborcare", "maya", "")
	view.EffectivePermissions = []RolePagePermission{{Page: PagePersonaAdmin, View: false}}
	markup := personaAdminRender(t, BuildPersonaAdminPage(view, client))
	if client.snapshotCalls != 0 {
		t.Fatal("denied persona administration called the data client")
	}
	if !strings.Contains(markup, "This page is for people who manage agents.") || strings.Contains(markup, "Comp Analyst") || strings.Contains(markup, "COMPENSATION") || strings.Contains(markup, "Publish") {
		t.Fatalf("denied persona administration leaked catalog data or controls:\n%s", markup)
	}
	if PageVisible(PagePersonaAdmin, []string{"manager"}) || PageVisible(PagePersonaAdmin, []string{"worker_self"}) || !PageVisible(PagePersonaAdmin, []string{RoleHCMAdmin}) {
		t.Fatal("persona admin route visibility did not preserve administrator-only policy")
	}
}

func TestTodo_AGENTUX_003(t *testing.T) {
	snapshot := PersonaAdminSnapshot{
		Available: true, PreviewPersonaID: "policy-helper", PreviewSubjectID: "ir-001-walt-brennan", PreviewConversationID: "leadership-private",
		Personas: []PersonaAdminPersona{{
			ID: "persona-policy-helper", Handle: "policy-helper", Name: "Policy Helper", Purpose: "Answers policy questions with citations.", Lifecycle: PersonaInReview,
			Owner: "ir-001-walt-brennan", Steward: "ir-003-loretta-haynes", Version: "4", Audience: "comp_admin, employees, org:ironridge-demo:executive",
			AudienceRoles: []PersonaAdminTarget{{ID: "comp_admin", Label: "Compensation administrator"}}, Organizations: []PersonaAdminTarget{{ID: "org:ironridge-demo:executive", Label: "Executive leadership"}},
			DerivedData: []string{"POLICY_DOCUMENT"}, ReviewRequired: true, ReviewApproved: true, Reviewer: "ir-008-curtis-bell",
			Skills: []PersonaAdminSkill{{ID: "hcmnext.skill.knowledge_search_with_citations", Description: "Finds approved policy passages and cites them.", Tier: "T0", DataClasses: []string{"POLICY_DOCUMENT"}}, {ID: "persona.chat_reply", Tier: "T2"}},
		}},
		SubjectOptions: []PersonaAdminTarget{{ID: "ir-001-walt-brennan", Label: "Walt Brennan", Role: "hcm_admin"}, {ID: "ir-003-loretta-haynes", Label: "Loretta Haynes", Role: "technical_steward"}, {ID: "ir-008-curtis-bell", Label: "Curtis Bell", Role: "reviewer"}},
		Conversations:  []PersonaAdminTarget{{ID: "leadership-private", Label: "Leadership", Kind: "PRIVATE_CHANNEL"}},
		Preview:        PersonaAdminPreview{Subject: "Walt Brennan", Conversation: "Leadership", ConversationKind: "PRIVATE_CHANNEL", Audience: "hcm_admin", EffectiveSkills: []PersonaAdminSkill{{ID: "persona.chat_reply", Tier: "T2"}}, AuthorizationReason: "Available through the administrator audience."},
	}
	client := &personaAdminTestClient{snapshot: snapshot}
	markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: snapshot, Client: client}))
	for _, want := range []string{
		"Agent setup", "New agent", "@policy-helper", "In review", "Walt Brennan", "Loretta Haynes", "Compensation administrator", "Employees", "Executive leadership", "Documents marked official", "No limits set",
		"Knowledge search with citations", "Finds answers in policy documents the person asking may read, and cites them.", "Read-only", "Chat reply", "Replies to the person who asked. Answers drawn from documents are sent to them privately.", "Posts a reply", "Not added to a conversation yet.",
		"Step 3 of 4: Ready to evaluate", "Run evaluation", "Checks this version against the approved scenarios.", "More", "Version history", "persona-policy-helper",
		"Walt Brennan — HCM administrator", "#Leadership · Private channel", "Walt Brennan can use Policy Helper in #Leadership.", "It can do: reply to Walt.",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("AGENTUX-003 persona surface missing %q:\n%s", want, markup)
		}
	}
	visibleMarkup := strings.Split(markup, `<details class="persona-admin-technical">`)[0]
	for _, forbidden := range []string{"AGENTP-006", ">IN_REVIEW<", ">POLICY_DOCUMENT<", "knowledge_search_with_citationsT0POLICY_DOCUMENT", "Not reported"} {
		if strings.Contains(visibleMarkup, forbidden) {
			t.Errorf("business-facing persona surface leaked %q:\n%s", forbidden, markup)
		}
	}
	if strings.Contains(markup, `id="persona-admin-lifecycle-in_review"`) {
		t.Fatalf("small catalog repeated the state as a group heading: %s", markup)
	}
}

func TestTodo_AGENTUX_003_Browser(t *testing.T) {
	snapshot := personaAdminSnapshot()
	snapshot.CommandStatus = "success"
	snapshot.CommandPersonaID = snapshot.Personas[0].ID
	snapshot.PreviewPersonaID = snapshot.Personas[0].ID
	snapshot.PreviewSubjectID = "person-00"
	snapshot.PreviewConversationID = "conversation-00"
	snapshot.SubjectOptions = nil
	snapshot.Conversations = nil
	for index := 0; index < 13; index++ {
		snapshot.SubjectOptions = append(snapshot.SubjectOptions, PersonaAdminTarget{ID: "person-" + string(rune('a'+index)), Label: "Person Name", Role: "employees"})
		snapshot.Conversations = append(snapshot.Conversations, PersonaAdminTarget{ID: "conversation-" + string(rune('a'+index)), Label: "Conversation", Kind: "GROUP"})
	}
	snapshot.Personas[0].StarterID = "policy-helper"
	snapshot.Personas[0].StarterVersion = 1
	snapshot.Personas[0].ChannelClasses = []string{"PRIVATE"}
	snapshot.Personas[0].Instructions = "Use the current approved compensation policy."
	markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{snapshot: snapshot}}))
	for _, want := range []string{
		`type="search"`, `data-persona-combobox="subject"`, `data-persona-combobox="conversation"`,
		`aria-current="step"`, `aria-live="polite"`, "Roll back to version 2", "Preview which conversations move back before making any changes.", "Retire Comp Analyst?", "Confirm pause?", "Confirm retirement",
		`data-persona-command-status="comp-analyst"`, "Command completed. The server confirmed the change.",
		">Edit</button>", "Create a new version of Comp Analyst", "Use the current approved compensation policy.", `persona-admin-reference-documents-slot`, `dir="ltr"`, `dir="auto"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("AGENTUX-003 browser contract missing %q:\n%s", want, markup)
		}
	}
	cardMarkup := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, snapshot.Personas[0], snapshot))
	if strings.Count(cardMarkup, `class="button primary" data-persona-command=`) > 1 || strings.Count(cardMarkup, `href="`+Path(PageAgents)+`?agent=comp-analyst"`) != 1 {
		t.Fatalf("an agent card exposed more than one primary lifecycle action:\n%s", cardMarkup)
	}
	draft := PersonaAdminPersona{ID: "draft", Name: "Draft Helper", Lifecycle: PersonaDraft}
	draftMarkup := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, draft, PersonaAdminSnapshot{}))
	if strings.Count(draftMarkup, `class="button primary"`) != 1 || !strings.Contains(draftMarkup, ">Request review</button>") {
		t.Fatalf("draft card must expose request review as its single primary next step:\n%s", draftMarkup)
	}
	emptyPreview := personaAdminRender(t, personaAdminPreview(ResolveProductLocale("en-US"), &personaAdminTestClient{}, PersonaAdminSnapshot{Personas: []PersonaAdminPersona{{ID: "draft", Name: "Draft Helper"}}}))
	if !strings.Contains(emptyPreview, "Choose an agent, a person and a conversation, then press Check.") || strings.Contains(emptyPreview, `class="persona-admin-preview-result"`) {
		t.Fatalf("access preview did not provide a helpful pre-selection empty state:\n%s", emptyPreview)
	}
	css := personaAdminStylesheet()
	for _, want := range []string{"--hcm-color-border", "--hcm-color-surface", "@media(max-width:50rem)", "@media(max-width:36rem)", "grid-template-columns:minmax(0,1fr)", ".persona-admin-hero,.persona-admin-page .persona-admin-catalog-heading{padding-inline:", ".persona-admin-editor[hidden]{display:none}", "background:var(--hcm-color-surface)", "input[readonly]"} {
		if !strings.Contains(css, want) {
			t.Errorf("responsive tokenized persona styles missing %q", want)
		}
	}
}

func TestTodo_AGENTUX_003_RegionalFailures(t *testing.T) {
	snapshot := personaAdminSnapshot()
	snapshot.CatalogState.Unavailable = true
	snapshot.TargetsState.Unavailable = true
	snapshot.CommandsState.Unavailable = true
	snapshot.StartersState.Unavailable = true
	snapshot.DocumentsState.Unavailable = true
	markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{snapshot: snapshot}}))
	for region, sentence := range map[string]string{
		"catalog": "Agent details could not be loaded", "targets": "People and conversations could not be loaded", "commands": "Agent actions could not be loaded", "starters": "New-agent templates could not be loaded", "documents": "Reference documents could not be loaded",
	} {
		if !strings.Contains(markup, `data-persona-admin-region="`+region+`"`) || !strings.Contains(markup, sentence) {
			t.Errorf("%s failure did not stay in its region: %s", region, markup)
		}
	}
	if strings.Count(markup, "Try again") < 5 || !strings.Contains(markup, "Comp Analyst") {
		t.Fatalf("regional failures removed catalog or retry actions: %s", markup)
	}
	emptyFailure := snapshot
	emptyFailure.Personas = nil
	markup = personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: emptyFailure, Client: &personaAdminTestClient{snapshot: emptyFailure}}))
	if strings.Contains(markup, "No agents are configured yet") || !strings.Contains(markup, "Agent details could not be loaded") || !strings.Contains(markup, `disabled`) {
		t.Fatalf("failed catalog or starter source was presented as an empty catalog: %s", markup)
	}
}

func TestTodo_AGENTUX_003_PublishedCardHasCurrentActionsAndPlacements(t *testing.T) {
	snapshot := personaAdminSnapshot()
	persona := snapshot.Personas[0]
	persona.Lifecycle = PersonaPublished
	persona.StarterID, persona.StarterVersion = "policy-helper", 1
	persona.ChannelClasses = []string{"PRIVATE"}
	persona.Installations = []PersonaAdminInstallation{{Version: persona.Version, ConversationID: "benefits", Conversation: "Benefits", Kind: "CHANNEL", ReplyPlacement: "thread"}, {Version: persona.Version, ConversationID: "direct", Conversation: "Walt Brennan", Kind: "DIRECT_MESSAGE", ReplyPlacement: "private"}}
	snapshot.Personas = []PersonaAdminPersona{persona}
	markup := personaAdminRender(t, personaAdminCatalogEntry(ResolveProductLocale("en-US"), &personaAdminTestClient{snapshot: snapshot}, persona, snapshot))
	for _, want := range []string{"#Benefits", "Public channel", "Direct conversation with Walt Brennan", "Direct message", ">Edit</button>", "Creates version 4 as a draft. Version 3 stays live until you publish.", "More", "Pause agent", "Retire"} {
		if !strings.Contains(markup, want) {
			t.Errorf("published card missing %q: %s", want, markup)
		}
	}
	if strings.Count(markup, `href="`+Path(PageAgents)+`?agent=comp-analyst"`) != 1 || strings.Count(markup, `class="button primary" data-persona-command=`) != 0 {
		t.Fatalf("published entry must offer one question link without a primary lifecycle command: %s", markup)
	}
}

func TestTodo_AGENTUX_014(t *testing.T) {
	persona := PersonaAdminPersona{ID: "policy-helper", Name: "Policy Helper", Version: "4", Lifecycle: PersonaInReview, ReviewRequired: true, ReviewApproved: true}
	snapshot := PersonaAdminSnapshot{CommandPermissionsAvailable: true, AllowedCommands: []string{"RUN_EVALUATION", "PUBLISH"}}
	before := personaAdminRender(t, personaAdminLifecycleActions(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, snapshot)[0])
	if !strings.Contains(before, "Run evaluation") || strings.Contains(before, ">Publish</button>") {
		t.Fatalf("reviewed version without evidence skipped evaluation: %s", before)
	}
	persona.EvaluationRef = "evaluation-4"
	afterNodes := personaAdminLifecycleActions(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, snapshot)
	after := personaAdminRender(t, html.Div(html.Props{}, afterNodes...))
	if !strings.Contains(after, ">Publish version 4</button>") || strings.Contains(after, "Run evaluation") {
		t.Fatalf("passing evaluation did not make publish the next action: %s", after)
	}
}

func TestTodo_AGENTUX_013(t *testing.T) {
	empty := PersonaAdminSnapshot{
		Personas:       []PersonaAdminPersona{{ID: "policy-helper", Name: "Policy Helper"}},
		SubjectOptions: []PersonaAdminTarget{{ID: "ana", Label: "Ana Flores"}},
		Conversations:  []PersonaAdminTarget{{ID: "benefits", Label: "#benefits", Kind: "CHANNEL"}},
	}
	markup := personaAdminRender(t, personaAdminPreview(ResolveProductLocale("en-US"), &personaAdminTestClient{}, empty))
	if !strings.Contains(markup, "Choose an agent, a person and a conversation, then press Check.") {
		t.Fatalf("preselection guidance is missing: %s", markup)
	}
	for _, misleading := range []string{"Who can use it", "Reply placement", "What it can read", "Nothing available", "No effective skills", "No additional warnings"} {
		if strings.Contains(markup, misleading) {
			t.Errorf("preselection preview rendered misleading result %q: %s", misleading, markup)
		}
	}
	ready := empty
	ready.PreviewPersonaID, ready.PreviewSubjectID, ready.PreviewConversationID = "policy-helper", "ana", "benefits"
	ready.Preview = PersonaAdminPreview{Subject: "Ana Flores", Conversation: "#benefits", ConversationKind: "CHANNEL", Audience: "everyone in the channel", ReplyPlacement: "Thread", DerivedData: []string{"POLICY_DOCUMENT"}}
	markup = personaAdminRender(t, personaAdminPreview(ResolveProductLocale("en-US"), &personaAdminTestClient{}, ready))
	for _, want := range []string{"Ana Flores can use Policy Helper in #benefits.", "It can read: Policy documents.", "It replies in the thread, visible to everyone in the channel."} {
		if !strings.Contains(markup, want) {
			t.Errorf("effective access sentence missing %q: %s", want, markup)
		}
	}
}

func TestTodo_AGENTUX_013_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		snapshot := PersonaAdminSnapshot{Personas: []PersonaAdminPersona{{ID: "p", Name: "Persona"}}, SubjectOptions: []PersonaAdminTarget{{ID: "u", Label: "User Name"}}, Conversations: []PersonaAdminTarget{{ID: "c", Label: "Conversation", Kind: "GROUP"}}}
		markup := personaAdminRender(t, personaAdminPreview(ResolveProductLocale(locale), &personaAdminTestClient{}, snapshot))
		if strings.Count(markup, `role="combobox"`) != 2 || !strings.Contains(markup, `<select`) || !strings.Contains(markup, `data-persona-combobox="persona"`) || !strings.Contains(markup, `aria-autocomplete="list"`) {
			t.Errorf("%s preview does not use an agent select and two filtering comboboxes: %s", locale, markup)
		}
		if locale == "ar" && !strings.Contains(markup, "اختر وكيلاً") {
			t.Errorf("Arabic preselection guidance is missing: %s", markup)
		}
	}
	css := personaAdminStylesheet()
	if !strings.Contains(css, `.persona-admin-preview-controls{display:grid;grid-template-columns:repeat(3,minmax(0,1fr))`) || !strings.Contains(css, `@media(max-width:50rem)`) || !strings.Contains(css, `.persona-admin-preview-controls,.persona-admin-page .agentdoc-picker-row,.persona-admin-page .persona-admin-installation{grid-template-columns:minmax(0,1fr)}`) {
		t.Fatalf("preview controls do not fit and stack at 800px: %s", css)
	}
}

func TestTodo_AGENTUX_014_Browser(t *testing.T) {
	persona := PersonaAdminPersona{ID: "policy-helper", Name: "Policy Helper", Version: "4", Lifecycle: PersonaInReview, ReviewRequired: true, ReviewApproved: true}
	snapshot := PersonaAdminSnapshot{CommandPermissionsAvailable: true, AllowedCommands: []string{"RUN_EVALUATION", "RETIRE"}}
	markup := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, snapshot))
	for _, want := range []string{`data-persona-admin-command-form="RUN_EVALUATION"`, `name="persona_id"`, `value="policy-helper"`, `data-evaluation-version="4"`, "Run evaluation", "usually takes a few minutes", "More", "Retire Policy Helper?"} {
		if !strings.Contains(markup, want) {
			t.Errorf("evaluation path missing %q: %s", want, markup)
		}
	}
	if strings.Count(markup, `class="button primary"`) != 1 || strings.Contains(markup, ">Publish</button>") || strings.Contains(markup, ">Rollback</") || strings.Contains(markup, ">Suspend</") {
		t.Fatalf("reviewed agent did not expose exactly one applicable primary next action: %s", markup)
	}
	if got := PersonaAdminEvaluationUnavailableText(ResolveProductLocale("en-US"), "4"); got != "Evaluation could not run for version 4." {
		t.Fatalf("unavailable evaluation guidance = %q", got)
	}
	running := persona
	running.EvaluationStatus = "RUNNING"
	if runningMarkup := personaAdminRender(t, personaAdminEvaluationSummary(ResolveProductLocale("en-US"), running)); !strings.Contains(runningMarkup, "Evaluation is running. Results will appear here.") {
		t.Fatalf("evaluation running state missing: %s", runningMarkup)
	}
	failed := persona
	failed.EvaluationStatus, failed.EvaluationPassed, failed.EvaluationFailed, failed.EvaluationFailuresURL = "FAILED", 7, 2, "/workspace/app/admin/evaluations/run-4"
	failedMarkup := personaAdminRender(t, personaAdminEvaluationSummary(ResolveProductLocale("en-US"), failed))
	if !strings.Contains(failedMarkup, "7 passed, 2 failed") || !strings.Contains(failedMarkup, "View failures") || !strings.Contains(failedMarkup, `href="/workspace/app/admin/evaluations/run-4"`) {
		t.Fatalf("evaluation result omitted counts or failures link: %s", failedMarkup)
	}
	failed.EvaluationFailuresURL = ""
	failed.EvaluationFailureNames = []string{"out-of-scope", "peer-injection-1"}
	failedMarkup = personaAdminRender(t, personaAdminEvaluationSummary(ResolveProductLocale("en-US"), failed))
	if !strings.Contains(failedMarkup, `href="#persona-admin-evaluation-failures-policy-helper"`) || !strings.Contains(failedMarkup, "Out of scope") || !strings.Contains(failedMarkup, "Peer injection 1") {
		t.Fatalf("inline failure names are not linked or readable: %s", failedMarkup)
	}
}

func TestTodo_AGENTUX_014_UnconfiguredRuntimeNamesPlatformTeam(t *testing.T) {
	persona := PersonaAdminPersona{ID: "policy-helper", Name: "Policy Helper", Version: "4", Lifecycle: PersonaInReview, ReviewRequired: true, ReviewApproved: true, Steward: "ir-003-loretta-haynes", StewardName: "Loretta Haynes"}
	snapshot := PersonaAdminSnapshot{CommandPermissionsAvailable: true, EvaluationRuntimeUnavailable: true}
	markup := personaAdminRender(t, personaAdminCard(ResolveProductLocale("en-US"), &personaAdminTestClient{}, persona, snapshot))
	if !strings.Contains(markup, "Evaluation is done by") || !strings.Contains(markup, "Loretta Haynes") || !strings.Contains(markup, "Message them") || strings.Contains(markup, "requires the Agent evaluator role") {
		t.Fatalf("unconfigured evaluator was presented as a permission denial: %s", markup)
	}
}

func TestTodo_AGENTUX_018_Personas(t *testing.T) {
	snapshot := personaAdminSnapshot()
	snapshot.Starters = nil
	markup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: PersonaAdminReady, Snapshot: snapshot, Client: &personaAdminTestClient{}}))
	for _, want := range []string{"Agent setup", `href="/workspace/app/admin">Admin</a>`, `aria-current="page" class="muted">Agents</span>`, "New agent", "Agents", "Each card shows what the agent can do and how close it is to being available.", "Not added to a conversation yet"} {
		if !strings.Contains(markup, want) {
			t.Errorf("agent vocabulary missing %q: %s", want, markup)
		}
	}
	visible := strings.Split(markup, `<details class="persona-admin-technical">`)[0]
	for _, forbidden := range []string{"Personas", "Persona catalog", "conversation installation", "Conversation installation", ">Handle<", ">Steward<"} {
		if strings.Contains(visible, forbidden) {
			t.Errorf("visible setup copy contains old vocabulary %q: %s", forbidden, visible)
		}
	}
	for locale, wants := range map[string][]string{
		"de-DE": {"Agenten einrichten", "Vergütungsadministration", "Beschäftigte", "HCM-Administration", "Geschäftsleitung", "Wissenssuche mit Quellenangaben", "Richtliniendokumente", "Evaluierung", "Zu Version 1 zurückkehren"},
		"ar":    {"إعداد الوكلاء", "مسؤول التعويضات", "الموظفون", "مسؤول إدارة رأس المال البشري", "القيادة التنفيذية", "البحث المعرفي مع الاستشهادات", "مستندات السياسات"},
	} {
		localized := PersonaAdminSnapshot{Personas: []PersonaAdminPersona{{ID: "agent", Name: "Policy Helper", Lifecycle: PersonaPublished, Version: "2", VersionHistory: []PersonaAdminVersionHistory{{Version: "1", Lifecycle: PersonaPublished, PublishedAt: "2026-09-30"}}, Audience: "comp_admin, employees, hcm_admin, org:ironridge-demo:executive", Organizations: []PersonaAdminTarget{{ID: "org:ironridge-demo:executive", Label: "Executive leadership"}}, DerivedData: []string{"POLICY_DOCUMENT"}, ReviewRequired: true, ReviewApproved: true, ReviewApprovedAt: "2026-09-30", EvaluationPassedAt: "2026-10-01", Skills: []PersonaAdminSkill{{ID: "hcmnext.skill.knowledge_search_with_citations", Tier: "T0"}}}}}
		localizedMarkup := personaAdminRender(t, PersonaAdminPage(PersonaAdminPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale(locale)}, State: PersonaAdminReady, Snapshot: localized, Client: &personaAdminTestClient{}}))
		for _, want := range wants {
			if !strings.Contains(localizedMarkup, want) {
				t.Errorf("%s setup missing localized vocabulary %q: %s", locale, want, localizedMarkup)
			}
		}
	}
}

func TestTodo_AGENTUX_018_LocaleCompleteness(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		resolved := ResolveProductLocale(locale)
		values := []string{
			PersonaRoleLabelForLocale(resolved, "comp_admin", ""), PersonaRoleLabelForLocale(resolved, "employees", ""), PersonaRoleLabelForLocale(resolved, "hcm_admin", ""), PersonaRoleLabelForLocale(resolved, "intent_author", ""), PersonaRoleLabelForLocale(resolved, "promotion_operator", ""),
			PersonaOrganizationLabelForLocale(resolved, "org:ironridge-demo:executive", "Executive leadership"),
			PersonaDataClassLabelForLocale(resolved, "POLICY_DOCUMENT"), PersonaSkillLabelForLocale(resolved, "hcmnext.skill.knowledge_search_with_citations", "", ""), PersonaSkillDescription(resolved, "persona.chat_reply", ""), PersonaTierLabel(resolved, "T0"),
		}
		for _, value := range values {
			if strings.TrimSpace(value) == "" || strings.Contains(value, "_") || strings.Contains(value, "hcmnext.") {
				t.Errorf("%s vocabulary left an untranslated value %q", locale, value)
			}
		}
	}
}

func TestTodo_AGENTUX_003_PillDoesNotWrap(t *testing.T) {
	css := personaAdminStylesheet()
	for _, want := range []string{".persona-admin-tier{flex:none;white-space:nowrap;overflow-wrap:normal}", ".persona-admin-skill-copy{display:grid;flex:1 1 0;", "@media(max-width:36rem)", ".persona-admin-skill{flex-direction:column}", ".persona-admin-lifecycle-progress ol{grid-template-columns:repeat(4,minmax(0,1fr))}"} {
		if !strings.Contains(css, want) {
			t.Errorf("persona polish stylesheet missing %q", want)
		}
	}
}
