package opsmeta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var ErrStaleLedgerInvariantScan = fmt.Errorf("opsmeta: stale ledger invariant scan")

// LedgerInvariantFinding is the redacted, typed projection of one scanner
// finding needed by the operational incident adapter.
type LedgerInvariantFinding struct {
	Code      string   `json:"code"`
	StreamKey string   `json:"stream_key"`
	Affected  []string `json:"affected"`
	Watermark int64    `json:"watermark"`
	Severity  string   `json:"severity"`
}

// LedgerInvariantScan is one complete scanner receipt.
type LedgerInvariantScan struct {
	Quality   string
	Watermark int64
	Findings  []LedgerInvariantFinding
	Digest    string
}

// LedgerInvariantRoutes are the operational owners for durable scanner
// incidents. Defaults match the established sending-domain and platform
// on-call routes; callers may override them through validated role config.
type LedgerInvariantRoutes struct {
	PrimaryOwner   string
	SecondaryRoute string
}

// RecordLedgerInvariantScan commits a scan receipt, refreshes stable open
// findings, routes newly observed findings into operational_incident, and
// resolves findings absent from a complete scan. The caller owns the tenant
// transaction so scan evidence and incident updates are atomic.
func RecordLedgerInvariantScan(ctx context.Context, tx dbport.Tx, tenant uuid.UUID,
	report LedgerInvariantScan, now time.Time, routes LedgerInvariantRoutes) error {
	if tx == nil || tenant == uuid.Nil || now.IsZero() || !validLedgerInvariantRoutes(routes) {
		return detail(ErrMissingScope, "ledger invariant scan transaction, tenant, time and routes are required")
	}
	if err := ensureTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 723019))`, "ledger-invariant-scan:"+tenant.String()); err != nil {
		return fmt.Errorf("opsmeta: serialize ledger invariant scan: %w", err)
	}
	evidence := strings.TrimPrefix(report.Digest, "sha256:")
	if !isHexDigest(evidence) || !oneOf(report.Quality, "HEALTHY", "DEGRADED", "UNKNOWN") || report.Watermark < 0 {
		return detail(ErrMissingLineage, "ledger invariant scan receipt is invalid")
	}
	if report.Quality == "HEALTHY" && len(report.Findings) != 0 || report.Quality != "HEALTHY" && len(report.Findings) == 0 {
		return detail(ErrMissingLineage, "ledger invariant scan quality does not match its findings")
	}
	if report.Findings == nil {
		report.Findings = []LedgerInvariantFinding{}
	}
	for _, finding := range report.Findings {
		if !validLedgerInvariantFinding(finding) {
			return detail(ErrMissingScope, "ledger invariant finding is incomplete")
		}
	}
	now = now.UTC().Truncate(time.Microsecond)
	// Scan time is captured before the database snapshot is read. A slow scan
	// must not overwrite the incident state produced by a newer scan.
	cursorRows, err := tx.Exec(ctx, `INSERT INTO ledger_invariant_scan_cursor
		(tenant_id, last_observed_at, evidence_digest) VALUES ($1,$2,$3)
		ON CONFLICT (tenant_id) DO UPDATE SET last_observed_at=EXCLUDED.last_observed_at,
		evidence_digest=EXCLUDED.evidence_digest
		WHERE ledger_invariant_scan_cursor.last_observed_at < EXCLUDED.last_observed_at`, tenant, now, evidence)
	if err != nil {
		return fmt.Errorf("opsmeta: fence ledger invariant scan: %w", err)
	}
	if cursorRows == 0 {
		return ErrStaleLedgerInvariantScan
	}
	findingsJSON, err := json.Marshal(report.Findings)
	if err != nil {
		return fmt.Errorf("opsmeta: encode ledger invariant findings: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_invariant_scan_observation
		(tenant_id, observation_id, quality, watermark, evidence_digest, observed_at, finding_count, findings)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, tenant, uuid.New(), string(report.Quality), report.Watermark,
		evidence, now, len(report.Findings), findingsJSON); err != nil {
		return fmt.Errorf("opsmeta: persist ledger invariant scan receipt: %w", err)
	}
	seen := make(map[string]struct{}, len(report.Findings))
	for _, finding := range report.Findings {
		key := ledgerFindingKey(finding)
		seen[key] = struct{}{}
		if err := recordLedgerFinding(ctx, tx, tenant, key, evidence, finding, now, routes); err != nil {
			return err
		}
	}
	// A scan with any finding cannot prove that every other invariant in the
	// affected streams is clear; UNKNOWN additionally means a stream was not
	// inspectable. Only a wholly healthy full scan resolves absent findings.
	if report.Quality != "HEALTHY" {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT finding_key, incident_key FROM ledger_invariant_scan_state
		WHERE tenant_id=$1 AND status='OPEN' FOR UPDATE`, tenant)
	if err != nil {
		return fmt.Errorf("opsmeta: list open ledger findings: %w", err)
	}
	type openFinding struct{ key, incidentKey string }
	var open []openFinding
	for rows.Next() {
		var item openFinding
		if err := rows.Scan(&item.key, &item.incidentKey); err != nil {
			rows.Close()
			return err
		}
		open = append(open, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range open {
		if _, active := seen[item.key]; active {
			continue
		}
		if err := ResolveIncidentsByKeyPrefix(ctx, tx, tenant, item.incidentKey, now); err != nil {
			return fmt.Errorf("opsmeta: resolve cleared ledger finding: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE ledger_invariant_scan_state SET status='RESOLVED'
			WHERE tenant_id=$1 AND finding_key=$2 AND status='OPEN'`, tenant, item.key); err != nil {
			return fmt.Errorf("opsmeta: resolve ledger finding state: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE ledger_invariant_repair_work SET status='RESOLVED', updated_at=$3
			WHERE tenant_id=$1 AND finding_key=$2 AND status='OPEN'`, tenant, item.key, now); err != nil {
			return fmt.Errorf("opsmeta: resolve ledger repair work: %w", err)
		}
	}
	return nil
}

func recordLedgerFinding(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, key, evidence string,
	finding LedgerInvariantFinding, now time.Time, routes LedgerInvariantRoutes) error {
	details, err := json.Marshal(map[string]any{
		"finding_key": key, "code": finding.Code, "stream_key": finding.StreamKey,
		"affected": finding.Affected, "watermark": finding.Watermark, "severity": finding.Severity,
		"evidence_digest": evidence,
	})
	if err != nil {
		return fmt.Errorf("opsmeta: encode ledger finding: %w", err)
	}
	var generation int
	var incidentID uuid.UUID
	var incidentKey, status string
	var firstSeen time.Time
	err = tx.QueryRow(ctx, `SELECT generation, incident_id, incident_key, status, first_seen_at
		FROM ledger_invariant_scan_state WHERE tenant_id=$1 AND finding_key=$2 FOR UPDATE`, tenant, key).
		Scan(&generation, &incidentID, &incidentKey, &status, &firstSeen)
	if err != nil && err != dbport.ErrNoRows {
		return fmt.Errorf("opsmeta: load ledger finding state: %w", err)
	}
	newIncident := err == dbport.ErrNoRows || status == "RESOLVED"
	if newIncident {
		if err == dbport.ErrNoRows {
			generation = 1
		} else {
			generation++
		}
		incidentID = uuid.NewSHA1(tenant, []byte(fmt.Sprintf("ledger-invariant/%s/%d", key, generation)))
		incidentKey = fmt.Sprintf("ledger-invariant:%s:%d", key, generation)
		firstSeen = now
	} else {
		if _, err := tx.Exec(ctx, `UPDATE operational_incident SET severity=$3,
			impact_revision=impact_revision+1, scope=scope || $4::jsonb, evidence_digest=$5
			WHERE tenant_id=$1 AND incident_id=$2`, tenant, incidentID, ledgerIncidentSeverity(finding.Severity), details, evidence); err != nil {
			return fmt.Errorf("opsmeta: refresh ledger finding incident: %w", err)
		}
	}
	repairID := uuid.NewSHA1(tenant, []byte(fmt.Sprintf("ledger-invariant-repair/%s/%d", key, generation)))
	repairRef := "ledger-invariant-repair:" + repairID.String()
	var detailMap map[string]any
	if err := json.Unmarshal(details, &detailMap); err != nil {
		return err
	}
	detailMap["repair_ref"] = repairRef
	details, err = json.Marshal(detailMap)
	if err != nil {
		return err
	}
	if newIncident {
		scope, err := json.Marshal(map[string]any{
			"service": "ledger-invariant-scanner", "finding_key": key, "finding": json.RawMessage(details),
		})
		if err != nil {
			return err
		}
		if _, err := RouteAlert(ctx, tx, AlertIncident{
			TenantID: tenant, IncidentID: incidentID, IncidentKey: incidentKey,
			Severity: ledgerIncidentSeverity(finding.Severity), Scope: scope,
			CorrelationKey: "ledger-invariant:" + key, EvidenceDigest: evidence,
			DeclaredAt: now, PrimaryOwner: routes.PrimaryOwner, SecondaryRoute: routes.SecondaryRoute,
			StormLimit: 10000, StormWindow: 24 * time.Hour,
		}); err != nil {
			return fmt.Errorf("opsmeta: route ledger finding incident: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_invariant_scan_state
		(tenant_id, finding_key, generation, incident_id, incident_key, status, first_seen_at,
		 last_seen_at, evidence_digest, finding_details)
		VALUES ($1,$2,$3,$4,$5,'OPEN',$6,$7,$8,$9)
		ON CONFLICT (tenant_id, finding_key) DO UPDATE SET generation=EXCLUDED.generation,
		incident_id=EXCLUDED.incident_id, incident_key=EXCLUDED.incident_key, status='OPEN',
		first_seen_at=EXCLUDED.first_seen_at, last_seen_at=EXCLUDED.last_seen_at,
		evidence_digest=EXCLUDED.evidence_digest, finding_details=EXCLUDED.finding_details`,
		tenant, key, generation, incidentID, incidentKey, firstSeen, now, evidence, details); err != nil {
		return fmt.Errorf("opsmeta: persist ledger finding state: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_invariant_repair_work
		(tenant_id, repair_id, finding_key, generation, incident_id, incident_key, status,
		 opened_at, updated_at, evidence_digest, finding_details)
		VALUES ($1,$2,$3,$4,$5,$6,'OPEN',$7,$7,$8,$9)
		ON CONFLICT (tenant_id, repair_id) DO UPDATE SET updated_at=EXCLUDED.updated_at,
		evidence_digest=EXCLUDED.evidence_digest, finding_details=EXCLUDED.finding_details`,
		tenant, repairID, key, generation, incidentID, incidentKey, now, evidence, details); err != nil {
		return fmt.Errorf("opsmeta: persist ledger repair handoff: %w", err)
	}
	return nil
}

// LedgerInvariantRepair is a persisted, tenant-scoped operator handoff. It
// records the work item and incident reference; it never executes a repair.
type LedgerInvariantRepair struct {
	RepairRef      string
	FindingKey     string
	Generation     int
	IncidentID     uuid.UUID
	IncidentKey    string
	Status         string
	OpenedAt       time.Time
	UpdatedAt      time.Time
	EvidenceDigest string
	Finding        LedgerInvariantFinding
}

// LoadLedgerInvariantRepair resolves the opaque repair reference to its real
// durable work item. The caller must establish tenant scope on q first.
func LoadLedgerInvariantRepair(ctx context.Context, q dbport.Querier, tenant uuid.UUID, ref string) (LedgerInvariantRepair, error) {
	const prefix = "ledger-invariant-repair:"
	if q == nil || tenant == uuid.Nil || !strings.HasPrefix(ref, prefix) {
		return LedgerInvariantRepair{}, detail(ErrMissingScope, "ledger repair reference and tenant query are required")
	}
	repairID, err := uuid.Parse(strings.TrimPrefix(ref, prefix))
	if err != nil || repairID == uuid.Nil {
		return LedgerInvariantRepair{}, detail(ErrMissingScope, "ledger repair reference is invalid")
	}
	var item LedgerInvariantRepair
	var details []byte
	err = q.QueryRow(ctx, `SELECT finding_key, generation, incident_id, incident_key, status,
		opened_at, updated_at, evidence_digest, finding_details
		FROM ledger_invariant_repair_work WHERE tenant_id=$1 AND repair_id=$2`, tenant, repairID).
		Scan(&item.FindingKey, &item.Generation, &item.IncidentID, &item.IncidentKey, &item.Status,
			&item.OpenedAt, &item.UpdatedAt, &item.EvidenceDigest, &details)
	if err != nil {
		return LedgerInvariantRepair{}, fmt.Errorf("opsmeta: load ledger repair handoff: %w", err)
	}
	item.RepairRef = ref
	if err := json.Unmarshal(details, &item.Finding); err != nil {
		return LedgerInvariantRepair{}, fmt.Errorf("opsmeta: decode ledger repair finding: %w", err)
	}
	return item, nil
}

// ListOpenLedgerInvariantRepairs consumes the tenant-scoped handoff queue in
// stable creation order. The caller must establish tenant scope on q first.
func ListOpenLedgerInvariantRepairs(ctx context.Context, q dbport.Querier, tenant uuid.UUID) ([]LedgerInvariantRepair, error) {
	if q == nil || tenant == uuid.Nil {
		return nil, detail(ErrMissingScope, "ledger repair queue query and tenant are required")
	}
	rows, err := q.Query(ctx, `SELECT repair_id, finding_key, generation, incident_id, incident_key, status,
		opened_at, updated_at, evidence_digest, finding_details
		FROM ledger_invariant_repair_work WHERE tenant_id=$1 AND status='OPEN'
		ORDER BY opened_at, repair_id`, tenant)
	if err != nil {
		return nil, fmt.Errorf("opsmeta: list ledger repair queue: %w", err)
	}
	defer rows.Close()
	var repairs []LedgerInvariantRepair
	for rows.Next() {
		var item LedgerInvariantRepair
		var repairID uuid.UUID
		var details []byte
		if err := rows.Scan(&repairID, &item.FindingKey, &item.Generation, &item.IncidentID, &item.IncidentKey,
			&item.Status, &item.OpenedAt, &item.UpdatedAt, &item.EvidenceDigest, &details); err != nil {
			return nil, err
		}
		item.RepairRef = "ledger-invariant-repair:" + repairID.String()
		if err := json.Unmarshal(details, &item.Finding); err != nil {
			return nil, fmt.Errorf("opsmeta: decode ledger repair finding: %w", err)
		}
		repairs = append(repairs, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return repairs, nil
}

func ledgerFindingKey(finding LedgerInvariantFinding) string {
	h := sha256.Sum256([]byte("ledger-invariant-finding.v1\x00" + finding.Code + "\x00" + finding.StreamKey))
	return hex.EncodeToString(h[:])
}

func ledgerIncidentSeverity(severity string) string {
	switch strings.ToLower(severity) {
	case "critical":
		return "SEV1"
	case "high":
		return "SEV2"
	case "medium":
		return "SEV3"
	default:
		return "SEV4"
	}
}

func validLedgerInvariantRoutes(routes LedgerInvariantRoutes) bool {
	return strings.TrimSpace(routes.PrimaryOwner) != "" && strings.TrimSpace(routes.SecondaryRoute) != "" &&
		routes.PrimaryOwner != routes.SecondaryRoute && len(routes.PrimaryOwner) <= 256 && len(routes.SecondaryRoute) <= 256 &&
		!strings.ContainsAny(routes.PrimaryOwner+routes.SecondaryRoute, "\r\n{}[]")
}

func validLedgerInvariantFinding(finding LedgerInvariantFinding) bool {
	knownCode := oneOf(finding.Code, "SEQUENCE_GAP", "DIGEST_MISMATCH", "ORPHAN_EVENT", "ORPHAN_OUTBOX",
		"ORPHAN_PROJECTION", "PROVENANCE_MISSING", "DUPLICATE_SETTLEMENT", "STREAM_UNSCANNABLE")
	if !knownCode || strings.TrimSpace(finding.StreamKey) == "" || len(finding.StreamKey) > 256 ||
		finding.Watermark < 0 || !oneOf(finding.Severity, "high", "medium", "low") ||
		len(finding.Affected) > 64 {
		return false
	}
	for _, affected := range finding.Affected {
		if len(affected) > 256 {
			return false
		}
	}
	return true
}
