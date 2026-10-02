package agentpersonastore

import (
	"context"
	"testing"
)

func TestIntegrate1RawPersonaCreationKeepsIcon(t *testing.T) {
	_, store := iconFixture(t)
	ctx := context.Background()
	draft := version(store.tenant, "raw-draft", 1)
	owner, steward := draftOwners(draft)
	if err := store.CreateDraft(ctx, draft, owner, steward, "user:business-owner", draft.CreatedAt); err != nil {
		t.Fatal(err)
	}
	original, err := store.GetIcon(ctx, draft.PersonaID)
	if err != nil || original.Revision != 1 || !original.Value.Valid() {
		t.Fatal("raw creation lost icon", original, err)
	}
	if err := store.PutVersion(ctx, version(store.tenant, draft.PersonaID, 2)); err != nil {
		t.Fatal(err)
	}
	if current, err := store.GetIcon(ctx, draft.PersonaID); err != nil || current != original {
		t.Fatal("version overwrote identity", current, err)
	}
	first := version(store.tenant, "raw-version", 1)
	if err := store.PutVersion(ctx, first); err != nil {
		t.Fatal(err)
	}
	if icon, err := store.GetIcon(ctx, first.PersonaID); err != nil || icon.Revision != 1 || !icon.Value.Valid() {
		t.Fatal("first version lost icon", icon, err)
	}
}
