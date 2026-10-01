package productui

import (
	"context"
	"strings"
	"testing"

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
			},
			{ID: "onboarding", Handle: "@onboarding", Name: "Onboarding Coordinator", Purpose: "Guide approved onboarding steps.", Lifecycle: PersonaInReview, Owner: "People Operations", Steward: "Ari Singh", Version: "1", Audience: "New hires", ReviewRequired: true},
		},
		SubjectOptions: []PersonaAdminTarget{{ID: "maya", Label: "Maya Chen"}},
		Conversations:  []PersonaAdminTarget{{ID: "hr-channel", Label: "#hr-ops"}},
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
	for _, want := range []string{"Personas", "Comp Analyst", "Published", "Owner", "Maya Chen", "Read compensation", "T0", "COMPENSATION", "#hr-ops", "30 / user / hour", "AGENTP-006 review", "Effective access preview", "Request review", "Publish", "Rollback", "Suspend", "Retire"} {
		if !strings.Contains(markup, want) {
			t.Errorf("persona admin missing %q:\n%s", want, markup)
		}
	}
	if !strings.Contains(markup, `data-lifecycle="PUBLISHED"`) || !strings.Contains(markup, `data-preview-subject="Maya Chen"`) {
		t.Fatalf("lifecycle or selected preview projection missing:\n%s", markup)
	}
	if !strings.Contains(markup, "disabled") {
		t.Fatal("unreviewed persona did not leave publication gated")
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
	if got := ResolveProductLocale("de-DE").Text("persona_admin.title"); got != "Personas" {
		t.Fatalf("German persona title is not translated: %q", got)
	}
	if got := ResolveProductLocale("ar").Text("persona_admin.title"); got != "الشخصيات" {
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
	if !strings.Contains(markup, "persona administration permission") || strings.Contains(markup, "Comp Analyst") || strings.Contains(markup, "COMPENSATION") || strings.Contains(markup, "Publish") {
		t.Fatalf("denied persona administration leaked catalog data or controls:\n%s", markup)
	}
	if PageVisible(PagePersonaAdmin, []string{"manager"}) || PageVisible(PagePersonaAdmin, []string{"worker_self"}) || !PageVisible(PagePersonaAdmin, []string{RoleHCMAdmin}) {
		t.Fatal("persona admin route visibility did not preserve administrator-only policy")
	}
}
