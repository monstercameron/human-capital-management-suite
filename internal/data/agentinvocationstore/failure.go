package agentinvocationstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PostFailure is an append-only owner-scoped outcome for an invocation that
// failed after the human post committed. It deliberately carries no error text.
type PostFailure struct {
	TenantID       string
	InvokerID      string
	ConversationID string
	ThreadID       string
	PostID         string
	Code           string
	Retryable      bool
	OccurredAt     time.Time
}

func validFailureCode(code string, retryable bool) bool {
	switch code {
	case "MODEL_UNAVAILABLE", "ADMISSION_UNAVAILABLE", "EXECUTION_UNAVAILABLE", "ANSWER_INTERRUPTED":
		return true
	case "OUTPUT_REJECTED", "DELIVERY_FAILED", "ADMISSION_REFUSED", "INVOCATION_FAILED", "DAILY_LIMIT_REACHED":
		return !retryable
	default:
		return false
	}
}

// RecordPostFailure records one sanitized outcome exactly once for its owner.
// Identical replays are harmless; identity or outcome changes conflict.
func (s *Store) RecordPostFailure(ctx context.Context, failure PostFailure) error {
	tenant, err := s.resolveTenant(failure.TenantID)
	if ctx == nil || err != nil || !validFailureCode(failure.Code, failure.Retryable) {
		return ErrInvalid
	}
	for _, field := range []string{failure.InvokerID, failure.ConversationID, failure.ThreadID, failure.PostID} {
		if field == "" || strings.TrimSpace(field) != field {
			return ErrInvalid
		}
	}
	return s.db.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO persona_invocation_failure (tenant_id,invoker_id,post_id,conversation_id,thread_id,failure_code,retryable) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (tenant_id,invoker_id,post_id) DO NOTHING`, tenant, failure.InvokerID, failure.PostID, failure.ConversationID, failure.ThreadID, failure.Code, failure.Retryable)
		if err != nil {
			return err
		}
		if n == 1 {
			return nil
		}
		var room, thread, code string
		var retryable bool
		if err := tx.QueryRow(ctx, `SELECT conversation_id,thread_id,failure_code,retryable FROM persona_invocation_failure WHERE tenant_id=$1 AND invoker_id=$2 AND post_id=$3`, tenant, failure.InvokerID, failure.PostID).Scan(&room, &thread, &code, &retryable); err != nil {
			return err
		}
		if room != failure.ConversationID || thread != failure.ThreadID || code != failure.Code || retryable != failure.Retryable {
			return ErrConflict
		}
		return nil
	})
}

// ListPostFailures reads only the exact caller and conversation within RLS.
func (s *Store) ListPostFailures(ctx context.Context, tenantID, invokerID, conversationID string) ([]PostFailure, error) {
	tenant, err := s.resolveTenant(tenantID)
	if ctx == nil || err != nil || strings.TrimSpace(invokerID) == "" || strings.TrimSpace(conversationID) == "" {
		return nil, ErrInvalid
	}
	out := make([]PostFailure, 0)
	err = s.db.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id::text,invoker_id,post_id,conversation_id,thread_id,failure_code,retryable,occurred_at FROM persona_invocation_failure WHERE tenant_id=$1 AND invoker_id=$2 AND conversation_id=$3 ORDER BY occurred_at DESC,post_id LIMIT 100`, tenant, invokerID, conversationID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var failure PostFailure
			if err := rows.Scan(&failure.TenantID, &failure.InvokerID, &failure.PostID, &failure.ConversationID, &failure.ThreadID, &failure.Code, &failure.Retryable, &failure.OccurredAt); err != nil {
				return err
			}
			failure.TenantID = tenantID
			out = append(out, failure)
		}
		return rows.Err()
	})
	return out, err
}

// LookupPostFailure exposes only an exact owner row, including for retry.
func (s *Store) LookupPostFailure(ctx context.Context, tenantID, invokerID, postID string) (PostFailure, error) {
	tenant, err := s.resolveTenant(tenantID)
	if ctx == nil || err != nil || strings.TrimSpace(invokerID) == "" || strings.TrimSpace(postID) == "" {
		return PostFailure{}, ErrInvalid
	}
	var failure PostFailure
	err = s.db.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		err := tx.QueryRow(ctx, `SELECT tenant_id::text,invoker_id,post_id,conversation_id,thread_id,failure_code,retryable,occurred_at FROM persona_invocation_failure WHERE tenant_id=$1 AND invoker_id=$2 AND post_id=$3`, tenant, invokerID, postID).Scan(&failure.TenantID, &failure.InvokerID, &failure.PostID, &failure.ConversationID, &failure.ThreadID, &failure.Code, &failure.Retryable, &failure.OccurredAt)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if err == nil {
		failure.TenantID = tenantID
	}
	return failure, err
}
