package chatstore

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3794. An offer to hand one linked work item's next step to another
// person, and that person's answer. The work itself stays in the project
// service; the row keeps only who offered what to whom, the note, and the owner
// and deadline the offer was made against.

const (
	HandoffOffered   = "OFFERED"
	HandoffAccepted  = "ACCEPTED"
	HandoffDeclined  = "DECLINED"
	HandoffWithdrawn = "WITHDRAWN"

	// HandoffFollowUp asks the person to take the next step; the work keeps its
	// owner. HandoffTransfer asks the person to become the work's owner.
	HandoffFollowUp = "FOLLOW_UP"
	HandoffTransfer = "TRANSFER"

	WorkHandoffActionMax = 200
	WorkHandoffNoteMax   = 1000
)

// WorkHandoff is one offer and, once made, its answer.
type WorkHandoff struct {
	TenantID, ID, ConversationID, SourcePostID string
	ProjectID, TaskID, Mode                    string
	FromID, ToID, NextAction, Note             string
	OwnerAtOffer, DueAtOffer                   string
	TaskRevision                               int64
	IdempotencyKey, Status                     string
	OfferedAt, DecidedAt                       time.Time
	DecidedBy                                  string
}

const workHandoffColumns = `tenant_id,id,conversation_id,source_post_id,project_id,task_id,mode,from_id,to_id,next_action,note,owner_at_offer,due_at_offer,task_revision,idempotency_key,status,offered_at,decided_by,decided_at`

func scanWorkHandoff(rows dbport.Rows) (WorkHandoff, error) {
	var h WorkHandoff
	var decided *time.Time
	if err := rows.Scan(&h.TenantID, &h.ID, &h.ConversationID, &h.SourcePostID, &h.ProjectID, &h.TaskID, &h.Mode, &h.FromID, &h.ToID, &h.NextAction, &h.Note,
		&h.OwnerAtOffer, &h.DueAtOffer, &h.TaskRevision, &h.IdempotencyKey, &h.Status, &h.OfferedAt, &h.DecidedBy, &decided); err != nil {
		return WorkHandoff{}, err
	}
	if decided != nil {
		h.DecidedAt = *decided
	}
	return h, nil
}

func validWorkHandoff(h WorkHandoff) bool {
	return h.TenantID != "" && strings.TrimSpace(h.ID) != "" && strings.TrimSpace(h.ConversationID) != "" && strings.TrimSpace(h.ProjectID) != "" &&
		strings.TrimSpace(h.TaskID) != "" && (h.Mode == HandoffFollowUp || h.Mode == HandoffTransfer) &&
		strings.TrimSpace(h.FromID) != "" && strings.TrimSpace(h.ToID) != "" && h.FromID != h.ToID &&
		strings.TrimSpace(h.NextAction) != "" && utf8.RuneCountInString(h.NextAction) <= WorkHandoffActionMax && utf8.RuneCountInString(h.Note) <= WorkHandoffNoteMax &&
		strings.TrimSpace(h.IdempotencyKey) != ""
}

func sameWorkHandoffAsk(a, b WorkHandoff) bool {
	return a.ConversationID == b.ConversationID && a.ProjectID == b.ProjectID && a.TaskID == b.TaskID && a.Mode == b.Mode && a.ToID == b.ToID &&
		a.NextAction == b.NextAction && a.Note == b.Note
}

