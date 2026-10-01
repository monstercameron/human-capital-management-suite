package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

func TestTodo_AGENTP_010_Integration_RuntimeBudgetReloadsAdmissionAndCurrentOwners(t *testing.T) {
	runtime, record, _, _, _ := backgroundAuthorityPostgresFixture(t)
	spec, err := personaRuntimeBudgetTask(record)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := agentbudget.NewWithClock(agentbudget.Policy{TaskDefault: spec.Limit, UserDaily: spec.Limit, TenantMonthly: spec.Limit}, runtime.cfg.Now)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := NewPersonaLedgerModelBudget(ledger, runtime.cfg.Agents, runtime, runtime.cfg.TenantUUID, runtime.cfg.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithPersonaBackgroundAdmission(context.Background(), record)
	request := agentbudget.Request{TaskID: record.ID, StepID: "actual-call-a", Fingerprint: "actual-call-a", Estimate: agentbudget.Usage{Steps: 1, Tokens: spec.Limit.Tokens, SpendMicros: spec.Limit.SpendMicros, WallClock: time.Second}}
	reservation, err := budget.Reserve(ctx, request)
	if err != nil {
		t.Fatalf("durable admission did not register current task: %v", err)
	}
	used := agentbudget.Usage{Steps: 1, Tokens: 10, SpendMicros: 5, WallClock: time.Millisecond}
	if err = reservation.Settle(used); err != nil {
		t.Fatal(err)
	}
	r := record.Request
	run := runstate.Run{ID: record.ID, AdmissionID: record.ID, TenantID: r.Source.TenantID, AgentID: r.Agent.AgentID, AgentVersion: r.Agent.Version,
		AgentDigest: r.Agent.Digest, ContextDigest: r.Context.Digest, Deadline: r.Deadline, PrincipalMode: r.Principal.Mode, ActorID: r.Principal.InvokerID}
	remaining, err := budget.RemainingPersonaRunModelBudget(ctx, record, run)
	if err != nil || remaining.MaxInputTokens+remaining.MaxOutputTokens != uint64(spec.Limit.Tokens-used.Tokens) || remaining.MaxCostMicros != uint64(spec.Limit.SpendMicros-used.SpendMicros) {
		t.Fatalf("settled owner capacity=%+v err=%v", remaining, err)
	}
	request.StepID, request.Fingerprint = "actual-call-b", "actual-call-b"
	request.Estimate.Tokens = int64(remaining.MaxInputTokens + remaining.MaxOutputTokens)
	request.Estimate.SpendMicros = int64(remaining.MaxCostMicros)
	second, err := budget.Reserve(ctx, request)
	if err != nil {
		t.Fatalf("continuation did not retain settled task capacity: %v", err)
	}
	if err = second.Settle(used); err != nil {
		t.Fatal(err)
	}
	if got := ledger.Snapshot().Tasks[0]; got.Used.Tokens != 2*used.Tokens || got.Used.SpendMicros != 2*used.SpendMicros || got.UserID != r.Principal.InvokerID || got.Limit != spec.Limit {
		t.Fatalf("continuation reset task or changed admitted owner: %+v", got)
	}
	// A later current owner denial is checked before any ledger mutation.
	runtime.cfg.Worker = &VerifiedPersonaPrivateChatWorkloadIdentitySource{}
	before := ledger.Snapshot().Tasks[0]
	request.StepID, request.Fingerprint = "revoked-call", "revoked-call"
	if _, err := budget.Reserve(ctx, request); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("revoked workload retained model budget: %v", err)
	}
	if got := ledger.Snapshot().Tasks[0]; got.Used != before.Used || got.Reserved != before.Reserved || got.Limit != before.Limit {
		t.Fatalf("denied current owner mutated budget: before=%+v after=%+v", before, got)
	}
	if _, err := budget.Reserve(context.Background(), request); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("unbound request obtained task budget: %v", err)
	}
}
