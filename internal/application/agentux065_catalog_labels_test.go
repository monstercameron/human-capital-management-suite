package application

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// The admin catalog projects each version's sealed label: the current
// version, the live version behind a newer draft, every placement, and
// every history row (AGENTUX-065).
func TestAgentUX065_CatalogCarriesSealedLabels(t *testing.T) {
	ctx, principal := catalogContext(t)
	v1profile := compositionPersonaProfile(t)
	v2profile := compositionPersonaProfile(t)
	v2profile.Version = 2
	v2profile.Instructions = "Answer with approved records, briefly."
	sealed1, err := agentpersona.Seal(v1profile)
	if err != nil {
		t.Fatal(err)
	}
	sealed2, err := agentpersona.Seal(v2profile)
	if err != nil {
		t.Fatal(err)
	}
	encoded1, _ := json.Marshal(v1profile)
	encoded2, _ := json.Marshal(v2profile)
	policy := agentpersonastore.ChannelPolicy{MaxTier: "T0", AllowedDataClasses: []string{"WORKFORCE"}, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPrivate}}
	store := &personaAdminCompositionStore{
		entries: []agentpersonastore.CatalogEntry{
			{
				Version:   agentpersonastore.PersonaVersion{TenantID: principal.Tenant(), PersonaID: v1profile.PersonaID, Version: 1, AgentVersion: "agent-v1", Handle: v1profile.Handle, DisplayName: v1profile.DisplayName, Profile: encoded1, ContentDigest: sealed1.Digest},
				Lifecycle: agentpersonastore.StatePublished, BusinessOwner: v1profile.Owner, Steward: v1profile.Steward,
				Installations: []agentpersonastore.CatalogInstallation{{ID: "installation-1", PersonaID: v1profile.PersonaID, PersonaVersion: 1, ConversationID: "conversation-1", ConversationClass: agentpersonastore.ConversationPrivate, ChannelPolicy: policy, Revision: 1, RevocationEpoch: 1, State: string(agentpersonastore.InstallationActive)}},
			},
			{
				Version:   agentpersonastore.PersonaVersion{TenantID: principal.Tenant(), PersonaID: v2profile.PersonaID, Version: 2, AgentVersion: "agent-v1", Handle: v2profile.Handle, DisplayName: v2profile.DisplayName, Profile: encoded2, ContentDigest: sealed2.Digest},
				Lifecycle: agentpersonastore.StateDraft, BusinessOwner: v2profile.Owner, Steward: v2profile.Steward,
			},
		},
		labels: map[int64]string{1: "1.0.0"},
	}
	client, err := NewPersonaAdminCatalogClient(PersonaAdminCatalogComposition{Store: store, Skills: catalogSkills{}, Targets: catalogTargets{users: []productui.PersonaAdminTarget{{ID: principal.Subject(), Label: "Admin"}}, conversations: []productui.PersonaAdminTarget{{ID: "conversation-1", Label: "Room"}}}, Grants: &catalogGrants{allowed: true}, Authorizer: &catalogAuth{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: string(principal.Tenant()), Principal: principal.Subject()})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Personas) != 1 {
		t.Fatalf("personas = %d; want 1", len(snapshot.Personas))
	}
	persona := snapshot.Personas[0]
	if persona.Version != "2" || persona.VersionLabel != "" {
		t.Fatalf("current draft = %q label %q; want 2 unlabeled", persona.Version, persona.VersionLabel)
	}
	if persona.LiveVersion != "1" || persona.LiveVersionLabel != "1.0.0" {
		t.Fatalf("live = %q label %q; want 1 / 1.0.0", persona.LiveVersion, persona.LiveVersionLabel)
	}
	if len(persona.Installations) != 1 || persona.Installations[0].VersionLabel != "1.0.0" {
		t.Fatalf("placements = %+v; want one at 1.0.0", persona.Installations)
	}
	labels := map[string]string{}
	for _, row := range persona.VersionHistory {
		labels[row.Version] = row.VersionLabel
	}
	if labels["1"] != "1.0.0" || labels["2"] != "" {
		t.Fatalf("history labels = %v; want 1->1.0.0, 2 unlabeled", labels)
	}
}

