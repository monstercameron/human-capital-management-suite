package application

import (
	"context"
	"errors"
	"math"
	"math/bits"
	"reflect"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

// CommonAgentLedgerBudget accounts for the actual admitted principal. The
// existing ledger's UserID slot does not create a human authorization identity.
type CommonAgentLedgerBudget struct {
	Runtime *CommonAgentRuntime
	Ledger  *agentbudget.Ledger
}

func NewCommonAgentLedgerBudget(runtime *CommonAgentRuntime, ledger *agentbudget.Ledger) (*CommonAgentLedgerBudget, error) {
	if runtime == nil || ledger == nil {
		return nil, agentbudget.ErrInvalid
	}
	return &CommonAgentLedgerBudget{Runtime: runtime, Ledger: ledger}, nil
}

func commonAgentBudgetTask(record agentrun.Record) (agentbudget.TaskSpec, error) {
	b := record.Request.Budget
	if agentrun.ValidateAdmissionRecord(record) != nil || record.Decision != agentrun.DecisionAccepted || record.Request.Persona != nil || commonAgentExecutionPrincipal(record.Request) == "" || b.MaxInputTokens > math.MaxInt64 || b.MaxOutputTokens > math.MaxInt64 || b.MaxInputTokens > math.MaxInt64-b.MaxOutputTokens || b.MaxCostMicros > math.MaxInt64 || !record.Request.Deadline.After(record.AdmittedAt) {
		return agentbudget.TaskSpec{}, agentbudget.ErrInvalid
	}
	return agentbudget.TaskSpec{ID: record.ID, TenantID: record.Request.Source.TenantID, UserID: commonAgentExecutionPrincipal(record.Request), Limit: agentbudget.Limits{Steps: 2, Tokens: int64(b.MaxInputTokens + b.MaxOutputTokens), SpendMicros: int64(b.MaxCostMicros), WallClock: record.Request.Deadline.Sub(record.AdmittedAt)}}, nil
}

func (b *CommonAgentLedgerBudget) Reserve(ctx context.Context, request agentbudget.Request) (agentmodel.Reservation, error) {
	if b == nil || b.Runtime == nil || b.Ledger == nil || ctx == nil {
		return nil, agentbudget.ErrInvalid
	}
	bound, ok := ctx.Value(commonAgentModelEvidenceKey{}).(commonAgentModelEvidence)
	if !ok || bound.Run.ID != request.TaskID || bound.Request.StepID != request.StepID {
		return nil, agentrun.ErrAuthorityRefusal
	}
	record, err := b.Runtime.GetAdmission(ctx, bound.Run.TenantID, request.TaskID)
	if err != nil || !reflect.DeepEqual(record, bound.Record) {
		return nil, agentrun.ErrAuthorityRefusal
	}
	run, err := b.Runtime.GetRun(ctx, bound.Run.TenantID, request.TaskID)
	if err != nil || commonAgentCheckExecutionIdentity(record, run) != nil || run.Fence != bound.Run.Fence || run.Version != bound.Run.Version {
		return nil, agentrun.ErrAuthorityRefusal
	}
	if err = b.Runtime.Recheck(ctx, run.TenantID, run.ID); err != nil {
		return nil, err
	}
	spec, err := commonAgentBudgetTask(record)
	if err != nil {
		return nil, err
	}
	if err = b.Ledger.OpenTask(spec); err != nil {
		if !errors.Is(err, agentbudget.ErrTaskExists) {
			return nil, err
		}
		if !personaRuntimeBudgetTaskMatches(b.Ledger.Snapshot(), spec) {
			return nil, agentrun.ErrAuthorityRefusal
		}
	}
	return b.Ledger.Reserve(ctx, request)
}

func (b *CommonAgentLedgerBudget) RemainingCommonAgentModelBudget(ctx context.Context, record agentrun.Record, run runstate.Run) (agentrun.Budget, error) {
	if b == nil || b.Runtime == nil || b.Ledger == nil || commonAgentCheckExecutionIdentity(record, run) != nil {
		return agentrun.Budget{}, agentbudget.ErrInvalid
	}
	if err := b.Runtime.Recheck(ctx, run.TenantID, run.ID); err != nil {
		return agentrun.Budget{}, err
	}
	spec, err := commonAgentBudgetTask(record)
	if err != nil {
		return agentrun.Budget{}, err
	}
	upper := record.Request.Budget
	snapshot := b.Ledger.Snapshot()
	for _, task := range snapshot.Tasks {
		if task.ID != record.ID {
			continue
		}
		if !personaRuntimeBudgetTaskMatches(snapshot, spec) || task.Paused != "" || task.Used.Tokens < 0 || task.Reserved.Tokens < 0 || task.Used.SpendMicros < 0 || task.Reserved.SpendMicros < 0 || task.Used.Steps < 0 || task.Reserved.Steps < 0 || task.Used.Tokens > task.Limit.Tokens || task.Reserved.Tokens > task.Limit.Tokens-task.Used.Tokens || task.Used.SpendMicros > task.Limit.SpendMicros || task.Reserved.SpendMicros > task.Limit.SpendMicros-task.Used.SpendMicros || task.Used.Steps > task.Limit.Steps || task.Reserved.Steps >= task.Limit.Steps-task.Used.Steps {
			return agentrun.Budget{}, agentbudget.ErrPaused
		}
		tokens := uint64(task.Limit.Tokens - task.Used.Tokens - task.Reserved.Tokens)
		cost := uint64(task.Limit.SpendMicros - task.Used.SpendMicros - task.Reserved.SpendMicros)
		if tokens < 2 || cost == 0 {
			return agentrun.Budget{}, agentbudget.ErrPaused
		}
		hi, lo := bits.Mul64(tokens, upper.MaxInputTokens)
		input, _ := bits.Div64(hi, lo, upper.MaxInputTokens+upper.MaxOutputTokens)
		if input == 0 {
			input = 1
		}
		if input >= tokens {
			input = tokens - 1
		}
		return agentrun.Budget{MaxInputTokens: minUint(input, upper.MaxInputTokens), MaxOutputTokens: minUint(tokens-input, upper.MaxOutputTokens), MaxCostMicros: minUint(cost, upper.MaxCostMicros)}, nil
	}
	return upper, nil
}

var _ agentmodel.Budget = (*CommonAgentLedgerBudget)(nil)
