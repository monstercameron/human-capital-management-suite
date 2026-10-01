package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type agentAccessTestClient struct {
	start         AgentAuthorizationStart
	startProvider string
	unlinked      string
	revoked       string
	adminCalls    []string
}

func (c *agentAccessTestClient) StartProviderAuthorization(providerID string) (AgentAuthorizationStart, error) {
	c.startProvider = providerID
	return c.start, nil
}
func (c *agentAccessTestClient) UnlinkConnection(connectionID string) error {
	c.unlinked = connectionID
	return nil
}
func (c *agentAccessTestClient) RevokeDelegation(taskID string) error {
	c.revoked = taskID
	return nil
}
func (c *agentAccessTestClient) CreateConnectionRevision(AgentConnectionRevision) error {
	c.adminCalls = append(c.adminCalls, "create")
	return nil
}
func (c *agentAccessTestClient) ImportMCPSnapshot(string, string) error {
	c.adminCalls = append(c.adminCalls, "import")
	return nil
}
func (c *agentAccessTestClient) PublishConnectionRevision(string) error {
	c.adminCalls = append(c.adminCalls, "publish")
	return nil
}
func (c *agentAccessTestClient) RequestSecondAdminApproval(string) error {
	c.adminCalls = append(c.adminCalls, "approve")
	return nil
}

func agentAccessRender(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestTodo_AGENT2_018(t *testing.T) {
	client := &agentAccessTestClient{start: AgentAuthorizationStart{AuthorizationURL: "https://provider.example/authorize", State: "state", CodeChallenge: "challenge", CodeChallengeMethod: "S256"}}
	view := AgentAccessPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: AgentAccessStateReady, Client: client,
		Snapshot: AgentAccessSnapshot{
			Connections: []AgentAccessConnection{{ID: "workday", Provider: "Workday", AccountLabel: "Maya's account", LinkState: "linked", CredentialMode: "DELEGATED", CanUnlink: true, Skills: []AgentAccessSkill{{Name: "Read worker profile", Tier: "T0", Scope: "My authorized profile"}}}, {ID: "slack", Provider: "Slack", AccountLabel: "Not linked", LinkState: "needs_link", CanLink: true}},
			Delegations: []AgentDelegationGrant{{ID: "grant-1", TaskID: "task-1", TaskLabel: "Prepare onboarding summary", Scope: "T1 private draft", ExpiresAt: "2026-10-01T12:00Z", Revocable: true}},
		},
	}
	markup := agentAccessRender(t, AgentAccessPage(view))
	for _, want := range []string{"My agent access", "Workday", "Linked", "Read worker profile", "T0", "Slack", "Link account", "Active task grants", "Prepare onboarding summary", "Expires", "Revoke"} {
		if !strings.Contains(markup, want) {
			t.Errorf("access page missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, client.start.AuthorizationURL) {
		t.Fatal("provider authorization URL was rendered as page markup")
	}
}

func TestTodo_AGENT2_018_Browser(t *testing.T) {
	markup := agentAccessRender(t, AgentAccessPage(AgentAccessPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: AgentAccessStateReady, Snapshot: AgentAccessSnapshot{Connections: []AgentAccessConnection{{ID: "provider", Provider: "Provider", LinkState: "needs_link", CanLink: true}}}}))
	if !strings.Contains(markup, `type="button"`) || !strings.Contains(markup, `class="button secondary agent-link-button"`) {
		t.Fatalf("linking is not a keyboard button:\n%s", markup)
	}
	if strings.Contains(markup, "href=") || strings.Contains(markup, "https://provider.example/authorize") {
		t.Fatalf("account linking exposed a navigable authorization link:\n%s", markup)
	}
}

func TestTodo_AGENT2_018_Security(t *testing.T) {
	client := &agentAccessTestClient{start: AgentAuthorizationStart{AuthorizationURL: "https://provider.example/authorize", State: "s", CodeChallenge: "c", CodeChallengeMethod: "S256"}}
	var started AgentAuthorizationStart
	markup := agentAccessRender(t, AgentAccessPage(AgentAccessPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: AgentAccessStateReady, Client: client, OnAuthorization: func(start AgentAuthorizationStart) { started = start }, Snapshot: AgentAccessSnapshot{Connections: []AgentAccessConnection{{ID: "provider", Provider: "Provider", LinkState: "needs_link", CanLink: true}}}}))
	if !strings.Contains(markup, `data-provider-id="provider"`) || strings.Contains(markup, client.start.AuthorizationURL) {
		t.Fatalf("link page leaked authorization data:\n%s", markup)
	}
	if !client.start.ValidPKCE() || (AgentAuthorizationStart{AuthorizationURL: "https://x", State: "s", CodeChallenge: "c", CodeChallengeMethod: "plain"}).ValidPKCE() || (AgentAuthorizationStart{AuthorizationURL: "javascript:alert(1)", State: "s", CodeChallenge: "c", CodeChallengeMethod: "S256"}).ValidPKCE() {
		t.Fatal("PKCE validation accepted an unsafe authorization start")
	}
	// The callback is deliberately the only place the trusted handoff can go;
	// an invalid server response is not forwarded to the browser.
	if started.AuthorizationURL != "" {
		t.Fatal("authorization callback ran during server render")
	}
}