// The rollout catalog names each published version's and placement's sealed
// label beside its sequence (AGENTUX-065).
func TestAgentUX065_RolloutCatalogCarriesSealedLabels(t *testing.T) {
	ctx, s, store, _ := versionRolloutFixture(t)
	store.labels = map[int64]string{1: "1.0.0", 2: "1.1.0"}
	r, err := s.Execute(ctx, AgentVersionRolloutCommand{Action: "CATALOG"})
	if err != nil || r.Catalog == nil {
		t.Fatal(err)
	}
	if len(r.Catalog.Versions) != 1 || r.Catalog.Versions[0].VersionLabel != "1.1.0" {
		t.Fatalf("catalog versions = %+v; want v2 at 1.1.0", r.Catalog.Versions)
	}
	for _, installation := range r.Catalog.Installations {
		if installation.VersionLabel != "1.0.0" {
			t.Fatalf("catalog installation = %+v; want v1 at 1.0.0", installation)
		}
	}
}

// The admin catalog previews the publish the author is about to make: the
// smallest choice the change allows and the label each choice would seal,
// computed and never typed (AGENTUX-065).
func TestAgentUX065_CatalogPreviewsPublish(t *testing.T) {
	ctx, principal := catalogContext(t)
	v1profile := compositionPersonaProfile(t)
	v2profile := compositionPersonaProfile(t)
	v2profile.Version = 2
	v2profile.Instructions = "Answer with approved records, briefly."
	sealed1, err := agentpersona.Seal(v1profile)
	if err != nil {
		t.Fatal(err)
	}
	sealed2, err := agentpersona.Seal(v2profile)
	if err != nil {
		t.Fatal(err)
	}
	encoded1, _ := json.Marshal(v1profile)
	encoded2, _ := json.Marshal(v2profile)
	store := &personaAdminCompositionStore{
		entries: []agentpersonastore.CatalogEntry{
			{Version: agentpersonastore.PersonaVersion{TenantID: principal.Tenant(), PersonaID: v1profile.PersonaID, Version: 1, AgentVersion: "agent-v1", Handle: v1profile.Handle, DisplayName: v1profile.DisplayName, Profile: encoded1, ContentDigest: sealed1.Digest}, Lifecycle: agentpersonastore.StatePublished, BusinessOwner: v1profile.Owner, Steward: v1profile.Steward},
			{Version: agentpersonastore.PersonaVersion{TenantID: principal.Tenant(), PersonaID: v2profile.PersonaID, Version: 2, AgentVersion: "agent-v1", Handle: v2profile.Handle, DisplayName: v2profile.DisplayName, Profile: encoded2, ContentDigest: sealed2.Digest}, Lifecycle: agentpersonastore.StateInReview, BusinessOwner: v2profile.Owner, Steward: v2profile.Steward},
		},
		labels: map[int64]string{1: "1.4.2"},
	}
	client, err := NewPersonaAdminCatalogClient(PersonaAdminCatalogComposition{Store: store, Skills: catalogSkills{}, Targets: catalogTargets{users: []productui.PersonaAdminTarget{{ID: principal.Subject(), Label: "Admin"}}}, Grants: &catalogGrants{allowed: true}, Authorizer: &catalogAuth{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: string(principal.Tenant()), Principal: principal.Subject()})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Personas) != 1 {
		t.Fatalf("personas = %d; want 1", len(snapshot.Personas))
	}
	preview := snapshot.Personas[0].PublishPreview
	if !preview.Available || preview.Required != "patch" || preview.Greatest != "1.4.2" || preview.Patch != "1.4.3" || preview.Minor != "1.5.0" || preview.Major != "2.0.0" {
		t.Fatalf("publish preview = %+v; want required patch from 1.4.2", preview)
	}
}
