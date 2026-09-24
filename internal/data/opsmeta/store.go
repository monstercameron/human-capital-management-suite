// Package opsmeta is the tenant-scoped store for the operational metadata
// migration 00032 creates (DB-015): incidents, backup runs and recovery runs.
//
// Two rules it holds, both also schema constraints:
//
//   - A recovery run cannot close on an unvalidated claim.
//     [CompleteRecoveryRun] requires the actual RPO and RTO it achieved and a
//     validation result of PASS or PARTIAL, and a RESTORE run must name the
//     backup it restored from.
//   - A completed backup is immutable for a stated window. A backup with no
//     immutability horizon is not a recovery source, and
//     [CompleteBackupRun] refuses to record one.
//
// DB-015's REFACTOR keeps operational telemetry out of business and assurance
// evidence: nothing here stores a metric series, a log line or a span. An
// incident carries the digest of its evidence, not the evidence.
package opsmeta

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

var (
	// ErrNilTenant is returned when a row omits its tenant or its own id.
	ErrNilTenant = errors.New("opsmeta: tenant or row id is nil")
	// ErrMissingLineage is returned when a recovery run omits the backup it
	// restored from, or an incident omits its evidence digest (DB-015 RED:
	// recovery evidence loses lineage).
	ErrMissingLineage = errors.New("opsmeta: recovery or incident lineage missing")
	// ErrUnvalidatedCompletion is returned when a run claims completion
	// without the actuals and validation that make the claim checkable.
	ErrUnvalidatedCompletion = errors.New("opsmeta: run completed without validated actuals")
	// ErrMutableBackup is returned when a completed backup declares no
	// immutability horizon.
	ErrMutableBackup = errors.New("opsmeta: a completed backup must be immutable for a stated window")
	// ErrMissingScope is returned when a row omits its scope.
	ErrMissingScope = errors.New("opsmeta: scope missing")
	// ErrInvalidEnum is returned for a value outside its declared set.
	ErrInvalidEnum = errors.New("opsmeta: value outside its declared set")
	// ErrInvalidInterval is returned for a reversed or empty interval.
	ErrInvalidInterval = errors.New("opsmeta: time interval invalid")
	// ErrAlertConflict is returned when a replay reuses a durable dedupe key
	// with different authoritative alert facts.
	ErrAlertConflict = errors.New("opsmeta: alert dedupe conflict")
	// ErrAlertStorm is returned when the durable tenant/window admission limit
	// has already been reached.
	ErrAlertStorm = errors.New("opsmeta: alert storm limit reached")
	// ErrAcknowledgementUnauthorized is returned when an actor other than the
	// assigned primary owner attempts to acknowledge an incident.
	ErrAcknowledgementUnauthorized = errors.New("opsmeta: acknowledgement unauthorized")
	// ErrAcknowledgementConflict is returned when a second acknowledgement
	// attempts to replace the durable actor, receipt, or observation time.
	ErrAcknowledgementConflict = errors.New("opsmeta: acknowledgement conflict")
)

// ErrDetail names the field behind one of the sentinels above.
type ErrDetail struct {
	Sentinel error
	Detail   string
}

func (e ErrDetail) Error() string { return fmt.Sprintf("%v: %s", e.Sentinel, e.Detail) }

func (e ErrDetail) Unwrap() error { return e.Sentinel }

func detail(sentinel error, format string, args ...any) error {
	return ErrDetail{Sentinel: sentinel, Detail: fmt.Sprintf(format, args...)}
}

// OperationsTables is the exact set of base tables migration 00032 creates for
// the operations family, sorted.
var OperationsTables = []string{
	"backup_run",
	"operational_incident",
	"recovery_run",
}

func ensureTenant(ctx context.Context, tx dbport.Execer, tenantID uuid.UUID) error {
	if tenantID == uuid.Nil {
		return ErrNilTenant
	}
	return tenancy.WithTenant(ctx, tx, tenantID)
}

