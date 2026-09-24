package workflowcontrol

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/operatorjournal"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/intervention"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

var evidence = []string{"ticket:INC-7", "log:driver-run-3"}

// durableJournal is the PostgreSQL operator journal over the fixture tenant.
func (f *fixture) durableJournal() operator.Journal {
	return &operatorjournal.Journal{DB: f.conn, TenantIDs: func(values.TenantId) (uuid.UUID, error) { return f.tenant, nil }}
}

// iv builds an intervention command.
func (f *fixture) iv(id uuid.UUID, version int64, key string, spec InterventionSpec) Command {
	cmd := f.cmd(id, version, key)
	if spec.EvidenceRefs == nil {
		spec.EvidenceRefs = evidence
	}
	cmd.Intervention = &spec
	if spec.Kind == intervention.Retry {
		retry := cmd
		retry.NodeID, retry.ExpectedAttempt = spec.NodeID, spec.ExpectedAttempt
		scopeOverride.Store(id.String(), retry.Scope(operator.KindWorkflowRetryNode))
	} else {
		scopeOverride.Delete(id.String())
	}
	return cmd
}

// decisions reads the recorded decisions of an instance.
func (f *fixture) decisions(id uuid.UUID) []intervention.Decision {
	f.t.Helper()
	var out []intervention.Decision
	f.tx(func(tx dbport.Tx) (err error) {
		out, err = intervention.DecisionStore{}.Load(context.Background(), tx, f.tenant, id)
		return err
	})
	return out
}

// receipt reads the durable operator receipt outcome for a key ("" if none).
func (f *fixture) receipt(key string) string {
	f.t.Helper()
	var outcome string
	err := f.db.QueryRow(context.Background(), `SELECT outcome FROM operator_control_receipt WHERE tenant_id = $1 AND idempotency_key = $2`, f.tenant, key).Scan(&outcome)
	if err != nil && !errors.Is(err, dbport.ErrNoRows) {
		f.t.Fatal(err)
	}
	return outcome
}

func latest(nodes []runtime.NodeExecution, id string) runtime.NodeExecution {
	var out runtime.NodeExecution
	for _, n := range nodes {
		if n.NodeID == id && n.Attempt >= out.Attempt {
			out = n
		}
	}
	return out
}

// accepted asserts an APPLIED intervention left one verified decision naming
// the observed transition and a durable APPLIED operator receipt.
func accepted(t *testing.T, f *fixture, what string, id uuid.UUID, key string, res Result, err error) intervention.Decision {
	t.Helper()
	expect(t, what, res, err, OutcomeApplied, "")
	ds := f.decisions(id)
	if len(ds) == 0 {
		t.Fatalf("%s: no decision recorded", what)
	}
	d := ds[len(ds)-1]
	if d.DecisionID.String() != res.DecisionID || d.Digest != res.DecisionDigest || d.IntentInstanceID != res.IntentInstanceID ||
		d.Observed.InstanceVersion != res.InstanceVersion || d.Verify() != nil || d.RequestedBy != "operator:ana" || len(d.EvidenceRefs) != 2 {
		t.Fatalf("%s: decision %+v does not match result %+v", what, d, res)
	}
	if got := f.receipt(key); got != string(operator.OutcomeApplied) {
		t.Fatalf("%s: operator receipt outcome = %q, want APPLIED", what, got)
	}
	return d
}

