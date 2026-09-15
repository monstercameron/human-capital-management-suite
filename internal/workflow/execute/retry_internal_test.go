package execute

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type noRetryTimers struct{}

type retryTestBeginner struct{}

func (retryTestBeginner) Begin(context.Context) (dbport.Tx, error) {
	return nil, errors.New("not used")
}

type retryTestSteps struct{}

func (retryTestSteps) Run(context.Context, StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.New("not used")
}

func (noRetryTimers) ScheduleRetry(context.Context, runtime.Executor, RetryTimerRequest) (TimerHandle, error) {
	return TimerHandle{}, errors.New("not used")
}

func validNodeRetry() *NodeRetryPolicy {
	return &NodeRetryPolicy{
		Backoffs:  map[string]RetryBackoff{"policy.retry.observation.bounded/v1": {BaseDelay: time.Second, MaxDelay: time.Minute, JitterFraction: 0.25}},
		Retryable: []runtime.FailureKind{runtime.FailureTransient},
		Budget:    ProvisionedRetryBudget(admission.NewProvisioner(), "dep", "v1"),
		Timers:    noRetryTimers{},
	}
}

// TestClassifyStableErrorClassFailsClosed maps only the stable vocabulary.
func TestClassifyStableErrorClassFailsClosed(t *testing.T) {
	for class, want := range map[string]RetryClassification{
		"TRANSIENT":         {Kind: runtime.FailureTransient},
		" timeout ":         {Kind: runtime.FailureTimeout},
		"THROTTLED":         {Kind: runtime.FailureThrottled},
		"UNAVAILABLE":       {Kind: runtime.FailureUnavailable},
		"DO_NOT_RETRY":      {Kind: runtime.FailurePermanent, DoNotRetry: true},
		"PROVIDER_REJECTED": {Kind: runtime.FailurePermanent},
		"":                  {Kind: runtime.FailurePermanent},
	} {
		if got := ClassifyStableErrorClass(class); got != want {
			t.Errorf("ClassifyStableErrorClass(%q) = %+v, want %+v", class, got, want)
		}
	}
	custom := &NodeRetryPolicy{Classify: func(string) RetryClassification { return RetryClassification{Kind: runtime.FailureThrottled} }}
	if got := custom.classify("ANY"); got.Kind != runtime.FailureThrottled {
		t.Fatalf("custom classifier ignored: %+v", got)
	}
	if got := (&NodeRetryPolicy{}).classify("TIMEOUT"); got.Kind != runtime.FailureTimeout {
		t.Fatalf("default classifier = %+v", got)
	}
}

// TestRetryJitterIsDeterministicAndBounded pins the replay-safe jitter.
func TestRetryJitterIsDeterministicAndBounded(t *testing.T) {
	tenant, instance := uuid.New(), uuid.New()
	delay := 40 * time.Second
	a := RetryJitter(tenant, instance, "node", 0.5)
	if a(2, delay) != RetryJitter(tenant, instance, "node", 0.5)(2, delay) {
		t.Fatal("same identity produced different jitter")
	}
	if a(2, delay) == a(3, delay) && a(3, delay) == a(4, delay) {
		t.Fatal("jitter ignores the attempt")
	}
	for attempt := 2; attempt < 50; attempt++ {
		got := a(attempt, delay)
		if got > delay || got < delay/2 || got%time.Millisecond != 0 {
			t.Fatalf("attempt %d jitter %s outside [%s, %s] or not whole milliseconds", attempt, got, delay/2, delay)
		}
	}
	if got := RetryJitter(tenant, instance, "node", 0)(2, delay); got != delay {
		t.Fatalf("zero fraction = %s, want %s", got, delay)
	}
}

// TestRetryBackoffKeyRoundTrips refuses keys that wake no later attempt.
func TestRetryBackoffKeyRoundTrips(t *testing.T) {
	if n, ok := retryBackoffAttempt(RetryBackoffKey(3)); !ok || n != 3 {
		t.Fatalf("round trip = %d, %v", n, ok)
	}
	for _, key := range []string{RetryBackoffKey(1), "retry-backoff|attempt:x", "digest", ""} {
		if _, ok := retryBackoffAttempt(key); ok {
			t.Errorf("key %q accepted", key)
		}
	}
}

