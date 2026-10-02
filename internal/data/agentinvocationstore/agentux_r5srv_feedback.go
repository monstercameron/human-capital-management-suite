package agentinvocationstore

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// AnswerFeedback is the single current value a person has assigned to one
// delivered answer. Request rows retain the idempotency history separately.
type AnswerFeedback struct {
	InvocationID string
	OutputID     string
	PersonID     string
	Helpful      bool
	Reason       string
	Active       bool
	Revision     uint64
	UpdatedAt    time.Time
}

func (s *Store) SubmitAnswerFeedback(ctx context.Context, tenantID, personID, invocationID string, helpful bool, reason, idempotencyKey string, at time.Time) (AnswerFeedback, error) {
	return s.setAnswerFeedback(ctx, tenantID, personID, invocationID, &helpful, reason, idempotencyKey, at)
}

func (s *Store) UndoAnswerFeedback(ctx context.Context, tenantID, personID, invocationID, idempotencyKey string, at time.Time) (AnswerFeedback, error) {
	return s.setAnswerFeedback(ctx, tenantID, personID, invocationID, nil, "", idempotencyKey, at)
}

func (s *Store) setAnswerFeedback(ctx context.Context, tenantID, personID, invocationID string, helpful *bool, reason, idempotencyKey string, at time.Time) (AnswerFeedback, error) {
	tenant, err := s.resolveTenant(tenantID)
	active := helpful != nil
	if ctx == nil || err != nil || !cleanFeedbackField(personID, 512) || !cleanFeedbackField(invocationID, 512) ||
		!cleanFeedbackField(idempotencyKey, 128) || len(idempotencyKey) < 8 || !utf8.ValidString(reason) || utf8.RuneCountInString(reason) > 500 || at.IsZero() {
		return AnswerFeedback{}, ErrInvalid
	}
	var result AnswerFeedback
	err = s.db.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var outputID string
		if err := tx.QueryRow(ctx, `SELECT output_id FROM persona_reply_receipt
			WHERE tenant_id=$1 AND invocation_id=$2 AND invoker_id=$3`, tenant, invocationID, personID).Scan(&outputID); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var helpfulValue any
		if helpful != nil {
			helpfulValue = *helpful
		}
		n, err := tx.Exec(ctx, `INSERT INTO persona_answer_feedback_request
			(tenant_id,output_id,person_id,idempotency_key,helpful,reason,active,occurred_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
			tenant, outputID, personID, idempotencyKey, helpfulValue, reason, active, at.UTC())
		if err != nil {
			return err
		}
		if n == 0 {
			var replayHelpful *bool
			var replayReason string
			var replayActive bool
			if err := tx.QueryRow(ctx, `SELECT helpful,reason,active FROM persona_answer_feedback_request
				WHERE tenant_id=$1 AND output_id=$2 AND person_id=$3 AND idempotency_key=$4`, tenant, outputID, personID, idempotencyKey).
				Scan(&replayHelpful, &replayReason, &replayActive); err != nil {
				return err
			}
			if replayActive != active || replayReason != reason || replayActive && (replayHelpful == nil || helpful == nil || *replayHelpful != *helpful) {
				return ErrConflict
			}
		}
		if n == 1 {
			if _, err := tx.Exec(ctx, `INSERT INTO persona_answer_feedback
				(tenant_id,output_id,person_id,helpful,reason,active,revision,updated_at)
				VALUES ($1,$2,$3,$4,$5,$6,1,$7)
				ON CONFLICT (tenant_id,output_id,person_id) DO UPDATE SET
				helpful=EXCLUDED.helpful,reason=EXCLUDED.reason,active=EXCLUDED.active,
				revision=persona_answer_feedback.revision+1,updated_at=EXCLUDED.updated_at`,
				tenant, outputID, personID, helpfulValue, reason, active, at.UTC()); err != nil {
				return err
			}
		}
		var storedHelpful *bool
		if err := tx.QueryRow(ctx, `SELECT helpful,reason,active,revision,updated_at FROM persona_answer_feedback
			WHERE tenant_id=$1 AND output_id=$2 AND person_id=$3`, tenant, outputID, personID).
			Scan(&storedHelpful, &result.Reason, &result.Active, &result.Revision, &result.UpdatedAt); err != nil {
			return err
		}
		result.InvocationID, result.OutputID, result.PersonID = invocationID, outputID, personID
		if storedHelpful != nil {
			result.Helpful = *storedHelpful
		}
		return nil
	})
	return result, err
}

func cleanFeedbackField(value string, limit int) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= limit && utf8.ValidString(value)
}
