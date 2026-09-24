package chatapps

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func chat044Service(t *testing.T) (*Service, Actor) {
	t.Helper()
	s, actor := fixture()
	installation, err := s.Repo.Get(context.Background(), "t1:c1:app")
	if err != nil {
		t.Fatal(err)
	}
	installation.Manifest.Agent.RatePerMinute = 2
	if err := s.Repo.Put(context.Background(), installation); err != nil {
		t.Fatal(err)
	}
	return s, actor
}

func TestTodo_CHAT_044(t *testing.T) {
	current := time.Unix(2_000, 0).UTC()
	s, _ := chat044Service(t)
	budget := NewTriggerBudget(func() time.Time { return current })
	if err := budget.ConfigureTenant("t1", TenantTriggerBudget{RatePerMinute: 2, CostPerMinute: 10}); err != nil {
		t.Fatal(err)
	}
	trigger := Trigger{Tenant: "t1", AgentInstallation: "t1:c1:app", Conversation: "c1", Source: string(TriggerMention), Cost: 6, IdempotencyKey: "one"}
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); err != nil {
		t.Fatalf("first declared trigger: %v", err)
	}
	trigger.IdempotencyKey = "two"
	trigger.Cost = 5
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); !errors.Is(err, ErrBudget) {
		t.Fatalf("tenant cost ceiling: %v", err)
	}
	trigger.Depth = 2
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); !errors.Is(err, ErrLoop) {
		t.Fatalf("cause depth: %v", err)
	}
	trigger.Depth = 0
	current = current.Add(time.Minute + time.Nanosecond)
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); err != nil {
		t.Fatalf("admission after budget window: %v", err)
	}
	trigger.IdempotencyKey = "three"
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); err != nil {
		t.Fatalf("second admission at rate ceiling: %v", err)
	}
	trigger.IdempotencyKey = "four"
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); !errors.Is(err, ErrBudget) {
		t.Fatalf("tenant rate ceiling: %v", err)
	}
	current = current.Add(time.Minute + time.Nanosecond)
	installation, err := s.Repo.Get(context.Background(), "t1:c1:app")
	if err != nil {
		t.Fatal(err)
	}
	installation.Manifest.Agent.RatePerMinute = 1
	if err := s.Repo.Put(context.Background(), installation); err != nil {
		t.Fatal(err)
	}
	trigger.IdempotencyKey = "five"
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); err != nil {
		t.Fatalf("first per-agent admission: %v", err)
	}
	trigger.IdempotencyKey = "six"
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); !errors.Is(err, ErrBudget) {
		t.Fatalf("per-agent rate ceiling: %v", err)
	}
}

func TestTodo_CHAT_044_Fault(t *testing.T) {
	s, _ := chat044Service(t)
	current := time.Unix(2_100, 0).UTC()
	budget := NewTriggerBudget(func() time.Time { return current })
	trigger := Trigger{Tenant: "t1", AgentInstallation: "t1:c1:app", Conversation: "c1", Source: string(TriggerMention), IdempotencyKey: "fault-1"}
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); !errors.Is(err, ErrBudget) {
		t.Fatalf("missing tenant ceiling did not fail closed: %v", err)
	}
	if err := budget.ConfigureTenant("t1", TenantTriggerBudget{RatePerMinute: 1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("partial policy accepted: %v", err)
	}
	if err := budget.ConfigureTenant("t1", TenantTriggerBudget{RatePerMinute: 1, CostPerMinute: 1}); err != nil {
		t.Fatal(err)
	}
	trigger.Cost = -1
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative cost accepted: %v", err)
	}
	trigger.Cost = 0
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); !errors.Is(err, ErrReplay) {
		t.Fatalf("duplicate logical trigger: %v", err)
	}
	current = current.Add(triggerBudgetWindow + time.Nanosecond)
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); !errors.Is(err, ErrReplay) {
		t.Fatalf("durable idempotency key did not survive the budget window: %v", err)
	}
}

type denyManagerAuthority struct{ authorityStub }

func (denyManagerAuthority) CanManageApp(context.Context, Actor, string) error { return ErrDenied }

func TestTodo_CHAT_044_Security(t *testing.T) {
	s, actor := chat044Service(t)
	budget := NewTriggerBudget(func() time.Time { return time.Unix(2_200, 0).UTC() })
	if err := budget.ConfigureTenant("t1", TenantTriggerBudget{RatePerMinute: 3, CostPerMinute: 30}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTriggerKillSwitch(context.Background(), actor, "t1:c1:app", budget, true); err != nil {
		t.Fatal(err)
	}
	trigger := Trigger{Tenant: "t1", AgentInstallation: "t1:c1:app", Conversation: "c1", Source: string(TriggerMention), IdempotencyKey: "stopped"}
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); !errors.Is(err, ErrSuspended) {
		t.Fatalf("paused installation admitted a trigger: %v", err)
	}
	if err := s.SetTriggerKillSwitch(context.Background(), Actor{Tenant: "elsewhere", Conversation: "c1", Principal: "p1"}, "t1:c1:app", budget, false); !errors.Is(err, ErrDenied) {
		t.Fatalf("foreign tenant changed kill switch: %v", err)
	}
	s.Authority = denyManagerAuthority{}
	if err := s.SetTriggerKillSwitch(context.Background(), actor, "t1:c1:app", budget, false); !errors.Is(err, ErrDenied) {
		t.Fatalf("non-manager changed kill switch: %v", err)
	}
	if err := s.AdmitTriggerWithBudget(context.Background(), trigger, budget); !errors.Is(err, ErrSuspended) {
		t.Fatalf("failed kill-switch mutation resumed trigger: %v", err)
	}
}

func TestTodo_CHAT_044_Golden(t *testing.T) {
	policy := TenantTriggerBudget{RatePerMinute: 12, CostPerMinute: 900}
	got, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"rate_per_minute":12,"cost_per_minute":900}`
	if string(got) != want {
		t.Fatalf("tenant trigger budget encoding=%s, want %s", got, want)
	}
}
