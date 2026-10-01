package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

func commonWorkerBudgetFixture(t *testing.T) (*CommonAgentLedgerBudget, *CommonAgentWorker, agentrun.Record, AgentModelExecutorRequest, context.Context, *commonAgentTestAuthority) {
	t.Helper()
	worker, record, source, _, authority, now := commonWorkerFixture(t, agentrun.ModeSponsored)
	run, err := worker.cfg.Runtime.Claim(context.Background(), record.Request.Source.TenantID, record.ID, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request, err := source.BuildCommonAgentModelWork(context.Background(), record, run)
	if err != nil {
		t.Fatal(err)
	}
	request = commonAgentBindModelStep(request, run, 1)
	limit := agentbudget.Limits{Steps: 100, Tokens: 1000000, SpendMicros: 1000000, WallClock: time.Hour}
	ledger, err := agentbudget.NewWithClock(agentbudget.Policy{TaskDefault: limit, UserDaily: limit, TenantMonthly: limit}, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	budget, err := NewCommonAgentLedgerBudget(worker.cfg.Runtime, ledger)
	if err != nil {
		t.Fatal(err)
	}
	return budget, worker, record, request, withCommonAgentModelEvidence(context.Background(), record, run, request), authority
}

func TestTodo_AGENT_033_CommonBudget(t *testing.T) {
	budget, worker, record, request, ctx, _ := commonWorkerBudgetFixture(t)
	estimate := agentbudget.Request{TaskID: record.ID, StepID: request.StepID, Fingerprint: "protected-prompt-digest", Estimate: agentbudget.Usage{Steps: 1, Tokens: 100, SpendMicros: 40, WallClock: time.Second}}
	if _, err := budget.Reserve(context.Background(), estimate); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("unbound reservation=%v", err)
	}
	reservation, err := budget.Reserve(ctx, estimate)
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Settle(agentbudget.Usage{Steps: 1, Tokens: 90, SpendMicros: 35, WallClock: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	run, err := worker.cfg.Runtime.GetRun(ctx, record.Request.Source.TenantID, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := budget.RemainingCommonAgentModelBudget(ctx, record, run)
	if err != nil || remaining.MaxCostMicros != record.Request.Budget.MaxCostMicros-35 || remaining.MaxInputTokens+remaining.MaxOutputTokens != record.Request.Budget.MaxInputTokens+record.Request.Budget.MaxOutputTokens-90 {
		t.Fatalf("remaining=%+v err=%v", remaining, err)
	}
	snapshot := budget.Ledger.Snapshot()
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].UserID != record.Request.Principal.AgentPrincipalID || snapshot.Tasks[0].UserID == record.Request.Principal.SponsorID {
		t.Fatalf("accounting principal=%+v", snapshot.Tasks)
	}
	request = commonAgentBindModelStep(request, run, 2)
	estimate.StepID = request.StepID
	reservation, err = budget.Reserve(withCommonAgentModelEvidence(ctx, record, run, request), estimate)
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Settle(agentbudget.Usage{Steps: 1, Tokens: 10, SpendMicros: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err = budget.RemainingCommonAgentModelBudget(ctx, record, run); !errors.Is(err, agentbudget.ErrPaused) {
		t.Fatalf("exhausted step budget=%v", err)
	}
}

func TestTodo_AGENT_033_CommonBudget_Security(t *testing.T) {
	budget, worker, record, request, ctx, authority := commonWorkerBudgetFixture(t)
	spec, err := commonAgentBudgetTask(record)
	if err != nil {
		t.Fatal(err)
	}
	spec.UserID = record.Request.Principal.SponsorID
	if err := budget.Ledger.OpenTask(spec); err != nil {
		t.Fatal(err)
	}
	estimate := agentbudget.Request{TaskID: record.ID, StepID: request.StepID, Fingerprint: "fingerprint", Estimate: agentbudget.Usage{Steps: 1, Tokens: 1, SpendMicros: 1}}
	if _, err := budget.Reserve(ctx, estimate); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("sponsor forged as accounting actor=%v", err)
	}
	authority.revoked = true
	run, _ := worker.cfg.Runtime.GetRun(ctx, record.Request.Source.TenantID, record.ID)
	if _, err := budget.RemainingCommonAgentModelBudget(ctx, record, run); err == nil {
		t.Fatal("revoked authority retained spend")
	}
	if _, err := NewCommonAgentLedgerBudget(nil, budget.Ledger); !errors.Is(err, agentbudget.ErrInvalid) {
		t.Fatalf("unconfigured ledger=%v", err)
	}
}
