package application

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"reflect"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PersonaLedgerBudget registers a task from its durable, currently authorized
// admission before reserving model capacity in the shared durable ledger.
type PersonaLedgerBudget struct {
	ledger *agentbudget.Ledger
	owners PersonaRuntimeModelOwnerDependencies
}

func NewPersonaLedgerModelBudget(ledger *agentbudget.Ledger, store *agentstore.Store, authority agentrun.Authority, tenantUUID func(values.TenantId) uuid.UUID, now func() time.Time) (*PersonaLedgerBudget, error) {
	owners := PersonaRuntimeModelOwnerDependencies{AgentStore: store, Authority: authority, TenantUUID: tenantUUID, Now: now}
	if ledger == nil || owners.AgentStore == nil || isNilPersonaOutputPort(owners.Authority) || owners.TenantUUID == nil || now == nil {
		return nil, agentbudget.ErrInvalid
	}
	return &PersonaLedgerBudget{ledger: ledger, owners: owners}, nil
}

func (b *PersonaLedgerBudget) Reserve(ctx context.Context, req agentbudget.Request) (agentmodel.Reservation, error) {
	if b == nil || ctx == nil || b.ledger == nil || req.TaskID == "" {
		return nil, agentbudget.ErrInvalid
	}
	// The context's admission is an identity hint. Reloading its immutable owner
	// row and current authority is mandatory before registering any capacity.
	request, ok := ctx.Value(personaBackgroundAdmissionKey{}).(agentrun.Request)
	if !ok || request.Source.TenantID == "" || !request.Deadline.After(b.owners.Now().UTC()) {
		return nil, agentrun.ErrAuthorityRefusal
	}
	tenant := values.TenantId(request.Source.TenantID)
	repo, err := agentrunstore.NewAdmissionRepository(b.owners.AgentStore, b.owners.TenantUUID(tenant), tenant)
	if err != nil {
		return nil, err
	}
	record, err := repo.GetByID(ctx, req.TaskID)
	if err != nil || record.Decision != agentrun.DecisionAccepted || !reflect.DeepEqual(record.Request, request) {
		return nil, agentrun.ErrAuthorityRefusal
	}
	current, err := b.owners.Authority.VerifyAdmission(ctx, record.Request)
	if err != nil || !reflect.DeepEqual(current, record.Authority) {
		return nil, agentrun.ErrAuthorityRefusal
	}
	spec, err := personaRuntimeBudgetTask(record)
	if err != nil {
		return nil, err
	}
	if err = b.ledger.OpenTask(spec); err != nil {
		if !errors.Is(err, agentbudget.ErrTaskExists) {
			return nil, err
		}
		if !personaRuntimeBudgetTaskMatches(b.ledger.Snapshot(), spec) {
			return nil, agentrun.ErrAuthorityRefusal
		}
	}
	return b.ledger.Reserve(ctx, req)
}

func personaRuntimeBudgetTask(record agentrun.Record) (agentbudget.TaskSpec, error) {
	r := record.Request
	b := r.Budget
	if agentrun.ValidateAdmissionRecord(record) != nil || record.Decision != agentrun.DecisionAccepted || r.Source.Kind != agentrun.SourcePersonaMention || r.Principal.Mode != agentrun.ModeOnBehalfOf ||
		b.MaxInputTokens > math.MaxInt64 || b.MaxOutputTokens > math.MaxInt64 || b.MaxInputTokens > math.MaxInt64-b.MaxOutputTokens || b.MaxCostMicros > math.MaxInt64 ||
		!r.Deadline.After(record.AdmittedAt) {
		return agentbudget.TaskSpec{}, agentbudget.ErrInvalid
	}
	// This executor permits one initial model call and one continuation after
	// a single governed tool proposal. It cannot authorize further model steps.
	return agentbudget.TaskSpec{ID: record.ID, TenantID: r.Source.TenantID, UserID: r.Principal.InvokerID, Limit: agentbudget.Limits{Steps: 2, Tokens: int64(b.MaxInputTokens + b.MaxOutputTokens), SpendMicros: int64(b.MaxCostMicros), WallClock: r.Deadline.Sub(record.AdmittedAt)}}, nil
}

