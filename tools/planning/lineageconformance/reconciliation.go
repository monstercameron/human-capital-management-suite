package lineageconformance

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
)

// ReadReconciliationRecord maps all persisted reconciliation jobs watching
// effectRef to the case's single RECONCILIATION link. The effect reference is
// the committed effect's idempotency key; local graph node ids are not used
// for lookup. No row means no record, preserving UNKNOWN for an unwatched
// effect. Policy and status pairs are carried as payload, and versions in the
// watermark make a later durable advance visible to lineage readers.
func ReadReconciliationRecord(ctx context.Context, ex reconcile.Executor, tenantID uuid.UUID, caseID, effectRef string) (Record, bool, error) {
	if tenantID == uuid.Nil || strings.TrimSpace(caseID) == "" || strings.TrimSpace(effectRef) == "" {
		return Record{}, false, fmt.Errorf("lineageconformance: tenant, case and effect reference are required")
	}
	jobs, err := (reconcile.PostgresStore{}).ListForEffect(ctx, ex, tenantID, effectRef)
	if err != nil {
		return Record{}, false, fmt.Errorf("lineageconformance: read reconciliation jobs: %w", err)
	}
	if len(jobs) == 0 {
		return Record{}, false, nil
	}
	record, err := ReconciliationRecord(caseID, tenantID, effectRef, jobs)
	if err != nil {
		return Record{}, false, err
	}
	return record, true, nil
}

// WithReconciliationRecords loads the concrete RECONCILIATION evidence that
// Compile requires before it can mark that link PROVEN. effectRefs maps each
// generated case ID to the committed effect's idempotency key. Missing keys
// and effects with no persisted jobs remain absent, so Compile reports the
// link UNKNOWN with a missing-evidence finding.
func WithReconciliationRecords(ctx context.Context, ex reconcile.Executor, in Input, tenantID uuid.UUID, effectRefs map[string]string) (Input, error) {
	cases, _ := GenerateCases(CaseInput{Witnesses: in.Witnesses, Definitions: in.Definitions, Bindings: in.Bindings})
	for _, c := range cases {
		effectRef := effectRefs[c.ID]
		if strings.TrimSpace(effectRef) == "" {
			continue
		}
		record, found, err := ReadReconciliationRecord(ctx, ex, tenantID, c.ID, effectRef)
		if err != nil {
			return Input{}, err
		}
		if found {
			in.reconciliationRecords = append(in.reconciliationRecords, record)
		}
	}
	return in, nil
}

// ReconciliationRecord maps jobs returned by RECON-001 for one effect to
// one lineage link. It also validates the store's tenant/effect scope before
// publishing any job state.
func ReconciliationRecord(caseID string, tenantID uuid.UUID, effectRef string, jobs []reconcile.Job) (Record, error) {
	if tenantID == uuid.Nil || strings.TrimSpace(caseID) == "" || strings.TrimSpace(effectRef) == "" {
		return Record{}, fmt.Errorf("lineageconformance: tenant, case and effect reference are required")
	}
	if len(jobs) == 0 {
		return Record{}, fmt.Errorf("lineageconformance: at least one reconciliation job is required")
	}
	pairs := make([]string, 0, len(jobs))
	versions := make([]string, 0, len(jobs))
	for _, job := range jobs {
		if job.TenantID != tenantID || job.EffectRef != effectRef || !job.Status.Valid() {
			return Record{}, fmt.Errorf("lineageconformance: invalid reconciliation job identity or state")
		}
		pairs = append(pairs, job.PolicyRef+"="+string(job.Status))
		versions = append(versions, job.PolicyRef+"@"+fmt.Sprint(job.Version))
	}
	return Record{
		ID: caseID + "#" + string(LinkReconciliation), Tenant: tenantID.String(), Case: caseID,
		Link:      LinkReconciliation,
		Watermark: "effect_reconciliation_job:" + effectRef + "@" + strings.Join(versions, ","),
		Payload:   strings.Join(pairs, ","),
	}, nil
}
