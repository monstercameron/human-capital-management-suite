package promotioninvalidation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Executor is the minimal database capability this package needs. A
// [dbport.Tx] and a [dbport.Conn] both satisfy it. The counter table carries
// tenant_isolation row-level security, so the caller must have scoped its
// transaction to a tenant (internal/data/tenancy.WithTenant) before calling
// [NextSequence], the same requirement internal/data/promotionguard's
// Executor documents.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// ErrInvalid means a required input was empty, zero, or malformed.
var ErrInvalid = errors.New("promotioninvalidation: invalid input")

// maxProjectionLen mirrors internal/transport/productquery's
// MaxProjectionNameSize bound: a projection name this package allocates a
// counter for is always one of productquery's wire projection names, so a
// value that could never appear on the wire is rejected here rather than
// silently accepted and stored.
const maxProjectionLen = 128

func normalizeProjection(projection string) (string, error) {
	trimmed := strings.TrimSpace(projection)
	if trimmed == "" || trimmed != projection || len(trimmed) > maxProjectionLen {
		return "", fmt.Errorf("%w: projection is empty, padded, or exceeds %d bytes", ErrInvalid, maxProjectionLen)
	}
	return trimmed, nil
}

// NextSequence allocates and durably records the next position in one
// tenant's counter for one invalidation projection, starting at 1.
//
// It is never a SELECT that decides the next value and a separate write that
// records it: the statement below is an INSERT .. ON CONFLICT .. DO UPDATE
// .. RETURNING that performs the allocation and the durable record in one
// database round trip, so PostgreSQL's own commit-time row lock -- not this
// function's control flow -- is what makes concurrent callers for the same
// (tenant, projection) serialize instead of racing. See
// TestTodo_PROMOUX_011_Race for the proof against genuinely concurrent
// PostgreSQL sessions, and TestTodo_PROMOUX_011_Integration for the
// sequential durability claim.
func NextSequence(ctx context.Context, ex Executor, tenantID uuid.UUID, projection string) (uint64, error) {
	if tenantID == uuid.Nil {
		return 0, fmt.Errorf("%w: tenant id is nil", ErrInvalid)
	}
	normalized, err := normalizeProjection(projection)
	if err != nil {
		return 0, err
	}
	if ex == nil {
		return 0, fmt.Errorf("%w: executor is nil", ErrInvalid)
	}

	var next uint64
	err = ex.QueryRow(ctx, `
		INSERT INTO promotion_invalidation_sequence (tenant_id, projection, next_sequence, updated_at)
		VALUES ($1, $2, 1, now())
		ON CONFLICT (tenant_id, projection)
		DO UPDATE SET next_sequence = promotion_invalidation_sequence.next_sequence + 1, updated_at = now()
		RETURNING next_sequence`,
		tenantID, normalized,
	).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("promotioninvalidation: allocate sequence: %w", err)
	}
	if next == 0 {
		// A zero return would be indistinguishable from "no sequence assigned
		// yet" to any caller that treats zero as a permissive default; the
		// CHECK constraint on next_sequence and the VALUES(...,1,...) starting
		// point make this unreachable, but the guard fails closed rather than
		// handing out a value a consumer could mistake for "current".
		return 0, fmt.Errorf("promotioninvalidation: allocated sequence was zero")
	}
	return next, nil
}