func object(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func decodeObject(raw json.RawMessage) (map[string]any, error) {
	raw = object(raw)
	if !json.Valid(raw) {
		return nil, ErrMissingScope
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, ErrMissingScope
	}
	return value, nil
}

func oneOf(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// operational_incident
// ---------------------------------------------------------------------------

// OperationalIncident is one declared operational failure and its lifecycle.
type OperationalIncident struct {
	TenantID       uuid.UUID       `json:"tenant_id"`
	IncidentID     uuid.UUID       `json:"incident_id"`
	IncidentKey    string          `json:"incident_key"`
	Severity       string          `json:"severity"`
	ImpactRevision int64           `json:"impact_revision"`
	Scope          json.RawMessage `json:"scope"`
	CorrelationKey string          `json:"correlation_key"`
	EvidenceDigest string          `json:"evidence_digest"`
	DeclaredAt     time.Time       `json:"declared_at"`
	ContainedAt    *time.Time      `json:"contained_at"`
	ResolvedAt     *time.Time      `json:"resolved_at"`
	ResidualRisk   string          `json:"residual_risk"`
	PostmortemRef  string          `json:"postmortem_ref"`
	Status         string          `json:"status"`
}

// AlertIncident is the redacted telemetry-to-incident input. It contains no
// metric payload or customer identifiers other than the tenant boundary.
type AlertIncident struct {
	TenantID       uuid.UUID
	IncidentID     uuid.UUID
	IncidentKey    string
	Severity       string
	Scope          json.RawMessage
	CorrelationKey string
	EvidenceDigest string
	DeclaredAt     time.Time
	PrimaryOwner   string
	SecondaryRoute string
	StormLimit     int
	StormWindow    time.Duration
}

type AlertIncidentResult struct {
	Incident OperationalIncident
	Created  bool
}

// RouteAlert persists a deduplicated incident in the real operations store.
// The unique incident key is the durable storm/dedupe fence; a concurrent
// winner is returned as an existing incident.
func RouteAlert(ctx context.Context, tx dbport.Tx, a AlertIncident) (AlertIncidentResult, error) {
	if a.TenantID == uuid.Nil || a.IncidentID == uuid.Nil || a.IncidentKey == "" || a.CorrelationKey == "" || a.EvidenceDigest == "" || a.DeclaredAt.IsZero() {
		return AlertIncidentResult{}, ErrMissingLineage
	}
	if a.PrimaryOwner == "" {
		return AlertIncidentResult{}, detail(ErrMissingScope, "alert primary owner")
	}
	if a.SecondaryRoute == "" {
		return AlertIncidentResult{}, detail(ErrMissingScope, "alert secondary route")
	}
	if a.PrimaryOwner == a.SecondaryRoute {
		return AlertIncidentResult{}, detail(ErrMissingScope, "alert fallback must differ from primary owner")
	}
	if !oneOf(a.Severity, "SEV1", "SEV2", "SEV3", "SEV4", "SEV5") {
		return AlertIncidentResult{}, detail(ErrInvalidEnum, "alert severity=%q", a.Severity)
	}
	if a.StormLimit < 1 || a.StormWindow <= 0 {
		return AlertIncidentResult{}, detail(ErrInvalidInterval, "alert storm policy")
	}
	if len(a.IncidentKey) > 512 || len(a.CorrelationKey) > 256 || !isHexDigest(a.EvidenceDigest) || len(a.PrimaryOwner) > 256 || len(a.SecondaryRoute) > 256 || len(a.Scope) > 4096 || strings.ContainsAny(a.PrimaryOwner+a.SecondaryRoute, "\r\n{}[]") {
		return AlertIncidentResult{}, detail(ErrMissingScope, "alert metadata exceeds bound")
	}
	a.DeclaredAt = a.DeclaredAt.UTC().Truncate(time.Microsecond)
	if err := ensureTenant(ctx, tx, a.TenantID); err != nil {
		return AlertIncidentResult{}, err
	}
	// Serialize admission for this tenant so concurrent distinct alerts cannot
	// all observe the same pre-limit count and overrun the durable fence.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 723007))`, a.TenantID.String()); err != nil {
		return AlertIncidentResult{}, err
	}
	var existing OperationalIncident
	if err := loadByKey(ctx, tx, a.TenantID, a.IncidentKey, &existing); err == nil {
		if err := sameAlert(existing, a); err != nil {
			return AlertIncidentResult{}, err
		}
		return AlertIncidentResult{Incident: existing}, nil
	} else if !errors.Is(err, dbport.ErrNoRows) {
		return AlertIncidentResult{}, err
	}
	scope := object(a.Scope)
	// Route metadata is bounded and safe; no source alert or tenant data is copied.
	meta, err := decodeObject(scope)
	if err != nil {
		return AlertIncidentResult{}, err
	}
	for _, reserved := range []string{"primary_owner", "secondary_route", "acknowledged", "acknowledged_by", "acknowledged_at", "acknowledgement_evidence"} {
		if _, exists := meta[reserved]; exists {
			return AlertIncidentResult{}, detail(ErrMissingScope, "alert scope contains reserved field %s", reserved)
		}
	}
	var active int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM operational_incident WHERE tenant_id=$1 AND status IN ('OPEN','CONTAINED') AND declared_at >= $2`, a.TenantID, a.DeclaredAt.Add(-a.StormWindow)).Scan(&active); err != nil {
		return AlertIncidentResult{}, err
	}
	if active >= a.StormLimit {
		return AlertIncidentResult{}, ErrAlertStorm
	}
	meta["primary_owner"] = a.PrimaryOwner
	meta["secondary_route"] = a.SecondaryRoute
	meta["acknowledged"] = false
	scope, _ = json.Marshal(meta)
	i := OperationalIncident{TenantID: a.TenantID, IncidentID: a.IncidentID, IncidentKey: a.IncidentKey, Severity: a.Severity, ImpactRevision: 1, Scope: scope, CorrelationKey: a.CorrelationKey, EvidenceDigest: a.EvidenceDigest, DeclaredAt: a.DeclaredAt, Status: "OPEN"}
	if err := i.Validate(); err != nil {
		return AlertIncidentResult{}, err
	}
	affected, err := tx.Exec(ctx, `INSERT INTO operational_incident (tenant_id, incident_id, incident_key, severity, impact_revision, scope, correlation_key, evidence_digest, declared_at, contained_at, resolved_at, residual_risk, postmortem_ref, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT (tenant_id, incident_key) DO NOTHING`,
		i.TenantID, i.IncidentID, i.IncidentKey, i.Severity, i.ImpactRevision, i.Scope, i.CorrelationKey, i.EvidenceDigest, i.DeclaredAt, i.ContainedAt, i.ResolvedAt, i.ResidualRisk, i.PostmortemRef, i.Status)
	if err != nil {
		return AlertIncidentResult{}, err
	}
	if affected == 1 {
		return AlertIncidentResult{Incident: i, Created: true}, nil
	}
	if affected != 0 {
		return AlertIncidentResult{}, detail(ErrAlertConflict, "unexpected insert row count %d", affected)
	}
	if err := loadByKey(ctx, tx, a.TenantID, a.IncidentKey, &existing); err != nil {
		return AlertIncidentResult{}, err
	}
	if err := sameAlert(existing, a); err != nil {
		return AlertIncidentResult{}, err
	}
	return AlertIncidentResult{Incident: existing}, nil
}

// ResolveIncidentsByKeyPrefix closes every active incident owned by a
// recurring operational check. The prefix must be a stable owner-scoped key;
// tenant scoping is enforced here and again by row-level security.
func ResolveIncidentsByKeyPrefix(ctx context.Context, tx dbport.Tx, tenant uuid.UUID, prefix string, at time.Time) error {
	if tenant == uuid.Nil || strings.TrimSpace(prefix) == "" || at.IsZero() {
		return ErrMissingScope
	}
	if err := ensureTenant(ctx, tx, tenant); err != nil {
		return err
	}
	at = at.UTC()
	_, err := tx.Exec(ctx, `UPDATE operational_incident SET contained_at=COALESCE(contained_at,$3), resolved_at=$3, status='RESOLVED', impact_revision=impact_revision+1 WHERE tenant_id=$1 AND left(incident_key,length($2))=$2 AND status IN ('OPEN','CONTAINED')`, tenant, prefix, at)
	return err
}

func sameAlert(existing OperationalIncident, a AlertIncident) error {
	if existing.Severity != a.Severity || existing.CorrelationKey != a.CorrelationKey || existing.EvidenceDigest != a.EvidenceDigest || !existing.DeclaredAt.Equal(a.DeclaredAt) {
		return ErrAlertConflict
	}
	meta, err := decodeObject(existing.Scope)
	if err != nil || meta["primary_owner"] != a.PrimaryOwner || meta["secondary_route"] != a.SecondaryRoute {
		return ErrAlertConflict
	}
	for _, owned := range []string{"primary_owner", "secondary_route", "acknowledged", "acknowledged_by", "acknowledged_at", "acknowledgement_evidence"} {
		delete(meta, owned)
	}
	existingScope, err := json.Marshal(meta)
	if err != nil {
		return ErrAlertConflict
	}
	supplied, err := decodeObject(a.Scope)
	if err != nil {
		return ErrAlertConflict
	}
	suppliedScope, err := json.Marshal(supplied)
	if err != nil || string(existingScope) != string(suppliedScope) {
		return ErrAlertConflict
	}
	return nil
}

func loadByKey(ctx context.Context, q dbport.Querier, tenant uuid.UUID, key string, out *OperationalIncident) error {
	var scope []byte
	var contained, resolved *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, incident_id, incident_key, severity, impact_revision, scope, correlation_key, evidence_digest, declared_at, contained_at, resolved_at, residual_risk, postmortem_ref, status FROM operational_incident WHERE tenant_id=$1 AND incident_key=$2`, tenant, key).
		Scan(&out.TenantID, &out.IncidentID, &out.IncidentKey, &out.Severity, &out.ImpactRevision, &scope, &out.CorrelationKey, &out.EvidenceDigest, &out.DeclaredAt, &contained, &resolved, &out.ResidualRisk, &out.PostmortemRef, &out.Status)
	if err == nil {
		out.Scope, out.ContainedAt, out.ResolvedAt = scope, contained, resolved
	}
	return err
}

// AcknowledgeOperationalIncident records only operator identity and time in
// the bounded scope metadata; it never accepts or persists a message body.
func AcknowledgeOperationalIncident(ctx context.Context, tx dbport.Tx, tenant, incident uuid.UUID, actor, evidenceDigest string, at time.Time) error {
	if tenant == uuid.Nil || incident == uuid.Nil || actor == "" || !isHexDigest(evidenceDigest) || at.IsZero() || len(actor) > 256 {
		return ErrInvalidInterval
	}
	if err := ensureTenant(ctx, tx, tenant); err != nil {
		return err
	}
	var scope []byte
	if err := tx.QueryRow(ctx, `SELECT scope FROM operational_incident WHERE tenant_id=$1 AND incident_id=$2 FOR UPDATE`, tenant, incident).Scan(&scope); err != nil {
		return err
	}
	meta, err := decodeObject(scope)
	if err != nil {
		return err
	}
	owner, ok := meta["primary_owner"].(string)
	if !ok || owner != actor {
		return ErrAcknowledgementUnauthorized
	}
	acknowledged, _ := meta["acknowledged"].(bool)
	wantTime := at.UTC().Format(time.RFC3339Nano)
	if acknowledged {
		if meta["acknowledged_by"] == actor && meta["acknowledgement_evidence"] == evidenceDigest && meta["acknowledged_at"] == wantTime {
			return nil
		}
		return ErrAcknowledgementConflict
	}
	meta["acknowledged"] = true
	meta["acknowledged_by"] = actor
	meta["acknowledged_at"] = wantTime
	meta["acknowledgement_evidence"] = evidenceDigest
	b, _ := json.Marshal(meta)
	n, err := tx.Exec(ctx, `UPDATE operational_incident SET scope=$3, impact_revision=impact_revision+1 WHERE tenant_id=$1 AND incident_id=$2`, tenant, incident, b)
	if err != nil {
		return err
	}
	if n == 0 {
		return dbport.ErrNoRows
	}
	return nil
}

func isHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// CustomerSafeEvidence is intentionally free of tenant, scope and payload data.
func CustomerSafeEvidence(i OperationalIncident) string {
	return "incident=" + i.EvidenceDigest + ";severity=" + i.Severity + ";status=" + i.Status
}

func (i OperationalIncident) Validate() error {
	if i.TenantID == uuid.Nil || i.IncidentID == uuid.Nil {
		return ErrNilTenant
	}
	if i.IncidentKey == "" || i.CorrelationKey == "" {
		return detail(ErrMissingScope, "operational_incident incident_key/correlation_key")
	}
	if i.EvidenceDigest == "" {
		return detail(ErrMissingLineage, "operational_incident.evidence_digest")
	}
	if _, err := decodeObject(i.Scope); err != nil {
		return err
	}
	if i.ImpactRevision < 1 {
		return detail(ErrInvalidEnum, "operational_incident.impact_revision=%d", i.ImpactRevision)
	}
	if !oneOf(i.Severity, "SEV1", "SEV2", "SEV3", "SEV4", "SEV5") {
		return detail(ErrInvalidEnum, "operational_incident.severity=%q", i.Severity)
	}
	if !oneOf(i.Status, "OPEN", "CONTAINED", "RESOLVED", "CLOSED") {
		return detail(ErrInvalidEnum, "operational_incident.status=%q", i.Status)
	}
	if i.ContainedAt != nil && i.ContainedAt.Before(i.DeclaredAt) {
		return detail(ErrInvalidInterval, "operational_incident.contained_at precedes declared_at")
	}
	if i.ResolvedAt != nil && (i.ContainedAt == nil || i.ResolvedAt.Before(*i.ContainedAt)) {
		return detail(ErrInvalidInterval, "operational_incident resolved before containment")
	}
	if oneOf(i.Status, "RESOLVED", "CLOSED") && i.ResolvedAt == nil {
		return detail(ErrInvalidInterval, "operational_incident %s without a resolved_at", i.Status)
	}
	if i.Status == "CLOSED" && i.PostmortemRef == "" {
		return detail(ErrMissingLineage, "operational_incident closed without a postmortem reference")
	}
	return nil
}

func InsertOperationalIncident(ctx context.Context, tx dbport.Tx, i OperationalIncident) error {
	if err := i.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, i.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO operational_incident (tenant_id, incident_id, incident_key, severity, impact_revision, scope, correlation_key, evidence_digest, declared_at, contained_at, resolved_at, residual_risk, postmortem_ref, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		i.TenantID, i.IncidentID, i.IncidentKey, i.Severity, i.ImpactRevision, object(i.Scope), i.CorrelationKey, i.EvidenceDigest, i.DeclaredAt, i.ContainedAt, i.ResolvedAt, i.ResidualRisk, i.PostmortemRef, i.Status)
	return err
}

func LoadOperationalIncident(ctx context.Context, q dbport.Querier, tenantID, incidentID uuid.UUID) (OperationalIncident, error) {
	var i OperationalIncident
	var scope []byte
	var contained, resolved *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, incident_id, incident_key, severity, impact_revision, scope, correlation_key, evidence_digest, declared_at, contained_at, resolved_at, residual_risk, postmortem_ref, status FROM operational_incident WHERE tenant_id=$1 AND incident_id=$2`, tenantID, incidentID).
		Scan(&i.TenantID, &i.IncidentID, &i.IncidentKey, &i.Severity, &i.ImpactRevision, &scope, &i.CorrelationKey, &i.EvidenceDigest, &i.DeclaredAt, &contained, &resolved, &i.ResidualRisk, &i.PostmortemRef, &i.Status)
	if err != nil {
		return OperationalIncident{}, err
	}
	i.Scope = scope
	i.ContainedAt = contained
	i.ResolvedAt = resolved
	return i, nil
}

// ---------------------------------------------------------------------------
// backup_run
// ---------------------------------------------------------------------------

// BackupRun is one backup of one store at one point in time.
type BackupRun struct {
	TenantID           uuid.UUID  `json:"tenant_id"`
	RunID              uuid.UUID  `json:"run_id"`
	PolicyKey          string     `json:"policy_key"`
	Plane              string     `json:"plane"`
	StoreRef           string     `json:"store_ref"`
	BackupMode         string     `json:"backup_mode"`
	PointInTime        time.Time  `json:"point_in_time"`
	Watermark          string     `json:"watermark"`
	LocationRef        string     `json:"location_ref"`
	KeyVersion         int64      `json:"key_version"`
	ManifestDigest     string     `json:"manifest_digest"`
	StartedAt          time.Time  `json:"started_at"`
	CompletedAt        *time.Time `json:"completed_at"`
	ImmutableUntil     *time.Time `json:"immutable_until"`
	VerificationResult string     `json:"verification_result"`
	Status             string     `json:"status"`
}

func (b BackupRun) Validate() error {
	if b.TenantID == uuid.Nil || b.RunID == uuid.Nil {
		return ErrNilTenant
	}
	if b.PolicyKey == "" || b.StoreRef == "" || b.Watermark == "" {
		return detail(ErrMissingScope, "backup_run policy_key/store_ref/watermark")
	}
	if b.ManifestDigest == "" {
		return detail(ErrMissingLineage, "backup_run.manifest_digest")
	}
	if b.KeyVersion < 1 {
		return detail(ErrInvalidEnum, "backup_run.key_version=%d", b.KeyVersion)
	}
	if !oneOf(b.Plane, "DATA", "CONTROL", "OBJECT_STORE", "SEARCH") {
		return detail(ErrInvalidEnum, "backup_run.plane=%q", b.Plane)
	}
	if !oneOf(b.BackupMode, "FULL", "INCREMENTAL", "SNAPSHOT") {
		return detail(ErrInvalidEnum, "backup_run.backup_mode=%q", b.BackupMode)
	}
	if !oneOf(b.Status, "RUNNING", "COMPLETED", "FAILED", "EXPIRED") {
		return detail(ErrInvalidEnum, "backup_run.status=%q", b.Status)
	}
	if b.CompletedAt != nil && b.CompletedAt.Before(b.StartedAt) {
		return detail(ErrInvalidInterval, "backup_run.completed_at precedes started_at")
	}
	if b.Status != "RUNNING" && b.CompletedAt == nil {
		return detail(ErrInvalidInterval, "backup_run in terminal status %s without completed_at", b.Status)
	}
	if b.Status == "COMPLETED" && (b.ImmutableUntil == nil || !b.ImmutableUntil.After(*b.CompletedAt)) {
		return ErrMutableBackup
	}
	return nil
}

func InsertBackupRun(ctx context.Context, tx dbport.Tx, b BackupRun) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, b.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO backup_run (tenant_id, run_id, policy_key, plane, store_ref, backup_mode, point_in_time, watermark, location_ref, key_version, manifest_digest, started_at, completed_at, immutable_until, verification_result, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		b.TenantID, b.RunID, b.PolicyKey, b.Plane, b.StoreRef, b.BackupMode, b.PointInTime, b.Watermark, b.LocationRef, b.KeyVersion, b.ManifestDigest, b.StartedAt, b.CompletedAt, b.ImmutableUntil, b.VerificationResult, b.Status)
	return err
}

func LoadBackupRun(ctx context.Context, q dbport.Querier, tenantID, runID uuid.UUID) (BackupRun, error) {
	var b BackupRun
	var completed, immutable *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, run_id, policy_key, plane, store_ref, backup_mode, point_in_time, watermark, location_ref, key_version, manifest_digest, started_at, completed_at, immutable_until, verification_result, status FROM backup_run WHERE tenant_id=$1 AND run_id=$2`, tenantID, runID).
		Scan(&b.TenantID, &b.RunID, &b.PolicyKey, &b.Plane, &b.StoreRef, &b.BackupMode, &b.PointInTime, &b.Watermark, &b.LocationRef, &b.KeyVersion, &b.ManifestDigest, &b.StartedAt, &completed, &immutable, &b.VerificationResult, &b.Status)
	if err != nil {
		return BackupRun{}, err
	}
	b.CompletedAt = completed
	b.ImmutableUntil = immutable
	return b, nil
}

