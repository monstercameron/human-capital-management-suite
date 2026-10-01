package agentstore

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func portableStoreFixture(t *testing.T) (*Store, Config, uuid.UUID, uuid.UUID, *pgtest.DB) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	ctx := context.Background()
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	a, b := uuid.New(), uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES ($1),($2)`, a, b)
	login, password := roleName("portable_login"), uuid.NewString()
	if err := createAgentLogin(ctx, db.SQL, login, password); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.SQL.ExecContext(context.Background(), "DROP ROLE "+login); err != nil {
			t.Error(err)
		}
	})
	config := Config{DSN: testDSN(t, db.URL, db.Schema, login, password, "postgres"), CoreDSN: "postgres://core:pw@127.0.0.1:5433/core", MaxConns: 2}
	store, err := New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store, config, a, b, db
}

func TestTodo_AGENT_045_Integration(t *testing.T) {
	store, _, a, b, db := portableStoreFixture(t)
	ctx := context.Background()
	m := portableDraftTestManifest()
	if err := store.SavePortableDefinitionDraft(ctx, a, m.OwnerID, m, "portable instructions"); err != nil {
		t.Fatal(err)
	}
	draft, err := store.GetPortableDefinitionDraft(ctx, a, m.ID)
	if err != nil || draft.State != "DRAFT" || draft.Manifest.ID != m.ID || draft.Instructions != "portable instructions" || draft.ActorID != m.OwnerID || draft.CreatedAt.IsZero() || len(draft.Manifest.ContextGrants) != 0 {
		t.Fatalf("stored draft %+v %v", draft, err)
	}
	if _, err = store.GetPortableDefinitionDraft(ctx, b, m.ID); !errors.Is(err, dbport.ErrNoRows) {
		t.Fatalf("cross tenant draft visible %v", err)
	}
	var installations, runs int
	if err = db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM persona_installations`).Scan(&installations); err != nil {
		t.Fatal(err)
	}
	if err = db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM agent_run_request`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if installations != 0 || runs != 0 {
		t.Fatal("import activated agent")
	}
	err = store.RunTenantTx(ctx, a, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agent_portable_draft SET state='DRAFT' WHERE tenant_id=$1 AND definition_id=$2`, a, m.ID)
		return err
	})
	if err == nil {
		t.Fatal("draft evidence mutable")
	}
}

func TestTodo_AGENT_045_Recovery(t *testing.T) {
	store, config, a, _, _ := portableStoreFixture(t)
	m := portableDraftTestManifest()
	ctx := context.Background()
	if err := store.SavePortableDefinitionDraft(ctx, a, m.OwnerID, m, "portable instructions"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	reopened, err := New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	draft, err := reopened.GetPortableDefinitionDraft(ctx, a, m.ID)
	if err != nil || draft.State != "DRAFT" || draft.Instructions != "portable instructions" {
		t.Fatalf("recovered %+v %v", draft, err)
	}
}

func TestTodo_AGENT_045_Fault(t *testing.T) {
	store, _, a, _, db := portableStoreFixture(t)
	ctx := context.Background()
	m := portableDraftTestManifest()
	if err := store.SavePortableDefinitionDraft(ctx, a, m.OwnerID, m, "portable instructions"); err != nil {
		t.Fatal(err)
	}
	m.InstructionsDigest = instructionDigest("new body failed draft")
	if err := store.SavePortableDefinitionDraft(ctx, a, m.OwnerID, m, "new body failed draft"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate draft error %v", err)
	}
	var leaked int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM agent_instruction_content WHERE tenant_id=$1 AND digest=$2`, a, m.InstructionsDigest).Scan(&leaked); err != nil {
		t.Fatal(err)
	}
	if leaked != 0 {
		t.Fatal("failed import leaked instruction content")
	}
	draft, err := store.GetPortableDefinitionDraft(ctx, a, m.ID)
	if err != nil || draft.Instructions != "portable instructions" {
		t.Fatal("failed draft overwrote retained draft")
	}
}

func TestTodo_AGENT_045_Race(t *testing.T) {
	store, _, a, _, _ := portableStoreFixture(t)
	ctx := context.Background()
	m := portableDraftTestManifest()
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- store.SavePortableDefinitionDraft(ctx, a, m.OwnerID, m, "portable instructions")
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}
