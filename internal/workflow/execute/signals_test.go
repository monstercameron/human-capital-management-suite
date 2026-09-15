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

// WF-RUN-005's driver-facing half is, like WF-RUN-004's, a drift check and
// some wiring; both are pure. The PostgreSQL proof that a SIGNAL node parks,
// resumes once and never twice lives with the scheduler dispatcher
// (internal/platform/execution/scheduler TestTodo_WF_RUN_005_ServedPath),
// where the real adapters are composed.

const signalFixtureNode = "signal_ack_received"

var (
	signalTestSignal       = uuid.MustParse("44444444-4444-4444-8444-444444444444")
	signalTestSubscription = uuid.MustParse("55555555-5555-4555-8555-555555555555")
)

func signalTestRequest() ResumeSignalRequest {
	return ResumeSignalRequest{
		Start:      runtime.StartRequest{TenantID: timerTestTenant},
		InstanceID: timerTestInstance, ExpectedInstanceVersion: 4,
		SignalID: signalTestSignal, SubscriptionID: signalTestSubscription, RecordedAt: timerTestAt,
	}
}

func matchedRow() MatchedSignal {
	return MatchedSignal{
		SignalID: signalTestSignal, SubscriptionID: signalTestSubscription, InstanceID: timerTestInstance,
		NodeID: signalFixtureNode, NodeAttempt: 1, Settled: true, ContinuationRef: "signal:" + signalTestSignal.String(),
	}
}

func TestCheckSignalDrift_DerivesTheOutcomeFromTheStoredReceipt(t *testing.T) {
	selection := runtime.WorkflowSelection{Plan: waitFixturePlan(t), WorkflowID: "wf"}
	outcome, err := checkSignalDrift(signalTestRequest(), selection, matchedRow())
	if err != nil {
		t.Fatalf("a settled receipt on a SIGNAL node was refused: %v", err)
	}
	want := frontier.NodeOutcome{NodeID: signalFixtureNode, Outcome: workflow.OutcomeSucceeded, OutputDigest: "signal:" + signalTestSignal.String()}
	if outcome != want {
		t.Fatalf("outcome = %+v, want %+v (the node and the reference come from the receipt)", outcome, want)
	}
}

func TestCheckSignalDrift_RefusesEveryDisagreementWithTheStoredReceipt(t *testing.T) {
	selection := runtime.WorkflowSelection{Plan: waitFixturePlan(t), WorkflowID: "wf"}
	for name, mutate := range map[string]func(*MatchedSignal){
		"a substituted signal":             func(r *MatchedSignal) { r.SignalID = uuid.New() },
		"a substituted subscription":       func(r *MatchedSignal) { r.SubscriptionID = uuid.New() },
		"an unsettled (refused) receipt":   func(r *MatchedSignal) { r.Settled = false },
		"a continuation naming raw data":   func(r *MatchedSignal) { r.ContinuationRef = `{"acknowledged":true}` },
		"a receipt for another instance":   func(r *MatchedSignal) { r.InstanceID = uuid.New() },
		"a node the plan does not declare": func(r *MatchedSignal) { r.NodeID = "no_such_node" },
		"a node that is not a SIGNAL":      func(r *MatchedSignal) { r.NodeID = waitFixtureNode },
	} {
		row := matchedRow()
		mutate(&row)
		if _, err := checkSignalDrift(signalTestRequest(), selection, row); !errors.Is(err, ErrSignalDrift) {
			t.Fatalf("%s: err = %v, want ErrSignalDrift", name, err)
		}
	}
}

func TestValidateResumeSignalConfig_RefusesIncompleteWiring(t *testing.T) {
	ctx := context.Background()
	if _, err := validateResumeSignalConfig(ctx, signalTestRequest(), nil); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("no SignalReader: err = %v, want ErrInvalidConfiguration", err)
	}
	for name, mutate := range map[string]func(*ResumeSignalRequest){
		"no tenant":            func(q *ResumeSignalRequest) { q.Start.TenantID = uuid.Nil },
		"no instance":          func(q *ResumeSignalRequest) { q.InstanceID = uuid.Nil },
		"no signal":            func(q *ResumeSignalRequest) { q.SignalID = uuid.Nil },
		"no subscription":      func(q *ResumeSignalRequest) { q.SubscriptionID = uuid.Nil },
		"no expected version":  func(q *ResumeSignalRequest) { q.ExpectedInstanceVersion = 0 },
		"no workflow resolver": func(q *ResumeSignalRequest) { q.Start.Resolver = nil },
	} {
		req := signalTestRequest()
		mutate(&req)
		if _, err := validateResumeSignalConfig(ctx, req, stubSignalReader{}); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("%s: err = %v, want ErrInvalidConfiguration", name, err)
		}
	}
	driver, err := New(Options{DB: stubBeginner{}, Steps: stubStepRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ResumeSignal(ctx, signalTestRequest()); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("ResumeSignal without a SignalReader: err = %v, want ErrInvalidConfiguration before any transaction", err)
	}
}

