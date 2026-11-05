// TestTodo_WF_TEST_010_Integration is the INTEGRATION leg of WF-TEST-010
// (issue #2687): a sandbox reset removes the sandbox tenant's rows through
// the tenant deletion path and proves it. Where SANDBOX-001's Recovery leg
// proves one run-then-reset cycle returns the global mutable count to its
// baseline, this test proves the removal itself: the reset's delete set
// partitions the schema catalog exactly (every tenant-scoped table is
// either deleted-through or preserved, nothing hand-listed and nothing
// skipped), every deleted table returns to its own per-table baseline, the
// run's own rows are gone rather than seeded over, consecutive resets stay
// flat (an append-over reseed would grow reset-over-reset), and the neighbor
// tenant never moves.
//
// The PRIMARY leg (run a draft durably with fenced connectors and
// emulators) and the SECURITY leg (no sandbox effect crosses the fence)
// need CONN-RT-006's emulator-through-the-runtime wiring first; see the
// WF-TEST-010 issue. This file covers only the reset-purge INTEGRATION leg,
// which the existing Reset/DeleteTenantRows substrate already supports.
package sandbox

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// test010TableCounts returns this tenant's row count for every tenant-scoped
// table the schema catalog names, preserved and deleted alike.
func test010TableCounts(t *testing.T, pool *pgxadapter.Pool, sb *Sandbox) map[string]int64 {
	t.Helper()
	ctx := context.Background()
	tables, err := tenantScopedTables(ctx, pool)
	if err != nil {
		t.Fatalf("tenantScopedTables: %v", err)
	}
	out := make(map[string]int64, len(tables))
	for _, table := range tables {
		var n int64
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+quoteIdent(table)+` WHERE tenant_id = $1`, sb.TenantID()).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		out[table] = n
	}
	return out
}

