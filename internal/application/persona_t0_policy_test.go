package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
)

type personaT0CatalogFake struct {
	record agentskills.SkillRecord
	err    error
}

func (f personaT0CatalogFake) ResolvePin(agentskills.SkillPin) (agentskills.SkillRecord, error) {
	if f.err != nil {
		return agentskills.SkillRecord{}, f.err
	}
	return f.record, nil
}

type personaT0RevocationFake struct {
	revoked bool
	err     error
	seen    []PersonaT0Invocation
}

func (f *personaT0RevocationFake) IsRevoked(_ context.Context, invocation PersonaT0Invocation) (bool, error) {
	f.seen = append(f.seen, invocation)
	return f.revoked, f.err
}

func personaT0PolicyFixture(t *testing.T) (*PersonaT0SkillPolicy, PersonaT0Invocation, agentskills.SkillPin, *personaT0RevocationFake) {
	t.Helper()
	invocation := PersonaT0Invocation{TenantID: "tenant-a", PersonaID: "persona-a", PersonaVersion: "7", InstallationID: "install-a", InvocationID: "invoke-a"}
	pin := agentskills.SkillPin{ID: "skill.read", Version: 2, Digest: "digest-2"}
	revocations := &personaT0RevocationFake{}
	policy, err := NewPersonaT0SkillPolicy(personaT0CatalogFake{record: agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, SideEffectTier: agentskills.TierT0}, Digest: pin.Digest, Status: agentskills.StatusActive, HighestCapabilityTier: agentskills.TierT0}}, revocations, []PersonaT0SkillPin{{Invocation: invocation, Pin: pin, Scopes: []string{"people.read"}}})
	if err != nil {
		t.Fatal(err)
	}
	return policy, invocation, pin, revocations
}

func TestTodo_AGENTP_008_T0PolicyExactBoundSkill(t *testing.T) {
	policy, invocation, pin, revocations := personaT0PolicyFixture(t)
	ok, err := policy.IsBoundT0Skill(context.Background(), invocation, pin, []string{"people.read"})
	if err != nil || !ok || len(revocations.seen) != 1 || revocations.seen[0] != invocation {
		t.Fatalf("bound T0 result=%v err=%v revocations=%+v", ok, err, revocations.seen)
	}
}

func TestTodo_AGENTP_008_T0PolicyBoundRun(t *testing.T) {
	policy, invocation, _, revocations := personaT0PolicyFixture(t)
	ok, err := policy.IsBoundT0Run(context.Background(), agentinvoke.RunRequest{
		InvocationID: invocation.InvocationID, TenantID: invocation.TenantID,
		PersonaID: invocation.PersonaID, PersonaVersion: invocation.PersonaVersion,
		InstallationID: invocation.InstallationID, Mode: agentinvoke.OnBehalfOf,
		Skills: agentinvoke.SkillScopes{"skill.read": {"people.read"}},
	})
	if err != nil || !ok || len(revocations.seen) != 1 {
		t.Fatalf("bound run result=%v err=%v revocations=%d", ok, err, len(revocations.seen))
	}
}

func TestTodo_AGENTP_008_T0PolicyBoundRunRejectsUnknownSkill(t *testing.T) {
	policy, invocation, _, _ := personaT0PolicyFixture(t)
	ok, err := policy.IsBoundT0Run(context.Background(), agentinvoke.RunRequest{
		InvocationID: invocation.InvocationID, TenantID: invocation.TenantID,
		PersonaID: invocation.PersonaID, PersonaVersion: invocation.PersonaVersion,
		InstallationID: invocation.InstallationID, Mode: agentinvoke.OnBehalfOf,
		Skills: agentinvoke.SkillScopes{"skill.forged": {"people.read"}},
	})
	if ok || !errors.Is(err, ErrPersonaT0Policy) {
		t.Fatalf("unknown skill result=%v err=%v", ok, err)
	}
}

func TestTodo_AGENTP_008_T0PolicyRejectsForgedInputs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PersonaT0Invocation, *agentskills.SkillPin, *[]string)
	}{
		{name: "wrong tenant", mutate: func(i *PersonaT0Invocation, _ *agentskills.SkillPin, _ *[]string) { i.TenantID = "tenant-b" }},
		{name: "wrong version", mutate: func(_ *PersonaT0Invocation, p *agentskills.SkillPin, _ *[]string) { p.Version = 1 }},
		{name: "wrong digest", mutate: func(_ *PersonaT0Invocation, p *agentskills.SkillPin, _ *[]string) { p.Digest = "forged" }},
		{name: "wrong scope", mutate: func(_ *PersonaT0Invocation, _ *agentskills.SkillPin, s *[]string) { *s = []string{"people.write"} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy, invocation, pin, _ := personaT0PolicyFixture(t)
			scopes := []string{"people.read"}
			tc.mutate(&invocation, &pin, &scopes)
			ok, err := policy.IsBoundT0Skill(context.Background(), invocation, pin, scopes)
			if ok || !errors.Is(err, ErrPersonaT0Policy) {
				t.Fatalf("forged binding result=%v err=%v", ok, err)
			}
		})
	}
}

func TestTodo_AGENTP_008_T0PolicyRejectsNonT0AndRevoked(t *testing.T) {
	policy, invocation, pin, revocations := personaT0PolicyFixture(t)
	catalog := policy.catalog.(personaT0CatalogFake)
	catalog.record.Definition.SideEffectTier = agentskills.TierPrivateDraft
	catalog.record.HighestCapabilityTier = agentskills.TierPrivateDraft
	policy.catalog = catalog
	if ok, err := policy.IsBoundT0Skill(context.Background(), invocation, pin, []string{"people.read"}); ok || !errors.Is(err, ErrPersonaT0Policy) {
		t.Fatalf("non-T0 result=%v err=%v", ok, err)
	}
	policy, invocation, pin, revocations = personaT0PolicyFixture(t)
	revocations.revoked = true
	if ok, err := policy.IsBoundT0Skill(context.Background(), invocation, pin, []string{"people.read"}); ok || !errors.Is(err, ErrPersonaT0Policy) {
		t.Fatalf("revoked result=%v err=%v", ok, err)
	}
}

func TestTodo_AGENTP_008_LegacyT0PolicyFailsClosed(t *testing.T) {
	policy, _, _, _ := personaT0PolicyFixture(t)
	ok, err := policy.IsT0Skill(context.Background(), "skill.read", []string{"people.read"})
	if ok || !errors.Is(err, ErrPersonaT0Context) {
		t.Fatalf("legacy result=%v err=%v, want fail closed context error", ok, err)
	}
}
