package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The Agents page drew a made-up glyph for an agent whose stored icon Chat and
// Agent setup draw, because the page's projection never read the stored icon.
func TestTodo_AGENTUX_074_AgentPageIcons(t *testing.T) {
	store, scoped, ctx, now, _ := agentIconApplicationFixture(t)
	agentIconCreateApplicationDraft(t, scoped, ctx, now, "assistant")
	stored, err := scoped.GetIcon(ctx, "assistant")
	if err != nil || !stored.Value.Valid() {
		t.Fatal("fixture agent has no stored icon", stored, err)
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		t.Fatal("fixture context carries no principal")
	}

	agents := []productui.AgentSummary{{ID: "assistant", Name: "Assistant"}, {ID: "never-stored", Name: "No Icon"}}
	agentUX074AttachStoredIcons(ctx, AgentIconProjection{Store: store}, principal, agents)
	if agents[0].Icon != stored.Value || agents[0].IconRevision != stored.Revision {
		t.Fatalf("the listed agent did not receive its stored icon: %+v, want %+v", agents[0], stored)
	}
	if agents[1].Icon.Valid() || agents[1].Name != "No Icon" {
		t.Fatalf("an agent with no stored icon must keep the zero icon and stay listed: %+v", agents[1])
	}
	if len(agents) != 2 {
		t.Fatal("decorating icons changed which agents are listed")
	}

	// The same icon reaches Agent setup through the catalog reader, so the two
	// pages agree by construction.
	catalog := AgentIconCatalogVersions{Base: catalogVersions{}, Store: store}
	if _, err := catalog.ListPersonaCatalogVersions(ctx, "tenant-a"); err != nil {
		t.Fatal("catalog reader", err)
	}

	// Without an icon source the list is returned as it came.
	plain := []productui.AgentSummary{{ID: "assistant", Name: "Assistant"}}
	agentUX074AttachStoredIcons(ctx, AgentIconProjection{}, principal, plain)
	if plain[0].Icon.Valid() {
		t.Fatal("an unwired icon source produced an icon")
	}
	agentUX074AttachStoredIcons(ctx, AgentIconProjection{Store: store}, nil, plain)
	if plain[0].Icon.Valid() {
		t.Fatal("icons were read without a verified principal")
	}

	// The served binding carries the icon into the page snapshot for an agent
	// the catalog admits, and only decorates what the catalog returned.
	pin := agentskills.SkillPin{ID: "skill.people.read", Version: 2, Digest: "digest-v2"}
	persona := pageCatalogBindingPersona(t, pin)
	agentIconCreateApplicationDraft(t, scoped, ctx, now, persona.Profile.PersonaID)
	coach, err := scoped.GetIcon(ctx, persona.Profile.PersonaID)
	if err != nil || !coach.Value.Valid() {
		t.Fatal("fixture persona has no stored icon", coach, err)
	}
	tasks := &pageCatalogBindingClient{snapshot: productui.AgentSnapshot{Availability: productui.AgentsAvailable}}
	personas := &pageCatalogBindingPersonas{items: []agentpersona.PersonaVersion{persona}}
	skills := pageCatalogBindingSkills{items: []agentskills.SkillRecord{{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, Description: "Read my profile"}, Digest: pin.Digest, Status: agentskills.StatusActive}}}
	binding, err := NewAgentPageCatalogBinding(tasks, personas, skills)
	if err != nil {
		t.Fatal(err)
	}
	request := productui.AgentSnapshotRequest{TenantID: principal.Tenant().String(), Principal: principal.Subject()}
	without, err := binding.Snapshot(ctx, request)
	if err != nil || len(without.Agents) != 1 || without.Agents[0].Icon.Valid() {
		t.Fatalf("binding without an icon source: %+v, %v", without.Agents, err)
	}
	binding.Icons = AgentIconProjection{Store: store}
	with, err := binding.Snapshot(ctx, request)
	if err != nil || len(with.Agents) != 1 || with.Agents[0].ID != persona.Profile.PersonaID {
		t.Fatalf("binding snapshot: %+v, %v", with.Agents, err)
	}
	if with.Agents[0].Icon != coach.Value || with.Agents[0].IconRevision != coach.Revision {
		t.Fatalf("the Agents page projection does not carry the stored icon: %+v, want %+v", with.Agents[0], coach)
	}
}
