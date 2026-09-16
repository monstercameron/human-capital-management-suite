package intervention

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

var interventionInstant = time.Date(2026, 9, 15, 13, 0, 0, 0, time.UTC)

func execPlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("compile promotion plan: %v", err)
	}
	return plan
}

// attempt is one in-memory node attempt a fact set carries.
type attempt struct {
	node    string
	attempt int
	status  runtime.NodeStatus
}

// facts builds an instance at status with frontier and node attempts.
func facts(t *testing.T, plan *workflow.CompiledWorkflow, status runtime.InstanceStatus, frontier []string, nodes ...attempt) Facts {
	t.Helper()
	tenant := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	inst, err := runtime.NewInstance(tenant, uuid.MustParse("22222222-2222-4222-8222-222222222222"), "cell-local", plan,
		workflow.ModeExecute, "sha256:input", "corr", interventionInstant)
	if err != nil {
		t.Fatal(err)
	}
	inst.RuntimeStatus, inst.CurrentNodeIDs, inst.InstanceVersion = status, frontier, 7
	f := Facts{Plan: plan, Instance: inst}
	for _, n := range nodes {
		cn, _ := plan.Node(n.node)
		ne := runtime.NewNodeExecution(tenant, inst.InstanceID, n.node, n.attempt, cn.Type, n.status)
		f.Nodes = append(f.Nodes, ne)
	}
	return f
}

func request(f Facts, kind Kind) Request {
	return Request{Kind: kind, InstanceID: f.Instance.InstanceID, ExpectedVersion: f.Instance.InstanceVersion,
		Reason: "INC-42 stuck promotion", EvidenceRefs: []string{"ticket:INC-42", "log:run-9"},
		RequestedBy: "operator:ana", RequestedAt: interventionInstant}
}

