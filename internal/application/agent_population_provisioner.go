package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	localDemoPopulationID = "employees"
	localDemoSource       = "local-demo/workforce-plan/v1"
)

var localDemoPopulationNamespace = uuid.MustParse("2a0c16f8-cf0f-4db5-93be-2f34d5b3c1f7")

// ErrLocalDemoPopulationProvisioning identifies a rejected local-demo seed.
var ErrLocalDemoPopulationProvisioning = errors.New("application: local-demo population provisioning rejected")

// LocalDemoPopulationProvisioner writes the exact employee population facts
// declared by a shipped demo workforce plan. It is intentionally a narrow
// development fixture and refuses tenants without a known demo pack.
type LocalDemoPopulationProvisioner struct {
	DB         dbport.Beginner
	TenantUUID func(values.TenantId) uuid.UUID
	Now        func() time.Time
}

// NewLocalDemoPopulationProvisioner constructs a tenant-scoped provisioner.
func NewLocalDemoPopulationProvisioner(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID, now func() time.Time) *LocalDemoPopulationProvisioner {
	return &LocalDemoPopulationProvisioner{DB: db, TenantUUID: tenantUUID, Now: now}
}

// LocalDemoPopulationProvisionSummary reports an idempotent population seed.
type LocalDemoPopulationProvisionSummary struct {
	Created  int
	Existing int
}

// Provision writes one current employees fact per active employee in the
// tenant's versioned workforce plan. Existing authority is never overwritten.
func (p *LocalDemoPopulationProvisioner) Provision(ctx context.Context, tenant string) (LocalDemoPopulationProvisionSummary, error) {
	pack, ok := demoworkforce.PackFor(tenant)
	if !ok {
		return LocalDemoPopulationProvisionSummary{}, fmt.Errorf("%w: tenant %q is not a shipped local-demo workforce", ErrLocalDemoPopulationProvisioning, tenant)
	}
	if p == nil || p.DB == nil || p.TenantUUID == nil {
		return LocalDemoPopulationProvisionSummary{}, fmt.Errorf("%w: database and tenant mapper are required", ErrLocalDemoPopulationProvisioning)
	}
	tenantKey := values.TenantId(strings.TrimSpace(tenant))
	if err := tenantKey.Validate(); err != nil {
		return LocalDemoPopulationProvisionSummary{}, fmt.Errorf("%w: tenant: %v", ErrLocalDemoPopulationProvisioning, err)
	}
	tenantID := p.TenantUUID(tenantKey)
	if tenantID == uuid.Nil {
		return LocalDemoPopulationProvisionSummary{}, fmt.Errorf("%w: tenant UUID is required", ErrLocalDemoPopulationProvisioning)
	}
	workers, err := pack.Plan(tenantID)
	if err != nil {
		return LocalDemoPopulationProvisionSummary{}, fmt.Errorf("%w: plan workforce: %v", ErrLocalDemoPopulationProvisioning, err)
	}
	tx, err := p.DB.Begin(ctx)
	if err != nil {
		return LocalDemoPopulationProvisionSummary{}, fmt.Errorf("begin local-demo population seed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return LocalDemoPopulationProvisionSummary{}, fmt.Errorf("scope local-demo population seed: %w", err)
	}
	at := time.Now().UTC()
	if p.Now != nil {
		at = p.Now().UTC()
	}
	var summary LocalDemoPopulationProvisionSummary
	for _, worker := range workers {
		if worker.Row.LifecycleStatus != "active" || worker.Row.WorkerType != "employee" {
			continue
		}
		if err := provisionEmployeePopulation(ctx, tx, tenantID, worker.Row.WorkerKey, at, &summary); err != nil {
			return LocalDemoPopulationProvisionSummary{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return LocalDemoPopulationProvisionSummary{}, fmt.Errorf("commit local-demo population seed: %w", err)
	}
	return summary, nil
}

func provisionEmployeePopulation(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, subject string, at time.Time, summary *LocalDemoPopulationProvisionSummary) error {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return fmt.Errorf("%w: employee subject is required", ErrLocalDemoPopulationProvisioning)
	}
	membershipID := uuid.NewSHA1(localDemoPopulationNamespace, []byte(tenantID.String()+"\x00"+subject))
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, tenantID.String()+":"+subject); err != nil {
		return fmt.Errorf("lock population authority for %s: %w", subject, err)
	}
	rows, err := tx.Query(ctx, `SELECT population_id,source_identity,revision,revoked_at IS NOT NULL,superseded_at IS NOT NULL,effective_from <= CURRENT_TIMESTAMP AND (effective_until IS NULL OR CURRENT_TIMESTAMP < effective_until) FROM agent_current_population WHERE tenant_id=$1 AND subject_ref=$2 ORDER BY recorded_at DESC FOR UPDATE`, tenantID, subject)
	if err != nil {
		return fmt.Errorf("read existing population authority for %s: %w", subject, err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var population, source string
		var revision int64
		var revoked, superseded, effective bool
		if err := rows.Scan(&population, &source, &revision, &revoked, &superseded, &effective); err != nil {
			return fmt.Errorf("scan existing population authority for %s: %w", subject, err)
		}
		if revoked || superseded {
			continue
		}
		if !effective {
			return fmt.Errorf("%w: subject %s has out-of-window population authority", ErrLocalDemoPopulationProvisioning, subject)
		}
		if found || population != localDemoPopulationID || source != localDemoSource || revision != 1 {
			return fmt.Errorf("%w: subject %s has conflicting current population authority", ErrLocalDemoPopulationProvisioning, subject)
		}
		found = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read existing population authority for %s: %w", subject, err)
	}
	if found {
		summary.Existing++
		return nil
	}
	inserted, err := tx.Exec(ctx, `INSERT INTO agent_current_population (tenant_id,membership_id,subject_ref,population_id,source_identity,revision,effective_from) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (tenant_id,membership_id) DO NOTHING`, tenantID, membershipID, subject, localDemoPopulationID, localDemoSource, int64(1), at)
	if err != nil {
		return fmt.Errorf("write employee population authority for %s: %w", subject, err)
	}
	if inserted == 0 {
		var population, source string
		var revision int64
		var revoked, superseded, effective bool
		if err := tx.QueryRow(ctx, `SELECT population_id,source_identity,revision,revoked_at IS NOT NULL,superseded_at IS NOT NULL,effective_from <= CURRENT_TIMESTAMP AND (effective_until IS NULL OR CURRENT_TIMESTAMP < effective_until) FROM agent_current_population WHERE tenant_id=$1 AND membership_id=$2`, tenantID, membershipID).Scan(&population, &source, &revision, &revoked, &superseded, &effective); err != nil {
			return fmt.Errorf("verify concurrent population authority for %s: %w", subject, err)
		}
		if population != localDemoPopulationID || source != localDemoSource || revision != 1 || revoked || superseded || !effective {
			return fmt.Errorf("%w: subject %s has conflicting concurrent population authority", ErrLocalDemoPopulationProvisioning, subject)
		}
		summary.Existing++
		return nil
	}
	summary.Created++
	return nil
}
