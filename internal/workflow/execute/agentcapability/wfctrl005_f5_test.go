package agentcapability

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/capabilityrunner"
)

// TestTodo_WF_CTRL_005_F5 is the #3620 (finding 5) acceptance: an in-flight
// agent node (its cause already bound to a run in the ledger) under an
// engaged kill switch takes its declared fallback route (the UNKNOWN
// outcome) instead of an error, without touching the runner; new runs
// stay refused. Every CAPABILITY node declares all four outcome edges,
// so the UNKNOWN edge is always the declared fallback.
func TestTodo_WF_CTRL_005_F5(t *testing.T) {
	arm := []struct {
		name string
		set  func(agent *PublishedAgent, deps *Deps)
	}{
		{"agent", func(agent *PublishedAgent, deps *Deps) { agent.KillSwitch = true }},
		{"capability", func(agent *PublishedAgent, deps *Deps) {
			deps.CapabilityKilled = func(capability.Key) bool { return true }
		}},
		{"tenant", func(agent *PublishedAgent, deps *Deps) {
			deps.Tenant = "tenant-a"
			deps.TenantKilled = func(string) bool { return true }
		}},
	}
	t.Run("in_flight_takes_fallback", func(t *testing.T) {
		for _, sw := range arm {
			t.Run(sw.name, func(t *testing.T) {
				agent := testAgent()
				ledger := NewMemoryLedger()
				runner := &fakeRunner{result: succeededResult()}
				deps := Deps{Runner: runner, Ledger: ledger}
				sw.set(&agent, &deps)
				call := testCall(agent)
				ledger.Record(CauseOf(call), "run-inflight-1")
				answer, err := Handler(agent, deps)(context.Background(), call)
				if err != nil {
					t.Fatalf("in-flight invoke err = %v, want fallback answer", err)
				}
				got, ok := answer.(capabilityrunner.CapabilityAnswer)
				if !ok {
					t.Fatalf("response is %T, want CapabilityAnswer", answer)
				}
				if got.Outcome != workflow.OutcomeUnknown {
					t.Fatalf("outcome = %s, want UNKNOWN", got.Outcome)
				}
				if len(got.Outputs) != 0 {
					t.Fatalf("outputs = %v, want empty: a killed run produced nothing", got.Outputs)
				}
				if !strings.Contains(got.Detail, "kill") || !strings.Contains(got.Detail, "fallback") {
					t.Fatalf("detail = %q, want the kill and fallback cited", got.Detail)
				}
				if runner.starts != 0 || runner.attaches != 0 {
					t.Fatalf("runner touched: starts=%d attaches=%d, want none", runner.starts, runner.attaches)
				}
				again, err := Handler(agent, deps)(context.Background(), call)
				if err != nil {
					t.Fatalf("second in-flight invoke err = %v, want fallback answer", err)
				}
				if !reflect.DeepEqual(answer, again) {
					t.Fatalf("fallback answers differ: %+v vs %+v", answer, again)
				}
			})
		}
	})
	t.Run("new_runs_still_refused", func(t *testing.T) {
		for _, sw := range arm {
			t.Run(sw.name, func(t *testing.T) {
				agent := testAgent()
				runner := &fakeRunner{result: succeededResult()}
				deps := Deps{Runner: runner, Ledger: NewMemoryLedger()}
				sw.set(&agent, &deps)
				_, err := Handler(agent, deps)(context.Background(), testCall(agent))
				if err == nil || !strings.Contains(err.Error(), "kill") {
					t.Fatalf("new invoke err = %v, want a kill-switch refusal", err)
				}
				if runner.starts != 0 || runner.attaches != 0 {
					t.Fatalf("runner touched: starts=%d attaches=%d, want none", runner.starts, runner.attaches)
				}
			})
		}
	})
	t.Run("nil_ledger_fails_closed", func(t *testing.T) {
		agent := testAgent()
		agent.KillSwitch = true
		runner := &fakeRunner{result: succeededResult()}
		deps := Deps{Runner: runner}
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("nil ledger panicked: %v, want a refusal error", recovered)
			}
		}()
		_, err := Handler(agent, deps)(context.Background(), testCall(agent))
		if err == nil || !strings.Contains(err.Error(), "kill") {
			t.Fatalf("nil-ledger invoke err = %v, want a kill-switch refusal", err)
		}
		if runner.starts != 0 || runner.attaches != 0 {
			t.Fatalf("runner touched: starts=%d attaches=%d, want none", runner.starts, runner.attaches)
		}
	})
}
