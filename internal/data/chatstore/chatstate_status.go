package chatstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

const chatstateColumns = `SELECT tenant_id,id,lifecycle,settings_revision,status_changed_by,status_changed_at,status_reason,status_until,name FROM chat_conversation`
const chatstateRead = chatstateColumns + ` WHERE tenant_id=$1 AND id=$2`

func chatstateScan(row dbport.Row) (chat.ChannelStatus, error) {
	var status chat.ChannelStatus
	err := row.Scan(&status.TenantID, &status.ConversationID, &status.Status, &status.Revision, &status.ChangedBy, &status.ChangedAt, &status.Reason, &status.Until, &status.Name)
	if errors.Is(err, dbport.ErrNoRows) {
		err = chat.ErrNotFound
	}
	return status, err
}

func (s *Store) ReadChannelStatus(ctx context.Context, tenantID, id string) (chat.ChannelStatus, error) {
	var status chat.ChannelStatus
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var err error
		status, err = chatstateScan(tx.QueryRow(ctx, chatstateRead, tenantID, id))
		return err
	})
	return status, err
}

func (s *Store) CommitChannelStatus(ctx context.Context, r chat.ChangeChannelStatusRequest, at time.Time, recheck func(context.Context, chat.ChannelStatus) error) (chat.ChannelStatus, error) {
	var result chat.ChannelStatus
	if r.Principal.TenantID != r.TenantID || r.Principal.SubjectID == "" {
		return result, chat.ErrPermissionDenied
	}
	if recheck == nil || r.TenantID == "" || r.ConversationID == "" || r.ExpectedRevision == 0 || strings.TrimSpace(r.Reason) == "" || len(r.Reason) > 2000 || at.IsZero() {
		return result, chat.ErrInvalidArgument
	}
	valid := false
	for _, rule := range chatpolicy.StatusRegistry() {
		if rule.Status == r.Status {
			valid = true
		}
	}
	if !valid || (r.Until != nil && (r.Status != chatpolicy.StatusLocked || !r.Until.After(at))) {
		return result, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, r.TenantID, func(tx dbport.Tx) error {
		if err := fenceContextWrite(ctx, tx, r.TenantID, r.ConversationID); err != nil {
			return err
		}
		current, err := chatstateScan(tx.QueryRow(ctx, chatstateRead+` FOR UPDATE`, r.TenantID, r.ConversationID))
		if err != nil {
			return err
		}
		if current.Revision != r.ExpectedRevision {
			return chat.ErrConflict
		}
		if err = recheck(ctx, current); err != nil {
			return err
		}
		if r.Status == chatpolicy.StatusArchived {
			if current.Effective(at) == chatpolicy.StatusLocked {
				return chat.ErrChannelHeld
			}
			var held bool
			err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_record_inventory i JOIN chat_record_hold h ON h.tenant_id=i.tenant_id AND h.hold_id IN (SELECT jsonb_array_elements_text(i.hold_ids)) WHERE i.tenant_id=$1 AND (i.conversation_id=$2 OR i.record_id='conversation:'||$2) AND h.released_at IS NULL)`, r.TenantID, r.ConversationID).Scan(&held)
			if err != nil {
				return err
			}
			if held {
				return chat.ErrChannelHeld
			}
		}
		result = chat.ChannelStatus{TenantID: r.TenantID, ConversationID: r.ConversationID, Status: r.Status, Revision: current.Revision + 1, ChangedBy: r.Principal.SubjectID, ChangedAt: &at, Reason: r.Reason, Until: r.Until}
		result.Name = current.Name
		return chatstateWrite(ctx, tx, result, r.Principal.TenantID)
	})
	return result, err
}

func chatstateWrite(ctx context.Context, tx dbport.Tx, status chat.ChannelStatus, actorHome string) error {
	_, err := tx.Exec(ctx, `UPDATE chat_conversation SET lifecycle=$3,settings_revision=$4,status_changed_by=$5,status_changed_at=$6,status_reason=$7,status_until=$8 WHERE tenant_id=$1 AND id=$2`, status.TenantID, status.ConversationID, status.Status, status.Revision, status.ChangedBy, status.ChangedAt, status.Reason, status.Until)
	if err != nil {
		return err
	}
	// The outbox event is the durable system line. Clients render it from the
	// localized status label and actor, never from a forged ordinary post.
	return chatstateEvent(ctx, tx, status, actorHome)
}

// SweepChannelStatuses is tenant scoped and bounded; read enforcement does not
// depend on the sweep being available or on a background job running on time.
func (s *Store) SweepChannelStatuses(ctx context.Context, tenantID string, at time.Time) (int, error) {
	count := 0
	if tenantID == "" || at.IsZero() {
		return count, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, chatstateColumns+` WHERE tenant_id=$1 AND lifecycle='LOCKED' AND status_until <= $2 ORDER BY id LIMIT 100 FOR UPDATE SKIP LOCKED`, tenantID, at)
		if err != nil {
			return err
		}
		statuses := []chat.ChannelStatus{}
		for rows.Next() {
			status, err := chatstateScan(rows)
			if err != nil {
				rows.Close()
				return err
			}
			statuses = append(statuses, status)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, status := range statuses {
			if err = fenceContextWrite(ctx, tx, status.TenantID, status.ConversationID); err != nil {
				return err
			}
			status.Status, status.Until, status.Revision = chatpolicy.StatusOpen, nil, status.Revision+1
			status.ChangedBy, status.ChangedAt, status.Reason = "system", &at, "Scheduled lock ended"
			if err = chatstateWrite(ctx, tx, status, tenantID); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

var _ chat.ChannelStatusStore = (*Adapter)(nil)
