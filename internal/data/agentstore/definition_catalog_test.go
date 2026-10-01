package agentstore

import (
	"context"
	"testing"
)

func TestTodo_AGENT_045_Integration_DefinitionCatalog(t *testing.T) {
	store, _, a, b, _ := portableStoreFixture(t)
	ctx := context.Background()
	for _, tenant := range []struct{ foreign bool }{{false}, {true}} {
		id := a
		name := "local-policy"
		if tenant.foreign {
			id, name = b, "foreign-policy"
		}
		m := testManifest(name, 1, "answer approved policy questions")
		seedManifestInstructions(t, store, id, m)
		if _, err := store.SaveManifest(ctx, id, m, 0); err != nil {
			t.Fatal(err)
		}
	}
	latest := testManifest("local-policy", 2, "answer updated policy questions")
	seedManifestInstructions(t, store, a, latest)
	if _, err := store.SaveManifest(ctx, a, latest, 1); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListCurrentManifests(ctx, a)
	if err != nil || len(rows) != 1 || rows[0].ID != latest.ID || rows[0].Version != 2 || rows[0].InstructionsDigest != latest.InstructionsDigest {
		t.Fatalf("catalog lost exact current version or tenant fence: %+v %v", rows, err)
	}
	other, err := store.ListCurrentManifests(ctx, b)
	if err != nil || len(other) != 1 || other[0].ID != "foreign-policy" {
		t.Fatalf("foreign catalog %+v %v", other, err)
	}
}
