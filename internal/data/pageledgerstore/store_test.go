package pageledgerstore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func countRolloutAnchors(t *testing.T, db *pgtest.DB, tenant string) int {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, pgstore.TenantID(tenant).String()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM page_rollout`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestTodo_REV_067_02_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantA, tenantB := "page-ledger-a-"+uuid.NewString(), "page-ledger-b-"+uuid.NewString()
	for _, tenant := range []string{tenantA, tenantB} {
		db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'page-test','Page ledger','ACTIVE',$3)`, pgstore.TenantID(tenant), tenant, time.Now().UTC())
	}
	if _, err := db.Conn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatalf("SET ROLE hcmnext_app: %v", err)
	}
	store := New(db.Conn, pgstore.TenantID)
	ctx := context.Background()
	log, err := productui.NewDurablePageRevisionLog(ctx, tenantA, store)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := productui.PageDefinitionSnapshot{Page: "studio", Route: "/studio", Title: "Studio", SearchTerms: []string{"builder"}}
	revision, err := log.RecordContext(ctx, "studio", snapshot, 1)
	if err != nil {
		t.Fatalf("RecordContext: %v", err)
	}
	rollout := productui.PageRollout{Page: "studio", Version: 1, Digest: revision.Digest, Scopes: []productui.RolloutScope{{Scope: "org-a", EffectiveFrom: 10}}}
	if err := log.RecordRolloutContext(ctx, rollout); err != nil {
		t.Fatalf("RecordRolloutContext: %v", err)
	}
	recovered, err := productui.NewDurablePageRevisionLog(ctx, tenantA, store)
	if err != nil {
		t.Fatalf("recovery: %v", err)
	}
	got, ok := recovered.Revision("studio", 1)
	if !ok || got.Digest != revision.Digest {
		t.Fatalf("revision = %#v, %v", got, ok)
	}
	rollouts := recovered.Rollouts()
	if len(rollouts) != 1 || !productui.RolloutLiveAt(rollouts[0], "org-a", 10) {
		t.Fatalf("recovered rollout = %#v", rollouts)
	}
	secondSnapshot := snapshot
	secondSnapshot.Title = "Studio v2"
	second, err := log.RecordContext(ctx, "studio", secondSnapshot, 2)
	if err != nil {
		t.Fatalf("RecordContext v2: %v", err)
	}
	rollout2 := productui.PageRollout{Page: "studio", Version: 2, Digest: second.Digest, Scopes: []productui.RolloutScope{{Scope: "org-a", EffectiveFrom: 20}}}
	if err := log.RecordRolloutContext(ctx, rollout2); err != nil {
		t.Fatalf("RecordRolloutContext v2: %v", err)
	}
	rollback := productui.PageRollout{Page: "studio", Version: 1, Digest: revision.Digest, Scopes: []productui.RolloutScope{{Scope: "org-a", EffectiveFrom: 30}}}
	rollback, err = log.RecordRollbackEventContext(ctx, rollback)
	if err != nil {
		t.Fatalf("RecordRollbackEventContext: %v", err)
	}
	retirement := productui.PageRetirement{Page: "studio", EffectiveFrom: 40, Reason: "scheduled closure"}
	if err := log.RecordRetirementContext(ctx, retirement); err != nil {
		t.Fatalf("RecordRetirementContext: %v", err)
	}
	if rollback.RecordVersion <= rollout2.Version {
		t.Fatalf("rollback record sequence = %d, want append order after v2", rollback.RecordVersion)
	}
	recovered, err = productui.NewDurablePageRevisionLog(ctx, tenantA, store)
	if err != nil {
		t.Fatalf("recovery after rollback: %v", err)
	}
	rollouts = recovered.Rollouts()
	if len(rollouts) != 3 || rollouts[2].RecordVersion != rollback.RecordVersion || rollouts[2].Version != 1 {
		t.Fatalf("recovered append-sequenced rollback = %#v", rollouts)
	}
	if retirements := recovered.Retirements(); len(retirements) != 1 || retirements[0] != retirement {
		t.Fatalf("recovered retirement = %#v", retirements)
	}
	if got := countRolloutAnchors(t, db, tenantA); got != 2 {
		t.Fatalf("tenant A rollout anchors = %d, want one row for each target version", got)
	}
	// A second populated tenant may publish the same page and version without
	// seeing or conflicting with tenant A's immutable rows.
	logB, err := productui.NewDurablePageRevisionLog(ctx, tenantB, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := logB.Revision("studio", 1); ok || len(logB.Rollouts()) != 0 {
		t.Fatalf("tenant B read tenant A publication before its own write: revision_present=%v rollouts=%#v", ok, logB.Rollouts())
	}
	snapshotB := productui.PageDefinitionSnapshot{Page: "studio", Route: "/other", Title: "Tenant B Studio", SearchTerms: []string{"other"}}
	revisionB, err := logB.RecordContext(ctx, "studio", snapshotB, 1)
	if err != nil {
		t.Fatal(err)
	}
	rolloutB := productui.PageRollout{Page: "studio", Version: 1, Digest: revisionB.Digest, Scopes: []productui.RolloutScope{{Scope: "org-b", EffectiveFrom: 10}}}
	if err := logB.RecordRolloutContext(ctx, rolloutB); err != nil {
		t.Fatal(err)
	}
	recoveredB, err := productui.NewDurablePageRevisionLog(ctx, tenantB, store)
	if err != nil {
		t.Fatal(err)
	}
	gotB, ok := recoveredB.Revision("studio", 1)
	if !ok || gotB.Digest != revisionB.Digest || gotB.Digest == revision.Digest {
		t.Fatalf("tenant B revision = %#v, ok=%v; tenant A digest=%s", gotB, ok, revision.Digest)
	}
	if got, ok := recovered.Revision("studio", 1); !ok || got.Digest != revision.Digest {
		t.Fatalf("tenant A was contaminated by tenant B: %#v, %v", got, ok)
	}
	if len(recovered.Rollouts()) != 3 || recovered.Rollouts()[0].Scopes[0].Scope != "org-a" {
		t.Fatalf("tenant A rollouts = %#v", recovered.Rollouts())
	}
	if len(recoveredB.Rollouts()) != 1 || recoveredB.Rollouts()[0].Scopes[0].Scope != "org-b" {
		t.Fatalf("tenant B rollouts = %#v", recoveredB.Rollouts())
	}
	if retirements := recoveredB.Retirements(); len(retirements) != 0 {
		t.Fatalf("tenant B read tenant A retirement: %#v", retirements)
	}
	if got := countRolloutAnchors(t, db, tenantB); got != 1 {
		t.Fatalf("tenant B rollout anchors = %d, want one", got)
	}
	if _, err := productui.NewDurablePageRevisionLog(ctx, uuid.NewString(), store); err != nil {
		t.Fatal(err)
	}
}
