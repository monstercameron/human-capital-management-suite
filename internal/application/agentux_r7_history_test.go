package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type agentUXR7HistoryTenant struct {
	personaAdminCompositionTenant
	events []agentpersonastore.LifecycleEvent
}

func (r agentUXR7HistoryTenant) ListLifecycle(_ context.Context, persona string, version int64) ([]agentpersonastore.LifecycleEvent, error) {
	var out []agentpersonastore.LifecycleEvent
	for _, event := range r.events {
		if event.PersonaID == persona && event.PersonaVersion == version {
			out = append(out, event)
		}
	}
	return out, nil
}

type agentUXR7HistoryStore struct {
	tenant values.TenantId
	reader agentUXR7HistoryTenant
}

func (r agentUXR7HistoryStore) ForTenant(_ context.Context, tenant values.TenantId) (PersonaAdminCatalogTenant, error) {
	if tenant != r.tenant {
		return nil, ErrPersonaCatalogDenied
	}
	return r.reader, nil
}

func TestAgentUXR7_L10_HistoryRetainsEarlierInstructionsAndPublisher(t *testing.T) {
	ctx, principal := catalogContext(t)
	profile := compositionPersonaProfile(t)
	store := agentUXR7HistoryStore{tenant: principal.Tenant()}
	at := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	for _, version := range []int64{1, 2} {
		profile.Version = uint32(version)
		profile.Instructions = "Earlier instructions"
		if version == 2 {
			profile.Instructions = "Current instructions"
		}
		sealed, err := agentpersona.Seal(profile)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(profile)
		if err != nil {
			t.Fatal(err)
		}
		store.reader.entries = append(store.reader.entries, agentpersonastore.CatalogEntry{
			Version:       agentpersonastore.PersonaVersion{TenantID: principal.Tenant(), PersonaID: profile.PersonaID, Version: version, AgentVersion: "agent-v1", Handle: profile.Handle, DisplayName: profile.DisplayName, Profile: encoded, ContentDigest: sealed.Digest},
			Lifecycle:     agentpersonastore.StatePublished,
			Installations: []agentpersonastore.CatalogInstallation{{State: string(agentpersonastore.InstallationActive)}, {State: "RETIRED"}},
		})
		store.reader.events = append(store.reader.events, agentpersonastore.LifecycleEvent{PersonaID: profile.PersonaID, PersonaVersion: version, To: agentpersonastore.StatePublished, ActorID: "publisher", OccurredAt: at.Add(time.Duration(version) * time.Hour)})
	}
	reader := personaAdminCatalogVersions{store: store}
	history, err := reader.ListPersonaAdminVersionHistory(ctx, principal.Tenant())
	if err != nil {
		t.Fatal(err)
	}
	rows := history[profile.PersonaID]
	if len(rows) != 2 || rows[0].Instructions != "Earlier instructions" || rows[1].Instructions != "Current instructions" {
		t.Fatalf("version history lost immutable instructions: %+v", rows)
	}
	for index, row := range rows {
		if row.PublishedBy != "publisher" || row.PublishedAt != at.Add(time.Duration(index+1)*time.Hour).Format(time.RFC3339) || row.ConversationCount != 1 {
			t.Fatalf("history invented publisher/date or counted inactive conversations: %+v", row)
		}
	}
	if _, err = reader.ListPersonaAdminVersionHistory(ctx, "other-tenant"); !errors.Is(err, ErrPersonaCatalogDenied) {
		t.Fatalf("history crossed tenant boundary: %v", err)
	}
}
