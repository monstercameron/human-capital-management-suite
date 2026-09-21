// Package budgetfacts is Unit 4's production, database-backed
// internal/domains/promotion/snapshot.BudgetFacts adapter: the read half of
// "simulate the approved proposal against the pool the reservation would be
// taken against," over the DB-010 workforce_budget table read through
// internal/data/aggregates.
//
// There is no judgment here. A scope with no pool, a pool of another type
// or unit, or a tenant this reader has no physical mapping for is reported
// as absent, never guessed at or defaulted. The snapshot records the
// withhold with its reason instead of simulating on a lie.
package budgetfacts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/budget"
	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// storedQuantityScale is the exact-decimal scale the aggregates budget
// table stores quantities at; it matches the reservation writer's own
// parsing of the same column rather than assuming a per-unit default.
const storedQuantityScale = 4

// Reader is the snapshot.BudgetFacts adapter.
type Reader struct {
	// DB opens the tenant-scoped, read-only transaction each read runs
	// inside. Every read below opens its own transaction and rolls it back:
	// this port never writes.
	DB dbport.Beginner
	// TenantUUID resolves the domain's logical tenant key to the physical
	// tenant UUID the aggregates tables key on.
	TenantUUID func(values.TenantId) uuid.UUID
}

var _ promosnapshot.BudgetFacts = Reader{}

// CompensationBudgetAt implements snapshot.BudgetFacts over the workforce
// budget pool held against the query scope.
//
// The pool identity is the same deterministic derivation the reservation
// writer holds against (demoworkforce.BudgetID over the scope), so the
// simulation always weighs the raise against the pool the commit would
// reserve from -- never a same-named stranger. The row's own scope and
// period are verified against the query before anything is returned.
func (r Reader) CompensationBudgetAt(ctx context.Context, q promosnapshot.BudgetQuery) (budget.BudgetAuthorityRef, bool, error) {
	if r.DB == nil || r.TenantUUID == nil {
		return budget.BudgetAuthorityRef{}, false, fmt.Errorf("budgetfacts: reader has no database and tenant resolver configured")
	}
	tenantID := r.TenantUUID(q.Tenant)
	if tenantID == uuid.Nil {
		return budget.BudgetAuthorityRef{}, false, nil
	}
	businessAt := q.AsOf.Time()

	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return budget.BudgetAuthorityRef{}, false, fmt.Errorf("budgetfacts: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return budget.BudgetAuthorityRef{}, false, fmt.Errorf("budgetfacts: scope tenant: %w", err)
	}

	pool, err := (aggregates.CompensationStore{}).CurrentWorkforceBudget(ctx, tx, tenantID, demoworkforce.BudgetID(q.Scope), businessAt)
	if err != nil {
		if errors.Is(err, aggregates.ErrNotFound) {
			return budget.BudgetAuthorityRef{}, false, nil
		}
		return budget.BudgetAuthorityRef{}, false, fmt.Errorf("budgetfacts: workforce budget: %w", err)
	}
	if pool.Scope != q.Scope || pool.Period != q.Period {
		// Same identity derivation, different pool: the scope holds no
		// pool for this period. Withheld, not coerced.
		return budget.BudgetAuthorityRef{}, false, nil
	}
	if budget.BudgetType(pool.BudgetType) != budget.CompensationPool || budget.Unit(pool.Unit) != budget.UnitMoney {
		// The simulation weighs a raise against a money compensation
		// pool. Anything else is a different instrument, not this one
		// misread.
		return budget.BudgetAuthorityRef{}, false, nil
	}

	available, err := values.NewDecimal(pool.AvailableQuantity, storedQuantityScale, values.RoundingExactRequired)
	if err != nil {
		return budget.BudgetAuthorityRef{}, false, fmt.Errorf("budgetfacts: available quantity: %w", err)
	}

	return budget.BudgetAuthorityRef{
		BudgetType:        budget.CompensationPool,
		OwnerSystem:       pool.OwnerSystem,
		Scope:             pool.Scope,
		Period:            pool.Period,
		Currency:          pool.Currency,
		Unit:              budget.UnitMoney,
		AvailableQuantity: available,
		BaselineVersion:   pool.BaselineVersion,
		Evidence: budget.ObservationEvidence{
			ObservationID:   "obs/" + pool.Digest,
			RetrievedAt:     values.NewInstant(time.Now().UTC()),
			SourceWatermark: values.NewInstant(pool.RecordedAt.UTC()),
			Digest:          pool.Digest,
		},
	}, true, nil
}
