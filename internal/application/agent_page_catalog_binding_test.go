package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type pageCatalogBindingClient struct {
	snapshot productui.AgentSnapshot
	calls    int
}

func (c *pageCatalogBindingClient) Snapshot(context.Context, productui.AgentSnapshotRequest) (productui.AgentSnapshot, error) {
	c.calls++
	return c.snapshot, nil
}

type pageCatalogBindingPersonas struct {
	calls  int
	items  []agentpersona.PersonaVersion
	result error
}

func (p *pageCatalogBindingPersonas) ListAvailable(context.Context, *trust.Principal) ([]agentpersona.PersonaVersion, error) {
	p.calls++
	return p.items, p.result
}

type pageCatalogBindingSkills struct {
	items []agentskills.SkillRecord
}

func (s pageCatalogBindingSkills) Discover(context.Context, *trust.Principal, string) ([]agentskills.SkillRecord, error) {
	return s.items, nil
}

func pageCatalogBindingPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "user-a", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

func pageCatalogBindingPersona(t *testing.T, pin agentskills.SkillPin) agentpersona.PersonaVersion {
	t.Helper()
	persona, err := agentpersona.Seal(agentpersona.PersonaProfile{
		Manifest: agentpersona.AgentManifestRef{ID: "agent.people-coach", Version: 1, Digest: "manifest-people-coach", SchemaVersion: 1},
		PersonaID: "persona.people-coach", Version: 1, Handle: "people-coach", DisplayName: "People Coach", AvatarRef: "avatar:people-coach",
		Purpose: "agent.self_service", Audience: agentpersona.Audience{Roles: []string{"employee"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-a"}},
		SkillPins: []agentskills.SkillPin{pin}, TierCeiling: agentskills.TierRead,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate},
		Instructions: "Answer within the declared skill set.", Owner: "owner", Steward: "steward", EvalSuiteRef: "eval:people-coach",
		EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1000},
	})
	if err != nil {
		t.Fatal(err)
	}
	return persona
}

func TestNewAgentPageCatalogBinding_RequiresAuthoritativeSources(t *testing.T) {
	tasks := &pageCatalogBindingClient{}
	if _, err := NewAgentPageCatalogBinding(nil, &pageCatalogBindingPersonas{}, pageCatalogBindingSkills{}); !errors.Is(err, ErrAgentPageCatalogBindingUnavailable) {
		t.Fatalf("nil task client error = %v", err)
	}
	if _, err := NewAgentPageCatalogBinding(tasks, nil, pageCatalogBindingSkills{}); !errors.Is(err, ErrAgentPageCatalogBindingUnavailable) {
		t.Fatalf("nil persona source error = %v", err)
	}
	if _, err := NewAgentPageCatalogBinding(tasks, &pageCatalogBindingPersonas{}, nil); !errors.Is(err, ErrAgentPageCatalogBindingUnavailable) {
		t.Fatalf("nil skill source error = %v", err)
	}
}

func TestAgentPageCatalogBinding_PreservesOwnerTaskProjection(t *testing.T) {
	principal := pageCatalogBindingPrincipal(t)
	pin := agentskills.SkillPin{ID: "skill.people.read", Version: 2, Digest: "digest-v2"}
	persona := pageCatalogBindingPersona(t, pin)
	tasks := &pageCatalogBindingClient{snapshot: productui.AgentSnapshot{
		Availability: productui.AgentsAvailable,
		Threads:      []productui.AgentThread{{ID: "thread-a", AgentID: "persona-a"}},
		Tasks:        []productui.AgentTask{{ID: "task-a", Goal: "Review policy", State: productui.AgentTaskRunning}},
	}}
	personas := &pageCatalogBindingPersonas{items: []agentpersona.PersonaVersion{persona}}
	skills := pageCatalogBindingSkills{items: []agentskills.SkillRecord{{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, Description: "Read my profile"}, Digest: pin.Digest, Status: agentskills.StatusActive}}}
	binding, err := NewAgentPageCatalogBinding(tasks, personas, skills)
	if err != nil {
		t.Fatal(err)
	}
	got, err := binding.Snapshot(trust.WithPrincipal(context.Background(), principal), productui.AgentSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if err != nil {
		t.Fatal(err)
	}
	if tasks.calls != 1 || personas.calls != 1 {
		t.Fatalf("calls tasks=%d personas=%d, want one each", tasks.calls, personas.calls)
	}
	if len(got.Tasks) != 1 || got.Tasks[0].ID != "task-a" || len(got.Threads) != 1 || got.Threads[0].ID != "thread-a" {
		t.Fatalf("owner projection changed: %+v", got)
	}
	if len(got.Agents) != 1 || got.Agents[0].ID != persona.Profile.PersonaID {
		t.Fatalf("persona catalog = %+v, want the current authorized persona", got.Agents)
	}
}

func TestAgentPageCatalogBinding_RejectsDetachedOrMismatchedTrust(t *testing.T) {
	tasks := &pageCatalogBindingClient{}
	personas := &pageCatalogBindingPersonas{}
	binding, err := NewAgentPageCatalogBinding(tasks, personas, pageCatalogBindingSkills{})
	if err != nil {
		t.Fatal(err)
	}
	principal := pageCatalogBindingPrincipal(t)
	cases := []struct {
		name string
		ctx  context.Context
		req  productui.AgentSnapshotRequest
	}{
		{name: "detached", ctx: context.Background(), req: productui.AgentSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"}},
		{name: "wrong tenant", ctx: trust.WithPrincipal(context.Background(), principal), req: productui.AgentSnapshotRequest{TenantID: "tenant-b", Principal: "user-a"}},
		{name: "wrong subject", ctx: trust.WithPrincipal(context.Background(), principal), req: productui.AgentSnapshotRequest{TenantID: "tenant-a", Principal: "user-b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := binding.Snapshot(tc.ctx, tc.req); !errors.Is(err, ErrAgentPageCatalogBindingUnavailable) {
				t.Fatalf("error = %v, want unavailable", err)
			}
		})
	}
	if tasks.calls != 0 || personas.calls != 0 {
		t.Fatalf("untrusted requests reached sources: tasks=%d personas=%d", tasks.calls, personas.calls)
	}
}

func TestAgentPageCatalogBinding_FailsClosedWhenCatalogSourceFails(t *testing.T) {
	principal := pageCatalogBindingPrincipal(t)
	tasks := &pageCatalogBindingClient{snapshot: productui.AgentSnapshot{
		Tasks: []productui.AgentTask{{ID: "task-a", State: productui.AgentTaskRunning}},
	}}
	binding, err := NewAgentPageCatalogBinding(tasks, &pageCatalogBindingPersonas{result: errors.New("audience unavailable")}, pageCatalogBindingSkills{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := binding.Snapshot(trust.WithPrincipal(context.Background(), principal), productui.AgentSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"}); err == nil {
		t.Fatal("catalog failure was rendered as a successful page")
	}
}