// TestTodo_WF_RUN_015 proves the typed taxonomy: every executable kind,
// evaluated against the durable facts it needs, yields exactly one immutable
// plan naming the runtime transition; the forbidden force kind does not exist.
func TestTodo_WF_RUN_015(t *testing.T) {
	plan := execPlan(t)
	replacement := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	cases := []struct {
		name  string
		facts Facts
		req   func(Request) Request
		check func(t *testing.T, p Plan)
	}{
		{"RETRY", facts(t, plan, runtime.InstanceRunning, []string{workflow.PromotionNodeEvaluateBand},
			attempt{workflow.PromotionNodeEvaluateBand, 2, runtime.NodeFailed}),
			func(r Request) Request {
				r.Kind, r.NodeID, r.ExpectedAttempt = Retry, workflow.PromotionNodeEvaluateBand, 2
				return r
			},
			func(t *testing.T, p Plan) {
				if p.Node.Status != runtime.NodeFailed || p.Target.Attempt != 3 || p.NodeTo != runtime.NodeReady {
					t.Fatalf("retry plan = %+v", p)
				}
			}},
		{"RESUME", facts(t, plan, runtime.InstancePaused, []string{"wait_effective_date"}),
			func(r Request) Request { r.Kind = Resume; return r },
			func(t *testing.T, p Plan) {
				if p.InstanceFrom != runtime.InstancePaused || p.InstanceTo != runtime.InstanceRunning {
					t.Fatalf("resume plan = %+v", p)
				}
			}},
		{"SKIP", facts(t, plan, runtime.InstanceRunning, []string{"wait_effective_date"},
			attempt{"wait_effective_date", 1, runtime.NodeWaiting}),
			func(r Request) Request { r.Kind, r.NodeID = Skip, "wait_effective_date"; return r },
			func(t *testing.T, p Plan) {
				if p.Route != "SUCCEEDED" || p.Settle != runtime.NodeSkipped || p.OutputDigest == "" {
					t.Fatalf("skip plan = %+v", p)
				}
			}},
		{"SATISFY", facts(t, plan, runtime.InstanceRunning, []string{"wait_effective_date"},
			attempt{"wait_effective_date", 1, runtime.NodeWaiting}),
			func(r Request) Request {
				r.Kind, r.NodeID, r.Route = Satisfy, "wait_effective_date", "SUCCEEDED"
				return r
			},
			func(t *testing.T, p Plan) {
				if p.Settle != "" || p.NodeTo != runtime.NodeSucceeded || p.OutputDigest != EvidenceDigest([]string{"log:run-9", "ticket:INC-42"}) {
					t.Fatalf("satisfy plan = %+v", p)
				}
			}},
		{"OVERRIDE", facts(t, plan, runtime.InstanceRunning, []string{"still_valid"},
			attempt{"still_valid", 1, runtime.NodeReady}),
			func(r Request) Request { r.Kind, r.NodeID, r.Route = Override, "still_valid", "VALID"; return r },
			func(t *testing.T, p Plan) {
				if p.Settle != runtime.NodeOverridden || p.Route != "VALID" {
					t.Fatalf("override plan = %+v", p)
				}
			}},
		{"REWIND", facts(t, plan, runtime.InstancePaused, []string{"still_valid"},
			attempt{"revalidate", 1, runtime.NodeSucceeded}, attempt{"still_valid", 1, runtime.NodeReady}),
			func(r Request) Request { r.Kind, r.TargetNodeID = Rewind, "revalidate"; return r },
			func(t *testing.T, p Plan) {
				if p.Target != (NodeRef{NodeID: "revalidate", Attempt: 2, Status: runtime.NodeReady}) ||
					!slices.Equal(p.Cancels, []NodeRef{{NodeID: "still_valid", Attempt: 1, Status: runtime.NodeReady}}) {
					t.Fatalf("rewind plan = %+v", p)
				}
			}},
		{"SUPERSEDE", func() Facts {
			f := facts(t, plan, runtime.InstanceRunning, []string{"wait_effective_date"}, attempt{"wait_effective_date", 1, runtime.NodeWaiting})
			rep := f.Instance
			rep.InstanceID = replacement
			f.Replacement = &rep
			return f
		}(),
			func(r Request) Request { r.Kind, r.Replacement = Supersede, replacement; return r },
			func(t *testing.T, p Plan) {
				if p.InstanceTo != runtime.InstanceSuperseded || p.Replacement != replacement || len(p.Cancels) != 1 {
					t.Fatalf("supersede plan = %+v", p)
				}
			}},
		{"CANCEL", facts(t, plan, runtime.InstanceRunning, []string{"wait_effective_date"},
			attempt{"wait_effective_date", 1, runtime.NodeWaiting}),
			func(r Request) Request { r.Kind = Cancel; return r },
			func(t *testing.T, p Plan) {
				if p.InstanceTo != runtime.InstanceCancelling {
					t.Fatalf("cancel plan = %+v", p)
				}
			}},
		{"RECONCILE", facts(t, plan, runtime.InstanceRepairRequired, nil,
			attempt{"execute_promotion", 1, runtime.NodeRunning}),
			func(r Request) Request {
				r.Kind, r.NodeID, r.Observation = Reconcile, "execute_promotion", ObservedApplied
				return r
			},
			func(t *testing.T, p Plan) {
				if p.NodeTo != runtime.NodeSucceeded || p.Observation != ObservedApplied {
					t.Fatalf("reconcile plan = %+v", p)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req(request(tc.facts, ""))
			p, err := Evaluate(req, tc.facts)
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if p.Kind != req.Kind || p.InstanceID != req.InstanceID || p.ExpectedVersion != req.ExpectedVersion {
				t.Fatalf("plan identity = %+v", p)
			}
			tc.check(t, p)
			if req.Kind.Capability() == "" {
				t.Fatalf("%s has no capability family", req.Kind)
			}
		})
	}
	if Override.Capability() == Skip.Capability() || !strings.HasPrefix(Override.Capability(), "workflow.override.") {
		t.Fatalf("override authority %q is not its own family", Override.Capability())
	}
	if Version() != 2 || len(Kinds()) != 10 {
		t.Fatalf("contract %d with %d kinds", Version(), len(Kinds()))
	}
}

// TestTodo_WF_RUN_015_ForceCompleteIsGone holds the plan's "no generic force
// operation": the kind is refused as unsupported and no production source
// under internal/ spells it.
func TestTodo_WF_RUN_015_ForceCompleteIsGone(t *testing.T) {
	plan := execPlan(t)
	f := facts(t, plan, runtime.InstanceRunning, []string{"wait_effective_date"})
	for _, k := range []Kind{"FORCE_COMPLETE_WITH_EVIDENCE", "FORCE_WORKFLOW", "PAUSE", "REASSIGN", ""} {
		req := request(f, k)
		if _, err := Evaluate(req, f); CodeOf(err) != CodeNotSupported || !errors.Is(err, ErrIntervention) {
			t.Fatalf("%q = %v, want %s", k, err, CodeNotSupported)
		}
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	needle := "FORCE_COMPLETE" // split so this file is not itself a hit
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), needle) || strings.Contains(string(src), "ForceComplete") {
			t.Errorf("%s still spells a force-complete intervention", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestTodo_WF_RUN_015_Fault covers every typed denial: an unsupported action,
// a missing requester, reason or evidence, a stale version, a no-op and a
// failed precondition, per kind.
func TestTodo_WF_RUN_015_Fault(t *testing.T) {
	plan := execPlan(t)
	running := facts(t, plan, runtime.InstanceRunning, []string{"wait_effective_date"},
		attempt{"wait_effective_date", 1, runtime.NodeWaiting}, attempt{"still_valid", 1, runtime.NodeSucceeded},
		attempt{workflow.PromotionNodeEvaluateBand, 1, runtime.NodeSucceeded}, attempt{"execute_promotion", 1, runtime.NodeRunning})
	replacementID := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	base := func(k Kind) Request { return request(running, k) }
	with := func(k Kind, fn func(*Request)) Request { r := base(k); fn(&r); return r }

	cases := []struct {
		name  string
		facts Facts
		req   Request
		code  string
	}{
		{"compensate has no runner", running, with(Compensate, func(r *Request) { r.NodeID = "execute_promotion" }), CodeNotSupported},
		{"missing requester", running, with(Resume, func(r *Request) { r.RequestedBy = " " }), CodeUnauthorized},
		{"missing reason", running, with(Resume, func(r *Request) { r.Reason = "" }), CodeReasonRequired},
		{"missing evidence", running, with(Resume, func(r *Request) { r.EvidenceRefs = []string{" ", ""} }), CodeEvidenceRequired},
		{"missing instant", running, with(Resume, func(r *Request) { r.RequestedAt = time.Time{} }), CodeInvalidRequest},
		{"missing version", running, with(Resume, func(r *Request) { r.ExpectedVersion = 0 }), CodeInvalidRequest},
		{"retry without attempt", running, with(Retry, func(r *Request) { r.NodeID = "x" }), CodeInvalidRequest},
		{"satisfy without route", running, with(Satisfy, func(r *Request) { r.NodeID = "wait_effective_date" }), CodeInvalidRequest},
		{"supersede itself", running, with(Supersede, func(r *Request) { r.Replacement = r.InstanceID }), CodeInvalidRequest},
		{"reconcile without observation", running, with(Reconcile, func(r *Request) { r.NodeID = "execute_promotion" }), CodeInvalidRequest},
		{"rewind without target", running, with(Rewind, func(*Request) {}), CodeInvalidRequest},
		{"skip without node", running, with(Skip, func(*Request) {}), CodeInvalidRequest},
		{"stale version", running, with(Resume, func(r *Request) { r.ExpectedVersion = 6 }), CodeStaleVersion},
		{"resume a running instance", running, base(Resume), CodeNoOp},
		{"resume a blocked instance", facts(t, plan, runtime.InstanceBlocked, []string{"still_valid"}), base(Resume), CodePreconditionFailed},
		{"terminal instance", facts(t, plan, runtime.InstanceCompleted, nil), base(Resume), CodePreconditionFailed},
		{"supersede a superseded instance", facts(t, plan, runtime.InstanceSuperseded, nil), with(Supersede, func(r *Request) { r.Replacement = uuid.New() }), CodeNoOp},
		{"unknown node", running, with(Skip, func(r *Request) { r.NodeID = "nope" }), CodePreconditionFailed},
		{"never activated", running, with(Skip, func(r *Request) { r.NodeID = "revalidate" }), CodePreconditionFailed},
		{"retry stale attempt", running, with(Retry, func(r *Request) { r.NodeID, r.ExpectedAttempt = "wait_effective_date", 3 }), CodeStaleVersion},
		{"retry a waiting node", running, with(Retry, func(r *Request) { r.NodeID, r.ExpectedAttempt = "wait_effective_date", 1 }), CodeNoOp},
		{"retry a succeeded node", running, with(Retry, func(r *Request) { r.NodeID, r.ExpectedAttempt = "still_valid", 1 }), CodePreconditionFailed},
		{"skip a settled node", running, with(Skip, func(r *Request) { r.NodeID = workflow.PromotionNodeEvaluateBand }), CodeNoOp},
		{"skip a paused instance", facts(t, plan, runtime.InstancePaused, []string{"wait_effective_date"}, attempt{"wait_effective_date", 1, runtime.NodeWaiting}),
			with(Skip, func(r *Request) { r.NodeID = "wait_effective_date" }), CodePreconditionFailed},
		{"skip off the frontier", running, with(Skip, func(r *Request) { r.NodeID = "execute_promotion" }), CodePreconditionFailed},
		{"skip an approval", facts(t, plan, runtime.InstanceRunning, []string{"approve_manager"}, attempt{"approve_manager", 1, runtime.NodeWaiting}),
			with(Skip, func(r *Request) { r.NodeID = "approve_manager" }), CodePreconditionFailed},
		{"skip an effect", facts(t, plan, runtime.InstanceRunning, []string{"execute_promotion"}, attempt{"execute_promotion", 1, runtime.NodeReady}),
			with(Skip, func(r *Request) { r.NodeID = "execute_promotion" }), CodePreconditionFailed},
		{"skip a running node", facts(t, plan, runtime.InstanceRunning, []string{"revalidate"}, attempt{"revalidate", 1, runtime.NodeRunning}),
			with(Skip, func(r *Request) { r.NodeID = "revalidate" }), CodePreconditionFailed},
		{"skip into a work item", facts(t, plan, runtime.InstanceRunning, []string{"reapproval_task"}, attempt{"reapproval_task", 1, runtime.NodeWaiting}),
			with(Skip, func(r *Request) { r.NodeID = "reapproval_task" }), CodeNotSupported},
		{"skip an end", facts(t, plan, runtime.InstanceRunning, []string{"end_complete"}, attempt{"end_complete", 1, runtime.NodeReady}),
			with(Skip, func(r *Request) { r.NodeID = "end_complete" }), CodeNotSupported},
		{"satisfy a decision", facts(t, plan, runtime.InstanceRunning, []string{"still_valid"}, attempt{"still_valid", 1, runtime.NodeReady}),
			with(Satisfy, func(r *Request) { r.NodeID, r.Route = "still_valid", "VALID" }), CodePreconditionFailed},
		{"satisfy a ready node", facts(t, plan, runtime.InstanceRunning, []string{"revalidate"}, attempt{"revalidate", 1, runtime.NodeReady}),
			with(Satisfy, func(r *Request) { r.NodeID, r.Route = "revalidate", "SUCCEEDED" }), CodePreconditionFailed},
		{"satisfy an effect", facts(t, plan, runtime.InstanceRunning, []string{"execute_promotion"}, attempt{"execute_promotion", 1, runtime.NodeWaiting}),
			with(Satisfy, func(r *Request) { r.NodeID, r.Route = "execute_promotion", "SUCCEEDED" }), CodePreconditionFailed},
		{"satisfy an undeclared route", running, with(Satisfy, func(r *Request) { r.NodeID, r.Route = "wait_effective_date", "APPROVED" }), CodePreconditionFailed},
		{"override a non-decision", running, with(Override, func(r *Request) { r.NodeID, r.Route = "wait_effective_date", "SUCCEEDED" }), CodePreconditionFailed},
		{"override into a work item", facts(t, plan, runtime.InstanceRunning, []string{"approve_manager"}, attempt{"approve_manager", 1, runtime.NodeWaiting}),
			with(Override, func(r *Request) { r.NodeID, r.Route = "approve_manager", "APPROVED" }), CodeNotSupported},
		{"rewind a running instance", running, with(Rewind, func(r *Request) { r.TargetNodeID = "still_valid" }), CodePreconditionFailed},
		{"rewind to an unknown node", facts(t, plan, runtime.InstancePaused, []string{"x"}), with(Rewind, func(r *Request) { r.TargetNodeID = "nope" }), CodePreconditionFailed},
		{"rewind to an unexecuted node", facts(t, plan, runtime.InstancePaused, []string{"wait_effective_date"}), with(Rewind, func(r *Request) { r.TargetNodeID = "revalidate" }), CodePreconditionFailed},
		{"rewind to where control is", facts(t, plan, runtime.InstancePaused, []string{"revalidate"}, attempt{"revalidate", 1, runtime.NodeReady}),
			with(Rewind, func(r *Request) { r.TargetNodeID = "revalidate" }), CodeNoOp},
		{"rewind past a committed effect", facts(t, plan, runtime.InstancePaused, []string{"observe_payroll"},
			attempt{"revalidate", 1, runtime.NodeSucceeded}, attempt{"execute_promotion", 1, runtime.NodeSucceeded}, attempt{"observe_payroll", 1, runtime.NodeReady}),
			with(Rewind, func(r *Request) { r.TargetNodeID = "revalidate" }), CodePreconditionFailed},
		{"rewind to a wait", facts(t, plan, runtime.InstancePaused, []string{"revalidate"}, attempt{"wait_effective_date", 1, runtime.NodeSucceeded}),
			with(Rewind, func(r *Request) { r.TargetNodeID = "wait_effective_date" }), CodeNotSupported},
		{"supersede without replacement row", running, with(Supersede, func(r *Request) { r.Replacement = uuid.New() }), CodePreconditionFailed},
		{"supersede past an in-flight effect", func() Facts {
			f := running
			rep := f.Instance
			rep.InstanceID = replacementID
			f.Replacement = &rep
			return f
		}(), with(Supersede, func(r *Request) { r.Replacement = replacementID }), CodePreconditionFailed},
		{"supersede to a cancelled replacement", func() Facts {
			f := facts(t, plan, runtime.InstanceRunning, []string{"wait_effective_date"})
			rep := f.Instance
			rep.InstanceID, rep.RuntimeStatus = replacementID, runtime.InstanceCancelled
			f.Replacement = &rep
			return f
		}(), with(Supersede, func(r *Request) { r.Replacement = replacementID }), CodePreconditionFailed},
		{"supersede a blocked instance", func() Facts {
			f := facts(t, plan, runtime.InstanceBlocked, []string{"still_valid"})
			rep := f.Instance
			rep.InstanceID, rep.RuntimeStatus = replacementID, runtime.InstanceRunning
			f.Replacement = &rep
			return f
		}(), with(Supersede, func(r *Request) { r.Replacement = replacementID }), CodePreconditionFailed},
		{"reconcile a live instance", running, with(Reconcile, func(r *Request) { r.NodeID, r.Observation = "execute_promotion", ObservedApplied }), CodePreconditionFailed},
		{"reconcile a read-only node", facts(t, plan, runtime.InstanceRepairRequired, nil, attempt{"revalidate", 1, runtime.NodeRunning}),
			with(Reconcile, func(r *Request) { r.NodeID, r.Observation = "revalidate", ObservedApplied }), CodePreconditionFailed},
		{"reconcile a settled effect", facts(t, plan, runtime.InstanceRepairRequired, nil, attempt{"execute_promotion", 1, runtime.NodeSucceeded}),
			with(Reconcile, func(r *Request) { r.NodeID, r.Observation = "execute_promotion", ObservedNotApplied }), CodeNoOp},
		{"cancel a cancelling instance", facts(t, plan, runtime.InstanceCancelling, []string{"wait_effective_date"}), base(Cancel), CodeNoOp},
		{"cancel a blocked instance", facts(t, plan, runtime.InstanceCancelling, []string{"x"}), base(Cancel), CodeNoOp},
		{"facts for another instance", Facts{Plan: plan, Instance: runtime.Instance{InstanceID: uuid.New()}}, base(Resume), CodeInvalidRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Evaluate(tc.req, tc.facts)
			if CodeOf(err) != tc.code || !errors.Is(err, ErrIntervention) {
				t.Fatalf("Evaluate = %+v, %v; want %s", p, err, tc.code)
			}
			if p.Kind != "" {
				t.Fatalf("a denial returned a plan %+v", p)
			}
		})
	}
	mismatch := running
	mismatch.Instance.CompiledPlanHash = "sha256:another-plan"
	if _, err := Evaluate(base(Resume), mismatch); CodeOf(err) != CodePreconditionFailed {
		t.Fatalf("mismatched plan = %v", err)
	}
	var e *Error
	if err := refuse(CodeNoOp, "", "detail"); !errors.As(err, &e) || e.ErrorCode() != CodeNoOp || (*Error)(nil).ErrorCode() != "" ||
		!strings.Contains((&Error{Code: "C", Ref: "r", Detail: "d", Err: errors.New("cause")}).Error(), "cause") {
		t.Fatalf("typed error = %v", err)
	}
}

// TestTodo_WF_RUN_015_Race evaluates one request from many goroutines: the
// evaluation shares nothing, so every goroutine derives the same plan and the
// same decision identity and digest.
func TestTodo_WF_RUN_015_Race(t *testing.T) {
	plan := execPlan(t)
	f := facts(t, plan, runtime.InstanceRunning, []string{"wait_effective_date"}, attempt{"wait_effective_date", 1, runtime.NodeWaiting})
	req := request(f, Skip)
	req.NodeID = "wait_effective_date"
	binding := Binding{TenantID: f.Instance.TenantID, OperatorKind: "WORKFLOW_SKIP", IntentInstanceID: "intent:operator:1", IdempotencyKey: "skip-1"}
	const workers = 24
	digests := make([]string, workers)
	var wg sync.WaitGroup
	for i := range digests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := Evaluate(req, f)
			if err != nil {
				return
			}
			d, err := NewDecision(req, p, binding, Observed{InstanceStatus: runtime.InstanceRunning, InstanceVersion: 9})
			if err == nil {
				digests[i] = d.DecisionID.String() + "/" + d.Digest
			}
		}(i)
	}
	wg.Wait()
	for i := range digests {
		if digests[i] == "" || digests[i] != digests[0] {
			t.Fatalf("worker %d derived %q, worker 0 %q", i, digests[i], digests[0])
		}
	}
}

