package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

type agentUserCatalogPersonas []agentpersona.PersonaVersion

func (p agentUserCatalogPersonas) ListAvailable(context.Context, *trust.Principal) ([]agentpersona.PersonaVersion, error) {
	return p, nil
}

type agentUserCatalogDiscovery struct {
	byPurpose map[string][]agentskills.SkillRecord
	err       error
	called    []string
}

type emptyAgentSkillCatalog struct{}

func (emptyAgentSkillCatalog) List() []agentskills.SkillRecord { return nil }
func (emptyAgentSkillCatalog) ResolvePin(agentskills.SkillPin) (agentskills.SkillRecord, error) {
	return agentskills.SkillRecord{}, agentskills.ErrUnknownSkill
}

type agentUserCatalogContext struct {
	user agentgate.UserContext
	call int
}

func (c *agentUserCatalogContext) Resolve(_ context.Context, principal *trust.Principal, _ string) (agentgate.UserContext, []agentgate.Subject, []authz.FieldID, error) {
	c.call++
	if principal.Subject() != c.user.Principal.Subject() {
		return agentgate.UserContext{}, nil, nil, errAgentUserCatalog
	}
	return c.user, nil, nil, nil
}

func (d *agentUserCatalogDiscovery) Discover(_ context.Context, _ *trust.Principal, purpose string) ([]agentskills.SkillRecord, error) {
	d.called = append(d.called, purpose)
	return d.byPurpose[purpose], d.err
}

func agentUserCatalogPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	at := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "user-a", SubjectKind: trust.SubjectKindHuman,
		Roles: []string{"employee"}, Purposes: []string{"agent.self_service"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-a", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "cred-a",
	})
	if err != nil {
		t.Fatalf("principal: %v", err)
	}
	return p
}

func agentUserCatalogPersona(t *testing.T, id, name, purpose string, pin agentskills.SkillPin) agentpersona.PersonaVersion {
	t.Helper()
	version, err := agentpersona.Seal(agentpersona.PersonaProfile{
		Manifest:  agentpersona.AgentManifestRef{ID: "agent." + id, Version: 1, Digest: "manifest-" + id, SchemaVersion: 1},
		PersonaID: "persona." + id, Version: 1, Handle: id, DisplayName: name, AvatarRef: "avatar:" + id,
		Purpose: purpose, Audience: agentpersona.Audience{Roles: []string{"employee"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-a"}},
		SkillPins: []agentskills.SkillPin{pin}, TierCeiling: agentskills.TierRead,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect},
		ChannelClasses:    []agentpersona.ChannelClass{agentpersona.ChannelPrivate},
		Instructions:      "Answer within the declared skill set.", Owner: "owner", Steward: "steward", EvalSuiteRef: "eval:" + id,
		EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1000},
	})
	if err != nil {
		t.Fatalf("seal persona: %v", err)
	}
	return version
}

// TestTodo_AGENT2_016 verifies that an installed persona is advertised only
// when AGENT2-005 discovery returns every exact skill version and digest.
func TestTodo_AGENT2_016_AvailablePersonaDiscovery(t *testing.T) {
	pin := agentskills.SkillPin{ID: "skill.people.read", Version: 2, Digest: "digest-v2"}
	persona := agentUserCatalogPersona(t, "people-coach", "People Coach", "agent.self_service", pin)
	allowed := agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, Description: "Read my profile"}, Digest: pin.Digest, Status: agentskills.StatusActive}
	discovery := &agentUserCatalogDiscovery{byPurpose: map[string][]agentskills.SkillRecord{"agent.self_service": {allowed}}}
	catalog := &AgentUserCatalog{Personas: agentUserCatalogPersonas{persona}, Skills: discovery}
	principal := agentUserCatalogPrincipal(t)
	got, err := catalog.List(trust.WithPrincipal(context.Background(), principal), principal)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != persona.Profile.PersonaID || got[0].Name != "People Coach" || got[0].Status != "Ready" {
		t.Fatalf("catalog = %+v", got)
	}
	if len(got[0].Skills) != 1 || got[0].Skills[0] != "Read my profile" {
		t.Fatalf("displayed skills = %v", got[0].Skills)
	}
	if len(discovery.called) != 1 || discovery.called[0] != persona.Profile.Purpose {
		t.Fatalf("discovery purposes = %v", discovery.called)
	}
}