// TestTodo_WF_RUN_015_Integration drives every supported intervention through
// the governed operator gateway over PostgreSQL: each accepted one performs
// its transition through the runtime, records one immutable decision with the
// transition read back, and journals an APPLIED operator receipt; replaying
// its key changes nothing.
func TestTodo_WF_RUN_015_Integration(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	c := f.controller(f.durableJournal())

	t.Run("RETRY", func(t *testing.T) {
		id, v := f.instance([]string{"evaluate_band"}, node{"evaluate_band", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeFailed}})
		cmd := f.iv(id, v, "iv-retry", InterventionSpec{Kind: intervention.Retry, NodeID: "evaluate_band", ExpectedAttempt: 1})
		res, err := c.Intervene(ctx, cmd)
		d := accepted(t, f, "retry", id, "iv-retry", res, err)
		_, nodes := f.load(id)
		if n := latest(nodes, "evaluate_band"); n.Attempt != 2 || n.Status != runtime.NodeReady || f.readyWork(id, "evaluate_band") != 1 ||
			d.Observed.Node != (intervention.NodeRef{NodeID: "evaluate_band", Attempt: 2, Status: runtime.NodeReady}) || d.OperatorKind != string(operator.KindWorkflowRetryNode) {
			t.Fatalf("retry left %+v, decision %+v", n, d)
		}
		replay, err := c.Intervene(ctx, cmd)
		if err != nil || !replay.Replayed || replay.DecisionID != res.DecisionID || len(f.decisions(id)) != 1 {
			t.Fatalf("replayed retry = %+v, %v", replay, err)
		}
	})

	t.Run("RESUME", func(t *testing.T) {
		id, v := f.instance([]string{"approve_manager"})
		paused, err := c.Pause(ctx, f.cmd(id, v, "iv-pause"))
		expect(t, "pause", paused, err, OutcomeApplied, "")
		res, err := c.Intervene(ctx, f.iv(id, paused.InstanceVersion, "iv-resume", InterventionSpec{Kind: intervention.Resume}))
		d := accepted(t, f, "resume", id, "iv-resume", res, err)
		if inst, _ := f.load(id); inst.RuntimeStatus != runtime.InstanceRunning || d.Plan.InstanceFrom != runtime.InstancePaused || d.Observed.InstanceStatus != runtime.InstanceRunning {
			t.Fatalf("resume left %s, decision %+v", inst.RuntimeStatus, d)
		}
	})

	t.Run("SKIP", func(t *testing.T) {
		id, v := f.instance([]string{"wait_effective_date"}, node{"wait_effective_date", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeWaiting}})
		res, err := c.Intervene(ctx, f.iv(id, v, "iv-skip", InterventionSpec{Kind: intervention.Skip, NodeID: "wait_effective_date"}))
		d := accepted(t, f, "skip", id, "iv-skip", res, err)
		inst, nodes := f.load(id)
		skipped := latest(nodes, "wait_effective_date")
		if skipped.Status != runtime.NodeSkipped || skipped.Refs.RepairRef != "intervention:"+res.DecisionID || skipped.OutputArtifactRef != intervention.EvidenceDigest(evidence) {
			t.Fatalf("skipped node = %+v", skipped)
		}
		if next := latest(nodes, "revalidate"); next.Status != runtime.NodeReady || len(inst.CurrentNodeIDs) != 1 || inst.CurrentNodeIDs[0] != "revalidate" || f.readyWork(id, "revalidate") != 1 {
			t.Fatalf("skip did not route to revalidate: frontier %v, node %+v", inst.CurrentNodeIDs, next)
		}
		if d.Plan.Settle != runtime.NodeSkipped || d.Observed.Node.Status != runtime.NodeSkipped || d.Capability != "workflow.repair.skip" {
			t.Fatalf("skip decision = %+v", d)
		}
	})

	t.Run("SATISFY", func(t *testing.T) {
		id, v := f.instance([]string{"wait_effective_date"}, node{"wait_effective_date", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeWaiting}})
		res, err := c.Intervene(ctx, f.iv(id, v, "iv-satisfy", InterventionSpec{Kind: intervention.Satisfy, NodeID: "wait_effective_date", Route: "SUCCEEDED"}))
		accepted(t, f, "satisfy", id, "iv-satisfy", res, err)
		inst, nodes := f.load(id)
		if n := latest(nodes, "wait_effective_date"); n.Status != runtime.NodeSucceeded || n.OutputArtifactRef != intervention.EvidenceDigest(evidence) || inst.CurrentNodeIDs[0] != "revalidate" {
			t.Fatalf("satisfied node = %+v, frontier %v", n, inst.CurrentNodeIDs)
		}
	})

	t.Run("OVERRIDE", func(t *testing.T) {
		id, v := f.instance([]string{"still_valid"}, node{"still_valid", nil})
		spec := InterventionSpec{Kind: intervention.Override, NodeID: "still_valid", Route: "VALID"}
		// A repair grant is not override authority.
		wrong, err := c.Intervene(ctx, f.iv(id, v, "iv-override-repair-grant", spec))
		expect(t, "override under a repair grant", wrong, err, OutcomeDenied, intervention.CodeUnauthorized)
		if wrong.Cause != operator.CodeAuthorityMismatch || len(f.decisions(id)) != 0 {
			t.Fatalf("override under a repair grant = %+v", wrong)
		}
		f.auth.mu.Lock()
		f.auth.role = jit.RoleIncidentResponder
		f.auth.mu.Unlock()
		defer func() { f.auth.mu.Lock(); f.auth.role = jit.RoleIntegrityRepair; f.auth.mu.Unlock() }()
		res, err := c.Intervene(ctx, f.iv(id, v, "iv-override", spec))
		d := accepted(t, f, "override", id, "iv-override", res, err)
		inst, nodes := f.load(id)
		if n := latest(nodes, "still_valid"); n.Status != runtime.NodeOverridden || inst.CurrentNodeIDs[0] != "execute_promotion" || d.Capability != "workflow.override.decision" {
			t.Fatalf("override left %+v, frontier %v, decision %+v", n, inst.CurrentNodeIDs, d)
		}
	})

	t.Run("REWIND", func(t *testing.T) {
		id, v := f.instance([]string{"still_valid"},
			node{"revalidate", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded}}, node{"still_valid", nil})
		paused, err := c.Pause(ctx, f.cmd(id, v, "iv-rewind-pause"))
		expect(t, "pause before rewind", paused, err, OutcomeApplied, "")
		res, err := c.Intervene(ctx, f.iv(id, paused.InstanceVersion, "iv-rewind", InterventionSpec{Kind: intervention.Rewind, TargetNodeID: "revalidate"}))
		d := accepted(t, f, "rewind", id, "iv-rewind", res, err)
		inst, nodes := f.load(id)
		first, again := nodes[0], latest(nodes, "revalidate")
		if len(nodes) != 3 || first.NodeID != "revalidate" || first.Status != runtime.NodeSucceeded || again.Attempt != 2 || again.Status != runtime.NodeReady ||
			latest(nodes, "still_valid").Status != runtime.NodeCancelled || inst.RuntimeStatus != runtime.InstancePaused || inst.CurrentNodeIDs[0] != "revalidate" ||
			f.readyWork(id, "revalidate") != 1 || d.Observed.Node.Attempt != 2 {
			t.Fatalf("rewind left %s %v with attempts %+v, decision %+v", inst.RuntimeStatus, inst.CurrentNodeIDs, nodes, d)
		}
	})

	t.Run("SUPERSEDE", func(t *testing.T) {
		id, v := f.instance([]string{"wait_effective_date"}, node{"wait_effective_date", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeWaiting}})
		replacement, _ := f.instance([]string{"snapshot_worker"})
		res, err := c.Intervene(ctx, f.iv(id, v, "iv-supersede", InterventionSpec{Kind: intervention.Supersede, Replacement: replacement}))
		d := accepted(t, f, "supersede", id, "iv-supersede", res, err)
		inst, nodes := f.load(id)
		if inst.RuntimeStatus != runtime.InstanceSuperseded || inst.CompletedAt == nil || inst.CompletionDimensions.RequestState != "SUPERSEDED" ||
			latest(nodes, "wait_effective_date").Status != runtime.NodeCancelled || d.Plan.Replacement != replacement {
			t.Fatalf("supersede left %+v, nodes %+v, decision %+v", inst, nodes, d)
		}
		if rep, _ := f.load(replacement); rep.RuntimeStatus != runtime.InstanceRunning {
			t.Fatalf("the replacement changed to %s", rep.RuntimeStatus)
		}
	})

	t.Run("CANCEL", func(t *testing.T) {
		id, v := f.instance([]string{"approve_manager"})
		res, err := c.Intervene(ctx, f.iv(id, v, "iv-cancel", InterventionSpec{Kind: intervention.Cancel}))
		d := accepted(t, f, "cancel", id, "iv-cancel", res, err)
		if inst, _ := f.load(id); inst.RuntimeStatus != runtime.InstanceCancelled || d.Plan.InstanceTo != runtime.InstanceCancelling ||
			d.Observed.InstanceStatus != runtime.InstanceCancelled || d.Capability != "workflow.instances.cancel" {
			t.Fatalf("cancel left %s, decision %+v", inst.RuntimeStatus, d)
		}
		twice, err := c.Intervene(ctx, f.iv(id, d.Observed.InstanceVersion, "iv-cancel-twice", InterventionSpec{Kind: intervention.Cancel}))
		expect(t, "cancel a cancelled instance", twice, err, OutcomeDenied, intervention.CodePreconditionFailed)
	})

	t.Run("RECONCILE", func(t *testing.T) {
		id, v := f.instance([]string{"execute_promotion"}, node{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning}})
		routed, err := c.Cancel(ctx, f.cmd(id, v, "iv-reconcile-cancel"))
		expect(t, "cancel an in-flight effect", routed, err, OutcomeRepairRequired, CodeEffectInFlight)
		res, err := c.Intervene(ctx, f.iv(id, routed.InstanceVersion, "iv-reconcile",
			InterventionSpec{Kind: intervention.Reconcile, NodeID: "execute_promotion", Observation: intervention.ObservedApplied}))
		d := accepted(t, f, "reconcile", id, "iv-reconcile", res, err)
		inst, nodes := f.load(id)
		n := latest(nodes, "execute_promotion")
		if n.Status != runtime.NodeSucceeded || len(n.Refs.EffectRefs) != 2 || n.CompletedAt == nil || inst.RuntimeStatus != runtime.InstanceRepairRequired ||
			d.Plan.Observation != intervention.ObservedApplied || d.Observed.Node.Status != runtime.NodeSucceeded {
			t.Fatalf("reconcile left %+v on a %s instance, decision %+v", n, inst.RuntimeStatus, d)
		}
	})
}

