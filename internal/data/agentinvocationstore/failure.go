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
	// RetryAt is when a mention refused by a mention ceiling can be asked
	// again, and MentionsAsked how many times the person asked in the hour when
	// it was the hourly per-person ceiling (zero otherwise). Both belong to the
	// MENTION_LIMIT_REACHED outcome and to no other (AGENTP-015).
	RetryAt       time.Time
	MentionsAsked int
}

// MentionLimitCode is the outcome of a mention refused by a mention ceiling.
const MentionLimitCode = "MENTION_LIMIT_REACHED"

// NoModelCode is the outcome of a question asked of an agent on a server that
// has no model to run it (AGENTUX-078).
const NoModelCode = "SERVER_HAS_NO_MODEL"

// retryableFailureCodes are the outcomes a person may be offered a retry for;
// every other code is stored as not retryable. Together with
// finalFailureCodes this is the list the table's check constraints must match
// (TestTodo_AGENTUX_078_FailureCodesMatchConstraint).
var retryableFailureCodes = []string{"MODEL_UNAVAILABLE", "ADMISSION_UNAVAILABLE", "EXECUTION_UNAVAILABLE", "ANSWER_INTERRUPTED"}

var finalFailureCodes = []string{"OUTPUT_REJECTED", "DELIVERY_FAILED", "ADMISSION_REFUSED", "INVOCATION_FAILED", "DAILY_LIMIT_REACHED", MentionLimitCode, NoModelCode}

func validFailureCode(code string, retryable bool) bool {
	for _, known := range retryableFailureCodes {
		if code == known {
			return true
		}
	}
	for _, known := range finalFailureCodes {
		if code == known {
			return !retryable
		}
	}
	return false
}

// retryAtValue is what the retry_at column holds: the time for a mention-limit
// outcome and NULL for every other, which the table's check enforces.
func (f PostFailure) retryAtValue() any {
	if f.Code != MentionLimitCode {
		return nil
	}
	return f.RetryAt.UTC()
}

// RecordPostFailure records one sanitized outcome exactly once for its owner.
// Identical replays are harmless; identity or outcome changes conflict.
func (s *Store) RecordPostFailure(ctx context.Context, failure PostFailure) error {
	tenant, err := s.resolveTenant(failure.TenantID)
	if ctx == nil || err != nil || !validFailureCode(failure.Code, failure.Retryable) {
		return ErrInvalid
	}
	// A mention-limit outcome without the time to ask again would leave the card
	// with nothing to say; any other outcome carries neither fact.
	if (failure.Code == MentionLimitCode) == failure.RetryAt.IsZero() || failure.MentionsAsked < 0 || (failure.Code != MentionLimitCode && failure.MentionsAsked != 0) {
		return ErrInvalid
	}
	for _, field := range []string{failure.InvokerID, failure.ConversationID, failure.ThreadID, failure.PostID} {
		if field == "" || strings.TrimSpace(field) != field {
			return ErrInvalid
		}
	}
	return s.db.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `INSERT INTO persona_invocation_failure (tenant_id,invoker_id,post_id,conversation_id,thread_id,failure_code,retryable,retry_at,mentions_asked) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (tenant_id,invoker_id,post_id) DO NOTHING`, tenant, failure.InvokerID, failure.PostID, failure.ConversationID, failure.ThreadID, failure.Code, failure.Retryable, failure.retryAtValue(), failure.MentionsAsked)
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
		rows, err := tx.Query(ctx, `SELECT tenant_id::text,invoker_id,post_id,conversation_id,thread_id,failure_code,retryable,occurred_at,retry_at,mentions_asked FROM persona_invocation_failure WHERE tenant_id=$1 AND invoker_id=$2 AND conversation_id=$3 ORDER BY occurred_at DESC,post_id LIMIT 100`, tenant, invokerID, conversationID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var failure PostFailure
			var retryAt *time.Time
			if err := rows.Scan(&failure.TenantID, &failure.InvokerID, &failure.PostID, &failure.ConversationID, &failure.ThreadID, &failure.Code, &failure.Retryable, &failure.OccurredAt, &retryAt, &failure.MentionsAsked); err != nil {
				return err
			}
			if retryAt != nil {
				failure.RetryAt = retryAt.UTC()
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
		var retryAt *time.Time
		err := tx.QueryRow(ctx, `SELECT tenant_id::text,invoker_id,post_id,conversation_id,thread_id,failure_code,retryable,occurred_at,retry_at,mentions_asked FROM persona_invocation_failure WHERE tenant_id=$1 AND invoker_id=$2 AND post_id=$3`, tenant, invokerID, postID).Scan(&failure.TenantID, &failure.InvokerID, &failure.PostID, &failure.ConversationID, &failure.ThreadID, &failure.Code, &failure.Retryable, &failure.OccurredAt, &retryAt, &failure.MentionsAsked)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if retryAt != nil {
			failure.RetryAt = retryAt.UTC()
		}
		return err
	})
	if err == nil {
		failure.TenantID = tenantID
	}
	return failure, err
}
