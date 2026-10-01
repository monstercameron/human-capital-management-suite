package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

type sourceSkillCatalog struct{ records []agentskills.SkillRecord }

func (c sourceSkillCatalog) List() []agentskills.SkillRecord { return c.records }
func (c sourceSkillCatalog) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	for _, record := range c.records {
		if record.Definition.Key() == pin.Key() {
			if record.Digest != pin.Digest {
				return agentskills.SkillRecord{}, agentskills.ErrDigestMismatch
			}
			return record, nil
		}
	}
	return agentskills.SkillRecord{}, agentskills.ErrUnknownSkill
}

type sourceCurrent struct{ user agentgate.UserContext }

func (c sourceCurrent) Resolve(context.Context, *trust.Principal, string) (agentgate.UserContext, []agentgate.Subject, []authz.FieldID, error) {
	return c.user, nil, nil, nil
}

func sourcePrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "user-a", SubjectKind: trust.SubjectKindHuman,
		Purposes:             []string{"purpose.read"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "cred",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTodo_AGENT2_005_AgentSkillSourceUsesCurrentRegistryAndPurpose(t *testing.T) {
	p := sourcePrincipal(t)
	record := agentskills.SkillRecord{Definition: agentskills.SkillDefinition{
		ID: "skill.read", Version: 1, Description: "Read", RequiredPurposes: []string{"purpose.read"},
	}, Digest: "digest", Status: agentskills.StatusActive}
	key := record.Definition.Key()
	source, err := NewAgentSkillSource(sourceSkillCatalog{records: []agentskills.SkillRecord{record}}, agentgate.StaticGrants{{
		ID: "grant", Tenant: p.Tenant(), Skill: key, Roles: []string{"employee"}, Purposes: []string{"purpose.read"},
		Population: "employees", OrganizationScopes: []string{"org-a"},
	}}, sourceCurrent{user: agentgate.UserContext{Principal: p, Population: "employees", Roles: []string{"employee"}, OrganizationScopes: []string{"org-a"}}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.Discover(trust.WithPrincipal(context.Background(), p), p, "purpose.read")
	if err != nil || len(got) != 1 || got[0].Digest != record.Digest {
		t.Fatalf("Discover() = %#v, %v; want current granted record", got, err)
	}
	resolved, err := source.ResolvePin(agentskills.SkillPin{ID: record.Definition.ID, Version: record.Definition.Version, Digest: record.Digest})
	if err != nil || resolved.Digest != record.Digest {
		t.Fatalf("ResolvePin() = %#v, %v", resolved, err)
	}
}

func TestTodo_AGENT2_005_AgentSkillSourceRejectsCallerAuthorityAndStalePin(t *testing.T) {
	p := sourcePrincipal(t)
	record := agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: "skill.read", Version: 1, Description: "Read", RequiredPurposes: []string{"purpose.read"}}, Digest: "digest", Status: agentskills.StatusActive}
	source, err := NewAgentSkillSource(sourceSkillCatalog{records: []agentskills.SkillRecord{record}}, agentgate.StaticGrants{{
		ID: "grant", Tenant: p.Tenant(), Skill: record.Definition.Key(), Roles: []string{"employee"}, Purposes: []string{"purpose.read"},
		Population: "employees", OrganizationScopes: []string{"org-a"},
	}}, sourceCurrent{user: agentgate.UserContext{Principal: p, Population: "employees", Roles: []string{"employee"}, OrganizationScopes: []string{"org-a"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Discover(context.Background(), p, ""); !errors.Is(err, errAgentSkillSource) {
		t.Fatalf("blank purpose error = %v", err)
	}
	if _, err := source.ResolvePin(agentskills.SkillPin{ID: record.Definition.ID, Version: 1, Digest: "stale"}); !errors.Is(err, agentskills.ErrDigestMismatch) {
		t.Fatalf("stale pin error = %v", err)
	}
	if _, err := source.Discover(context.Background(), p, "purpose.read"); !errors.Is(err, errAgentSkillSource) {
		t.Fatalf("detached principal error = %v", err)
	}
}