// TestNodeRetryPolicyIsValidated refuses a policy Decide could not evaluate.
func TestNodeRetryPolicyIsValidated(t *testing.T) {
	steps := retryTestSteps{}
	if _, err := New(Options{DB: retryTestBeginner{}, Steps: steps, NodeRetry: validNodeRetry()}); err != nil {
		t.Fatalf("valid policy refused: %v", err)
	}
	for name, edit := range map[string]func(p *NodeRetryPolicy){
		"no budget":    func(p *NodeRetryPolicy) { p.Budget = nil },
		"no timers":    func(p *NodeRetryPolicy) { p.Timers = nil },
		"no retryable": func(p *NodeRetryPolicy) { p.Retryable = nil },
		"no backoffs":  func(p *NodeRetryPolicy) { p.Backoffs = nil },
		"zero base":    func(p *NodeRetryPolicy) { p.Backoffs["x"] = RetryBackoff{MaxDelay: time.Second} },
		"cap below base": func(p *NodeRetryPolicy) {
			p.Backoffs["x"] = RetryBackoff{BaseDelay: time.Minute, MaxDelay: time.Second}
		},
		"jitter above 1": func(p *NodeRetryPolicy) {
			p.Backoffs["x"] = RetryBackoff{BaseDelay: time.Second, MaxDelay: time.Second, JitterFraction: 1.5}
		},
		"negative deadline": func(p *NodeRetryPolicy) {
			p.Backoffs["x"] = RetryBackoff{BaseDelay: time.Second, MaxDelay: time.Second, Deadline: -time.Second}
		},
	} {
		p := validNodeRetry()
		edit(p)
		if _, err := New(Options{DB: retryTestBeginner{}, Steps: steps, NodeRetry: p}); !errors.Is(err, ErrInvalidConfiguration) {
			t.Errorf("%s: New = %v, want ErrInvalidConfiguration", name, err)
		}
	}
}

// TestProvisionedRetryBudgetSharesOneBudgetPerNode proves nested callers for
// the same instance node draw from one allowance of MaxAttempts-1.
func TestProvisionedRetryBudgetSharesOneBudgetPerNode(t *testing.T) {
	provisioner := admission.NewProvisioner()
	fn := ProvisionedRetryBudget(provisioner, "payroll", "v1")
	req := RetryBudgetRequest{TenantID: uuid.New(), InstanceID: uuid.New(), NodeID: "observe", MaxAttempts: 3,
		Retryable: []runtime.FailureKind{runtime.FailureTransient, runtime.FailureTimeout, runtime.FailureThrottled, runtime.FailureUnavailable, runtime.FailurePermanent}}
	first, err := fn(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fn(context.Background(), req)
	if err != nil || second != first {
		t.Fatalf("second ref = %+v, %v; want the same budget %+v", second, err, first)
	}
	snapshot, _ := provisioner.Snapshot(first.BudgetID)
	if snapshot.Allowed != 2 || len(snapshot.Retryable) != 4 || !strings.Contains(first.LogicalOperationID, req.InstanceID.String()) {
		t.Fatalf("budget = %+v, want 2 allowed retries over the 4 retryable classes", snapshot)
	}
	if _, err := ProvisionedRetryBudget(nil, "payroll", "v1")(context.Background(), req); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("nil provisioner = %v", err)
	}
	if _, err := fn(context.Background(), RetryBudgetRequest{TenantID: req.TenantID, InstanceID: uuid.New(), NodeID: "n", MaxAttempts: 0}); err == nil {
		t.Fatal("a budget with no retryable class was provisioned")
	}
}

