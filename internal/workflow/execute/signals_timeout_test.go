package execute

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// The timeout half of the driver-facing signal contract is, like the matched
// half, a drift check and some wiring; both are pure. The PostgreSQL proof
// that an expired wait advances TIMED_OUT once and never twice lives with
// the scheduler dispatcher, where the real adapters are composed.

func signalTimeoutTestRequest() ResumeSignalTimeoutRequest {
	return ResumeSignalTimeoutRequest{
		Start:      runtime.StartRequest{TenantID: timerTestTenant},
		InstanceID: timerTestInstance, ExpectedInstanceVersion: 4,
		SubscriptionID: signalTestSubscription, RecordedAt: timerTestAt,
	}
}

func expiredRow() ExpiredSubscription {
	return ExpiredSubscription{
		SubscriptionID: signalTestSubscription, InstanceID: timerTestInstance,
		NodeID: signalFixtureNode, NodeAttempt: 1, Settled: true,
		ContinuationRef: "signal-timeout:" + signalTestSubscription.String(),
	}
}

func TestCheckSignalTimeout_DerivesTimedOutFromTheStoredExpiry(t *testing.T) {
	selection := runtime.WorkflowSelection{Plan: waitFixturePlan(t), WorkflowID: "wf"}
	outcome, err := checkSignalTimeout(signalTimeoutTestRequest(), selection, expiredRow())
	if err != nil {
		t.Fatalf("a settled expiry on a SIGNAL node was refused: %v", err)
	}
	want := frontier.NodeOutcome{NodeID: signalFixtureNode, Outcome: workflow.Outcome("TIMED_OUT"),
		OutputDigest: "signal-timeout:" + signalTestSubscription.String()}
	if outcome != want {
		t.Fatalf("outcome = %+v, want %+v (TIMED_OUT with the timeout reference, never a signal)", outcome, want)
	}
}

func TestCheckSignalTimeout_RefusesEveryDisagreementWithTheStoredExpiry(t *testing.T) {
	selection := runtime.WorkflowSelection{Plan: waitFixturePlan(t), WorkflowID: "wf"}
	for name, mutate := range map[string]func(*ExpiredSubscription){
		"a substituted subscription":       func(r *ExpiredSubscription) { r.SubscriptionID = uuid.New() },
		"an unsettled (open) wait":         func(r *ExpiredSubscription) { r.Settled = false },
		"a continuation naming a signal":   func(r *ExpiredSubscription) { r.ContinuationRef = "signal:" + signalTestSignal.String() },
		"an expiry for another instance":   func(r *ExpiredSubscription) { r.InstanceID = uuid.New() },
		"a node the plan does not declare": func(r *ExpiredSubscription) { r.NodeID = "no_such_node" },
		"a node that is not a SIGNAL":      func(r *ExpiredSubscription) { r.NodeID = waitFixtureNode },
	} {
		row := expiredRow()
		mutate(&row)
		if _, err := checkSignalTimeout(signalTimeoutTestRequest(), selection, row); !errors.Is(err, ErrSignalDrift) {
			t.Fatalf("%s: err = %v, want ErrSignalDrift", name, err)
		}
	}
}

func TestValidateResumeSignalTimeoutConfig_RefusesIncompleteWiring(t *testing.T) {
	ctx := context.Background()
	if _, err := validateResumeSignalTimeoutConfig(ctx, signalTimeoutTestRequest(), nil); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("no SignalTimeoutReader: err = %v, want ErrInvalidConfiguration", err)
	}
	for name, mutate := range map[string]func(*ResumeSignalTimeoutRequest){
		"no tenant":            func(q *ResumeSignalTimeoutRequest) { q.Start.TenantID = uuid.Nil },
		"no instance":          func(q *ResumeSignalTimeoutRequest) { q.InstanceID = uuid.Nil },
		"no subscription":      func(q *ResumeSignalTimeoutRequest) { q.SubscriptionID = uuid.Nil },
		"no expected version":  func(q *ResumeSignalTimeoutRequest) { q.ExpectedInstanceVersion = 0 },
		"no workflow resolver": func(q *ResumeSignalTimeoutRequest) { q.Start.Resolver = nil },
	} {
		req := signalTimeoutTestRequest()
		mutate(&req)
		if _, err := validateResumeSignalTimeoutConfig(ctx, req, stubSignalTimeoutReader{}); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("%s: err = %v, want ErrInvalidConfiguration", name, err)
		}
	}
	driver, err := New(Options{DB: stubBeginner{}, Steps: stubStepRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ResumeSignalTimeout(ctx, signalTimeoutTestRequest()); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("ResumeSignalTimeout without a SignalTimeoutReader: err = %v, want ErrInvalidConfiguration before any transaction", err)
	}
}

type stubSignalTimeoutReader struct{}

func (stubSignalTimeoutReader) LoadExpiredSubscription(context.Context, runtime.Executor, ExpiredSubscriptionQuery) (ExpiredSubscription, error) {
	return ExpiredSubscription{}, errors.New("stub signal timeout reader must not be called")
}
