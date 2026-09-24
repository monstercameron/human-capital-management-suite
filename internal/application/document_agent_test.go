package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// fakeAgentAuthority is a minimal AgentInstallationAuthority for tests: a
// fixed map from agent ID to its currently granted scopes, with a
// distinct sentinel for "agent not found" so tests can tell a missing
// installation apart from an installation with zero scopes.
type fakeAgentAuthority struct {
	scopes map[string][]string
}

var errNoInstallation = errors.New("fake authority: no installation")

func (f fakeAgentAuthority) InstalledScopes(_ context.Context, _, agentID string) ([]string, error) {
	scopes, ok := f.scopes[agentID]
	if !ok {
		return nil, errNoInstallation
	}
	return scopes, nil
}

// TestTodo_HUB_031 is the PRIMARY test for HUB-031: an agent holding the
// blanket document-search scope retrieves exactly the currently deployed
// version the requesting user may themselves read, with exact version
// id, status and deployed time as citation.
func TestTodo_HUB_031(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner, requester, agentID = "tenant-agent", "u-owner", "u-requester", "agent-1"

	docID, versionID := deployedSearchFixture(t, svc, ctx, tenant, owner, "Benefits Guide", "# Benefits\n\nHealth and retirement benefits guide.\n")
	if err := svc.ShareDocument(ctx, tenant, owner, docID, requester, ""); err != nil {
		t.Fatal(err)
	}

	authority := fakeAgentAuthority{scopes: map[string][]string{agentID: {DocumentSearchScope}}}
	agent := newDocumentAgentService(svc.store, authority)

	res, err := agent.AgentSearchDocuments(ctx, tenant, agentID, requester, "benefits", DocumentSearchFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].DocumentID != docID || res.Hits[0].VersionID != versionID {
		t.Fatalf("agent search wrong result: %+v", res)
	}
	if res.Hits[0].Status != "deployed" || res.Hits[0].DeployedAt.IsZero() {
		t.Fatalf("agent search missing exact version/status citation: %+v", res.Hits[0])
	}

	// A team-scoped (non-blanket) installation may search when the caller
	// names exactly that team and the document is granted to it.
	if _, err := svc.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{
		DocumentID: docID, SubjectKind: "team", SubjectID: "team-people-ops",
		Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow, Issuer: owner,
	}); err != nil {
		t.Fatal(err)
	}
	scopedAuthority := fakeAgentAuthority{scopes: map[string][]string{agentID: {DocumentSearchScope + ":team:team-people-ops"}}}
	scopedAgent := newDocumentAgentService(svc.store, scopedAuthority)
	res, err = scopedAgent.AgentSearchDocuments(ctx, tenant, agentID, requester, "benefits", DocumentSearchFilters{TeamID: "team-people-ops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].DocumentID != docID {
		t.Fatalf("team-scoped agent search wrong result: %+v", res)
	}
}