// TestKeepRetryReasonCarriesTheDecision keeps the retry decision's reason in
// a poison filing, and a budget exhaustion only with a repair route.
func TestKeepRetryReasonCarriesTheDecision(t *testing.T) {
	base := func() runtime.QuarantineSpec {
		return runtime.QuarantineSpec{Terminal: runtime.RetryRoute{Decision: runtime.RouteTerminal, Reason: runtime.ReasonAttemptsExhausted}}
	}
	spec := base()
	keepRetryReason(&spec, PoisonWorkPolicy{}, nil)
	if spec.Terminal.Reason != runtime.ReasonAttemptsExhausted {
		t.Fatalf("nil route changed the reason: %+v", spec.Terminal)
	}
	spec = base()
	keepRetryReason(&spec, PoisonWorkPolicy{}, &runtime.RetryRoute{Reason: runtime.ReasonDeadlineExceeded})
	if spec.Terminal.Reason != runtime.ReasonDeadlineExceeded {
		t.Fatalf("deadline reason = %+v", spec.Terminal)
	}
	spec = base()
	keepRetryReason(&spec, PoisonWorkPolicy{}, &runtime.RetryRoute{Reason: runtime.ReasonBudgetExhausted, RepairRoute: "repair:budget"})
	if spec.Terminal.Reason != runtime.ReasonBudgetExhausted || spec.Terminal.RepairRoute != "repair:budget" {
		t.Fatalf("budget with decision route = %+v", spec.Terminal)
	}
	spec = base()
	keepRetryReason(&spec, PoisonWorkPolicy{RepairRoute: "repair:policy"}, &runtime.RetryRoute{Reason: runtime.ReasonBudgetExhausted, RepairRoute: "repair:budget"})
	if spec.Terminal.RepairRoute != "repair:policy" {
		t.Fatalf("policy repair route not preferred: %+v", spec.Terminal)
	}
	spec = base()
	keepRetryReason(&spec, PoisonWorkPolicy{}, &runtime.RetryRoute{Reason: runtime.ReasonBudgetExhausted})
	if spec.Terminal.Reason != runtime.ReasonAttemptsExhausted {
		t.Fatalf("routeless budget exhaustion = %+v, want attempts exhausted", spec.Terminal)
	}
	var none *nodeRetryDecision
	if none.poisonRoute() != nil || (&nodeRetryDecision{kind: RetryDecisionAfter}).poisonRoute() != nil {
		t.Fatal("a non-terminal decision produced a poison route")
	}
	if route := (&nodeRetryDecision{kind: RetryDecisionTerminal, route: runtime.RetryRoute{Reason: runtime.ReasonDoNotRetry}}).poisonRoute(); route == nil || route.Reason != runtime.ReasonDoNotRetry {
		t.Fatalf("terminal poison route = %+v", route)
	}
}

// TestCheckRetryTimerRefusesDrift refuses every row a retry may not wake on.
func TestCheckRetryTimerRefusesDrift(t *testing.T) {
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	instance, timerID := uuid.New(), uuid.New()
	req := ResumeTimerRequest{InstanceID: instance, TimerID: timerID}
	good := FiredTimer{TimerID: timerID, InstanceID: instance, NodeID: promotionexec.NodeObservePayroll,
		Key: RetryBackoffKey(2), State: TimerStateFired, Kind: TimerKindRetryBackoff}
	node, attempt, err := checkRetryTimer(req, plan, good)
	if err != nil || node.ID != promotionexec.NodeObservePayroll || attempt != 2 {
		t.Fatalf("good row = %s/%d, %v", node.ID, attempt, err)
	}
	for name, edit := range map[string]func(r *FiredTimer, q *ResumeTimerRequest){
		"pending":          func(r *FiredTimer, _ *ResumeTimerRequest) { r.State = "PENDING" },
		"other instance":   func(r *FiredTimer, _ *ResumeTimerRequest) { r.InstanceID = uuid.New() },
		"other timer":      func(r *FiredTimer, _ *ResumeTimerRequest) { r.TimerID = uuid.New() },
		"node w/o retry":   func(r *FiredTimer, _ *ResumeTimerRequest) { r.NodeID = promotionexec.NodeEndComplete },
		"unknown node":     func(r *FiredTimer, _ *ResumeTimerRequest) { r.NodeID = "ghost" },
		"bad key":          func(r *FiredTimer, _ *ResumeTimerRequest) { r.Key = "digest" },
		"beyond the cap":   func(r *FiredTimer, _ *ResumeTimerRequest) { r.Key = RetryBackoffKey(3) },
		"outcome mismatch": func(_ *FiredTimer, q *ResumeTimerRequest) { q.Outcome = frontier.NodeOutcome{NodeID: "other"} },
	} {
		row, q := good, req
		edit(&row, &q)
		if _, _, err := checkRetryTimer(q, plan, row); !errors.Is(err, ErrTimerDrift) {
			t.Errorf("%s: checkRetryTimer = %v, want ErrTimerDrift", name, err)
		}
	}
}