// CompleteBackupRun closes a running backup. It refuses a completion with no
// immutability horizon: a mutable backup is not a recovery source.
func CompleteBackupRun(ctx context.Context, tx dbport.Tx, tenantID, runID uuid.UUID, completedAt, immutableUntil time.Time, verification string) error {
	if tenantID == uuid.Nil || runID == uuid.Nil {
		return ErrNilTenant
	}
	if !immutableUntil.After(completedAt) {
		return ErrMutableBackup
	}
	if !oneOf(verification, "PENDING", "PASS", "FAIL", "PARTIAL", "UNKNOWN") {
		return detail(ErrInvalidEnum, "backup verification=%q", verification)
	}
	if err := ensureTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	affected, err := tx.Exec(ctx, `UPDATE backup_run SET status='COMPLETED', completed_at=$3, immutable_until=$4, verification_result=$5 WHERE tenant_id=$1 AND run_id=$2 AND status='RUNNING'`, tenantID, runID, completedAt, immutableUntil, verification)
	if err != nil {
		return err
	}
	if affected == 0 {
		return dbport.ErrNoRows
	}
	return nil
}

// ---------------------------------------------------------------------------
// recovery_run
// ---------------------------------------------------------------------------

// RecoveryRun is one exercise or execution of a recovery plan.
type RecoveryRun struct {
	TenantID            uuid.UUID  `json:"tenant_id"`
	RunID               uuid.UUID  `json:"run_id"`
	ScenarioKey         string     `json:"scenario_key"`
	PlanRef             string     `json:"plan_ref"`
	BackupRunID         *uuid.UUID `json:"backup_run_id"`
	IncidentID          *uuid.UUID `json:"incident_id"`
	RecoveryMode        string     `json:"recovery_mode"`
	IsolatedEnvironment string     `json:"isolated_environment"`
	TargetRPOSeconds    int64      `json:"target_rpo_seconds"`
	TargetRTOSeconds    int64      `json:"target_rto_seconds"`
	ActualRPOSeconds    *int64     `json:"actual_rpo_seconds"`
	ActualRTOSeconds    *int64     `json:"actual_rto_seconds"`
	ValidationResult    string     `json:"validation_result"`
	StartedAt           time.Time  `json:"started_at"`
	CompletedAt         *time.Time `json:"completed_at"`
	Status              string     `json:"status"`
}