// TestTodo_WF_RUN_015_Mutation proves a sealed decision detects any change to
// what was decided, and that a decision cannot be sealed without authority or
// an observed transition.
func TestTodo_WF_RUN_015_Mutation(t *testing.T) {
	plan := execPlan(t)
	f := facts(t, plan, runtime.InstancePaused, []string{"wait_effective_date"})
	req := request(f, Resume)
	p, err := Evaluate(req, f)
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{TenantID: f.Instance.TenantID, OperatorKind: "WORKFLOW_RESUME", IntentInstanceID: "intent:operator:1", IdempotencyKey: "resume-1"}
	observed := Observed{InstanceStatus: runtime.InstanceRunning, InstanceVersion: 8}
	d, err := NewDecision(req, p, binding, observed)
	if err != nil || d.Verify() != nil || d.Capability != "workflow.instances.resume" {
		t.Fatalf("decision = %+v, %v", d, err)
	}
	for name, mutate := range map[string]func(*Decision){
		"reason":   func(d *Decision) { d.Reason = "tampered" },
		"evidence": func(d *Decision) { d.EvidenceRefs = append(d.EvidenceRefs, "forged") },
		"plan":     func(d *Decision) { d.Plan.InstanceTo = runtime.InstanceCompleted },
		"observed": func(d *Decision) { d.Observed.InstanceStatus = runtime.InstanceCompleted },
		"operator": func(d *Decision) { d.RequestedBy = "operator:mallory" },
	} {
		m := d
		m.EvidenceRefs = slices.Clone(d.EvidenceRefs)
		mutate(&m)
		if CodeOf(m.Verify()) != CodeDecisionMutated {
			t.Errorf("%s mutation was accepted", name)
		}
	}
	if _, err := NewDecision(req, p, Binding{TenantID: binding.TenantID}, observed); CodeOf(err) != CodeUnauthorized {
		t.Fatalf("unbound decision = %v", err)
	}
	if _, err := NewDecision(req, p, binding, Observed{InstanceVersion: req.ExpectedVersion}); CodeOf(err) != CodeNoOp {
		t.Fatalf("decision without a transition = %v", err)
	}
	other := req
	other.Kind = Retry
	if _, err := NewDecision(other, p, binding, observed); CodeOf(err) != CodeInvalidRequest {
		t.Fatalf("decision for another request = %v", err)
	}
	if DecisionID(binding.TenantID, req, "a") == DecisionID(binding.TenantID, req, "b") {
		t.Fatal("decision identity ignores the idempotency key")
	}
}
