package opsmeta_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/opsmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_REV_012_03_OperationalRoutingIntegration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "rev012-03-route")
	routes := opsmeta.LedgerInvariantRoutes{PrimaryOwner: "operations-on-call", SecondaryRoute: "team:platform-oncall"}
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	report := opsmeta.LedgerInvariantScan{Quality: "DEGRADED", Watermark: 12, Digest: "sha256:" + digestOf("scan-one"), Findings: []opsmeta.LedgerInvariantFinding{
		{Code: "SEQUENCE_GAP", StreamKey: "worker-stream-1", Affected: []string{"sequence 13"}, Watermark: 12, Severity: "high"},
	}}

	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		return opsmeta.RecordLedgerInvariantScan(ctx, tx, tenant, report, now, routes)
	}); err != nil {
		t.Fatalf("record first degraded scan: %v", err)
	}
	var initialIncidentCount int
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM operational_incident WHERE tenant_id=$1 AND incident_key LIKE 'ledger-invariant:%' AND status='OPEN'`, tenant).Scan(&initialIncidentCount); err != nil {
			return err
		}
		if initialIncidentCount != 1 {
			t.Fatalf("open routed incidents = %d, want one", initialIncidentCount)
		}
		var owner, route string
		if err := tx.QueryRow(ctx, `SELECT scope->>'primary_owner', scope->>'secondary_route' FROM operational_incident WHERE tenant_id=$1 AND incident_key LIKE 'ledger-invariant:%'`, tenant).Scan(&owner, &route); err != nil {
			return err
		}
		if owner != routes.PrimaryOwner || route != routes.SecondaryRoute {
			t.Fatalf("incident routes=(%q,%q), want (%q,%q)", owner, route, routes.PrimaryOwner, routes.SecondaryRoute)
		}
		return nil
	})

	// The same finding in later scan receipts refreshes one incident instead of
	// creating a new alert each minute; the observation history is append-only.
	updated := report
	updated.Digest = "sha256:" + digestOf("scan-two")
	updated.Findings = append([]opsmeta.LedgerInvariantFinding(nil), report.Findings...)
	updated.Findings[0].Watermark = 13
	updated.Findings[0].Affected = []string{"sequence 13", "sequence 14"}
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		return opsmeta.RecordLedgerInvariantScan(ctx, tx, tenant, updated, now.Add(time.Minute), routes)
	}); err != nil {
		t.Fatalf("record repeated finding: %v", err)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var incidents, observations, revision int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM operational_incident WHERE tenant_id=$1 AND incident_key LIKE 'ledger-invariant:%'`, tenant).Scan(&incidents); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ledger_invariant_scan_observation WHERE tenant_id=$1`, tenant).Scan(&observations); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT impact_revision FROM operational_incident WHERE tenant_id=$1 AND incident_key LIKE 'ledger-invariant:%'`, tenant).Scan(&revision); err != nil {
			return err
		}
		if incidents != 1 || observations != 2 || revision != 2 {
			t.Fatalf("repeated scan incidents=%d observations=%d incident revision=%d, want 1/2/2", incidents, observations, revision)
		}
		var details []byte
		if err := tx.QueryRow(ctx, `SELECT finding_details FROM ledger_invariant_scan_state WHERE tenant_id=$1 AND status='OPEN'`, tenant).Scan(&details); err != nil {
			return err
		}
		var finding map[string]any
		if err := json.Unmarshal(details, &finding); err != nil {
			return err
		}
		repairRef, ok := finding["repair_ref"].(string)
		if !ok || !strings.HasPrefix(repairRef, "ledger-invariant-repair:") {
			t.Fatalf("finding repair_ref does not name a durable handoff: %s", details)
		}
		repair, err := opsmeta.LoadLedgerInvariantRepair(ctx, tx, tenant, repairRef)
		if err != nil {
			return err
		}
		if repair.Status != "OPEN" || repair.IncidentKey == "" || repair.Generation != 1 || repair.EvidenceDigest != digestOf("scan-two") ||
			repair.Finding.Code != "SEQUENCE_GAP" || repair.Finding.StreamKey != "worker-stream-1" ||
			repair.Finding.Severity != "high" || repair.Finding.Watermark != 13 ||
			len(repair.Finding.Affected) != 2 || repair.Finding.Affected[1] != "sequence 14" {
			t.Fatalf("loaded repair handoff = %#v", repair)
		}
		queue, err := opsmeta.ListOpenLedgerInvariantRepairs(ctx, tx, tenant)
		if err != nil {
			return err
		}
		if len(queue) != 1 || queue[0].RepairRef != repairRef || queue[0].Finding.StreamKey != "worker-stream-1" {
			t.Fatalf("consumed repair queue = %#v", queue)
		}
		return nil
	})

	// A healthy scan that started before the latest finding scan may finish
	// later. Its stale timestamp must not resolve newer incident state.
	staleHealthy := opsmeta.LedgerInvariantScan{Quality: "HEALTHY", Watermark: 99, Digest: "sha256:" + digestOf("stale-healthy")}
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		return opsmeta.RecordLedgerInvariantScan(ctx, tx, tenant, staleHealthy, now.Add(30*time.Second), routes)
	}); !errors.Is(err, opsmeta.ErrStaleLedgerInvariantScan) {
		t.Fatalf("older scan error = %v, want stale scan sentinel", err)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var observations, open int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ledger_invariant_scan_observation WHERE tenant_id=$1`, tenant).Scan(&observations); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ledger_invariant_scan_state WHERE tenant_id=$1 AND status='OPEN'`, tenant).Scan(&open); err != nil {
			return err
		}
		if observations != 2 || open != 1 {
			t.Fatalf("stale scan changed state: observations=%d open=%d, want 2/1", observations, open)
		}
		return nil
	})

	// A complete healthy scan is evidence to resolve the finding. An UNKNOWN
	// scan would retain it because it cannot prove absence.
	unknown := opsmeta.LedgerInvariantScan{Quality: "UNKNOWN", Watermark: 13, Digest: "sha256:" + digestOf("scan-unknown"),
		Findings: []opsmeta.LedgerInvariantFinding{{Code: "STREAM_UNSCANNABLE", StreamKey: "worker-stream-2", Severity: "high"}}}
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		return opsmeta.RecordLedgerInvariantScan(ctx, tx, tenant, unknown, now.Add(2*time.Minute), routes)
	}); err != nil {
		t.Fatalf("record unknown scan: %v", err)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var stillOpen int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM operational_incident WHERE tenant_id=$1 AND incident_key LIKE 'ledger-invariant:%' AND status='OPEN'`, tenant).Scan(&stillOpen); err != nil {
			return err
		}
		if stillOpen != 2 {
			t.Fatalf("unknown scan open incidents=%d, want original plus unscannable", stillOpen)
		}
		return nil
	})

	healthy := opsmeta.LedgerInvariantScan{Quality: "HEALTHY", Watermark: 13, Digest: "sha256:" + digestOf("scan-healthy")}
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		return opsmeta.RecordLedgerInvariantScan(ctx, tx, tenant, healthy, now.Add(3*time.Minute), routes)
	}); err != nil {
		t.Fatalf("record healthy scan: %v", err)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var open, resolved int
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status='OPEN'), count(*) FILTER (WHERE status='RESOLVED') FROM operational_incident WHERE tenant_id=$1 AND incident_key LIKE 'ledger-invariant:%'`, tenant).Scan(&open, &resolved); err != nil {
			return err
		}
		if open != 0 || resolved != 2 {
			t.Fatalf("healthy scan incident states open=%d resolved=%d, want 0/2", open, resolved)
		}
		var scope []byte
		if err := tx.QueryRow(ctx, `SELECT finding_details FROM ledger_invariant_scan_state WHERE tenant_id=$1 AND status='RESOLVED' ORDER BY finding_key LIMIT 1`, tenant).Scan(&scope); err != nil {
			return err
		}
		var detail map[string]any
		if err := json.Unmarshal(scope, &detail); err != nil {
			return err
		}
		if !strings.HasPrefix(detail["repair_ref"].(string), "ledger-invariant-repair:") {
			t.Fatalf("finding details lack durable repair handoff reference: %s", scope)
		}
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM ledger_invariant_repair_work WHERE tenant_id=$1`, tenant).Scan(&status); err != nil {
			return err
		}
		if status != "RESOLVED" {
			t.Fatalf("healthy scan repair handoff status=%q, want RESOLVED", status)
		}
		return nil
	})
}

func TestTodo_REV_012_03_RejectsUnroutableScannerIncidents(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "rev012-03-invalid-routes")
	report := opsmeta.LedgerInvariantScan{Quality: "HEALTHY", Digest: "sha256:" + digestOf("healthy")}
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		return opsmeta.RecordLedgerInvariantScan(ctx, tx, tenant, report, fixedInstant, opsmeta.LedgerInvariantRoutes{PrimaryOwner: "same", SecondaryRoute: "same"})
	})
	if err == nil {
		t.Fatal("scanner accepted identical incident primary and secondary routes")
	}
}

func TestTodo_REV_012_03_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	olderConn, newerConn := appConn(t, db), appConn(t, db)
	tenant := insertTenant(t, db, "rev012-03-scan-race")
	routes := opsmeta.LedgerInvariantRoutes{PrimaryOwner: "operations-on-call", SecondaryRoute: "team:platform-oncall"}
	startAt := time.Date(2026, 9, 24, 14, 0, 0, 0, time.UTC)
	older := opsmeta.LedgerInvariantScan{Quality: "HEALTHY", Digest: "sha256:" + digestOf("older-healthy")}
	newer := opsmeta.LedgerInvariantScan{Quality: "DEGRADED", Digest: "sha256:" + digestOf("newer-degraded"), Findings: []opsmeta.LedgerInvariantFinding{
		{Code: "ORPHAN_EVENT", StreamKey: "worker-race", Affected: []string{"event-1"}, Watermark: 4, Severity: "high"},
	}}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		errs <- inTenantTxErr(olderConn, tenant, func(tx dbport.Tx) error {
			return opsmeta.RecordLedgerInvariantScan(ctx, tx, tenant, older, startAt, routes)
		})
	}()
	go func() {
		defer wg.Done()
		<-start
		errs <- inTenantTxErr(newerConn, tenant, func(tx dbport.Tx) error {
			return opsmeta.RecordLedgerInvariantScan(ctx, tx, tenant, newer, startAt.Add(time.Minute), routes)
		})
	}()
	close(start)
	wg.Wait()
	close(errs)
	var staleCount int
	for err := range errs {
		if errors.Is(err, opsmeta.ErrStaleLedgerInvariantScan) {
			staleCount++
		} else if err != nil {
			t.Fatalf("concurrent scan persist: %v", err)
		}
	}
	if staleCount > 1 {
		t.Fatalf("stale scan rejections=%d, want at most one", staleCount)
	}
	inTenantTx(t, olderConn, tenant, func(tx dbport.Tx) error {
		var observed time.Time
		var digest string
		if err := tx.QueryRow(ctx, `SELECT last_observed_at, evidence_digest FROM ledger_invariant_scan_cursor WHERE tenant_id=$1`, tenant).Scan(&observed, &digest); err != nil {
			return err
		}
		if !observed.Equal(startAt.Add(time.Minute)) || digest != digestOf("newer-degraded") {
			t.Fatalf("scan cursor=(%s,%s), want latest degraded observation", observed, digest)
		}
		var open int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ledger_invariant_scan_state WHERE tenant_id=$1 AND status='OPEN'`, tenant).Scan(&open); err != nil {
			return err
		}
		if open != 1 {
			t.Fatalf("latest degraded finding open count=%d, want 1", open)
		}
		return nil
	})
}