func personaRuntimeBudgetTaskMatches(snapshot agentbudget.Snapshot, spec agentbudget.TaskSpec) bool {
	for _, task := range snapshot.Tasks {
		if task.ID == spec.ID {
			return task.TenantID == spec.TenantID && task.UserID == spec.UserID && personaRuntimeBudgetLimitsMatch(task.Limit, spec.Limit) && task.ParentTaskID == "" && task.Depth == 0
		}
	}
	return false
}

// personaRuntimeBudgetLimitsMatch compares the wall-clock limit at the
// precision the run record keeps after it is stored. The limit is derived
// from two timestamps, and a record read back from PostgreSQL carries
// microseconds where the admitting process had nanoseconds.
func personaRuntimeBudgetLimitsMatch(stored, derived agentbudget.Limits) bool {
	delta := stored.WallClock - derived.WallClock
	if delta < 0 {
		delta = -delta
	}
	stored.WallClock, derived.WallClock = 0, 0
	return stored == derived && delta < 2*time.Microsecond
}

var _ agentmodel.Budget = (*PersonaLedgerBudget)(nil)

func (b *PersonaLedgerBudget) RemainingPersonaRunModelBudget(ctx context.Context, record agentrun.Record, run runstate.Run) (agentrun.Budget, error) {
	if b == nil || ctx == nil || b.ledger == nil || validatePersonaModelWorkBinding(record, run) != nil {
		return agentrun.Budget{}, agentbudget.ErrInvalid
	}
	return personaRuntimeRemainingBudget(b.ledger.Snapshot(), record)
}

func personaRuntimeRemainingBudget(snapshot agentbudget.Snapshot, record agentrun.Record) (agentrun.Budget, error) {
	spec, err := personaRuntimeBudgetTask(record)
	if err != nil {
		return agentrun.Budget{}, err
	}
	upper := record.Request.Budget
	for _, task := range snapshot.Tasks {
		if task.ID != record.ID {
			continue
		}
		if !personaRuntimeBudgetTaskMatches(snapshot, spec) || task.Used.Tokens < 0 || task.Reserved.Tokens < 0 || task.Used.SpendMicros < 0 || task.Reserved.SpendMicros < 0 ||
			task.Used.Steps < 0 || task.Reserved.Steps < 0 || task.Used.Steps > task.Limit.Steps || task.Reserved.Steps > task.Limit.Steps-task.Used.Steps ||
			task.Used.Tokens > task.Limit.Tokens || task.Reserved.Tokens > task.Limit.Tokens-task.Used.Tokens || task.Used.SpendMicros > task.Limit.SpendMicros || task.Reserved.SpendMicros > task.Limit.SpendMicros-task.Used.SpendMicros || task.Paused != "" {
			return agentrun.Budget{}, fmt.Errorf("%w: reason=%q used=%+v reserved=%+v limit=%+v", agentbudget.ErrPaused, task.Paused, task.Used, task.Reserved, task.Limit)
		}
		left := uint64(task.Limit.Tokens - task.Used.Tokens - task.Reserved.Tokens)
		cost := uint64(task.Limit.SpendMicros - task.Used.SpendMicros - task.Reserved.SpendMicros)
		if left < 2 || cost == 0 || task.Used.Steps+task.Reserved.Steps >= task.Limit.Steps {
			return agentrun.Budget{}, fmt.Errorf("%w: nothing left for another step (tokens left %d, cost left %d, steps %d+%d of %d)", agentbudget.ErrPaused, left, cost, task.Used.Steps, task.Reserved.Steps, task.Limit.Steps)
		}
		// Retain the admitted input/output ratio while shrinking both ceilings.
		// Wide multiplication avoids overflow at large valid token limits.
		hi, lo := bits.Mul64(left, upper.MaxInputTokens)
		input, _ := bits.Div64(hi, lo, upper.MaxInputTokens+upper.MaxOutputTokens)
		if input == 0 {
			input = 1
		}
		if input >= left {
			input = left - 1
		}
		return agentrun.Budget{MaxInputTokens: minUint(input, upper.MaxInputTokens), MaxOutputTokens: minUint(left-input, upper.MaxOutputTokens), MaxCostMicros: minUint(cost, upper.MaxCostMicros)}, nil
	}
	return upper, nil
}