// test010IntentDecisions counts every decision row the durable run recorded
// for one intent, whatever kind each row carries.
func test010IntentDecisions(t *testing.T, pool *pgxadapter.Pool, sb *Sandbox, intentID string) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM intent_decision WHERE tenant_id = $1 AND intent_id::text = $2`,
		sb.TenantID(), intentID).Scan(&n); err != nil {
		t.Fatalf("count intent_decision for %s: %v", intentID, err)
	}
	return n
}

func test010EqualStringSets(a []string, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for _, s := range a {
		if !b[s] {
			return false
		}
	}
	return true
}

func test010EqualStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTodo_WF_TEST_010_Integration(t *testing.T) {
	pool := newSandboxPool(t)
	neighbor := newTestSandbox(t, pool, "test010-neighbor")
	sb := newTestSandbox(t, pool, "test010")
	ctx := context.Background()

	neighborBefore := totalTenantRows(t, pool, neighbor)
	if neighborBefore == 0 {
		t.Fatal("neighbor sandbox seeded no rows; the disjointness leg would prove nothing")
	}

	// Baseline: a reset names this tenant, partitions the catalog exactly,
	// and establishes the per-table counts every later reset must return to.
	baseline, err := sb.Reset(ctx)
	if err != nil {
		t.Fatalf("baseline Reset: %v", err)
	}
	if baseline.TenantID != sb.TenantID() {
		t.Fatal("baseline Reset reported a tenant id that is not this sandbox's own")
	}
	catalog, err := tenantScopedTables(ctx, pool)
	if err != nil {
		t.Fatalf("tenantScopedTables: %v", err)
	}
	partition := make(map[string]bool, len(catalog))
	for table := range baseline.Deleted {
		partition[table] = true
	}
	for _, table := range baseline.Preserved {
		if partition[table] {
			t.Fatalf("table %s is both deleted and preserved; the reset path must partition the catalog", table)
		}
		partition[table] = true
	}
	catalogSet := make(map[string]bool, len(catalog))
	for _, table := range catalog {
		catalogSet[table] = true
	}
	if !test010EqualStringSets(catalog, partition) {
		t.Fatalf("reset covered %d tables, catalog names %d; the tenant deletion path must consider every tenant-scoped table",
			len(partition), len(catalog))
	}
	if len(baseline.Deleted) == 0 || len(baseline.Preserved) == 0 {
		t.Fatalf("reset deleted %d tables and preserved %d; a real schema has both",
			len(baseline.Deleted), len(baseline.Preserved))
	}
	counts0 := test010TableCounts(t, pool, sb)

	// A durable run grows this tenant's mutable state and records its own
	// decision rows.
	proof, err := sb.RunPromotionProof(ctx, promotionInput())
	if err != nil {
		t.Fatalf("RunPromotionProof: %v", err)
	}
	counts1 := test010TableCounts(t, pool, sb)
	var grown []string
	for table, n := range counts1 {
		if n > counts0[table] {
			grown = append(grown, table)
		}
	}
	if len(grown) == 0 {
		t.Fatal("the durable run grew no table; the removal legs below would prove nothing")
	}
	if got := test010IntentDecisions(t, pool, sb, proof.IntentID); got == 0 {
		t.Fatalf("the durable run recorded no intent_decision row for intent %s; grown tables: %v", proof.IntentID, grown)
	}

	// The reset after the run removes: same digest, same partition, every
	// deleted table back to its own baseline count, every preserved table
	// untouched at its post-run count, every deleted count exactly the
	// pre-reset count (the delete removed all of it), and this run's own
	// decision rows gone.
	afterRun, err := sb.Reset(ctx)
	if err != nil {
		t.Fatalf("post-run Reset: %v", err)
	}
	if afterRun.SeedDigest != baseline.SeedDigest {
		t.Fatalf("post-run seed digest %q, want the baseline %q", afterRun.SeedDigest, baseline.SeedDigest)
	}
	if !test010EqualStringSlices(afterRun.Preserved, baseline.Preserved) {
		t.Fatal("the preserved set moved between resets; the deletion path must be deterministic")
	}
	if len(afterRun.Deleted) != len(baseline.Deleted) {
		t.Fatalf("post-run reset deleted through %d tables, baseline %d; the partition must be stable",
			len(afterRun.Deleted), len(baseline.Deleted))
	}
	for table := range baseline.Deleted {
		if _, ok := afterRun.Deleted[table]; !ok {
			t.Fatalf("table %s left the deleted set between resets; the partition must be stable", table)
		}
	}
	counts2 := test010TableCounts(t, pool, sb)
	for table := range baseline.Deleted {
		if counts2[table] != counts0[table] {
			t.Fatalf("table %s holds %d rows after reset, want its baseline %d (pre-reset %d): the reset appended over data instead of removing it",
				table, counts2[table], counts0[table], counts1[table])
		}
		if afterRun.Deleted[table] != counts1[table] {
			t.Fatalf("table %s deleted %d rows but held %d before the reset; the tenant deletion path must remove exactly what is there",
				table, afterRun.Deleted[table], counts1[table])
		}
	}
	for _, table := range baseline.Preserved {
		if counts2[table] != counts1[table] {
			t.Fatalf("preserved table %s holds %d rows after reset, want its untouched post-run %d",
				table, counts2[table], counts1[table])
		}
	}
	if got := test010IntentDecisions(t, pool, sb, proof.IntentID); got != 0 {
		t.Fatalf("intent %s still owns %d intent_decision rows after reset; the run's data was not removed", proof.IntentID, got)
	}
	if got := totalTenantRows(t, pool, neighbor); got != neighborBefore {
		t.Fatalf("neighbor moved from %d to %d rows during this sandbox's run and reset", neighborBefore, got)
	}

	// Consecutive resets with no run between them stay flat: each one
	// deletes exactly the reseed and restores exactly the baseline. An
	// append-over reseed would grow every deleted table reset-over-reset.
	for i := 0; i < 2; i++ {
		again, err := sb.Reset(ctx)
		if err != nil {
			t.Fatalf("flat reset %d: %v", i+2, err)
		}
		if again.SeedDigest != baseline.SeedDigest {
			t.Fatalf("flat reset %d digest %q, want the baseline %q", i+2, again.SeedDigest, baseline.SeedDigest)
		}
		if !test010EqualStringSlices(again.Preserved, baseline.Preserved) {
			t.Fatalf("flat reset %d moved the preserved set", i+2)
		}
		counts := test010TableCounts(t, pool, sb)
		for table := range baseline.Deleted {
			if counts[table] != counts0[table] {
				t.Fatalf("flat reset %d: table %s holds %d rows, want its baseline %d",
					i+2, table, counts[table], counts0[table])
			}
			if again.Deleted[table] != counts0[table] {
				t.Fatalf("flat reset %d: table %s deleted %d rows but the reseed holds %d; rows accumulated across resets",
					i+2, table, again.Deleted[table], counts0[table])
			}
		}
		for _, table := range baseline.Preserved {
			if counts[table] != counts1[table] {
				t.Fatalf("flat reset %d: preserved table %s holds %d rows, want its untouched %d",
					i+2, table, counts[table], counts1[table])
			}
		}
	}

	// A second full cycle with a new durable run removes that run's rows
	// too, while the first run's rows stay gone.
	proof2, err := sb.RunPromotionProof(ctx, promotionInput())
	if err != nil {
		t.Fatalf("second RunPromotionProof: %v", err)
	}
	if proof2.IntentID == proof.IntentID {
		t.Fatalf("the second run reused intent %s; the two cycles would not be independent", proof.IntentID)
	}
	if got := test010IntentDecisions(t, pool, sb, proof2.IntentID); got == 0 {
		t.Fatalf("the second run recorded no intent_decision row for intent %s", proof2.IntentID)
	}
	counts3 := test010TableCounts(t, pool, sb)
	stillGrown := false
	for table, n := range counts3 {
		if n > counts0[table] {
			stillGrown = true
			break
		}
	}
	if !stillGrown {
		t.Fatal("the second run grew no table; its removal leg would prove nothing")
	}
	afterSecond, err := sb.Reset(ctx)
	if err != nil {
		t.Fatalf("post-second-run Reset: %v", err)
	}
	if afterSecond.SeedDigest != baseline.SeedDigest {
		t.Fatalf("post-second-run digest %q, want the baseline %q", afterSecond.SeedDigest, baseline.SeedDigest)
	}
	counts4 := test010TableCounts(t, pool, sb)
	for table := range baseline.Deleted {
		if counts4[table] != counts0[table] {
			t.Fatalf("table %s holds %d rows after the second cycle, want its baseline %d",
				table, counts4[table], counts0[table])
		}
	}
	for _, table := range baseline.Preserved {
		if counts4[table] != counts3[table] {
			t.Fatalf("preserved table %s holds %d rows after the second cycle, want its untouched post-run %d",
				table, counts4[table], counts3[table])
		}
	}
	if got := test010IntentDecisions(t, pool, sb, proof2.IntentID); got != 0 {
		t.Fatalf("second intent %s still owns %d intent_decision rows after reset", proof2.IntentID, got)
	}
	if got := test010IntentDecisions(t, pool, sb, proof.IntentID); got != 0 {
		t.Fatalf("first intent %s regained %d intent_decision rows; a reset must never restore removed data", proof.IntentID, got)
	}

	if got := totalTenantRows(t, pool, neighbor); got != neighborBefore {
		t.Fatalf("neighbor moved from %d to %d rows across the full test", neighborBefore, got)
	}
}
