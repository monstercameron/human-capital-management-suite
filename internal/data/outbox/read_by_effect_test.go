package outbox_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
)

// TestReadByEffectIdentityFindsTheEnqueuedRowAndNothingElse proves the
// inspector's outbox read resolves an effect identity to the row it enqueued,
// reports an effect that enqueued nothing as not found without an error, and
// never answers another tenant's question with this tenant's row.
func TestReadByEffectIdentityFindsTheEnqueuedRowAndNothingElse(t *testing.T) {
	f := newEvent001Fixture(t)
	ctx := context.Background()
	enqueued := f.enqueue(t, "effect:read-by-identity", outbox.CriticalityP2)

	got, found, err := outbox.ReadByEffectIdentity(ctx, f.db.Conn, f.tenant, "effect:read-by-identity")
	if err != nil || !found {
		t.Fatalf("ReadByEffectIdentity = found %v, err %v; want the enqueued row", found, err)
	}
	if got.OutboxID != enqueued.OutboxID || got.Status != enqueued.Status || got.EffectIdentity != enqueued.EffectIdentity {
		t.Errorf("row = %s %s %s, want %s %s %s", got.OutboxID, got.Status, got.EffectIdentity,
			enqueued.OutboxID, enqueued.Status, enqueued.EffectIdentity)
	}

	if _, found, err := outbox.ReadByEffectIdentity(ctx, f.db.Conn, f.tenant, "effect:never-enqueued"); err != nil || found {
		t.Fatalf("an effect that enqueued nothing = found %v, err %v; want not found and no error", found, err)
	}
	if _, found, err := outbox.ReadByEffectIdentity(ctx, f.db.Conn, uuid.New(), "effect:read-by-identity"); err != nil || found {
		t.Fatalf("another tenant's read = found %v, err %v; want not found and no error", found, err)
	}
}
