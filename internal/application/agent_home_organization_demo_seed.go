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
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	localDemoHomeOrganizationSource   = "local-demo/workforce-plan/v1"
	localDemoHomeOrganizationRevision = int64(1)
)

// ErrLocalDemoHomeOrganizationProvisioning identifies a rejected local-demo
// home-organization seed. The seed is only valid for shipped demo tenants.
var ErrLocalDemoHomeOrganizationProvisioning = errors.New("application: local-demo home organization provisioning rejected")

// LocalDemoHomeOrganizationProvisioner writes home-organization facts from a
// versioned demo workforce plan. It never derives identity from role grants.
type LocalDemoHomeOrganizationProvisioner struct {
	Directory localDemoHomeOrganizationDirectory
	Now       func() time.Time
}

type localDemoHomeOrganizationDirectory interface {
	PersonaHomeOrganizationDirectory
	ProvisionHomeOrganization(context.Context, values.TenantId, string, string, string, int64, time.Time) error
}

// NewLocalDemoHomeOrganizationProvisioner constructs a tenant-scoped demo
// home-organization provisioner.
func NewLocalDemoHomeOrganizationProvisioner(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID, now func() time.Time) *LocalDemoHomeOrganizationProvisioner {
	return &LocalDemoHomeOrganizationProvisioner{
		Directory: NewAgentHomeOrganizationDirectoryDB(db, tenantUUID),
		Now:       now,
	}
}

// LocalDemoHomeOrganizationProvisionSummary reports facts created and exact
// facts replayed during an idempotent seed.
type LocalDemoHomeOrganizationProvisionSummary struct {
	Created  int
	Existing int
}

// Provision writes one current home-organization fact per active employee in
// the tenant's explicit, versioned workforce plan. Existing exact facts are
// replay-safe; conflicting facts fail closed.
func (p *LocalDemoHomeOrganizationProvisioner) Provision(ctx context.Context, tenant string) (LocalDemoHomeOrganizationProvisionSummary, error) {
	pack, ok := demoworkforce.PackFor(strings.TrimSpace(tenant))
	if !ok {
		return LocalDemoHomeOrganizationProvisionSummary{}, fmt.Errorf("%w: tenant %q is not a shipped local-demo workforce", ErrLocalDemoHomeOrganizationProvisioning, tenant)
	}
	if p == nil || p.Directory == nil {
		return LocalDemoHomeOrganizationProvisionSummary{}, fmt.Errorf("%w: directory is required", ErrLocalDemoHomeOrganizationProvisioning)
	}
	tenantKey := values.TenantId(strings.TrimSpace(tenant))
	if err := tenantKey.Validate(); err != nil {
		return LocalDemoHomeOrganizationProvisionSummary{}, fmt.Errorf("%w: tenant: %v", ErrLocalDemoHomeOrganizationProvisioning, err)
	}
	workers, err := pack.Plan(pgstore.TenantID(string(tenantKey)))
	if err != nil {
		return LocalDemoHomeOrganizationProvisionSummary{}, fmt.Errorf("%w: plan workforce: %v", ErrLocalDemoHomeOrganizationProvisioning, err)
	}
	at := time.Now().UTC()
	if p.Now != nil {
		at = p.Now().UTC()
	}
	var summary LocalDemoHomeOrganizationProvisionSummary
	for _, worker := range workers {
		if worker.Row.LifecycleStatus != "active" || worker.Row.WorkerType != "employee" {
			continue
		}
		subject := strings.TrimSpace(worker.Row.WorkerKey)
		unit := strings.TrimSpace(worker.Organization.Code)
		if subject == "" || unit == "" {
			return LocalDemoHomeOrganizationProvisionSummary{}, fmt.Errorf("%w: plan contains an employee without an explicit home unit", ErrLocalDemoHomeOrganizationProvisioning)
		}
		organization := "org:" + pack.Key + ":" + unit
		current, readErr := p.Directory.CurrentHomeOrganization(ctx, tenantKey, subject)
		if readErr != nil && !errors.Is(readErr, ErrAgentHomeOrganizationUnavailable) {
			return LocalDemoHomeOrganizationProvisionSummary{}, fmt.Errorf("%w: read subject %s: %v", ErrLocalDemoHomeOrganizationProvisioning, subject, readErr)
		}
		existing := readErr == nil
		if existing && current != organization {
			return LocalDemoHomeOrganizationProvisionSummary{}, fmt.Errorf("%w: subject %s has home organization %q, plan requires %q", ErrLocalDemoHomeOrganizationProvisioning, subject, current, organization)
		}
		if err := p.Directory.ProvisionHomeOrganization(ctx, tenantKey, subject, organization, localDemoHomeOrganizationSource, localDemoHomeOrganizationRevision, at); err != nil {
			return LocalDemoHomeOrganizationProvisionSummary{}, fmt.Errorf("%w: subject %s: %v", ErrLocalDemoHomeOrganizationProvisioning, subject, err)
		}
		if existing {
			summary.Existing++
		} else {
			summary.Created++
		}
	}
	return summary, nil
}
