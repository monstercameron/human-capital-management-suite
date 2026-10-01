package application

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ReconcileRestoredAgentInstallations compares the recovered installation
// projection with the exact current version and the current core principal.
// It only suspends; restoration never creates principals or grants.
func ReconcileRestoredAgentInstallations(ctx context.Context, agents *agentstore.Store, personas *agentpersonastore.Store, core dbport.Beginner, tenant values.TenantId, tenantID uuid.UUID, now time.Time) (int, error) {
	if ctx == nil || agents == nil || personas == nil || core == nil || tenant.Validate() != nil || tenantID == uuid.Nil || now.IsZero() {
		return 0, agentstore.ErrBackupInvalid
	}
	scoped, err := personas.Scoped(tenant)
	if err != nil {
		return 0, err
	}
	orphans, err := scoped.ReconcileInstallations(ctx)
	if err != nil {
		return 0, err
	}
	rows, err := agents.RestoredInstallations(ctx, tenantID)
	if err != nil {
		return len(orphans), err
	}
	changed := len(orphans)
	for _, item := range rows {
		reason := ""
		if !item.Published {
			reason = "PERSONA_PUBLICATION_MISSING_AFTER_RESTORE"
		} else if item.PrincipalID == uuid.Nil {
			reason = "AGENT_PRINCIPAL_MISSING_AFTER_RESTORE"
		} else {
			tx, err := core.Begin(ctx)
			if err != nil {
				return changed, err
			}
			if err = tenancy.WithTenant(ctx, tx, tenantID); err != nil {
				_ = tx.Rollback(ctx)
				return changed, err
			}
			var active bool
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principal WHERE tenant_id=$1 AND principal_id=$2 AND kind IN ('SERVICE','SYSTEM') AND lifecycle='ACTIVE' AND (expires_at IS NULL OR expires_at>$3))`, tenantID, item.PrincipalID, now).Scan(&active)
			_ = tx.Rollback(ctx)
			if err != nil {
				return changed, err
			}
			if !active {
				reason = "AGENT_PRINCIPAL_RETIRED_AFTER_RESTORE"
			}
		}
		if reason != "" {
			if err = agents.SuspendRestoredInstallation(ctx, tenantID, item, reason); err != nil {
				return changed, err
			}
			changed++
		}
	}
	// This includes a suspension imported from an older backup and a process
	// that stopped after suspension before revoking the security scope.
	inactive, err := agents.InactiveRestoredInstallationIDs(ctx, tenantID)
	if err != nil {
		return changed, err
	}
	for _, id := range inactive {
		if _, err = agents.RevokePersonaSecurityScope(ctx, tenantID, agentstore.PersonaSecurityScope{Kind: "INSTALLATION", Key: id}, "INSTALLATION_INACTIVE_AFTER_RESTORE", now); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func reconcileAgentInstallationsOnStart(ctx context.Context, cfg ServeConfig, agents *agentstore.Store, personas *agentpersonastore.Store) error {
	core, err := pgxadapter.NewPool(ctx, cfg.DatabaseURL, map[string]string{"role": "hcmnext_app"})
	if err != nil {
		return fmt.Errorf("agent restore current trust owner: %w", err)
	}
	defer core.Close()
	for _, key := range cfg.ServedTenants() {
		tenant := values.TenantId(key)
		if _, err := ReconcileRestoredAgentInstallations(ctx, agents, personas, core, tenant, pgstore.TenantID(key), time.Now().UTC()); err != nil {
			return fmt.Errorf("agent restore installation reconciliation: %w", err)
		}
	}
	return nil
}
