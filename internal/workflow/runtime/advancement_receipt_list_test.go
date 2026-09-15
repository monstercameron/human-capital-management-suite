package runtime_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestLoadAdvancementReceiptsVerifiesEveryStoredReceipt proves the
// inspector's receipt read returns the committed advancement with its storage
// key, request digest and verified receipt digest; that a tampered payload is
// still returned but marked unverified rather than dropped; and that another
// tenant reads nothing.
func TestLoadAdvancementReceiptsVerifiesEveryStoredReceipt(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "receipt-list")
	otherTenant := insertTenant(t, db, "receipt-list-other")
	pf := newPromotionFixture(t, values.TenantId("receipt-list-tenant"), "intent:receipt-list")
	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-receipt-list"))
	req := snapshotAdvanceRequest(tenantID, start.InstanceID, pf.Plan, start.InstanceVersion,
		runtime.NewMemorySink(), "sha256:receipt-list")
	applied, err := advanceOnce(t, conn, tenantID, req)
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}

	load := func(tenant, instance uuid.UUID) []runtime.AdvancementReceiptRecord {
		t.Helper()
		var out []runtime.AdvancementReceiptRecord
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var loadErr error
			out, loadErr = runtime.LoadAdvancementReceipts(context.Background(), tx, tenant, instance)
			return loadErr
		})
		return out
	}

	records := load(tenantID, start.InstanceID)
	if len(records) != 1 {
		t.Fatalf("loaded %d receipts, want 1", len(records))
	}
	rec := records[0]
	if !rec.DigestVerified {
		t.Fatalf("an untouched receipt did not verify: %+v", rec)
	}
	if rec.NodeID != workflow.PromotionNodeSnapshotWorker || rec.Attempt != 1 ||
		rec.ExpectedInstanceVersion != start.InstanceVersion ||
		rec.ResultingInstanceVersion != applied.NewInstanceVersion {
		t.Errorf("receipt key = %s/%d %d->%d, want %s/1 %d->%d", rec.NodeID, rec.Attempt,
			rec.ExpectedInstanceVersion, rec.ResultingInstanceVersion,
			workflow.PromotionNodeSnapshotWorker, start.InstanceVersion, applied.NewInstanceVersion)
	}
	if rec.StoredReceiptDigest != applied.Digest() || rec.Receipt.Digest() != applied.Digest() {
		t.Errorf("receipt digest = %s (stored %s), want %s", rec.Receipt.Digest(), rec.StoredReceiptDigest, applied.Digest())
	}
	if rec.RequestDigest == "" || rec.RecordedAt.IsZero() {
		t.Errorf("request digest %q / recorded at %v not read back", rec.RequestDigest, rec.RecordedAt)
	}

	if leaked := load(otherTenant, start.InstanceID); len(leaked) != 0 {
		t.Fatalf("another tenant read %d of this instance's receipts", len(leaked))
	}

	db.Exec(t, `UPDATE workflow_advancement_receipt
		SET receipt = jsonb_set(receipt, '{output_digest}', '"sha256:tampered"')
		WHERE tenant_id = $1 AND instance_id = $2`, tenantID, start.InstanceID)
	tampered := load(tenantID, start.InstanceID)
	if len(tampered) != 1 {
		t.Fatalf("a tampered receipt was dropped: %d rows", len(tampered))
	}
	if tampered[0].DigestVerified {
		t.Fatal("a tampered receipt payload still verified")
	}
}