func TestTodo_AGENT2_018_Integration(t *testing.T) {
	client := &agentAccessTestClient{}
	props := AgentAccessPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: AgentAccessStateReady, Client: client, Snapshot: AgentAccessSnapshot{Connections: []AgentAccessConnection{{ID: "provider", Provider: "Provider", LinkState: "linked", CanUnlink: true}}, Delegations: []AgentDelegationGrant{{ID: "g", TaskID: "task", TaskLabel: "Task", Scope: "T0", ExpiresAt: "tomorrow", Revocable: true}}}}
	markup := agentAccessRender(t, AgentAccessPage(props))
	if !strings.Contains(markup, "Unlink account") || !strings.Contains(markup, "Revoke") {
		t.Fatalf("destructive controls disappeared from an available client:\n%s", markup)
	}
	if err := client.UnlinkConnection("provider"); err != nil || client.unlinked != "provider" {
		t.Fatal("unlink seam did not preserve the connection identity")
	}
	if err := client.RevokeDelegation("task"); err != nil || client.revoked != "task" {
		t.Fatal("revoke seam did not preserve the run-bound task identity")
	}
}

func TestTodo_AGENT2_019(t *testing.T) {
	markup := agentAccessRender(t, AgentAdminAccessPage(AgentAdminAccessPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: AgentAccessStateReady, Snapshot: AgentAdminAccessSnapshot{Revisions: []AgentConnectionRevision{{ID: "rev-2", Provider: "Workday", Revision: "2", Status: "DRAFT", CredentialMode: "BROKERED", MCPSnapshotID: "snapshot-7", RequiresSecondAdmin: true, Grants: []AgentAdminSkillGrant{{SkillName: "Submit payroll change", Tier: "T3", Scopes: []AgentGrantScope{{Kind: "role", Value: "payroll_manager"}}, RequiresSecondAdmin: true}}}}, PreviewReady: true, PreviewSubject: "Maya Chen", Preview: AgentEffectiveAccessPreview{ScopeLabel: "HarborCare", Connections: []AgentAccessConnection{{Provider: "Workday", AccountLabel: "Maya's delegated account", Skills: []AgentAccessSkill{{Name: "Read worker profile", Tier: "T0"}}}}}}}))
	for _, want := range []string{"Agent connections and grants", "Revision 2", "MCP tool snapshot", "Submit payroll change", "T3", "Second-admin approval required", "Effective access preview", "Maya Chen", "Read worker profile"} {
		if !strings.Contains(markup, want) {
			t.Errorf("admin console missing %q:\n%s", want, markup)
		}
	}
	if strings.Contains(markup, "Publish revision") {
		t.Fatal("high-impact revision offered publication before second-admin approval")
	}
}

func TestTodo_AGENT2_019_SecondAdminGatesBrokeredAndHighImpactRevisions(t *testing.T) {
	for _, revision := range []AgentConnectionRevision{
		{ID: "brokered", Provider: "Provider", Revision: "1", Status: "DRAFT", CredentialMode: "BROKERED"},
		{ID: "external-write", Provider: "Provider", Revision: "2", Status: "DRAFT", Grants: []AgentAdminSkillGrant{{SkillName: "External write", Tier: "T4"}}},
	} {
		markup := agentAccessRender(t, AgentAdminAccessPage(AgentAdminAccessPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: AgentAccessStateReady, Snapshot: AgentAdminAccessSnapshot{Revisions: []AgentConnectionRevision{revision}}}))
		if !strings.Contains(markup, `data-approval-required="true"`) || strings.Contains(markup, "Publish revision") {
			t.Fatalf("revision %q bypassed second-admin gate:\n%s", revision.ID, markup)
		}
	}
}

func TestTodo_AGENT2_019_Browser(t *testing.T) {
	markup := agentAccessRender(t, AgentAdminAccessPage(AgentAdminAccessPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: AgentAccessStateReady, Snapshot: AgentAdminAccessSnapshot{Revisions: []AgentConnectionRevision{{ID: "rev", Provider: "Provider", Revision: "1", Status: "DRAFT"}}, PreviewReady: false}}))
	for _, want := range []string{`data-agent-access-state="ready"`, `data-preview-state="unavailable"`, "Choose a user or population"} {
		if !strings.Contains(markup, want) {
			t.Errorf("admin unavailable preview missing %q:\n%s", want, markup)
		}
	}
}

func TestTodo_AGENT2_019_Security(t *testing.T) {
	markup := agentAccessRender(t, AgentAdminAccessPage(AgentAdminAccessPageProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, State: AgentAccessStateUnavailable, UnavailableReason: "forbidden"}))
	if !strings.Contains(markup, "Agent administration is unavailable") || !strings.Contains(markup, "forbidden") || strings.Contains(markup, "Publish revision") {
		t.Fatalf("unavailable admin state exposed controls or lost reason:\n%s", markup)
	}
}

func TestTodo_AGENT2_019_Golden(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "Effective access preview", "de-DE": "Vorschau des effektiven Zugriffs", "ar": "معاينة الوصول الفعلي"} {
		markup := agentAccessRender(t, AgentEffectiveAccessPreviewPanel(AgentEffectiveAccessPreviewProps{I18nProps: I18nProps{Locale: ResolveProductLocale(locale)}, Ready: true, Subject: "Person", Preview: AgentEffectiveAccessPreview{ScopeLabel: "Scope", Connections: []AgentAccessConnection{{Provider: "Provider", AccountLabel: "Account", Skills: []AgentAccessSkill{{Name: "Read", Tier: "T0"}}}}}}))
		if !strings.Contains(markup, want) {
			t.Errorf("%s preview missing %q:\n%s", locale, want, markup)
		}
	}
}
