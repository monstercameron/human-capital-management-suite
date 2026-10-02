package application

import (
	"context"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Only immutable delivery receipts can link a removed post to an accepted run.
type chatremoveRunTrace struct {
	DB         personaRunTenantTxRunner
	TenantUUID func(values.TenantId) uuid.UUID
}

func (s chatremoveRunTrace) RunForModerationPost(ctx context.Context, tenant, conversation, post string) (string, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || p.Tenant().String() != tenant {
		return "", chat.ErrPermissionDenied
	}
	if s.DB == nil || s.TenantUUID == nil || conversation == "" || post == "" {
		return "", chat.ErrUnavailable
	}
	id := s.TenantUUID(p.Tenant())
	if id == uuid.Nil {
		return "", chat.ErrPermissionDenied
	}
	var run string
	err := s.DB.RunTenantTx(ctx, id, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT f.run_id
			FROM persona_reply_receipt r JOIN persona_final_outputs f
			ON f.tenant_id=r.tenant_id AND f.output_id=r.output_id AND f.invocation_id=r.invocation_id
			WHERE r.tenant_id=$1 AND f.run_id IS NOT NULL AND
			((r.conversation_id=$2 AND r.public_post_id=$3) OR
			 (r.private_conversation_id=$2 AND r.receipt->>'PrivatePostID'=$3)) LIMIT 2`, id, conversation, post)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if run != "" {
				return chat.ErrConflict
			}
			if err = rows.Scan(&run); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		return "", err
	}
	return run, nil
}
