package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrAgentHomeOrganizationUnavailable identifies an absent or ambiguous
// current home-organization fact. Callers must fail closed on this error.
var ErrAgentHomeOrganizationUnavailable = errors.New("application: home organization unavailable")

// AgentHomeOrganizationDirectoryDB reads and writes the authoritative,
// tenant-scoped home-organization fact. It never consults role visibility.
type AgentHomeOrganizationDirectoryDB struct {
	DB         dbport.Beginner
	TenantUUID func(values.TenantId) uuid.UUID
}

// NewAgentHomeOrganizationDirectoryDB constructs a home-organization
// directory over a tenant database.
func NewAgentHomeOrganizationDirectoryDB(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID) *AgentHomeOrganizationDirectoryDB {
	return &AgentHomeOrganizationDirectoryDB{DB: db, TenantUUID: tenantUUID}
}

// CurrentHomeOrganization returns exactly one current organization for a
// subject, or ErrAgentHomeOrganizationUnavailable when absent or ambiguous.
func (d *AgentHomeOrganizationDirectoryDB) CurrentHomeOrganization(ctx context.Context, tenant values.TenantId, subject string) (string, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "", fmt.Errorf("%w: subject is required", ErrAgentHomeOrganizationUnavailable)
	}
	var organizations []string
	err := d.read(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT organization_scope_id FROM agent_current_home_organization WHERE tenant_id=$1 AND subject_ref=$2 AND effective_from <= CURRENT_TIMESTAMP AND (effective_until IS NULL OR CURRENT_TIMESTAMP < effective_until) AND revoked_at IS NULL AND superseded_at IS NULL ORDER BY organization_scope_id`, tenantID, subject)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var organization string
			if err := rows.Scan(&organization); err != nil {
				return err
			}
			organization = strings.TrimSpace(organization)
			if organization == "" {
				return fmt.Errorf("%w: empty organization authority", ErrAgentHomeOrganizationUnavailable)
			}
			organizations = append(organizations, organization)
		}
		return rows.Err()
	})
	if err != nil {
		return "", fmt.Errorf("read current home organization: %w", err)
	}
	if len(organizations) != 1 {
		return "", fmt.Errorf("%w: expected one current organization, found %d", ErrAgentHomeOrganizationUnavailable, len(organizations))
	}
	return organizations[0], nil
}

// ProvisionHomeOrganization records a new current fact. An identical current
// fact is idempotent; a lower or equal revision with different content is
// rejected, and a higher revision supersedes the prior fact.
func (d *AgentHomeOrganizationDirectoryDB) ProvisionHomeOrganization(ctx context.Context, tenant values.TenantId, subject, organization, source string, revision int64, effectiveFrom time.Time) error {
	subject, organization, source = strings.TrimSpace(subject), strings.TrimSpace(organization), strings.TrimSpace(source)
	if subject == "" || organization == "" || source == "" || revision < 1 || effectiveFrom.IsZero() {
		return fmt.Errorf("%w: incomplete home-organization fact", ErrAgentHomeOrganizationUnavailable)
	}
	if effectiveFrom.After(time.Now().UTC()) {
		return fmt.Errorf("%w: future effective time requires scheduled activation", ErrAgentHomeOrganizationUnavailable)
	}
	return d.write(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		// Serialize first assertions for the same tenant and subject as well as
		// revisions of an existing fact; the partial unique index is the final
		// integrity backstop, not the normal concurrency path.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2, 0))`, tenantID, subject); err != nil {
			return fmt.Errorf("lock home-organization revision: %w", err)
		}
		var currentID uuid.UUID
		var currentOrganization, currentSource string
		var currentRevision int64
		var currentEffectiveFrom time.Time
		err := tx.QueryRow(ctx, `SELECT fact_id, organization_scope_id, source_identity, revision, effective_from FROM agent_current_home_organization WHERE tenant_id=$1 AND subject_ref=$2 AND revoked_at IS NULL AND superseded_at IS NULL FOR UPDATE`, tenantID, subject).Scan(&currentID, &currentOrganization, &currentSource, &currentRevision, &currentEffectiveFrom)
		if err == nil {
			if currentOrganization == organization && currentSource == source && currentRevision == revision && currentEffectiveFrom.Equal(effectiveFrom) {
				return nil
			}
			if currentSource != source {
				return fmt.Errorf("%w: source identity %q cannot replace current source %q without an explicit transfer", ErrAgentHomeOrganizationUnavailable, source, currentSource)
			}
			if revision <= currentRevision {
				return fmt.Errorf("%w: revision %d does not advance current revision %d", ErrAgentHomeOrganizationUnavailable, revision, currentRevision)
			}
			if effectiveFrom.Before(currentEffectiveFrom) {
				return fmt.Errorf("%w: effective time %s precedes current effective time %s", ErrAgentHomeOrganizationUnavailable, effectiveFrom.UTC().Format(time.RFC3339), currentEffectiveFrom.UTC().Format(time.RFC3339))
			}
		} else if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		factID := uuid.New()
		if err == nil {
			if _, err := tx.Exec(ctx, `UPDATE agent_current_home_organization SET superseded_at=GREATEST($3, recorded_at, CURRENT_TIMESTAMP), superseded_by=$4 WHERE tenant_id=$1 AND fact_id=$2`, tenantID, currentID, effectiveFrom, factID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO agent_current_home_organization (tenant_id,fact_id,subject_ref,organization_scope_id,source_identity,revision,effective_from) VALUES ($1,$2,$3,$4,$5,$6,$7)`, tenantID, factID, subject, organization, source, revision, effectiveFrom)
		return err
	})
}

// RevokeHomeOrganization closes the current fact with explicit provenance.
func (d *AgentHomeOrganizationDirectoryDB) RevokeHomeOrganization(ctx context.Context, tenant values.TenantId, subject, revokedBy, reason string, at time.Time) error {
	subject, revokedBy, reason = strings.TrimSpace(subject), strings.TrimSpace(revokedBy), strings.TrimSpace(reason)
	if subject == "" || revokedBy == "" || reason == "" || at.IsZero() {
		return fmt.Errorf("%w: incomplete home-organization revocation", ErrAgentHomeOrganizationUnavailable)
	}
	if at.After(time.Now().UTC()) {
		return fmt.Errorf("%w: future revocation time requires scheduled activation", ErrAgentHomeOrganizationUnavailable)
	}
	return d.write(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		count, err := tx.Exec(ctx, `UPDATE agent_current_home_organization SET revoked_at=GREATEST($3, recorded_at, CURRENT_TIMESTAMP), revoked_by=$4, revocation_reason=$5 WHERE tenant_id=$1 AND subject_ref=$2 AND revoked_at IS NULL AND superseded_at IS NULL`, tenantID, subject, at, revokedBy, reason)
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("%w: current fact is absent or ambiguous", ErrAgentHomeOrganizationUnavailable)
		}
		return nil
	})
}

func (d *AgentHomeOrganizationDirectoryDB) read(ctx context.Context, tenant values.TenantId, fn func(dbport.Tx, uuid.UUID) error) error {
	return d.withTx(ctx, tenant, fn, false)
}

func (d *AgentHomeOrganizationDirectoryDB) write(ctx context.Context, tenant values.TenantId, fn func(dbport.Tx, uuid.UUID) error) error {
	return d.withTx(ctx, tenant, fn, true)
}

func (d *AgentHomeOrganizationDirectoryDB) withTx(ctx context.Context, tenant values.TenantId, fn func(dbport.Tx, uuid.UUID) error, commit bool) error {
	if d == nil || d.DB == nil || d.TenantUUID == nil || tenant.Validate() != nil {
		return ErrAgentHomeOrganizationUnavailable
	}
	tenantID := d.TenantUUID(tenant)
	if tenantID == uuid.Nil {
		return ErrAgentHomeOrganizationUnavailable
	}
	tx, err := d.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin home-organization transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return fmt.Errorf("scope home-organization transaction: %w", err)
	}
	if err := fn(tx, tenantID); err != nil {
		return err
	}
	if commit {
		return tx.Commit(ctx)
	}
	return nil
}