// TestTodo_WF_RUN_015_Fault proves every denial writes nothing: no workflow
// state, no decision and no operator receipt, whether the intervention is a
// no-op, unsupported, missing its reason, evidence or authority, or would
// route into work the gateway cannot materialize.
func TestTodo_WF_RUN_015_Fault(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	c := f.controller(f.durableJournal())
	id, v := f.instance([]string{"wait_effective_date"}, node{"wait_effective_date", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeWaiting}})
	task, tv := f.instance([]string{"reapproval_task"}, node{"reapproval_task", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeWaiting}})
	skip := InterventionSpec{Kind: intervention.Skip, NodeID: "wait_effective_date"}

	cases := []struct {
		name  string
		setup func() func()
		cmd   Command
		code  string
		cause string
	}{
		{"resume is a no-op", nil, f.iv(id, v, "f-noop", InterventionSpec{Kind: intervention.Resume}), intervention.CodeNoOp, ""},
		{"force complete does not exist", nil, f.iv(id, v, "f-force", InterventionSpec{Kind: "FORCE_COMPLETE_WITH_EVIDENCE"}), intervention.CodeNotSupported, ""},
		{"compensate has no runner", nil, f.iv(id, v, "f-compensate", InterventionSpec{Kind: intervention.Compensate, NodeID: "wait_effective_date"}), intervention.CodeNotSupported, ""},
		{"missing reason", nil, func() Command { c := f.iv(id, v, "f-reason", skip); c.ReasonRef = ""; return c }(), intervention.CodeReasonRequired, ""},
		{"missing evidence", nil, f.iv(id, v, "f-evidence", InterventionSpec{Kind: intervention.Skip, NodeID: "wait_effective_date", EvidenceRefs: []string{}}), intervention.CodeEvidenceRequired, ""},
		{"stale version", nil, f.iv(id, v-1, "f-stale", skip), intervention.CodeStaleVersion, ""},
		{"skip into a work item", nil, f.iv(task, tv, "f-workitem", InterventionSpec{Kind: intervention.Skip, NodeID: "reapproval_task"}), intervention.CodeNotSupported, ""},
		{"unknown instance", nil, f.iv(uuid.New(), 3, "f-unknown", skip), runtime.CodeInstanceNotFound, ""},
		{"no grant", func() func() {
			f.auth.mu.Lock()
			f.auth.role = jit.RoleSupportReadOnly
			f.auth.mu.Unlock()
			return func() { f.auth.mu.Lock(); f.auth.role = jit.RoleIntegrityRepair; f.auth.mu.Unlock() }
		}, f.iv(id, v, "f-grant", skip), intervention.CodeUnauthorized, operator.CodeAuthorityMismatch},
		{"no second approver", func() func() {
			f.auth.mu.Lock()
			f.auth.dual = false
			f.auth.mu.Unlock()
			return func() { f.auth.mu.Lock(); f.auth.dual = true; f.auth.mu.Unlock() }
		}, f.iv(id, v, "f-dual", skip), intervention.CodeUnauthorized, operator.CodeDualControlRequired},
		{"no simulation", func() func() {
			f.auth.mu.Lock()
			f.auth.simulate = false
			f.auth.mu.Unlock()
			return func() { f.auth.mu.Lock(); f.auth.simulate = true; f.auth.mu.Unlock() }
		}, f.iv(id, v, "f-simulation", skip), intervention.CodeUnauthorized, operator.CodeSimulationRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				defer tc.setup()()
			}
			f.iv(tc.cmd.InstanceID, tc.cmd.ExpectedVersion, tc.cmd.IdempotencyKey, *tc.cmd.Intervention) // refresh the simulation scope
			res, err := c.Intervene(ctx, tc.cmd)
			expect(t, tc.name, res, err, OutcomeDenied, tc.code)
			if res.Cause != tc.cause || res.DecisionID != "" {
				t.Fatalf("%s = %+v, want cause %q and no decision", tc.name, res, tc.cause)
			}
			if got := f.receipt(tc.cmd.IdempotencyKey); got != "" {
				t.Fatalf("%s journaled a %s receipt", tc.name, got)
			}
		})
	}
	for _, inst := range []uuid.UUID{id, task} {
		if now, nodes := f.load(inst); (inst == id && now.InstanceVersion != v) || (inst == task && now.InstanceVersion != tv) || len(nodes) != 1 || len(f.decisions(inst)) != 0 {
			t.Fatalf("a denied intervention changed instance %s: v%d, %d attempts, %d decisions", inst, now.InstanceVersion, len(nodes), len(f.decisions(inst)))
		}
	}

	// Malformed wiring and commands are refused as errors, not outcomes.
	if _, err := c.Intervene(ctx, f.cmd(id, v, "f-plain")); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("a command without an intervention = %v", err)
	}
	noTenant := f.iv(id, v, "f-tenant", skip)
	noTenant.TenantID = uuid.Nil
	if _, err := c.Intervene(ctx, noTenant); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("a command without a tenant = %v", err)
	}
	if _, _, err := c.Simulate(ctx, operator.KindWorkflowSkip, f.cmd(id, v, "f-sim-plain")); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("simulating an intervention-only kind without an intervention = %v", err)
	}
	mismatched := f.iv(id, v, "f-sim-mismatch", skip)
	if _, _, err := c.Simulate(ctx, operator.KindWorkflowOverride, mismatched); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("simulating an intervention under another kind = %v", err)
	}
	boom := errors.New("journal down")
	broken := f.controller(&flakyLookup{MemoryJournal: operator.NewMemoryJournal(), err: boom})
	if _, err := broken.Intervene(ctx, f.iv(id, v, "f-journal", skip)); !errors.Is(err, boom) {
		t.Fatalf("a journal lookup fault = %v", err)
	}
	if k, ok := OperatorKindFor("NOPE"); ok || k != "" {
		t.Fatalf("an undeclared kind mapped to %q", k)
	}
	if got, err := decodeEffect(encodeEffect(Result{Outcome: OutcomeApplied, InstanceID: id, DecisionID: "d", DecisionDigest: "sha256:x"})); err != nil || got.DecisionID != "d" || got.DecisionDigest != "sha256:x" {
		t.Fatalf("decision effect round trip = %+v, %v", got, err)
	}
}

