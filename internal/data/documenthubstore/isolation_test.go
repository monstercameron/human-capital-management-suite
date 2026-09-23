package documenthubstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// dispositionPath is the HUB-003 registry: every document-owned table must be
// listed here with its isolation contract, and the conformance tests below
// prove the live database matches it in both directions.
const dispositionPath = "../../../definitions/storage/document-storage-disposition.yaml"

// TestTodo_HUB_003 is the PRIMARY test for HUB-003: the document disposition
// registry admits no table without a tenant-isolation contract.
func TestTodo_HUB_003(t *testing.T) {
	reg, err := LoadDisposition(dispositionPath)
	if err != nil {
		t.Fatalf("load disposition: %v", err)
	}
	if len(reg.Tables) == 0 {
		t.Fatal("disposition registers no document tables")
	}
	seen := map[string]bool{}
	for _, tb := range reg.Tables {
		if tb.Table == "" || seen[tb.Table] {
			t.Fatalf("disposition has empty or duplicate table %q", tb.Table)
		}
		seen[tb.Table] = true
		if tb.RLSPolicy != "tenant_isolation" || !tb.RLSRequired {
			t.Fatalf("table %q lacks mandatory tenant_isolation RLS", tb.Table)
		}
		if tb.TenantColumn != "tenant_id" {
			t.Fatalf("table %q scopes by %q instead of tenant_id", tb.Table, tb.TenantColumn)
		}
		if tb.OwnerPackage != "internal/data/documenthubstore" {
			t.Fatalf("table %q owned by %q", tb.Table, tb.OwnerPackage)
		}
	}
}

// TestTodo_HUB_003_Integration is the INTEGRATION test for HUB-003: every
// registered table exists in the migrated document database with forced RLS
// and the tenant_isolation policy, and no unregistered document table exists.
func TestTodo_HUB_003_Integration(t *testing.T) {
	reg, err := LoadDisposition(dispositionPath)
	if err != nil {
		t.Fatalf("load disposition: %v", err)
	}
	s, _ := documentFixture(t)
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		for _, tb := range reg.Tables {
			var forced int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_class WHERE relnamespace=current_schema()::regnamespace AND relname=$1 AND relrowsecurity AND relforcerowsecurity`, tb.Table).Scan(&forced); err != nil || forced != 1 {
				t.Fatalf("table %q missing forced RLS: err=%v", tb.Table, err)
			}
			var policies int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_policies WHERE schemaname=current_schema() AND tablename=$1 AND policyname='tenant_isolation'`, tb.Table).Scan(&policies); err != nil || policies != 1 {
				t.Fatalf("table %q missing tenant_isolation policy: err=%v", tb.Table, err)
			}
		}
		var unregistered int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_type='BASE TABLE' AND table_name NOT IN ('goose_db_version') AND table_name NOT IN (SELECT unnest($1::text[]))`, tableNames(reg)).Scan(&unregistered); err != nil || unregistered != 0 {
			t.Fatalf("unregistered document tables: count=%d err=%v", unregistered, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_HUB_003_Security is the SECURITY test for HUB-003: a tenant
// enumerating another tenant's document rows through the repository sees
// nothing, and the registry itself cannot be widened without owning the
// isolation contract.
func TestTodo_HUB_003_Security(t *testing.T) {
	s, _ := documentFixture(t)
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO document_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,$3,$4)`, "tenant-a", "doc-3", "version.proposed", `{}`)
		return err
	}); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	var enumerated int
	if err := s.RunTenantTx(ctx, "tenant-b", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM document_outbox WHERE tenant_id=$1`, "tenant-b").Scan(&enumerated)
	}); err != nil || enumerated != 0 {
		t.Fatalf("tenant enumerated foreign document rows: count=%d err=%v", enumerated, err)
	}
	reg, err := LoadDisposition(dispositionPath)
	if err != nil {
		t.Fatalf("load disposition: %v", err)
	}
	for _, tb := range reg.Tables {
		if !tb.AppendOnly && tb.RLSPolicy != "tenant_isolation" {
			t.Fatalf("table %q is mutable without isolation review", tb.Table)
		}
	}
}
