package application

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_3757_EmptyCatalogHasNoDocumentFailure: a tenant with no agents has no
// reference documents to read, so an unwired document reader must not show the
// "could not be loaded" banner. A catalog with an agent still reports it.
func TestTodo_3757_EmptyCatalogHasNoDocumentFailure(t *testing.T) {
	ctx, principal := catalogContext(t)
	targets := catalogTargets{users: []productui.PersonaAdminTarget{{ID: principal.Subject(), Label: "Admin User"}}, conversations: []productui.PersonaAdminTarget{{ID: "general", Label: "General", Kind: "CHANNEL"}}}
	empty := PersonaAdminCatalogService{Versions: catalogVersions{}, Installations: catalogInstalls{}, Targets: targets, Skills: catalogSkills{}, Grants: &catalogGrants{allowed: true}, Authorizer: &catalogAuth{}, Starters: &catalogStarterSource{catalog: productui.PersonaAdminStarterCatalog{Available: true}}}

	snapshot, err := empty.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Personas) != 0 {
		t.Fatalf("empty catalog listed agents: %+v", snapshot.Personas)
	}
	if snapshot.DocumentsState.Unavailable {
		t.Fatalf("an empty catalog with no document reader reports a document load failure: %+v", snapshot.DocumentsState)
	}

	withAgent := empty
	withAgent.Versions = catalogVersions{agentUXSetupVersion(t, agentpersona.StateInReview)}
	snapshot, err = withAgent.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Personas) != 1 || !snapshot.DocumentsState.Unavailable {
		t.Fatalf("an agent with no document reader must still report documents unavailable: personas=%d state=%+v", len(snapshot.Personas), snapshot.DocumentsState)
	}
}
