package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
)

func TestTodo_AGENTP_010_RuntimeBudgetRegistersExactAdmissionWithoutReset(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	req := foregroundAuthorityRequest(now)
	id, err := agentrun.AdmissionRequestID(req.Source)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := agentrun.AdmissionRequestDigest(req)
	if err != nil {
		t.Fatal(err)
	}
	record := agentrun.Record{ID: id, Request: req, RequestDigest: digest, AdmittedAt: now, Decision: agentrun.DecisionAccepted,
		Authority: agentrun.AuthoritySnapshot{Agent: req.Agent, Principal: req.Principal, InstallationID: req.InstallationID, Audience: req.Audience, Context: req.Context, BudgetCeiling: req.Budget, GrantRef: req.Principal.DelegatedCredentialRef, PolicyDigest: foregroundDigest("policy")}}
	spec, err := personaRuntimeBudgetTask(record)
	if err != nil || spec.ID != id || spec.TenantID != req.Source.TenantID || spec.UserID != req.Principal.InvokerID || spec.Limit.Tokens != 2 || spec.Limit.Steps != 2 || spec.Limit.SpendMicros != 1 || spec.Limit.WallClock != time.Hour {
		t.Fatalf("spec=%+v err=%v", spec, err)
	}
	ledger, err := agentbudget.NewWithClock(agentbudget.Policy{TaskDefault: spec.Limit, UserDaily: spec.Limit, TenantMonthly: spec.Limit}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err = ledger.OpenTask(spec); err != nil {
		t.Fatal(err)
	}
	r, err := ledger.Reserve(context.Background(), agentbudget.Request{TaskID: spec.ID, StepID: "call-a", Fingerprint: "digest-a", Estimate: agentbudget.Usage{Steps: 1, Tokens: 1, SpendMicros: 1, WallClock: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Settle(agentbudget.Usage{Steps: 1, Tokens: 1, SpendMicros: 1, WallClock: time.Second}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(ledger.OpenTask(spec), agentbudget.ErrTaskExists) || !personaRuntimeBudgetTaskMatches(ledger.Snapshot(), spec) {
		t.Fatal("exact task replay rejected or reset")
	}
	if ledger.Snapshot().Tasks[0].Used.SpendMicros != 1 {
		t.Fatal("replay discarded settled use")
	}
	changed := spec
	changed.UserID = "another"
	if personaRuntimeBudgetTaskMatches(ledger.Snapshot(), changed) {
		t.Fatal("foreign owner matched task")
	}
	changed = spec
	changed.Limit.Tokens++
	if personaRuntimeBudgetTaskMatches(ledger.Snapshot(), changed) {
		t.Fatal("broadened limit matched task")
	}
	record.Decision = agentrun.DecisionRefused
	if _, err := personaRuntimeBudgetTask(record); !errors.Is(err, agentbudget.ErrInvalid) {
		t.Fatalf("refused request registered: %v", err)
	}
	if _, err := NewPersonaLedgerModelBudget(nil, nil, nil, nil, nil); !errors.Is(err, agentbudget.ErrInvalid) {
		t.Fatalf("missing budget owners: %v", err)
	}
}

func TestTodo_AGENTP_010_RuntimeModelContinuationUsesRemainingCapacity(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	req := foregroundAuthorityRequest(now)
	req.Budget = agentrun.Budget{MaxInputTokens: 6, MaxOutputTokens: 2, MaxCostMicros: 20}
	id, _ := agentrun.AdmissionRequestID(req.Source)
	digest, _ := agentrun.AdmissionRequestDigest(req)
	record := agentrun.Record{ID: id, Request: req, RequestDigest: digest, AdmittedAt: now, Decision: agentrun.DecisionAccepted,
		Authority: agentrun.AuthoritySnapshot{Agent: req.Agent, Principal: req.Principal, InstallationID: req.InstallationID, Audience: req.Audience, Context: req.Context, BudgetCeiling: req.Budget, GrantRef: req.Principal.DelegatedCredentialRef, PolicyDigest: foregroundDigest("policy")}}
	spec, err := personaRuntimeBudgetTask(record)
	if err != nil {
		t.Fatal(err)
	}
	before, err := personaRuntimeRemainingBudget(agentbudget.Snapshot{}, record)
	if err != nil || before != req.Budget {
		t.Fatalf("initial capacity=%+v err=%v", before, err)
	}
	snapshot := agentbudget.Snapshot{Tasks: []agentbudget.TaskSnapshot{{ID: id, TenantID: spec.TenantID, UserID: spec.UserID, Limit: spec.Limit, Used: agentbudget.Usage{Steps: 1, Tokens: 3, SpendMicros: 4, WallClock: time.Second}}}}
	remaining, err := personaRuntimeRemainingBudget(snapshot, record)
	if err != nil || remaining.MaxInputTokens+remaining.MaxOutputTokens != 5 || remaining.MaxCostMicros != 16 || remaining.MaxInputTokens > req.Budget.MaxInputTokens || remaining.MaxOutputTokens > req.Budget.MaxOutputTokens {
		t.Fatalf("remaining=%+v err=%v", remaining, err)
	}
	// Concurrent reserved capacity also belongs to the same task ceiling.
	snapshot.Tasks[0].Reserved = agentbudget.Usage{Steps: 1, Tokens: 1, SpendMicros: 1}
	if _, err := personaRuntimeRemainingBudget(snapshot, record); !errors.Is(err, agentbudget.ErrPaused) {
		t.Fatalf("spent step capacity reused: %v", err)
	}
	snapshot.Tasks[0].Reserved = agentbudget.Usage{}
	snapshot.Tasks[0].Used.Tokens = spec.Limit.Tokens
	if _, err := personaRuntimeRemainingBudget(snapshot, record); !errors.Is(err, agentbudget.ErrPaused) {
		t.Fatalf("spent token capacity reused: %v", err)
	}
}