func (r RecoveryRun) Validate() error {
	if r.TenantID == uuid.Nil || r.RunID == uuid.Nil {
		return ErrNilTenant
	}
	if r.ScenarioKey == "" || r.PlanRef == "" {
		return detail(ErrMissingScope, "recovery_run scenario_key/plan_ref")
	}
	if !oneOf(r.RecoveryMode, "RESTORE", "REBUILD", "REPLAY") {
		return detail(ErrInvalidEnum, "recovery_run.recovery_mode=%q", r.RecoveryMode)
	}
	if !oneOf(r.Status, "RUNNING", "COMPLETED", "FAILED", "ABORTED") {
		return detail(ErrInvalidEnum, "recovery_run.status=%q", r.Status)
	}
	if !oneOf(r.ValidationResult, "PENDING", "PASS", "FAIL", "PARTIAL", "UNKNOWN") {
		return detail(ErrInvalidEnum, "recovery_run.validation_result=%q", r.ValidationResult)
	}
	if r.TargetRPOSeconds < 0 || r.TargetRTOSeconds < 0 {
		return detail(ErrInvalidEnum, "recovery_run objectives must be non-negative")
	}
	if r.RecoveryMode == "RESTORE" && r.BackupRunID == nil {
		return detail(ErrMissingLineage, "recovery_run RESTORE without a backup_run_id")
	}
	if r.CompletedAt != nil && r.CompletedAt.Before(r.StartedAt) {
		return detail(ErrInvalidInterval, "recovery_run.completed_at precedes started_at")
	}
	if r.Status == "COMPLETED" {
		if r.CompletedAt == nil || r.ActualRPOSeconds == nil || r.ActualRTOSeconds == nil {
			return ErrUnvalidatedCompletion
		}
		if !oneOf(r.ValidationResult, "PASS", "PARTIAL") {
			return ErrUnvalidatedCompletion
		}
	}
	return nil
}

