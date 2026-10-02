package application

import (
	"context"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// WorkspaceDocumentDirectory uses the same tenant-owned worker directory as
// discovery. It does not reduce membership to one chat room's audience.
type WorkspaceDocumentDirectory struct {
	DB         dbport.Beginner
	TenantUUID func(values.TenantId) uuid.UUID
}

func (d WorkspaceDocumentDirectory) WorkspaceDocumentMembers(ctx context.Context, tenant string) ([]string, error) {
	if ctx == nil || d.DB == nil || d.TenantUUID == nil || values.TenantId(tenant).Validate() != nil {
		return nil, ErrPersonaAudienceDirectoryFactsMissing
	}
	tx, err := d.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	id := d.TenantUUID(values.TenantId(tenant))
	if id == uuid.Nil {
		return nil, ErrPersonaAudienceDirectoryFactsMissing
	}
	if err := tenancy.WithTenant(ctx, tx, id); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT worker_key FROM journey_worker WHERE tenant_id=$1 ORDER BY worker_key`, id)
	if err != nil {
		return nil, err
	}
	var out []string
	for rows.Next() {
		var member string
		if err := rows.Scan(&member); err != nil {
			rows.Close()
			return nil, err
		}
		if member == "" {
			rows.Close()
			return nil, ErrPersonaAudienceDirectoryFactsMissing
		}
		out = append(out, member)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrPersonaAudienceDirectoryFactsMissing
	}
	return out, tx.Commit(ctx)
}
