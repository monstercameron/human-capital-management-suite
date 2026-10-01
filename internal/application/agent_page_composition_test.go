package application

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agentclient"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type pageCompositionRunner struct{ tasks []agentrun.AgentTask }

func (r pageCompositionRunner) UserTasks(context.Context, string) ([]agentrun.AgentTask, error) {
	return r.tasks, nil
}
func (pageCompositionRunner) BudgetUsage(string) (agentbudget.Limits, agentbudget.Limits, bool) {
	return agentbudget.Limits{}, agentbudget.Limits{}, false
}

type pageCompositionRunners struct {
	tenant values.TenantId
	runner agentclient.TaskReader
}

func (r *pageCompositionRunners) Runner(context.Context, values.TenantId) (agentclient.TaskReader, error) {
	return r.runner, nil
}

type pageCompositionBackend struct {
	tenant values.TenantId
	item   agentpersonastore.PersonaVersion
}

func (b *pageCompositionBackend) ListAvailable(_ context.Context, tenant values.TenantId, _ []agentpersonastore.AvailableInstallation) ([]agentpersonastore.PersonaVersion, error) {
	b.tenant = tenant
	return []agentpersonastore.PersonaVersion{b.item}, nil
}

type pageCompositionAudience struct {
	principal *trust.Principal
}

func (a *pageCompositionAudience) ResolveAvailablePersonaInstallations(_ context.Context, principal *trust.Principal) ([]agentpersonastore.AvailableInstallation, error) {
	a.principal = principal
	return []agentpersonastore.AvailableInstallation{{InstallationID: "install-1", PersonaID: "persona-coach", PersonaVersion: 1}}, nil
}

type pageCompositionSkills struct{ record agentskills.SkillRecord }

func (s pageCompositionSkills) Discover(context.Context, *trust.Principal, string) ([]agentskills.SkillRecord, error) {
	return []agentskills.SkillRecord{s.record}, nil
}

func TestComposeAgentPageClient_FailsClosedWithoutTrustedSources(t *testing.T) {
	if got := ComposeAgentPageClient(AgentPageCompositionInput{}); got != nil {
		t.Fatal("empty composition returned an available client")
	}
}

func TestComposeAgentPageClient_FailsClosedWithoutSkillAuthority(t *testing.T) {
	in := AgentPageCompositionInput{
		Runners:        &pageCompositionRunners{runner: pageCompositionRunner{}},
		PersonaBackend: &pageCompositionBackend{},
		Audience:       &pageCompositionAudience{},
	}
	if got := ComposeAgentPageClient(in); got != nil {
		t.Fatal("composition without skill authority returned an available client")
	}
}

func TestComposeAgentPageClient_BindsExactTenantAndCurrentUser(t *testing.T) {
	principal := agentUserCatalogPrincipal(t)
	pin := agentskills.SkillPin{ID: "skill.people.read", Version: 2, Digest: "digest-v2"}
	sealed := agentUserCatalogPersona(t, "people-coach", "People Coach", "agent.self_service", pin)
	profile, err := json.Marshal(sealed.Profile)
	if err != nil {
		t.Fatal(err)
	}
	backend := &pageCompositionBackend{item: agentpersonastore.PersonaVersion{TenantID: principal.Tenant(), PersonaID: sealed.Profile.PersonaID, Version: int64(sealed.Profile.Version), Profile: profile, ContentDigest: sealed.Digest}}
	audience := &pageCompositionAudience{}
	runners := &pageCompositionRunners{tenant: principal.Tenant(), runner: pageCompositionRunner{}}
	client := ComposeAgentPageClient(AgentPageCompositionInput{
		Runners: runners, PersonaBackend: backend, Audience: audience,
		Skills: pageCompositionSkills{record: agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, Description: "Read my profile"}, Digest: pin.Digest, Status: agentskills.StatusActive}},
	})
	if client == nil {
		t.Fatal("valid composition returned nil")
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
	snapshot, err := client.Snapshot(ctx, productui.AgentSnapshotRequest{TenantID: principal.Tenant().String(), Principal: principal.Subject()})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Agents) != 1 || snapshot.Agents[0].ID != sealed.Profile.PersonaID {
		t.Fatalf("agents = %+v", snapshot.Agents)
	}
	if backend.tenant != principal.Tenant() || audience.principal != principal {
		t.Fatalf("authority binding tenant=%q principal=%p want tenant=%q principal=%p", backend.tenant, audience.principal, principal.Tenant(), principal)
	}
	_, err = client.Snapshot(ctx, productui.AgentSnapshotRequest{TenantID: "other-tenant", Principal: principal.Subject()})
	if err == nil {
		t.Fatal("cross-tenant request was accepted")
	}
}

func TestComposeAgentPageClient_RejectsDetachedTrustContext(t *testing.T) {
	client := ComposeAgentPageClient(AgentPageCompositionInput{Runners: &pageCompositionRunners{runner: pageCompositionRunner{}}, PersonaBackend: &pageCompositionBackend{}, Audience: &pageCompositionAudience{}, Skills: pageCompositionSkills{}})
	if client == nil {
		t.Fatal("valid dependency shape returned nil")
	}
	_, err := client.Snapshot(context.Background(), productui.AgentSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if err == nil {
		t.Fatal("detached trust context was accepted")
	}
}