// OfferWorkHandoff records an offer. It reports created=false and returns the
// standing offer when the same attempt is made again, or when an offer of the
// same kind for the same work to the same person is still open, so a retry or a
// second click never makes a second offer.
func (s *Store) OfferWorkHandoff(ctx context.Context, h WorkHandoff) (WorkHandoff, bool, error) {
	if !validWorkHandoff(h) {
		return WorkHandoff{}, false, chat.ErrInvalidArgument
	}
	if h.OfferedAt.IsZero() {
		h.OfferedAt = time.Now().UTC()
	}
	var out WorkHandoff
	created := false
	err := s.RunTenantTx(ctx, h.TenantID, func(tx dbport.Tx) error {
		if prior, ok, err := queryOneWorkHandoff(ctx, tx, `SELECT `+workHandoffColumns+` FROM chat_work_handoff WHERE tenant_id=$1 AND from_id=$2 AND idempotency_key=$3`, h.TenantID, h.FromID, h.IdempotencyKey); err != nil {
			return err
		} else if ok {
			if !sameWorkHandoffAsk(prior, h) {
				return chat.ErrInvalidArgument
			}
			out = prior
			return nil
		}
		if prior, ok, err := queryOneWorkHandoff(ctx, tx, `SELECT `+workHandoffColumns+` FROM chat_work_handoff WHERE tenant_id=$1 AND project_id=$2 AND task_id=$3 AND to_id=$4 AND mode=$5 AND status='OFFERED'`, h.TenantID, h.ProjectID, h.TaskID, h.ToID, h.Mode); err != nil {
			return err
		} else if ok {
			out = prior
			return nil
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chat_work_handoff(`+workHandoffColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'OFFERED',$16,'',NULL)`,
			h.TenantID, h.ID, h.ConversationID, h.SourcePostID, h.ProjectID, h.TaskID, h.Mode, h.FromID, h.ToID, h.NextAction, h.Note, h.OwnerAtOffer, h.DueAtOffer, h.TaskRevision, h.IdempotencyKey, h.OfferedAt); err != nil {
			return err
		}
		h.Status = HandoffOffered
		out, created = h, true
		return nil
	})
	return out, created, err
}

func queryOneWorkHandoff(ctx context.Context, tx dbport.Tx, query string, args ...any) (WorkHandoff, bool, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return WorkHandoff{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return WorkHandoff{}, false, rows.Err()
	}
	h, err := scanWorkHandoff(rows)
	return h, err == nil, err
}

// GetWorkHandoff reads one offer of a tenant.
func (s *Store) GetWorkHandoff(ctx context.Context, tenant, id string) (WorkHandoff, bool, error) {
	var out WorkHandoff
	found := false
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		out, found, err = queryOneWorkHandoff(ctx, tx, `SELECT `+workHandoffColumns+` FROM chat_work_handoff WHERE tenant_id=$1 AND id=$2`, tenant, id)
		return err
	})
	return out, found, err
}

// ListWorkHandoffs lists the offers of one conversation that the person made or
// was offered, newest first. Nobody else's offers are returned.
func (s *Store) ListWorkHandoffs(ctx context.Context, tenant, conversation, person string, limit int) ([]WorkHandoff, error) {
	if limit < 1 || limit > 200 {
		limit = 200
	}
	var out []WorkHandoff
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+workHandoffColumns+` FROM chat_work_handoff WHERE tenant_id=$1 AND conversation_id=$2 AND (from_id=$3 OR to_id=$3) ORDER BY offered_at DESC, id LIMIT $4`, tenant, conversation, person, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			h, err := scanWorkHandoff(rows)
			if err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	})
	return out, err
}

// DecideWorkHandoff answers an open offer. Only an offer that is still open
// changes; changed=false returns the answer already standing.
func (s *Store) DecideWorkHandoff(ctx context.Context, tenant, id, status, by string, at time.Time) (WorkHandoff, bool, error) {
	if status != HandoffAccepted && status != HandoffDeclined && status != HandoffWithdrawn || strings.TrimSpace(by) == "" || tenant == "" || id == "" {
		return WorkHandoff{}, false, chat.ErrInvalidArgument
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	var out WorkHandoff
	changed := false
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE chat_work_handoff SET status=$3, decided_by=$4, decided_at=$5 WHERE tenant_id=$1 AND id=$2 AND status='OFFERED'`, tenant, id, status, by, at)
		if err != nil {
			return err
		}
		changed = n == 1
		out, _, err = queryOneWorkHandoff(ctx, tx, `SELECT `+workHandoffColumns+` FROM chat_work_handoff WHERE tenant_id=$1 AND id=$2`, tenant, id)
		return err
	})
	return out, changed, err
}
