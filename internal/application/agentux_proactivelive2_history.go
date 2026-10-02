package application

import (
	"context"
	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func (r *AgentAnnouncementRuntime) AnnouncementAttempts(ctx context.Context, tenant uuid.UUID, id string) ([]agentstore.AnnouncementOccurrence, error) {
	var attempts []agentstore.AnnouncementOccurrence
	err := r.Agents.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT occurrence_id,result,reason,message_id,attempted_at FROM agent_announcement_occurrence WHERE tenant_id=$1 AND announcement_id=$2 ORDER BY attempted_at DESC,occurrence_id DESC`, tenant, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			attempt := agentstore.AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: id}
			if err := rows.Scan(&attempt.OccurrenceID, &attempt.Result, &attempt.Reason, &attempt.MessageID, &attempt.AttemptedAt); err != nil {
				return err
			}
			attempts = append(attempts, attempt)
		}
		return rows.Err()
	})
	return attempts, err
}

// Before any manual retry, reconcile the previous write's durable receipt.
// A failed checkpoint can follow a successful Chat commit; that is success,
// not permission to create another announcement with a fresh idempotency key.
func (r *AgentAnnouncementRunner) retryCommittedAnnouncement(ctx context.Context, record agentstore.Announcement, occurrence string) (bool, error) {
	if record.LastResult != agentstore.AnnouncementFailed || record.LastOccurrence == "" {
		return false, nil
	}
	recovery, ok := r.Delivery.(interface {
		RecoverAnnouncementOccurrence(context.Context, agentstore.Announcement, string) (string, bool, error)
	})
	if !ok {
		return false, nil
	}
	var found bool
	err := r.Store.WithAnnouncementFence(ctx, record.TenantID, record.ID, func() error {
		// Reload after the fence so overlapping retries all reconcile the same receipt.
		current, err := r.Store.Get(ctx, record.TenantID, record.ID)
		if err != nil {
			return err
		}
		prior := record.LastOccurrence
		message, committed, err := recovery.RecoverAnnouncementOccurrence(ctx, current, prior)
		if err != nil || !committed {
			return err
		}
		found = true
		if current.LastResult == agentstore.AnnouncementPosted {
			return nil
		}
		_, err = r.Store.RecordOccurrence(ctx, agentstore.AnnouncementOccurrence{TenantID: current.TenantID.String(), AnnouncementID: current.ID, OccurrenceID: occurrence, Result: agentstore.AnnouncementPosted, MessageID: message, AttemptedAt: r.Now().UTC()})
		return err
	})
	return found, err
}