type stubSignalReader struct{}

func (stubSignalReader) LoadMatchedSignal(context.Context, runtime.Executor, MatchedSignalQuery) (MatchedSignal, error) {
	return MatchedSignal{}, errors.New("stub signal reader must not be called")
}

type recordingSubscriber struct {
	requests []SignalSubscriptionRequest
	handle   SignalHandle
	err      error
}

func (r *recordingSubscriber) CreateSubscription(_ context.Context, _ runtime.Executor, req SignalSubscriptionRequest) (SignalHandle, error) {
	r.requests = append(r.requests, req)
	return r.handle, r.err
}

func signalContinuation(node string) runtime.ContinuationRecord {
	return runtime.ContinuationRecord{
		TenantID: timerTestTenant, InstanceID: timerTestInstance, SourceNodeID: waitFixtureNode, SourceAttempt: 1,
		TargetNodeID: node, Kind: frontier.IntentSignalSubscriptionRequired, TargetAttempt: 1, RecordedAt: timerTestAt,
	}
}

func TestRequireSignalSubscription_WithoutAPortKeepsTheHistoricalRefusal(t *testing.T) {
	sink := &continuationSink{plan: waitFixturePlan(t)}
	if err := sink.requireSignalSubscription(context.Background(), nil, signalContinuation(signalFixtureNode)); !errors.Is(err, ErrUnsupportedContinuation) {
		t.Fatalf("no SignalSubscriber: err = %v, want ErrUnsupportedContinuation", err)
	}
}

func TestRequireSignalSubscription_ParksThroughThePort(t *testing.T) {
	plan := waitFixturePlan(t)
	port := &recordingSubscriber{handle: SignalHandle{SubscriptionID: signalTestSubscription, NodeID: signalFixtureNode, Attempt: 1}}
	sink := &continuationSink{plan: plan, signals: port, correlationID: "corr:1", subjectRefs: []string{"employment:jane"}}
	if err := sink.requireSignalSubscription(context.Background(), nil, signalContinuation(signalFixtureNode)); err != nil {
		t.Fatalf("parking a SIGNAL node: %v", err)
	}
	if len(port.requests) != 1 {
		t.Fatalf("subscriber calls = %d, want 1", len(port.requests))
	}
	got := port.requests[0]
	if got.Node.ID != signalFixtureNode || got.Node.Signal == nil || got.Plan != plan || got.CorrelationID != "corr:1" ||
		len(got.SubjectRefs) != 1 || !got.CreatedAt.Equal(timerTestAt) {
		t.Fatalf("subscription request = %+v, want the compiled SIGNAL node, pinned plan and instance facts", got)
	}

	for name, tc := range map[string]struct {
		sink *continuationSink
		node string
		want error
	}{
		"no pinned plan":              {sink: &continuationSink{signals: port}, node: signalFixtureNode, want: ErrInvalidConfiguration},
		"a node that is not SIGNAL":   {sink: &continuationSink{plan: plan, signals: port}, node: waitFixtureNode, want: ErrInvalidConfiguration},
		"a handle for another node":   {sink: &continuationSink{plan: plan, signals: &recordingSubscriber{handle: SignalHandle{NodeID: "elsewhere"}}}, node: signalFixtureNode, want: ErrInvalidConfiguration},
		"a failing durable subscribe": {sink: &continuationSink{plan: plan, signals: &recordingSubscriber{err: ErrSignalDrift}}, node: signalFixtureNode, want: ErrSignalDrift},
	} {
		if err := tc.sink.requireSignalSubscription(context.Background(), nil, signalContinuation(tc.node)); !errors.Is(err, tc.want) {
			t.Fatalf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
}

func TestReadyAndParked_ASignalSubscriptionParks(t *testing.T) {
	ready, parked := readyAndParked([]runtime.ContinuationRecord{
		{Kind: frontier.IntentReady, TargetNodeID: "b"},
		{Kind: frontier.IntentSignalSubscriptionRequired, TargetNodeID: signalFixtureNode},
		{Kind: frontier.IntentReady, TargetNodeID: "a"},
	})
	if !parked || len(ready) != 2 || ready[0] != "a" {
		t.Fatalf("readyAndParked = %v, %t; want sorted READY nodes and parked", ready, parked)
	}
}