func InsertRecoveryRun(ctx context.Context, tx dbport.Tx, r RecoveryRun) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := ensureTenant(ctx, tx, r.TenantID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO recovery_run (tenant_id, run_id, scenario_key, plan_ref, backup_run_id, incident_id, recovery_mode, isolated_environment, target_rpo_seconds, target_rto_seconds, actual_rpo_seconds, actual_rto_seconds, validation_result, started_at, completed_at, status) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		r.TenantID, r.RunID, r.ScenarioKey, r.PlanRef, r.BackupRunID, r.IncidentID, r.RecoveryMode, r.IsolatedEnvironment, r.TargetRPOSeconds, r.TargetRTOSeconds, r.ActualRPOSeconds, r.ActualRTOSeconds, r.ValidationResult, r.StartedAt, r.CompletedAt, r.Status)
	return err
}

func LoadRecoveryRun(ctx context.Context, q dbport.Querier, tenantID, runID uuid.UUID) (RecoveryRun, error) {
	var r RecoveryRun
	var backup, incident *uuid.UUID
	var rpo, rto *int64
	var completed *time.Time
	err := q.QueryRow(ctx, `SELECT tenant_id, run_id, scenario_key, plan_ref, backup_run_id, incident_id, recovery_mode, isolated_environment, target_rpo_seconds, target_rto_seconds, actual_rpo_seconds, actual_rto_seconds, validation_result, started_at, completed_at, status FROM recovery_run WHERE tenant_id=$1 AND run_id=$2`, tenantID, runID).
		Scan(&r.TenantID, &r.RunID, &r.ScenarioKey, &r.PlanRef, &backup, &incident, &r.RecoveryMode, &r.IsolatedEnvironment, &r.TargetRPOSeconds, &r.TargetRTOSeconds, &rpo, &rto, &r.ValidationResult, &r.StartedAt, &completed, &r.Status)
	if err != nil {
		return RecoveryRun{}, err
	}
	r.BackupRunID = backup
	r.IncidentID = incident
	r.ActualRPOSeconds = rpo
	r.ActualRTOSeconds = rto
	r.CompletedAt = completed
	return r, nil
}

// CompleteRecoveryRun closes a running recovery with the actuals it achieved
// and the validation that checked them. It refuses an unvalidated completion.
func CompleteRecoveryRun(ctx context.Context, tx dbport.Tx, tenantID, runID uuid.UUID, completedAt time.Time, actualRPO, actualRTO int64, validation string) error {
	if tenantID == uuid.Nil || runID == uuid.Nil {
		return ErrNilTenant
	}
	if actualRPO < 0 || actualRTO < 0 {
		return detail(ErrInvalidEnum, "recovery actuals must be non-negative")
	}
	if !oneOf(validation, "PASS", "PARTIAL") {
		return detail(ErrUnvalidatedCompletion, "validation=%q", validation)
	}
	if err := ensureTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	affected, err := tx.Exec(ctx, `UPDATE recovery_run SET status='COMPLETED', completed_at=$3, actual_rpo_seconds=$4, actual_rto_seconds=$5, validation_result=$6 WHERE tenant_id=$1 AND run_id=$2 AND status='RUNNING'`, tenantID, runID, completedAt, actualRPO, actualRTO, validation)
	if err != nil {
		return err
	}
	if affected == 0 {
		return dbport.ErrNoRows
	}
	return nil
}
