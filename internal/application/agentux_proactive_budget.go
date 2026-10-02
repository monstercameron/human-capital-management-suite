package application

import (
	"context"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"math"
	"reflect"
	"time"
)

type announcementLedgerBudget struct {
	mention *PersonaLedgerBudget
	runtime *AgentAnnouncementRuntime
}

func (b announcementLedgerBudget) Reserve(ctx context.Context, request agentbudget.Request) (agentmodel.Reservation, error) {
	source, ok := ctx.Value(personaBackgroundAdmissionKey{}).(agentrun.Request)
	if !ok || source.Source.Kind != agentrun.SourceAnnouncement {
		return b.mention.Reserve(ctx, request)
	}
	r := b.runtime
	repo, err := r.admissionRepository(source.Source.TenantID)
	if err != nil {
		return nil, err
	}
	record, err := repo.GetByID(ctx, request.TaskID)
	if err != nil || !reflect.DeepEqual(record.Request, source) {
		return nil, agentrun.ErrAuthorityRefusal
	}
	current, err := r.VerifyAdmission(ctx, record.Request)
	if err != nil || current != record.Authority {
		return nil, agentrun.ErrAuthorityRefusal
	}
	spec, err := announcementBudgetTask(record)
	if err != nil {
		return nil, err
	}
	if err := b.mention.ledger.OpenTask(spec); err != nil {
		if !errors.Is(err, agentbudget.ErrTaskExists) || !personaRuntimeBudgetTaskMatches(b.mention.ledger.Snapshot(), spec) {
			return nil, err
		}
	}
	return b.mention.ledger.Reserve(ctx, request)
}

func announcementBudgetTask(record agentrun.Record) (agentbudget.TaskSpec, error) {
	r := record.Request
	b := r.Budget
	if agentrun.ValidateAdmissionRecord(record) != nil || record.Decision != agentrun.DecisionAccepted || r.Source.Kind != agentrun.SourceAnnouncement || r.Principal.Mode != agentrun.ModeSponsored || b.MaxInputTokens > math.MaxInt64 || b.MaxOutputTokens > math.MaxInt64 || b.MaxInputTokens > math.MaxInt64-b.MaxOutputTokens || b.MaxCostMicros > math.MaxInt64 {
		return agentbudget.TaskSpec{}, agentbudget.ErrInvalid
	}
	return agentbudget.TaskSpec{ID: record.ID, TenantID: r.Source.TenantID, UserID: r.Principal.SponsorID, Limit: agentbudget.Limits{Steps: 2, Tokens: int64(b.MaxInputTokens + b.MaxOutputTokens), SpendMicros: int64(b.MaxCostMicros), WallClock: r.Deadline.Sub(record.AdmittedAt)}}, nil
}

// A repair spends only the capacity left in the original admitted task. It
// never expands the token or cost ceiling to make a second call fit.
func (b announcementLedgerBudget) RemainingPersonaRunModelBudget(ctx context.Context, record agentrun.Record, run runstate.Run) (agentrun.Budget, error) {
	if record.Request.Source.Kind != agentrun.SourceAnnouncement {
		return b.mention.RemainingPersonaRunModelBudget(ctx, record, run)
	}
	if agentrun.ValidateAdmissionRecord(record) != nil || record.Request.Principal.Mode != agentrun.ModeSponsored ||
		run.ID != record.ID || run.AdmissionID != record.ID || run.RequestDigest != record.RequestDigest ||
		run.TenantID != record.Request.Source.TenantID || run.AgentID != record.Authority.Agent.AgentID ||
		run.AgentVersion != record.Authority.Agent.Version || run.AgentDigest != record.Authority.Agent.Digest ||
		run.ContextDigest != record.Authority.Context.Digest || !run.Deadline.Truncate(time.Microsecond).Equal(record.Request.Deadline.Truncate(time.Microsecond)) ||
		run.PrincipalMode != agentrun.ModeSponsored || run.ActorID != record.Request.Principal.SponsorID {
		return agentrun.Budget{}, agentbudget.ErrInvalid
	}
	spec, err := announcementBudgetTask(record)
	if err != nil {
		return agentrun.Budget{}, err
	}
	upper := record.Request.Budget
	snapshot := b.mention.ledger.Snapshot()
	for _, task := range snapshot.Tasks {
		if task.ID != record.ID {
			continue
		}
		if !personaRuntimeBudgetTaskMatches(snapshot, spec) || task.Paused != "" || task.Used.Tokens < 0 || task.Reserved.Tokens < 0 || task.Used.SpendMicros < 0 || task.Reserved.SpendMicros < 0 || task.Used.Steps < 0 || task.Reserved.Steps < 0 || task.Used.Tokens > task.Limit.Tokens || task.Reserved.Tokens > task.Limit.Tokens-task.Used.Tokens || task.Used.SpendMicros > task.Limit.SpendMicros || task.Reserved.SpendMicros > task.Limit.SpendMicros-task.Used.SpendMicros || task.Used.Steps+task.Reserved.Steps >= task.Limit.Steps {
			return agentrun.Budget{}, agentbudget.ErrPaused
		}
		tokens := uint64(task.Limit.Tokens - task.Used.Tokens - task.Reserved.Tokens)
		cost := uint64(task.Limit.SpendMicros - task.Used.SpendMicros - task.Reserved.SpendMicros)
		if tokens < 2 || cost == 0 {
			return agentrun.Budget{}, agentbudget.ErrPaused
		}
		output := minUint(upper.MaxOutputTokens, tokens/2)
		return agentrun.Budget{MaxInputTokens: minUint(upper.MaxInputTokens, tokens-output), MaxOutputTokens: output, MaxCostMicros: minUint(upper.MaxCostMicros, cost)}, nil
	}
	return upper, nil
}
