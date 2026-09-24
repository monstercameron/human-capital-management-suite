package shadow_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/shadow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

type counts struct{ shadow.RowCounts }

func (c counts) Counts(context.Context) (shadow.RowCounts, error) { return c.RowCounts, nil }

func plan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	env, err := simulate.NewPromotionEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := env.Registry()
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := workflow.CompilePromotionReference(registry)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func TestTodo_WF_RUN_014(t *testing.T) {
	p := plan(t)
	contract, err := shadow.ContractFor(intent.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	initial := shadow.State{"tenant/fact": []byte("original")}
	result, err := shadow.Run(context.Background(), p, shadow.Options{Contract: contract, State: initial, Reads: initial, Steps: shadow.StepRunnerFunc(func(ctx context.Context, req shadow.StepRequest) (shadow.StepResult, error) {
		if req.Node.Type == workflow.StepCapability {
			if err := req.Ports.Effects.Invoke(ctx, req.Node.ID, shadow.AttemptExternal, "payroll", req.At); err == nil {
				t.Fatal("effect unexpectedly admitted")
			}
		}
		if req.Node.Type == workflow.StepEnd {
			return shadow.StepResult{TerminalCode: "PROMOTION_WOULD_COMMIT"}, nil
		}
		req.View.Set("tenant/fact", []byte("candidate"))
		if req.Node.Type == workflow.StepDecision {
			return shadow.StepResult{RouteKey: req.Node.Routes[0]}, nil
		}
		return shadow.StepResult{Outcome: workflow.OutcomeSucceeded}, nil
	}), RowCounter: counts{RowCounts: shadow.RowCounts{LedgerEvent: 2, WorkItem: 3, WorkflowTimer: 4, Outbox: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Complete || result.Terminal.Code != shadow.TerminalNotExecuted || result.Terminal.BusinessCode != "PROMOTION_WOULD_COMMIT" {
		t.Fatalf("result = %#v", result)
	}
	if string(initial["tenant/fact"]) != "original" || string(result.State["tenant/fact"]) != "candidate" {
		t.Fatalf("copy-on-write state leaked: initial=%q result=%q", initial["tenant/fact"], result.State["tenant/fact"])
	}
	if len(result.Effects) != 3 || result.RowCountsBefore != result.RowCountsAfter {
		t.Fatalf("effects/counts = %#v %#v", result.Effects, result.RowCountsAfter)
	}
}

func TestTodo_WF_RUN_014_Race(t *testing.T) {
	p := plan(t)
	contract, err := shadow.ContractFor(intent.EnvironmentTest)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 8
	var wg sync.WaitGroup
	results := make(chan shadow.Result, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := shadow.Run(context.Background(), p, shadow.Options{Contract: contract, Steps: shadow.StepRunnerFunc(func(context.Context, shadow.StepRequest) (shadow.StepResult, error) {
				return shadow.StepResult{Outcome: workflow.OutcomeSucceeded, TerminalCode: "DONE"}, nil
			})})
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	completed := 0
	for result := range results {
		if result.Complete && result.Terminal.Code == shadow.TerminalNotExecuted {
			completed++
		}
	}
	if completed != workers {
		t.Fatalf("concurrent shadow runs completed safely=%d, want %d", completed, workers)
	}
}

func TestTodo_WF_RUN_014_Fault(t *testing.T) {
	p := plan(t)
	contract, _ := shadow.ContractFor(intent.EnvironmentTest)
	_, err := shadow.Run(context.Background(), p, shadow.Options{Contract: intent.ModeContract{Mode: intent.ModeExecute, Environment: intent.EnvironmentTest}, Steps: shadow.StepRunnerFunc(func(context.Context, shadow.StepRequest) (shadow.StepResult, error) { return shadow.StepResult{}, nil })})
	if !errors.Is(err, shadow.ErrShadow) || shadow.CodeOf(err) != shadow.CodeModeRefused {
		t.Fatalf("wrong mode refusal: %v", err)
	}
	if _, err := shadow.Run(context.Background(), p, shadow.Options{Contract: contract, Steps: shadow.StepRunnerFunc(func(context.Context, shadow.StepRequest) (shadow.StepResult, error) {
		return shadow.StepResult{RouteKey: "NOT_DECLARED"}, nil
	})}); !errors.Is(err, shadow.ErrShadow) {
		t.Fatalf("missing route not refused: %v", err)
	}
}

func TestTodo_WF_RUN_014_Mutation(t *testing.T) {
	left := shadow.Trace{WorkflowID: "promotion", PlanDigest: "candidate", Entries: []shadow.TraceEntry{{Sequence: 1, NodeID: "calc", OutputDigest: "new"}}}
	right := shadow.Trace{WorkflowID: "promotion", PlanDigest: "live", Entries: []shadow.TraceEntry{{Sequence: 1, NodeID: "calc", OutputDigest: "old"}}}
	report := shadow.Compare(left, right)
	if report.Equal || len(report.Divergences) != 2 {
		t.Fatalf("report = %#v", report)
	}
}
