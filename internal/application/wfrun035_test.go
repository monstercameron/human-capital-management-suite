package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// versionTestPool opens a pool on a fresh test schema, closed with the test.
func versionTestPool(t *testing.T) *pgxadapter.Pool {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestTodo_WF_RUN_035_Recovery proves the served execution authority keeps
// its workflow version registry durably: the composed version store is the
// PostgreSQL registry, never the in-memory one; the shipped versions are
// ACTIVE on a recorded release approval that is not the publisher's; a
// recomposition (a restart) records no second approval or activation; and a
// version an operator quarantined stays quarantined across the restart.
func TestTodo_WF_RUN_035_Recovery(t *testing.T) {
	ctx := context.Background()
	pool := versionTestPool(t)
	cfg := executionServeConfig()
	compose := func() app.CellConfig {
		t.Helper()
		evidence := app.NewMemoryEvidenceSink()
		cellConfig := app.CellConfig{Evidence: evidence}
		if err := ComposeExecutionAuthority(&cellConfig, pool, evidence, cfg); err != nil {
			t.Fatalf("ComposeExecutionAuthority: %v", err)
		}
		return cellConfig
	}
	counts := func() (approvals, transitions int) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM workflow_version_approval), (SELECT count(*) FROM workflow_version_transition)`).Scan(&approvals, &transitions); err != nil {
			t.Fatalf("count registry rows: %v", err)
		}
		return approvals, transitions
	}

	first := compose()
	store, ok := first.ExecutionVersions.(workflowversionstore.Store)
	if !ok {
		t.Fatalf("serve composed %T as the workflow version store, want the durable registry", first.ExecutionVersions)
	}
	var active []version.CompiledVersion
	rows, err := pool.Query(ctx, `SELECT compiled_plan_digest FROM workflow_compiled_version WHERE status = 'ACTIVE' ORDER BY workflow_id`)
	if err != nil {
		t.Fatalf("list active versions: %v", err)
	}
	var digests []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan: %v", err)
		}
		digests = append(digests, d)
	}
	rows.Close()
	for _, d := range digests {
		v, found, err := store.GetByDigest(d)
		if err != nil || !found {
			t.Fatalf("GetByDigest(%s) = %v, %v", d, found, err)
		}
		active = append(active, v)
	}
	if len(active) != 2 {
		t.Fatalf("ACTIVE shipped versions = %d, want the approval and execute promotion workflows", len(active))
	}
	for _, v := range active {
		if len(v.Approvals) != 1 || v.Approvals[0].ApprovedBy == v.PublishedBy {
			t.Fatalf("%s activation history = %+v, want one approval by someone other than %s", v.WorkflowID, v.Approvals, v.PublishedBy)
		}
	}
	approvals, transitions := counts()

	compose()
	if a, tr := counts(); a != approvals || tr != transitions {
		t.Fatalf("a restart recorded %d approvals and %d transitions, want %d and %d", a, tr, approvals, transitions)
	}

	quarantined := active[0]
	if _, err := version.Quarantine(store, quarantined.CompiledPlanDigest, "incident", "principal:incident-commander", "authority:incident",
		version.ActivationEvidence{ApprovedAt: quarantined.PublishedAt}); err != nil {
		t.Fatalf("Quarantine: %v", err)
	}
	restarted := compose()
	after, found, err := restarted.ExecutionVersions.GetByDigest(quarantined.CompiledPlanDigest)
	if err != nil || !found || after.Status != version.StatusQuarantined {
		t.Fatalf("after restart the quarantined version is %s (found %v, %v), want QUARANTINED", after.Status, found, err)
	}
}
