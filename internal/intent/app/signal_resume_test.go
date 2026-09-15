package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type timerOnlyExecutor struct{}

func (timerOnlyExecutor) Execute(context.Context, runtime.StartRequest) (ExecutionResult, error) {
	return ExecutionResult{}, errors.New("not called")
}

func (timerOnlyExecutor) Resume(context.Context, ExecutionResumeRequest) (ExecutionResult, error) {
	return ExecutionResult{}, errors.New("not called")
}

func (timerOnlyExecutor) ResumeTimer(context.Context, ExecutionTimerResumeRequest) (ExecutionResult, error) {
	return ExecutionResult{}, errors.New("not called")
}

type signalExecutor struct {
	timerOnlyExecutor
	calls int
}

func (e *signalExecutor) ResumeSignal(context.Context, ExecutionSignalResumeRequest) (ExecutionResult, error) {
	e.calls++
	return ExecutionResult{}, errors.New("not reached")
}

type refusingBeginner struct{}

func (refusingBeginner) Begin(context.Context) (dbport.Tx, error) {
	return nil, errors.New("database unavailable")
}

// TestTodo_WF_RUN_005_CellResumeMatchedSignalRefusesBeforeResuming pins the
// served signal resume's refusals: malformed receipt identity, an executor
// that cannot resume signals, and every preparation refusal it shares with
// ResumeFiredTimer, all before the executor is ever called.
func TestTodo_WF_RUN_005_CellResumeMatchedSignalRefusesBeforeResuming(t *testing.T) {
	signalID, subscriptionID, instanceID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	var nilCell *Cell
	if _, err := nilCell.ResumeMatchedSignal(context.Background(), instanceID, "await", 1, "not-a-uuid", subscriptionID); err == nil ||
		!strings.Contains(err.Error(), "signal id") {
		t.Fatalf("malformed signal id: err = %v", err)
	}
	if _, err := nilCell.ResumeMatchedSignal(context.Background(), instanceID, "await", 1, signalID, "not-a-uuid"); err == nil ||
		!strings.Contains(err.Error(), "subscription id") {
		t.Fatalf("malformed subscription id: err = %v", err)
	}
	if _, err := nilCell.ResumeMatchedSignal(context.Background(), instanceID, "await", 1, signalID, subscriptionID); err == nil ||
		!strings.Contains(err.Error(), "signal resume has no intent service") {
		t.Fatalf("no cell: err = %v", err)
	}
	timerOnly := &Cell{Service: &IntentService{executor: timerOnlyExecutor{}}}
	if _, err := timerOnly.ResumeMatchedSignal(context.Background(), instanceID, "await", 1, signalID, subscriptionID); !errors.Is(err, ErrSignalResumeUnsupported) {
		t.Fatalf("timer-only executor: err = %v, want ErrSignalResumeUnsupported", err)
	}

	executor := &signalExecutor{}
	cases := map[string]struct {
		cell    *Cell
		ctx     context.Context
		node    string
		attempt int
		want    string
	}{
		"no journey": {cell: &Cell{Service: &IntentService{executor: executor}},
			ctx: context.Background(), node: "await", attempt: 1, want: "has no execution journey"},
		"incomplete authority": {cell: &Cell{Service: &IntentService{executor: executor}, Journey: &journeyEngine{db: refusingBeginner{}}},
			ctx: context.Background(), node: "await", attempt: 1, want: "has incomplete execution authority"},
	}
	for name, tc := range cases {
		if _, err := tc.cell.ResumeMatchedSignal(tc.ctx, instanceID, tc.node, tc.attempt, signalID, subscriptionID); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	if executor.calls != 0 {
		t.Fatalf("executor resumed %d times through a refused preparation", executor.calls)
	}
}