type flakyLookup struct {
	*operator.MemoryJournal
	err error
}

func (j *flakyLookup) Lookup(context.Context, values.TenantId, string) (operator.Receipt, bool, error) {
	return operator.Receipt{}, false, j.err
}

// TestTodo_WF_RUN_015_Race runs eight skips of one node under distinct keys
// from eight connections at once: they converge to exactly one decision and
// one runtime transition; every other one is denied without a decision.
func TestTodo_WF_RUN_015_Race(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	journal := operator.NewMemoryJournal()
	id, v := f.instance([]string{"wait_effective_date"}, node{"wait_effective_date", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeWaiting}})
	f.iv(id, v, "seed", InterventionSpec{Kind: intervention.Skip})
	const workers = 8
	results := make([]Result, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctl := f.controllerWith(journal, plans{f.plan}, f0conn(t, f.db))
			cmd := f.cmd(id, v, fmt.Sprintf("race-skip-%d", i))
			cmd.Intervention = &InterventionSpec{Kind: intervention.Skip, NodeID: "wait_effective_date", EvidenceRefs: evidence}
			results[i], errs[i] = ctl.Intervene(ctx, cmd)
		}()
	}
	wg.Wait()
	applied := 0
	for i := range results {
		switch {
		case errs[i] != nil:
			t.Fatalf("worker %d failed: %v", i, errs[i])
		case results[i].Outcome == OutcomeApplied:
			applied++
		case results[i].Outcome != OutcomeDenied || results[i].DecisionID != "":
			t.Fatalf("worker %d = %+v, want DENIED without a decision", i, results[i])
		}
	}
	inst, nodes := f.load(id)
	revalidations := 0
	for _, n := range nodes {
		if n.NodeID == "revalidate" {
			revalidations++
		}
	}
	if applied != 1 || len(f.decisions(id)) != 1 || revalidations != 1 || inst.InstanceVersion <= v || f.readyWork(id, "revalidate") != 1 {
		t.Fatalf("applied %d, decisions %d, revalidate attempts %d, ready work %d; want exactly one of each", applied, len(f.decisions(id)), revalidations, f.readyWork(id, "revalidate"))
	}
}

