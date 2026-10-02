package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// The count on an Agent setup placement row is what an ordinary member of the
// room reads, not what the viewing administrator reads (AGENTUX-034). The
// administrator may open a restricted document the members may not; the row
// must not count it, and must list only titles the member reads.
func TestTodo_AGENTUX_034_MemberCount(t *testing.T) {
	service, ctx := agentUXSetup3CatalogService(t)
	service.Targets = agentUXSetup3CatalogTargets{
		users: []productui.PersonaAdminTarget{{ID: "user-a", Label: "Walt Brennan"}, {ID: "user-b", Label: "Robin Patel"}},
		conversations: []productui.PersonaAdminTarget{{ID: "general", Label: "general", Kind: "CHANNEL", Members: []productui.PersonaAdminTarget{
			{ID: "user-a", Role: "hcm_admin"}, {ID: "user-b", Role: "employee"},
		}}},
		names: map[string]string{"ir-001-walt-brennan": "Walt Brennan", "ir-003-loretta-haynes": "Loretta Haynes", "ir-008-curtis-bell": "Curtis Bell"},
	}
	service.Documents = agentUXR5SetupDocuments{byActor: map[string][]PersonaAdminPlacementDocument{
		"user-a": {{DocumentID: "visible", Title: "Visible policy"}, {DocumentID: "restricted", Title: "Restricted policy"}},
		"user-b": {{DocumentID: "visible", Title: "Visible policy"}},
	}}
	snapshot, err := service.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, persona := range snapshot.Personas {
		for _, installation := range persona.Installations {
			if installation.ConversationID != "general" {
				continue
			}
			found = true
			if installation.OfficialDocumentCount == nil || *installation.OfficialDocumentCount != 1 {
				t.Fatalf("the row counts %v documents; the member reads one (the administrator's two include a restricted one)", installation.OfficialDocumentCount)
			}
			if len(installation.OfficialDocumentTitles) != 1 || installation.OfficialDocumentTitles[0] != "Visible policy" {
				t.Fatalf("titles = %v, want only what the member reads", installation.OfficialDocumentTitles)
			}
		}
	}
	if !found {
		t.Fatalf("no placement row for general in %+v", snapshot.Personas)
	}
}

func TestTodo_AGENTUX_034_TypicalMember(t *testing.T) {
	room := productui.PersonaAdminTarget{Members: []productui.PersonaAdminTarget{{ID: "admin", Role: "hcm_admin"}, {ID: "other-admin", Role: "hcm_admin"}, {ID: "member", Role: "employee"}}}
	if got := agentux034TypicalMember(room, "admin"); got != "member" {
		t.Fatalf("typical member = %q, want the ordinary member", got)
	}
	room.Members = room.Members[:2]
	if got := agentux034TypicalMember(room, "admin"); got != "other-admin" {
		t.Fatalf("with only administrators the other administrator is read, got %q", got)
	}
	room.Members = room.Members[:1]
	if got := agentux034TypicalMember(room, "admin"); got != "admin" {
		t.Fatalf("alone in the room the viewer is read, got %q", got)
	}
}