// TestTodo_AGENT2_016_RefusesUnavailableSkills proves a grant or version
// change after installation cannot leave a persona advertised as usable.
func TestTodo_AGENT2_016_RefusesUnavailableSkills(t *testing.T) {
	pin := agentskills.SkillPin{ID: "skill.people.read", Version: 2, Digest: "digest-v2"}
	persona := agentUserCatalogPersona(t, "people-coach", "People Coach", "agent.self_service", pin)
	for name, discovered := range map[string][]agentskills.SkillRecord{
		"not granted":   {},
		"wrong version": {{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: 1, Description: "Old profile"}, Digest: pin.Digest, Status: agentskills.StatusActive}},
		"wrong digest":  {{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, Description: "Changed profile"}, Digest: "new-digest", Status: agentskills.StatusActive}},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := &AgentUserCatalog{Personas: agentUserCatalogPersonas{persona}, Skills: &agentUserCatalogDiscovery{byPurpose: map[string][]agentskills.SkillRecord{"agent.self_service": discovered}}}
			principal := agentUserCatalogPrincipal(t)
			got, err := catalog.List(trust.WithPrincipal(context.Background(), principal), principal)
			if err != nil || len(got) != 0 {
				t.Fatalf("catalog = %+v, err = %v; want unavailable persona omitted", got, err)
			}
		})
	}
}

// TestTodo_AGENT2_016_FailsClosed ensures discovery failures are not rendered
// as an empty successful catalog, which would hide a policy outage.
func TestTodo_AGENT2_016_FailsClosed(t *testing.T) {
	pin := agentskills.SkillPin{ID: "skill.people.read", Version: 1, Digest: "digest-v1"}
	persona := agentUserCatalogPersona(t, "people-coach", "People Coach", "agent.self_service", pin)
	catalog := &AgentUserCatalog{Personas: agentUserCatalogPersonas{persona}, Skills: &agentUserCatalogDiscovery{err: errors.New("policy store unavailable")}}
	principal := agentUserCatalogPrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), principal)
	if _, err := catalog.List(ctx, principal); err == nil {
		t.Fatal("discovery error was hidden as an empty catalog")
	}
	if _, err := (*AgentUserCatalog)(nil).List(ctx, principal); err == nil {
		t.Fatal("nil catalog was accepted")
	}
	if _, err := catalog.List(ctx, nil); err == nil {
		t.Fatal("nil principal was accepted")
	}
	if _, err := catalog.List(context.Background(), principal); err == nil {
		t.Fatal("a principal detached from the verified request context was accepted")
	}
}

// TestTodo_AGENT2_016_GateDiscovery proves the production catalog adapter
// derives current user context before invoking the AGENT2-005 gate.
func TestTodo_AGENT2_016_GateDiscovery(t *testing.T) {
	principal := agentUserCatalogPrincipal(t)
	current := &agentUserCatalogContext{user: agentgate.UserContext{Principal: principal, Population: "employees", Roles: []string{"employee"}, OrganizationScopes: []string{"org-a"}}}
	gate, err := agentgate.New(agentgate.Config{Skills: emptyAgentSkillCatalog{}, Grants: agentgate.StaticGrants{}})
	if err != nil {
		t.Fatalf("agentgate.New: %v", err)
	}
	discoverer := GateAgentSkillDiscoverer{Gate: gate, Context: current}
	got, err := discoverer.Discover(context.Background(), principal, "agent.self_service")
	if err != nil || len(got) != 0 || current.call != 1 {
		t.Fatalf("Discover = %v, %v; context resolutions = %d", got, err, current.call)
	}
	if _, err := discoverer.Discover(context.Background(), nil, "agent.self_service"); err == nil {
		t.Fatal("nil principal was accepted")
	}
	if current.call != 1 {
		t.Fatal("invalid principal reached current authority resolution")
	}
}