// TestTodo_HUB_031_Security is the SECURITY test for HUB-031: an agent
// without an active installation, without the document-search scope, or
// attempting to search outside its granted team/channel scope is refused
// with a typed authorization error, never a silent empty result, and no
// document ever surfaces for a requester who lacks their own read grant
// even when the agent itself is fully authorized.
func TestTodo_HUB_031_Security(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner, requester, agentID = "tenant-agent-sec", "u-owner", "u-requester", "agent-1"

	docID, _ := deployedSearchFixture(t, svc, ctx, tenant, owner, "Payroll Policy", "# Payroll\n\nConfidential payroll details.\n")
	if err := svc.ShareDocument(ctx, tenant, owner, docID, requester, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{
		DocumentID: docID, SubjectKind: "team", SubjectID: "team-finance",
		Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow, Issuer: owner,
	}); err != nil {
		t.Fatal(err)
	}

	// No installation at all: refused, not empty.
	noInstall := newDocumentAgentService(svc.store, fakeAgentAuthority{scopes: map[string][]string{}})
	if _, err := noInstall.AgentSearchDocuments(ctx, tenant, agentID, requester, "payroll", DocumentSearchFilters{}); !errors.Is(err, ErrAgentNotInstalled) {
		t.Fatalf("uninstalled agent not refused: %v", err)
	}

	// Installed but with an unrelated scope only: refused as escalation.
	unrelated := newDocumentAgentService(svc.store, fakeAgentAuthority{scopes: map[string][]string{agentID: {"chat.posts.read"}}})
	if _, err := unrelated.AgentSearchDocuments(ctx, tenant, agentID, requester, "payroll", DocumentSearchFilters{}); !errors.Is(err, ErrAgentSearchEscalation) {
		t.Fatalf("scope-less agent not refused as escalation: %v", err)
	}

	// Installed with a different team's scope: requesting the finance
	// team the document actually belongs to is refused, not silently
	// narrowed to the agent's own (unrelated) team.
	wrongTeam := newDocumentAgentService(svc.store, fakeAgentAuthority{scopes: map[string][]string{agentID: {DocumentSearchScope + ":team:team-marketing"}}})
	if _, err := wrongTeam.AgentSearchDocuments(ctx, tenant, agentID, requester, "payroll", DocumentSearchFilters{TeamID: "team-finance"}); !errors.Is(err, ErrAgentSearchEscalation) {
		t.Fatalf("wrong-team agent not refused: %v", err)
	}

	// Installed with the finance team's scope but no filter named at all:
	// an unbounded request from a scoped (non-blanket) installation is
	// itself an escalation attempt.
	financeScoped := newDocumentAgentService(svc.store, fakeAgentAuthority{scopes: map[string][]string{agentID: {DocumentSearchScope + ":team:team-finance"}}})
	if _, err := financeScoped.AgentSearchDocuments(ctx, tenant, agentID, requester, "payroll", DocumentSearchFilters{}); !errors.Is(err, ErrAgentSearchEscalation) {
		t.Fatalf("unbounded scoped request not refused: %v", err)
	}

	// A fully authorized agent (blanket scope) still discloses nothing to
	// a requester who has no read grant of their own: the intersection
	// with the requester's grant is not bypassable via the agent.
	blanket := newDocumentAgentService(svc.store, fakeAgentAuthority{scopes: map[string][]string{agentID: {DocumentSearchScope}}})
	res, err := blanket.AgentSearchDocuments(ctx, tenant, agentID, "u-stranger", "payroll", DocumentSearchFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("agent leaked document to a requester without their own grant: %+v", res)
	}
}

// TestTodo_HUB_031_Integration is the INTEGRATION test for HUB-031: the
// agent search reaches the real pgtest-backed store end to end, and
// revoking the document's team grant removes it from a team-scoped
// agent's next search.
func TestTodo_HUB_031_Integration(t *testing.T) {
	svc := documentServiceFixture(t)
	ctx := context.Background()
	const tenant, owner, requester, agentID = "tenant-agent-int", "u-owner", "u-requester", "agent-1"

	docID, versionID := deployedSearchFixture(t, svc, ctx, tenant, owner, "Onboarding Runbook", "# Onboarding\n\nStep by step onboarding runbook.\n")
	if err := svc.ShareDocument(ctx, tenant, owner, docID, requester, ""); err != nil {
		t.Fatal(err)
	}
	grant, err := svc.store.GrantAction(ctx, tenant, documenthubstore.GrantInput{
		DocumentID: docID, SubjectKind: "team", SubjectID: "team-people-ops",
		Action: documenthubstore.ActionRead, Effect: documenthubstore.EffectAllow, Issuer: owner,
	})
	if err != nil {
		t.Fatal(err)
	}

	agent := newDocumentAgentService(svc.store, fakeAgentAuthority{scopes: map[string][]string{agentID: {DocumentSearchScope + ":team:team-people-ops"}}})
	res, err := agent.AgentSearchDocuments(ctx, tenant, agentID, requester, "onboarding", DocumentSearchFilters{TeamID: "team-people-ops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].VersionID != versionID {
		t.Fatalf("integration agent search missing deployed row: %+v", res.Hits)
	}

	if err := svc.store.RevokeGrant(ctx, tenant, grant.ID, owner); err != nil {
		t.Fatal(err)
	}
	res, err = agent.AgentSearchDocuments(ctx, tenant, agentID, requester, "onboarding", DocumentSearchFilters{TeamID: "team-people-ops"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("revoked team grant still searchable via agent: %+v", res.Hits)
	}
}
