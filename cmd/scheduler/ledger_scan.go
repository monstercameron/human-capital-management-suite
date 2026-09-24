package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
	"github.com/monstercameron/human-capital-management-suite/internal/data/opsmeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

const defaultInvariantScanInterval = time.Minute

type invariantScanQuerier interface {
	dbport.Beginner
}

// runInvariantScanner registers DATA-012 verification as a recurring scheduler
// workload. Every receipt is logged with its digest and actionable finding refs.
func runInvariantScanner(ctx context.Context, db invariantScanQuerier, tenant uuid.UUID,
	interval time.Duration, logger bootstrap.Logger) error {
	return runInvariantScannerConfigured(ctx, db, tenant, interval, logger, time.Now,
		opsmeta.LedgerInvariantRoutes{PrimaryOwner: defaultInvariantScanOwner, SecondaryRoute: defaultInvariantScanRoute})
}

func runInvariantScannerConfigured(ctx context.Context, db invariantScanQuerier, tenant uuid.UUID,
	interval time.Duration, logger bootstrap.Logger, clock func() time.Time, routes opsmeta.LedgerInvariantRoutes) error {
	if db == nil || tenant == uuid.Nil || interval <= 0 || logger == nil || clock == nil ||
		!validLedgerInvariantRoutes(routes) {
		return fmt.Errorf("ledger scanner requires database, tenant, positive interval and logger")
	}
	run := func() error {
		startedAt := clock().UTC()
		report, err := scanTenantLedger(ctx, db, tenant)
		if err != nil {
			logger.Error("ledger.invariant_scan.failed", "tenant_id", tenant.String(), "error", err)
			return err
		}
		repairs, err := persistLedgerScan(ctx, db, tenant, report, startedAt, routes)
		if err != nil {
			if errors.Is(err, opsmeta.ErrStaleLedgerInvariantScan) {
				logger.Info("ledger.invariant_scan.stale_snapshot_ignored", "tenant_id", tenant.String(), "evidence_digest", report.Digest)
				return nil
			}
			logger.Error("ledger.invariant_scan.routing_failed", "tenant_id", tenant.String(), "error", err)
			return err
		}
		if len(repairs) > 0 {
			report.RepairRef = repairs[0].RepairRef
			report.IncidentRef = repairs[0].IncidentKey
			for i := range report.Findings {
				for _, repair := range repairs {
					if repair.Finding.Code == report.Findings[i].Code && repair.Finding.StreamKey == report.Findings[i].StreamKey {
						report.Findings[i].RepairRef = repair.RepairRef
						report.Findings[i].IncidentRef = repair.IncidentKey
						break
					}
				}
			}
		}
		raw, _ := json.Marshal(report.Findings)
		if report.Quality != hashchain.QualityHealthy {
			logger.Error("ledger.invariant_scan.action_required", "tenant_id", tenant.String(),
				"quality", string(report.Quality), "watermark", report.Watermark,
				"findings", string(raw), "incident_ref", report.IncidentRef,
				"repair_ref", report.RepairRef, "evidence_digest", report.Digest)
		} else {
			logger.Info("ledger.invariant_scan.completed", "tenant_id", tenant.String(),
				"quality", string(report.Quality), "watermark", report.Watermark,
				"evidence_digest", report.Digest)
		}
		return nil
	}
	if err := run(); err != nil {
		// A transient read or route failure is surfaced and retried on the next
		// cadence without terminating the scheduler workload.
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_ = run()
		}
	}
}

func validLedgerInvariantRoutes(routes opsmeta.LedgerInvariantRoutes) bool {
	return routes.PrimaryOwner != "" && routes.SecondaryRoute != "" && routes.PrimaryOwner != routes.SecondaryRoute
}