// TestTodo_WF_RUN_015_Mutation proves an intervention's executor cannot be
// used as a side door and that ordinary operator code never edits workflow
// tables: every workflow write in this package and in the intervention
// package goes through the runtime, and no gateway-less executor call writes.
func TestTodo_WF_RUN_015_Mutation(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	c := f.controller(operator.NewMemoryJournal())
	id, v := f.instance([]string{"wait_effective_date"}, node{"wait_effective_date", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeWaiting}})
	cmd := f.iv(id, v, "side-door", InterventionSpec{Kind: intervention.Skip, NodeID: "wait_effective_date"})
	if _, err := c.applyIntervention(operator.KindWorkflowSkip)(withCommand(ctx, cmd), operator.Authorization{}, operator.Request{}); operator.CodeOf(err) != operator.CodeUnauthorizedEffect {
		t.Fatalf("executor without gateway authorization = %v", err)
	}
	if now, nodes := f.load(id); now.InstanceVersion != v || latest(nodes, "wait_effective_date").Status != runtime.NodeWaiting || len(f.decisions(id)) != 0 {
		t.Fatalf("the side door wrote: v%d %+v", now.InstanceVersion, nodes)
	}
	// Same key, different evidence: the gateway refuses the replay.
	first, err := c.Intervene(ctx, cmd)
	expect(t, "skip", first, err, OutcomeApplied, "")
	forged := f.iv(id, v, "side-door", InterventionSpec{Kind: intervention.Skip, NodeID: "wait_effective_date", EvidenceRefs: []string{"forged:evidence"}})
	if res, err := c.Intervene(ctx, forged); err != nil || res.Outcome != OutcomeDenied || res.Code != operator.CodeIdempotencyConflict {
		t.Fatalf("replay with different evidence = %+v, %v", res, err)
	}

	raw := regexp.MustCompile(`(?i)(UPDATE|DELETE\s+FROM|INSERT\s+INTO)\s+workflow_(instance|node_execution|continuation|advancement_receipt)\b`)
	for _, dir := range []string{".", filepath.Join("..", "..", "..", "workflow", "intervention")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			src, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if raw.Match(src) {
				t.Errorf("%s writes a workflow runtime table directly", filepath.Join(dir, e.Name()))
			}
		}
	}
}
