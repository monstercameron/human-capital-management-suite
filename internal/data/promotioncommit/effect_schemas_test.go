package promotioncommit_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionterminal"
)

// TestRegisterEffectSchemasPublishesExactlyTheRenderedSchemas proves the
// composition act WF-RUN-034 added: registering the payload schemas the
// terminal resolver renders makes a commit's outbox legs admissible, the
// registration is idempotent, and it refuses to run without a transaction and
// a tenant. Registering nothing leaves the outbox's own foreign key to refuse
// an unpublished schema, which TestTodo_PROMO_005_Mutation proves it still
// does.
func TestRegisterEffectSchemasPublishesExactlyTheRenderedSchemas(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	refs := promotionterminal.EffectSchemaRefs()
	if len(refs) != 2 {
		t.Fatalf("rendered effect schemas = %v, want the payroll and IAM syncs", refs)
	}
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := promotioncommit.RegisterEffectSchemas(ctx, tx, f.tenant, refs...); err != nil {
		t.Fatalf("RegisterEffectSchemas: %v", err)
	}
	if err := promotioncommit.RegisterEffectSchemas(ctx, tx, f.tenant, refs...); err != nil {
		t.Fatalf("replayed RegisterEffectSchemas: %v", err)
	}
	var registered int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM payload_schema WHERE tenant_id = $1 AND schema_ref = ANY($2)`, f.tenant, refs).Scan(&registered); err != nil {
		t.Fatal(err)
	}
	if registered != len(refs) {
		t.Fatalf("registered schemas = %d, want %d exactly once each", registered, len(refs))
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// With the schemas published, the promotion's own effect legs commit.
	receipt, err := commitCommand(t, f, promotioncommit.Writer{}, f.command(t))
	if err != nil || len(receipt.OutboxIDs) != 2 {
		t.Fatalf("commit with registered effect schemas = %+v, %v", receipt, err)
	}

	if err := promotioncommit.RegisterEffectSchemas(ctx, nil, f.tenant, refs...); err == nil {
		t.Fatal("registration without a transaction was accepted")
	}
	tx2, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx2.Rollback(ctx) }()
	if err := promotioncommit.RegisterEffectSchemas(ctx, tx2, uuid.Nil, refs...); err == nil {
		t.Fatal("registration without a tenant was accepted")
	}
}
