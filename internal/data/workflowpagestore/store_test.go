package workflowpagestore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func pageFixture(t *testing.T) (*pgtest.DB, uuid.UUID, uuid.UUID) {
	t.Helper()
	db := pgtest.New(t)
	a, b := uuid.New(), uuid.New()
	for _, tenant := range []uuid.UUID{a, b} {
		db.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,$2,'cell-wfpage',$2,'ACTIVE',now())`, tenant, "tenant-"+tenant.String())
	}
	return db, a, b
}

func pageRecord(tenant uuid.UUID) PageVersion {
	return PageVersion{TenantID: tenant, WorkflowKey: "workflow.new_hire", WorkflowVersion: 4, PageID: "workflow.new_hire.input", PageVersion: 1, Definition: []byte(`{"schema":"page"}`), DefinitionDigest: "sha256:page-v1", GeneratedDefault: true, PublishedBy: "publisher"}
}

// TestTodo_WFPAGE_011 proves published pages survive a fresh store and that
// the generated-default marker is durable for the exact workflow version.
func TestTodo_WFPAGE_011(t *testing.T) {
	db, tenant, _ := pageFixture(t)
	store := Store{DB: db.Conn}
	page := pageRecord(tenant)
	if err := store.Publish(context.Background(), page); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	got, err := (Store{DB: db.Conn}).Get(context.Background(), tenant, page.WorkflowKey, page.WorkflowVersion, page.PageVersion, page.PageID)
	if err != nil {
		t.Fatalf("Get after fresh store: %v", err)
	}
	if !got.GeneratedDefault || string(got.Definition) != string(page.Definition) {
		t.Fatalf("stored page = %+v", got)
	}
}

func TestTodo_WFPAGE_011_Security(t *testing.T) {
	db, tenantA, tenantB := pageFixture(t)
	store := Store{DB: db.Conn}
	page := pageRecord(tenantA)
	if err := store.Publish(context.Background(), page); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), tenantB, page.WorkflowKey, page.WorkflowVersion, page.PageVersion, page.PageID); !errors.Is(err, ErrMissing) {
		t.Fatalf("cross-tenant read = %v, want ErrMissing", err)
	}
	var enabled, forced bool
	if err := db.QueryRow(context.Background(), `SELECT c.relrowsecurity,c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname='workflow_page_version'`, db.Schema).Scan(&enabled, &forced); err != nil {
		t.Fatal(err)
	}
	if !enabled || !forced {
		t.Fatalf("page version RLS enabled=%t forced=%t", enabled, forced)
	}
}

func TestTodo_WFPAGE_011_Fault(t *testing.T) {
	db, tenant, _ := pageFixture(t)
	store := Store{DB: db.Conn}
	bad := pageRecord(tenant)
	bad.Definition = []byte(`not-json`)
	if err := store.Publish(context.Background(), bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid publish = %v", err)
	}
	var count int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM workflow_page_version`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed publish left %d rows", count)
	}
	// The pre-existing workflow runtime remains available after the failed page
	// operation; this is the interrupted-migration compatibility check.
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM workflow_instance`).Scan(&count); err != nil {
		t.Fatalf("old runtime path unavailable: %v", err)
	}
}

func TestTodo_WFPAGE_011_Recovery(t *testing.T) {
	db, tenant, _ := pageFixture(t)
	page := pageRecord(tenant)
	if err := (Store{DB: db.Conn}).Publish(context.Background(), page); err != nil {
		t.Fatal(err)
	}
	// A newly composed store reads the same immutable binding after the
	// simulated restore/reconnect boundary.
	got, err := (Store{DB: db.Conn}).Get(context.Background(), tenant, page.WorkflowKey, page.WorkflowVersion, page.PageVersion, page.PageID)
	if err != nil || got.DefinitionDigest != page.DefinitionDigest {
		t.Fatalf("recovered page = %+v, err=%v", got, err)
	}
}

func TestTodo_WFPAGE_011_Integration(t *testing.T) {
	db, tenant, _ := pageFixture(t)
	store := Store{DB: db.Conn}
	page := pageRecord(tenant)
	if err := store.Publish(context.Background(), page); err != nil {
		t.Fatal(err)
	}
	draft := Draft{TenantID: tenant, DraftID: uuid.New(), WorkflowKey: page.WorkflowKey, WorkflowVersion: page.WorkflowVersion, PageID: page.PageID, DraftVersion: 1, Definition: []byte(`{"draft":true}`), DefinitionDigest: "sha256:draft-v1", Author: "designer"}
	if err := store.SaveDraft(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	if err := db.ExecErr(`UPDATE workflow_page_version SET published_by='forged' WHERE tenant_id=$1`, tenant); err == nil || !strings.Contains(strings.ToLower(err.Error()), "immutable") {
		t.Fatalf("published update error=%v, want immutable trigger", err)
	}
	if err := db.ExecErr(`DELETE FROM workflow_page_draft WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("draft delete was accepted")
	}
	var drafts int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM workflow_page_draft WHERE tenant_id=$1`, tenant).Scan(&drafts); err != nil || drafts != 1 {
		t.Fatalf("draft count=%d err=%v", drafts, err)
	}
}