func persistLedgerScan(ctx context.Context, db dbport.Beginner, tenant uuid.UUID, report hashchain.ScanReport,
	observedAt time.Time, routes opsmeta.LedgerInvariantRoutes) ([]opsmeta.LedgerInvariantRepair, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin ledger scan incident transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return nil, err
	}
	scan := opsmeta.LedgerInvariantScan{
		Quality: string(report.Quality), Watermark: report.Watermark, Digest: report.Digest,
		Findings: make([]opsmeta.LedgerInvariantFinding, 0, len(report.Findings)),
	}
	for _, finding := range report.Findings {
		scan.Findings = append(scan.Findings, opsmeta.LedgerInvariantFinding{
			Code: finding.Code, StreamKey: finding.StreamKey, Affected: finding.Affected,
			Watermark: finding.Watermark, Severity: finding.Severity,
		})
	}
	if err := opsmeta.RecordLedgerInvariantScan(ctx, tx, tenant, scan, observedAt, routes); err != nil {
		return nil, err
	}
	repairs, err := opsmeta.ListOpenLedgerInvariantRepairs(ctx, tx, tenant)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit ledger scan incident transaction: %w", err)
	}
	return repairs, nil
}

func scanTenantLedger(ctx context.Context, db invariantScanQuerier, tenant uuid.UUID) (hashchain.ScanReport, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return hashchain.ScanReport{}, fmt.Errorf("begin ledger scan: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return hashchain.ScanReport{}, err
	}
	rows, err := tx.Query(ctx, `SELECT stream_key FROM ledger_stream ORDER BY stream_key`)
	if err != nil {
		return hashchain.ScanReport{}, fmt.Errorf("list ledger streams: %w", err)
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return hashchain.ScanReport{}, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return hashchain.ScanReport{}, err
	}
	rows.Close()
	report, err := scanLedgerKeys(ctx, tx, tenant, keys)
	if err != nil {
		return hashchain.ScanReport{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return hashchain.ScanReport{}, fmt.Errorf("finish ledger scan snapshot: %w", err)
	}
	return report, nil
}

func scanLedgerKeys(ctx context.Context, tx dbport.Querier, tenant uuid.UUID, keys []string) (hashchain.ScanReport, error) {
	registry, err := hashchain.NewRegistry()
	if err != nil {
		return hashchain.ScanReport{}, err
	}
	digester := hashchain.NewDigester(registry)
	var streams []hashchain.StreamView
	for _, key := range keys {
		links, err := hashchain.ReadLinks(ctx, tx, tenant, key)
		if err != nil {
			return hashchain.ScanReport{}, err
		}
		events, err := hashchain.ReadEventDigests(ctx, tx, tenant, key)
		if err != nil {
			return hashchain.ScanReport{}, err
		}
		view := hashchain.StreamView{StreamKey: key, Links: links, Events: events, ProjectedThrough: 0}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(last_applied_sequence), 0) FROM projection_checkpoint WHERE tenant_id=$1 AND stream_key=$2`, tenant, key).Scan(&view.ProjectedThrough); err != nil {
			return hashchain.ScanReport{}, err
		}
		pRows, err := tx.Query(ctx, `SELECT event_id FROM provenance_record WHERE tenant_id=$1 AND stream_key=$2 AND event_id IS NOT NULL`, tenant, key)
		if err != nil {
			return hashchain.ScanReport{}, err
		}
		for pRows.Next() {
			var id uuid.UUID
			if err := pRows.Scan(&id); err != nil {
				pRows.Close()
				return hashchain.ScanReport{}, err
			}
			view.Provenance = append(view.Provenance, id)
		}
		if err := pRows.Err(); err != nil {
			pRows.Close()
			return hashchain.ScanReport{}, err
		}
		pRows.Close()
		// Outbox and settlement snapshots stay unavailable here. Direct ledger
		// append paths legitimately omit outbox rows, while outbox effects and
		// settlement records have no universal ledger-stream foreign key. Passing
		// empty slices would assert false relationships and create false findings;
		// these relational classes need their own exact-key scanner before they
		// can be included in this workload's coverage claim.
		streams = append(streams, view)
	}
	return hashchain.Scan(digester, hashchain.ScanInput{Mode: hashchain.ScanFull, Streams: streams})
}