// TestDecideAndApplyRetryRefuseInconsistentState covers the refusals that
// happen before any storage is touched.
func TestDecideAndApplyRetryRefuseInconsistentState(t *testing.T) {
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	run := runContext{selection: runtime.WorkflowSelection{WorkflowID: plan.WorkflowID, Plan: plan}, instanceID: uuid.New()}
	failed := frontier.NodeOutcome{NodeID: promotionexec.NodeObservePayroll, Failed: true, ErrorClass: "TRANSIENT"}

	// No NodeRetry: the pre-WF-RUN-006 exhaustion routing, no decision.
	legacy := &Driver{}
	routed, decision, err := legacy.decideRetry(context.Background(), nil, run, failed, 2, time.Now())
	if err != nil || decision != nil || routed.Failed || routed.Outcome == "" {
		t.Fatalf("legacy exhausted observe = %+v %+v %v, want the exhaustion route", routed, decision, err)
	}
	if same, decision, err := legacy.decideRetry(context.Background(), nil, run, frontier.NodeOutcome{NodeID: "ghost", Failed: true}, 1, time.Now()); err != nil || decision != nil || same.NodeID != "ghost" {
		t.Fatalf("unknown node = %+v %+v %v", same, decision, err)
	}

	unresolved := validNodeRetry()
	unresolved.Backoffs = map[string]RetryBackoff{"other": {BaseDelay: time.Second, MaxDelay: time.Second}}
	d := &Driver{opts: Options{NodeRetry: unresolved}}
	if _, _, err := d.decideRetry(context.Background(), nil, run, failed, 1, time.Now()); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("unresolved backoff = %v, want ErrInvalidConfiguration", err)
	}
	success := frontier.NodeOutcome{NodeID: promotionexec.NodeObservePayroll, Outcome: workflow.Outcome("PASS")}
	if out, decision, err := d.decideRetry(context.Background(), nil, run, success, 1, time.Now()); err != nil || decision != nil || out != success {
		t.Fatalf("success outcome = %+v %+v %v", out, decision, err)
	}

	node, _ := plan.Node(promotionexec.NodeObservePayroll)
	advanced := runtime.AdvanceReceipt{CompletedState: string(runtime.NodeSucceeded)}
	if _, err := d.applyRetry(context.Background(), nil, run, &advanced, &nodeRetryDecision{kind: RetryDecisionAfter, node: node}, time.Now()); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("retry over a settled attempt = %v, want ErrInvalidConfiguration", err)
	}
	if timers, err := d.applyRetry(context.Background(), nil, run, &advanced, &nodeRetryDecision{kind: RetryDecisionTerminal}, time.Now()); err != nil || timers != nil {
		t.Fatalf("terminal decision applied %v, %v", timers, err)
	}
	if !retryParked([]TimerHandle{{NodeID: "a"}, {NodeID: "b", RetryBackoff: true}}, "b") || retryParked([]TimerHandle{{NodeID: "a"}}, "a") {
		t.Fatal("retryParked misreads the handles")
	}
	if latest := latestExecution([]runtime.NodeExecution{{NodeID: "n", Attempt: 1}, {NodeID: "n", Attempt: 3}, {NodeID: "m", Attempt: 9}}, "n"); latest.Attempt != 3 {
		t.Fatalf("latest attempt = %d, want 3", latest.Attempt)
	}
}
