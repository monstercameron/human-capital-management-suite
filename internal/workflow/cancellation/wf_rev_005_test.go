package cancellation_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// scriptEffectObserver is the WF-REV-005 test double for the EffectObserver
// port: it resolves scripted effects as produced or absent, fails scripted
// ones, and counts every call so the test proves observation is bounded to
// one call per unconfirmed effect.
type scriptEffectObserver struct {
	mu       sync.Mutex
	produced map[string]bool
	absent   map[string]bool
	fail     map[string]error
	calls    map[string]int
}

func (s *scriptEffectObserver) ObserveEffect(_ context.Context, _ cancellation.Executor, req cancellation.ObserveEffectRequest) (cancellation.ObservedEffect, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls == nil {
		s.calls = map[string]int{}
	}
	s.calls[req.EffectID]++
	if err, ok := s.fail[req.EffectID]; ok {
		return cancellation.ObservedEffect{}, err
	}
	if s.absent[req.EffectID] {
		return cancellation.ObservedEffect{Produced: false}, nil
	}
	if s.produced[req.EffectID] {
		return cancellation.ObservedEffect{Produced: true}, nil
	}
	return cancellation.ObservedEffect{}, errors.New("cancellation test: no observation scripted for " + req.EffectID)
}

func (s *scriptEffectObserver) called(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[id]
}

// twoCompensablePlan binds a published compensation to both promotion
// writes, so one run can hold a settled compensable effect beside an
// unconfirmed one.
func twoCompensablePlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for i := range plan.Nodes {
		switch plan.Nodes[i].ID {
		case promotionexec.NodeExecutePromotion:
			plan.Nodes[i].CompensationRef = &workflow.ResolvedReference{Kind: workflow.RefCompensation, ID: "compensation.promotion.reverse", Version: "1"}
		case promotionexec.NodeCompensateHold:
			plan.Nodes[i].CompensationRef = &workflow.ResolvedReference{Kind: workflow.RefCompensation, ID: "compensation.budget.release", Version: "1"}
		}
	}
	return plan
}

func (f *fixture) decideObserved(id uuid.UUID, plan *workflow.CompiledWorkflow, observer cancellation.EffectObserver) (cancellation.Outcome, error) {
	var out cancellation.Outcome
	err := f.tx(f.conn, func(tx dbport.Tx) error {
		var err error
		out, err = cancellation.Decide(context.Background(), tx, cancellation.Request{TenantID: f.tenant, InstanceID: id,
			Plan: plan, Observer: observer, Reason: "PROMOTION_WITHDRAWN", RequestedBy: "principal:operator", RecordedAt: at})
		return err
	})
	return out, err
}

func effectByID(effects []workflow.EffectDisposition) map[string]workflow.EffectDisposition {
	out := make(map[string]workflow.EffectDisposition, len(effects))
	for _, e := range effects {
		out[e.ID] = e
	}
	return out
}

