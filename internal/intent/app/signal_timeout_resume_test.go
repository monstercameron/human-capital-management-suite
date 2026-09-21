package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type timeoutExecutor struct {
	timerOnlyExecutor
	calls int
}

func (e *timeoutExecutor) ResumeSignalTimeout(context.Context, ExecutionSignalTimeoutResumeRequest) (ExecutionResult, error) {
	e.calls++
	return ExecutionResult{}, errors.New("not reached")
}

// TestCellResumeExpiredSignalRefusesBeforeResuming pins the served timeout
// resume's refusals: malformed wait identity, an executor that cannot resume
// timeouts, and every preparation refusal it shares with the matched-signal
// resume, all before the executor is ever called.
func TestCellResumeExpiredSignalRefusesBeforeResuming(t *testing.T) {
	subscriptionID, instanceID := uuid.NewString(), uuid.NewString()
	var nilCell *Cell
	if _, err := nilCell.ResumeExpiredSignal(context.Background(), instanceID, "await", 1, "not-a-uuid"); err == nil ||
		!strings.Contains(err.Error(), "subscription id") {
		t.Fatalf("malformed subscription id: err = %v", err)
	}
	if _, err := nilCell.ResumeExpiredSignal(context.Background(), instanceID, "await", 1, subscriptionID); err == nil ||
		!strings.Contains(err.Error(), "signal-timeout resume has no intent service") {
		t.Fatalf("no cell: err = %v", err)
	}
	timerOnly := &Cell{Service: &IntentService{executor: timerOnlyExecutor{}}}
	if _, err := timerOnly.ResumeExpiredSignal(context.Background(), instanceID, "await", 1, subscriptionID); !errors.Is(err, ErrSignalTimeoutResumeUnsupported) {
		t.Fatalf("timer-only executor: err = %v, want ErrSignalTimeoutResumeUnsupported", err)
	}

	executor := &timeoutExecutor{}
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
		if _, err := tc.cell.ResumeExpiredSignal(tc.ctx, instanceID, tc.node, tc.attempt, subscriptionID); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	if executor.calls != 0 {
		t.Fatalf("executor resumed %d times through a refused preparation", executor.calls)
	}
}
