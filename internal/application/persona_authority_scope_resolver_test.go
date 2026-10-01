package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type authorityScopeCatalog struct{ records []agentskills.SkillRecord }

func (c authorityScopeCatalog) List() []agentskills.SkillRecord { return c.records }
func (c authorityScopeCatalog) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
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

func authorityScopeFixture(t *testing.T, tier agentskills.SideEffectTier, digest string) (agentpersona.PersonaProfile, agentpersonastore.ActiveInstallation, authorityScopeCatalog) {
	t.Helper()
	pin := agentskills.SkillPin{ID: "people.read", Version: 1, Digest: digest}
	record := agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, SideEffectTier: tier, DataClassesRead: []string{"WORKFORCE"}}, Digest: digest, Status: agentskills.StatusActive, ResolvedOperations: []agentskills.ResolvedOperation{{HasCapability: true, Capability: capability.Record{Definition: capability.Definition{ID: "people.read", Version: 1, AuthZScopeRef: "scope:people.read"}}}}}
	profile := agentpersona.PersonaProfile{PersonaID: "persona-a", Version: 1, SkillPins: []agentskills.SkillPin{pin}, TierCeiling: agentskills.TierT4}
	installation := agentpersonastore.ActiveInstallation{InstallationID: "install-a", PersonaID: "persona-a", PersonaVersion: 1, ConversationID: "room-a"}
	return profile, installation, authorityScopeCatalog{records: []agentskills.SkillRecord{record}}
}

func TestTodo_AGENTP_008_PersonaAuthorityScopeResolverUsesExactPinnedScopes(t *testing.T) {
	profile, installation, catalog := authorityScopeFixture(t, agentskills.TierT0, "digest-a")
	resolver, err := NewPersonaAuthorityScopeResolver(catalog)
	if err != nil {
		t.Fatal(err)
	}
	persona, install, channel, err := resolver.ResolvePersonaScopes(context.Background(), values.TenantId("tenant-a"), profile, installation, agentpersonastore.ChannelPolicy{MaxTier: "T0", AllowedDataClasses: []string{"WORKFORCE"}})
	if err != nil {
		t.Fatal(err)
	}
	want := agentinvoke.SkillScopes{"people.read": {"scope:people.read"}}
	if !agentinvoke.SkillScopesSubset(want, persona) || !agentinvoke.SkillScopesSubset(want, install) || !agentinvoke.SkillScopesSubset(want, channel) {
		t.Fatalf("scopes = %#v %#v %#v", persona, install, channel)
	}
}

func TestTodo_AGENTP_008_PersonaAuthorityScopeResolverRejectsForgedAndStalePins(t *testing.T) {
	profile, installation, catalog := authorityScopeFixture(t, agentskills.TierT0, "digest-a")
	resolver, _ := NewPersonaAuthorityScopeResolver(catalog)
	for name, digest := range map[string]string{"forged": "digest-forged", "stale": "digest-old"} {
		t.Run(name, func(t *testing.T) {
			profile.SkillPins[0].Digest = digest
			if _, _, _, err := resolver.ResolvePersonaScopes(context.Background(), values.TenantId("tenant-a"), profile, installation, agentpersonastore.ChannelPolicy{MaxTier: "T0", AllowedDataClasses: []string{"WORKFORCE"}}); err == nil || !errors.Is(err, errPersonaAuthorityScopeResolver) {
				t.Fatalf("err=%v", err)
			}
		})
		profile.SkillPins[0].Digest = "digest-a"
	}
}

func TestTodo_AGENTP_008_PersonaAuthorityScopeResolverNarrowingAndNonT0(t *testing.T) {
	profile, installation, catalog := authorityScopeFixture(t, agentskills.TierT1, "digest-a")
	resolver, _ := NewPersonaAuthorityScopeResolver(catalog)
	if _, _, _, err := resolver.ResolvePersonaScopes(context.Background(), values.TenantId("tenant-a"), profile, installation, agentpersonastore.ChannelPolicy{MaxTier: "T0", AllowedDataClasses: []string{"WORKFORCE"}}); err == nil {
		t.Fatal("non-T0 pin escaped T0 policy")
	}
	profile, installation, catalog = authorityScopeFixture(t, agentskills.TierT0, "digest-a")
	resolver, _ = NewPersonaAuthorityScopeResolver(catalog)
	if _, _, _, err := resolver.ResolvePersonaScopes(context.Background(), values.TenantId("tenant-a"), profile, installation, agentpersonastore.ChannelPolicy{MaxTier: "T0", AllowedDataClasses: []string{"OTHER"}}); err == nil {
		t.Fatal("disallowed data class escaped channel narrowing")
	}
}
