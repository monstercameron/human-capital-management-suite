package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestAgentUXGeneral_SuspensionReasons(t *testing.T) {
	cases := map[string]string{
		"AGENT_PRINCIPAL_MISSING_AFTER_RESTORE":     "this version has no runtime identity",
		"AGENT_PRINCIPAL_RETIRED_AFTER_RESTORE":     "this version's runtime identity is no longer active",
		"PERSONA_PUBLICATION_MISSING_AFTER_RESTORE": "this version is no longer published",
		"PERSONA_VERSION_MISSING_AFTER_RESTORE":     "this version is no longer available",
		"UNRECOGNIZED":                              "this installation needs attention from the person who manages it",
	}
	for code, want := range cases {
		if got := personaCatalogSuspensionMessage(code); got != want {
			t.Errorf("reason %q = %q, want %q", code, got, want)
		}
	}
}

func TestAgentUXGeneral_StoppedVersionPlacementProjection(t *testing.T) {
	ctx, _ := catalogContext(t)
	profile := validPersonaProfileForLifecycleTest(t, "owner")
	active := PersonaCatalogInstallation{ID: "active", PersonaID: profile.Profile.PersonaID, PersonaVersion: profile.Profile.Version, ConversationID: "general", Active: true, State: string(agentpersonastore.InstallationActive), ChannelClass: string(agentpersona.ChannelPublic), ConversationKind: string(agentpersona.ConversationChannel), MaxTier: agentskills.TierT0}
	stopped := active
	stopped.ID, stopped.ConversationID, stopped.Active, stopped.State = "stopped", "direct", false, string(agentpersonastore.InstallationSuspended)
	stopped.PersonaVersion++
	stopped.SuspensionReason = "AGENT_PRINCIPAL_MISSING_AFTER_RESTORE"
	service := &PersonaAdminCatalogService{Versions: catalogVersions{{Profile: profile, Lifecycle: agentpersona.StatePublished}}, Installations: catalogInstalls{active, stopped}, Targets: catalogTargets{conversations: []productui.PersonaAdminTarget{{ID: "general", Label: "General"}, {ID: "direct", Label: "Direct"}}}, Authorizer: &catalogAuth{}}
	snapshot, err := service.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if err != nil || len(snapshot.Personas) != 1 || len(snapshot.Personas[0].Installations) != 2 {
		t.Fatalf("mixed-version placement projection=%+v err=%v", snapshot, err)
	}
	if snapshot.Personas[0].Installations[1].InstallationID != "stopped" || snapshot.Personas[0].Installations[1].Version == snapshot.Personas[0].Installations[0].Version {
		t.Fatalf("stopped older/newer placement disappeared: %+v", snapshot.Personas[0].Installations)
	}
}

type agentUXGeneralRuntimeReady bool

func (r agentUXGeneralRuntimeReady) PersonaRuntimeReady(context.Context, values.TenantId, string, int64) bool {
	return bool(r)
}

func TestAgentUXGeneral_StoppedProjection(t *testing.T) {
	in := PersonaCatalogInstallation{ID: "stop", PersonaID: "assistant", PersonaVersion: 2, ConversationID: "general", State: string(agentpersonastore.InstallationSuspended), SuspensionReason: "AGENT_PRINCIPAL_MISSING_AFTER_RESTORE"}
	stopped := ProjectPersonaCatalogInstallationRuntime(context.Background(), "tenant-a", in, agentUXGeneralRuntimeReady(false))
	if stopped.SuspensionMessage != "this version has no runtime identity" || stopped.RestartAvailable {
		t.Fatalf("uncured placement=%+v", stopped)
	}
	repaired := ProjectPersonaCatalogInstallationRuntime(context.Background(), "tenant-a", in, agentUXGeneralRuntimeReady(true))
	if !repaired.RestartAvailable || repaired.Active {
		t.Fatalf("repaired placement=%+v", repaired)
	}
	if got := ProjectPersonaCatalogInstallationRuntime(context.Background(), "tenant-a", in, nil); got.RestartAvailable {
		t.Fatal("missing runtime offered restart")
	}
	active := in
	active.ID = "active"
	active.Active = true
	active.State = string(agentpersonastore.InstallationActive)
	active.PersonaVersion = 1
	rows := personaCatalogVisibleInstallations([]PersonaCatalogInstallation{in, active, in})
	if len(rows) != 1 || rows[0].ID != "active" {
		t.Fatalf("duplicate projection=%+v", rows)
	}
	older := in
	older.PersonaVersion = 1
	rows = personaCatalogVisibleInstallations([]PersonaCatalogInstallation{older, in})
	if len(rows) != 1 || rows[0].PersonaVersion != 2 {
		t.Fatalf("stopped duplicates=%+v", rows)
	}
}
