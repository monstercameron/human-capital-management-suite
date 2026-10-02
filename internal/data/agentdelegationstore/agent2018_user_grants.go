package agentdelegationstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// UserGrant is one active run-bound grant of a user, as the user's own access
// page lists it. It carries no authority material.
type UserGrant struct {
	GrantID   string
	TaskID    string
	Purpose   string
	Skills    []string
	ExpiresAt time.Time
}

func (s *Store) userTx(ctx context.Context, tenant values.TenantId) (dbport.Tx, uuid.UUID, error) {
	if s == nil || s.db == nil || s.tenantUUID == nil || ctx == nil {
		return nil, uuid.Nil, fmt.Errorf("%w: nil store or context", ErrInvalid)
	}
	if err := tenant.Validate(); err != nil {
		return nil, uuid.Nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	id := s.tenantUUID(tenant)
	if id == uuid.Nil {
		return nil, uuid.Nil, fmt.Errorf("%w: unknown tenant %q", ErrInvalid, tenant)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("agentdelegationstore: begin: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, id); err != nil {
		_ = tx.Rollback(ctx)
		return nil, uuid.Nil, err
	}
	return tx, id, nil
}

// ActiveUserGrants lists the user's grants that are not revoked and have not
// expired at the given time, soonest expiry first.
func (s *Store) ActiveUserGrants(ctx context.Context, tenant values.TenantId, userID string, at time.Time) ([]UserGrant, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("%w: user is required", ErrInvalid)
	}
	tx, id, err := s.userTx(ctx, tenant)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT grant_id,task_id,purpose,skills,expires_at FROM agent_delegation_grant
		WHERE tenant_id=$1 AND user_id=$2 AND NOT revoked AND not_before<=$3 AND expires_at>$3 ORDER BY expires_at,grant_id`, id, userID, at.UTC())
	if err != nil {
		return nil, fmt.Errorf("agentdelegationstore: list user grants: %w", err)
	}
	defer rows.Close()
	var out []UserGrant
	for rows.Next() {
		var grant UserGrant
		if err := rows.Scan(&grant.GrantID, &grant.TaskID, &grant.Purpose, &grant.Skills, &grant.ExpiresAt); err != nil {
			return nil, fmt.Errorf("agentdelegationstore: scan user grant: %w", err)
		}
		grant.ExpiresAt = grant.ExpiresAt.UTC()
		out = append(out, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentdelegationstore: read user grants: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// RevokeUserTaskGrants revokes every active grant of one task that belongs to
// the user and returns how many it revoked. A task of another user matches
// nothing, so it is indistinguishable from an unknown task. The runner reads
// the grant's revoked flag before every step, so the next step is refused.
func (s *Store) RevokeUserTaskGrants(ctx context.Context, tenant values.TenantId, userID, taskID, reason string) (int64, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(taskID) == "" || strings.TrimSpace(reason) == "" {
		return 0, fmt.Errorf("%w: user, task and reason are required", ErrInvalid)
	}
	tx, id, err := s.userTx(ctx, tenant)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	affected, err := tx.Exec(ctx, `UPDATE agent_delegation_grant
		SET revoked=true, revoked_reason=$4, revoked_at=now(), authority=jsonb_set(authority,'{Revoked}','true')
		WHERE tenant_id=$1 AND user_id=$2 AND task_id=$3 AND NOT revoked`, id, userID, taskID, reason)
	if err != nil {
		return 0, fmt.Errorf("agentdelegationstore: revoke user task grants: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("agentdelegationstore: commit user task revoke: %w", err)
	}
	return affected, nil
}