// TestTodo_WF_REV_005_Observation proves the served half of the contract:
// cancellation observes every unconfirmed effect before deciding. An
// in-flight compensable effect confirmed produced requires compensation
// instead of sending the whole run to repair; one confirmed never produced
// needs no verdict; one the observer cannot resolve leaves only itself
// unresolved while its settled sibling is still decided.
func TestTodo_WF_REV_005_Observation(t *testing.T) {
	t.Run("observed produced effect requires compensation", func(t *testing.T) {
		f := newFixture(t)
		plan := compensablePlan(t)
		id := f.instance(plan, []string{promotionexec.NodeExecutePromotion},
			node{id: promotionexec.NodeExecutePromotion, statuses: []runtime.NodeStatus{runtime.NodeRunning}})
		observer := &scriptEffectObserver{produced: map[string]bool{"execute_promotion#1": true}}
		out, err := f.decideObserved(id, plan, observer)
		if err != nil {
			t.Fatal(err)
		}
		if out.Decision != workflow.CompensationRequired || out.Instance.RuntimeStatus != runtime.InstanceCancelling {
			t.Fatalf("outcome = %+v", out)
		}
		effects := effectByID(out.Evidence.Effects)
		got, ok := effects["execute_promotion#1"]
		if !ok || got.Verdict != workflow.EffectCompensate ||
			got.Disposition != "COMPENSATE:compensation.promotion.reverse@1" {
			t.Fatalf("effect evidence = %+v", out.Evidence.Effects)
		}
		if len(out.CompensationRefs) != 1 || out.CompensationRefs[0] != "execute_promotion#1=compensation.promotion.reverse@1" {
			t.Fatalf("obligation = %v", out.CompensationRefs)
		}
		if r, ok := out.Blocking(); !ok || r.Code != cancellation.ReasonEffectCompensation {
			t.Fatalf("blocking = %+v, %v", r, ok)
		}
		if n := observer.called("execute_promotion#1"); n != 1 {
			t.Fatalf("observer called %d times, want exactly once", n)
		}
	})

	t.Run("observed absent effect needs no verdict", func(t *testing.T) {
		f := newFixture(t)
		id := f.instance(f.plan, []string{promotionexec.NodeExecutePromotion},
			node{id: promotionexec.NodeExecutePromotion, statuses: []runtime.NodeStatus{runtime.NodeRunning}})
		observer := &scriptEffectObserver{absent: map[string]bool{"execute_promotion#1": true}}
		out, err := f.decideObserved(id, f.plan, observer)
		if err != nil {
			t.Fatal(err)
		}
		if out.Decision != workflow.Cancelled || out.Instance.RuntimeStatus != runtime.InstanceCancelled {
			t.Fatalf("outcome = %+v: a never-produced effect must not block a clean cancel", out)
		}
		effects := effectByID(out.Evidence.Effects)
		got, ok := effects["execute_promotion#1"]
		if !ok || got.Disposition != "NOT_PRODUCED" || got.Verdict != "" {
			t.Fatalf("effect evidence = %+v, want NOT_PRODUCED with no verdict", out.Evidence.Effects)
		}
	})

	t.Run("unresolvable effect leaves only itself unresolved", func(t *testing.T) {
		f := newFixture(t)
		plan := twoCompensablePlan(t)
		id := f.instance(plan, []string{promotionexec.NodeObservePayroll},
			node{id: promotionexec.NodeExecutePromotion, statuses: []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded}},
			node{id: promotionexec.NodeCompensateHold, statuses: []runtime.NodeStatus{runtime.NodeRunning}})
		observer := &scriptEffectObserver{fail: map[string]error{"compensate_budget_hold#1": errors.New("observation timeout")}}
		out, err := f.decideObserved(id, plan, observer)
		if err != nil {
			t.Fatal(err)
		}
		if out.Decision != workflow.RepairRequired {
			t.Fatalf("decision = %s, want REPAIR_REQUIRED", out.Decision)
		}
		effects := effectByID(out.Evidence.Effects)
		if got := effects["execute_promotion#1"]; got.Verdict != workflow.EffectCompensate ||
			got.Disposition != "COMPENSATE:compensation.promotion.reverse@1" {
			t.Fatalf("settled sibling = %+v, want its COMPENSATE verdict despite the unresolved effect", got)
		}
		if got := effects["compensate_budget_hold#1"]; got.Verdict != workflow.EffectUnresolved ||
			got.Disposition != "AMBIGUOUS" {
			t.Fatalf("timed-out effect = %+v, want UNRESOLVED/AMBIGUOUS", got)
		}
		if r, _ := out.Blocking(); r.Code != cancellation.ReasonEffectAmbiguous {
			t.Fatalf("blocking = %+v, want the ambiguous effect", r)
		}
		if n := observer.called("compensate_budget_hold#1"); n != 1 {
			t.Fatalf("observer called %d times for the unconfirmed effect, want exactly once", n)
		}
		if n := observer.called("execute_promotion#1"); n != 0 {
			t.Fatalf("observer called %d times for the settled effect, want none", n)
		}
	})
}
