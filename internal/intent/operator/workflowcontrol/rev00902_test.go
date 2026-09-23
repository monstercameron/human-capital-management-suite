package workflowcontrol

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/intervention"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// interveneRequest builds the transport-shaped request REV-009-02's served
// surface sends: every typed parameter travels as a string, exactly as a
// generated RPC message would carry it.
func interveneRequest(f *fixture, kind operator.Kind, id uuid.UUID, version int64, key string) Request {
	return Request{Kind: kind, Tenant: f.key, InstanceID: id.String(), ExpectedVersion: uint64(version),
		IdempotencyKey: key, ReasonRef: "INC-1", Operator: "operator:ana",
		EvidenceRefs: []string{"ticket:INC-7", "log:driver-run-3"}}
}

// TestTodo_REV_009_02 is the PRIMARY test: the transport-shaped Handle
// endpoint covers the six previously untriggerable intervention kinds. Each
// one performs its governed transition through the Controller against
// embedded PostgreSQL and records its immutable decision.
func TestTodo_REV_009_02(t *testing.T) {
	ctx := context.Background()

	t.Run("SKIP", func(t *testing.T) {
		f := newFixture(t)
		c := f.controller(f.durableJournal())
		ids := func(values.TenantId) (uuid.UUID, error) { return f.tenant, nil }
		id, v := f.instance([]string{"wait_effective_date"}, node{"wait_effective_date", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeWaiting}})
		req := interveneRequest(f, operator.KindWorkflowSkip, id, v, "rev00902-skip")
		req.NodeID = "wait_effective_date"
		res, err := c.Handle(ctx, ids, req)
		if err != nil {
			t.Fatalf("Handle skip: %v", err)
		}
		if res.Outcome != OutcomeApplied || res.DecisionID == "" || res.DecisionDigest == "" {
			t.Fatalf("Handle skip = %+v, want APPLIED with a recorded decision", res)
		}
		if ds := f.decisions(id); len(ds) != 1 || ds[0].DecisionID.String() != res.DecisionID || ds[0].Verify() != nil {
			t.Fatalf("skip decisions = %+v, want the one Handle reported", ds)
		}
		if n := latest(mustLoad(t, f, id), "wait_effective_date"); n.Status != runtime.NodeSkipped {
			t.Fatalf("skipped node = %+v, want SKIPPED", n)
		}
	})

	t.Run("SATISFY", func(t *testing.T) {
		f := newFixture(t)
		c := f.controller(f.durableJournal())
		ids := func(values.TenantId) (uuid.UUID, error) { return f.tenant, nil }
		id, v := f.instance([]string{"wait_effective_date"}, node{"wait_effective_date", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeWaiting}})
		req := interveneRequest(f, operator.KindWorkflowSatisfy, id, v, "rev00902-satisfy")
		req.NodeID, req.Route = "wait_effective_date", "SUCCEEDED"
		res, err := c.Handle(ctx, ids, req)
		if err != nil {
			t.Fatalf("Handle satisfy: %v", err)
		}
		if res.Outcome != OutcomeApplied || res.DecisionID == "" {
			t.Fatalf("Handle satisfy = %+v, want APPLIED with a recorded decision", res)
		}
		if n := latest(mustLoad(t, f, id), "wait_effective_date"); n.Status != runtime.NodeSucceeded {
			t.Fatalf("satisfied node = %+v, want SUCCEEDED", n)
		}
	})

	t.Run("OVERRIDE", func(t *testing.T) {
		f := newFixture(t)
		c := f.controller(f.durableJournal())
		ids := func(values.TenantId) (uuid.UUID, error) { return f.tenant, nil }
		id, v := f.instance([]string{"still_valid"}, node{"still_valid", nil})
		// Override is exceptional authority: a repair grant is refused, an
		// incident-responder grant is accepted.
		req := interveneRequest(f, operator.KindWorkflowOverride, id, v, "rev00902-override-repair-grant")
		req.NodeID, req.Route = "still_valid", "VALID"
		denied, err := c.Handle(ctx, ids, req)
		if err != nil {
			t.Fatalf("Handle override under a repair grant: %v", err)
		}
		if denied.Outcome != OutcomeDenied || denied.Code != intervention.CodeUnauthorized {
			t.Fatalf("override under a repair grant = %+v, want DENIED %s", denied, intervention.CodeUnauthorized)
		}
		f.auth.mu.Lock()
		f.auth.role = jit.RoleIncidentResponder
		f.auth.mu.Unlock()
		defer func() { f.auth.mu.Lock(); f.auth.role = jit.RoleIntegrityRepair; f.auth.mu.Unlock() }()
		req.IdempotencyKey = "rev00902-override"
		res, err := c.Handle(ctx, ids, req)
		if err != nil {
			t.Fatalf("Handle override: %v", err)
		}
		if res.Outcome != OutcomeApplied || res.DecisionID == "" {
			t.Fatalf("Handle override = %+v, want APPLIED with a recorded decision", res)
		}
		if n := latest(mustLoad(t, f, id), "still_valid"); n.Status != runtime.NodeOverridden {
			t.Fatalf("overridden node = %+v, want OVERRIDDEN", n)
		}
	})

	t.Run("REWIND", func(t *testing.T) {
		f := newFixture(t)
		c := f.controller(f.durableJournal())
		ids := func(values.TenantId) (uuid.UUID, error) { return f.tenant, nil }
		id, v := f.instance([]string{"still_valid"},
			node{"revalidate", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeSucceeded}}, node{"still_valid", nil})
		paused, err := c.Handle(ctx, ids, interveneRequest(f, operator.KindWorkflowPause, id, v, "rev00902-rewind-pause"))
		if err != nil || paused.Outcome != OutcomeApplied {
			t.Fatalf("pause before rewind = %+v, %v", paused, err)
		}
		req := interveneRequest(f, operator.KindWorkflowRewind, id, int64(paused.InstanceVersion), "rev00902-rewind")
		req.TargetNodeID = "revalidate"
		res, err := c.Handle(ctx, ids, req)
		if err != nil {
			t.Fatalf("Handle rewind: %v", err)
		}
		if res.Outcome != OutcomeApplied || res.DecisionID == "" {
			t.Fatalf("Handle rewind = %+v, want APPLIED with a recorded decision", res)
		}
		inst, nodes := f.load(id)
		if again := latest(nodes, "revalidate"); again.Attempt != 2 || again.Status != runtime.NodeReady || inst.CurrentNodeIDs[0] != "revalidate" {
			t.Fatalf("rewind left frontier %v, node %+v", inst.CurrentNodeIDs, again)
		}
	})

	t.Run("SUPERSEDE", func(t *testing.T) {
		f := newFixture(t)
		c := f.controller(f.durableJournal())
		ids := func(values.TenantId) (uuid.UUID, error) { return f.tenant, nil }
		id, v := f.instance([]string{"wait_effective_date"}, node{"wait_effective_date", []runtime.NodeStatus{runtime.NodeRunning, runtime.NodeWaiting}})
		replacement, _ := f.instance([]string{"snapshot_worker"})
		req := interveneRequest(f, operator.KindWorkflowSupersede, id, v, "rev00902-supersede")
		req.Replacement = replacement.String()
		res, err := c.Handle(ctx, ids, req)
		if err != nil {
			t.Fatalf("Handle supersede: %v", err)
		}
		if res.Outcome != OutcomeApplied || res.DecisionID == "" {
			t.Fatalf("Handle supersede = %+v, want APPLIED with a recorded decision", res)
		}
		if inst, _ := f.load(id); inst.RuntimeStatus != runtime.InstanceSuperseded {
			t.Fatalf("superseded instance = %s, want SUPERSEDED", inst.RuntimeStatus)
		}
	})

	t.Run("RECONCILE", func(t *testing.T) {
		f := newFixture(t)
		c := f.controller(f.durableJournal())
		ids := func(values.TenantId) (uuid.UUID, error) { return f.tenant, nil }
		id, v := f.instance([]string{"execute_promotion"}, node{"execute_promotion", []runtime.NodeStatus{runtime.NodeRunning}})
		// The cancel routes an in-flight effect to repair; the reconcile
		// records what the operator observed. Both travel through Handle.
		cancelled, err := c.Handle(ctx, ids, interveneRequest(f, operator.KindWorkflowCancel, id, v, "rev00902-reconcile-cancel"))
		if err != nil || cancelled.Outcome != OutcomeRepairRequired {
			t.Fatalf("cancel an in-flight effect = %+v, %v; want REPAIR_REQUIRED", cancelled, err)
		}
		req := interveneRequest(f, operator.KindWorkflowReconcile, id, int64(cancelled.InstanceVersion), "rev00902-reconcile")
		req.NodeID, req.Observation = "execute_promotion", string(intervention.ObservedApplied)
		res, err := c.Handle(ctx, ids, req)
		if err != nil {
			t.Fatalf("Handle reconcile: %v", err)
		}
		if res.Outcome != OutcomeApplied || res.DecisionID == "" {
			t.Fatalf("Handle reconcile = %+v, want APPLIED with a recorded decision", res)
		}
		if n := latest(mustLoad(t, f, id), "execute_promotion"); n.Status != runtime.NodeSucceeded {
			t.Fatalf("reconciled node = %+v, want SUCCEEDED", n)
		}
	})

	t.Run("REFUSALS", func(t *testing.T) {
		f := newFixture(t)
		c := f.controller(operator.NewMemoryJournal())
		ids := func(values.TenantId) (uuid.UUID, error) { return f.tenant, nil }
		id, v := f.instance([]string{"approve_manager"})
		// Compensate names a kind with no composed runner: the endpoint
		// refuses it before anything is recorded.
		req := interveneRequest(f, operator.KindWorkflowCompensate, id, v, "rev00902-compensate")
		if _, err := c.Handle(ctx, ids, req); !errors.Is(err, ErrInvalidCommand) {
			t.Fatalf("Handle compensate = %v, want ErrInvalidCommand", err)
		}
		// A skip without evidence is denied by the intervention contract,
		// not executed.
		bare := interveneRequest(f, operator.KindWorkflowSkip, id, v, "rev00902-no-evidence")
		bare.NodeID, bare.EvidenceRefs = "approve_manager", nil
		denied, err := c.Handle(ctx, ids, bare)
		if err != nil {
			t.Fatalf("Handle skip without evidence: %v", err)
		}
		if denied.Outcome != OutcomeDenied || denied.Code != intervention.CodeEvidenceRequired {
			t.Fatalf("skip without evidence = %+v, want DENIED %s", denied, intervention.CodeEvidenceRequired)
		}
		if len(f.decisions(id)) != 0 {
			t.Fatal("a refused intervention recorded a decision")
		}
	})
}

func mustLoad(t *testing.T, f *fixture, id uuid.UUID) []runtime.NodeExecution {
	t.Helper()
	_, nodes := f.load(id)
	return nodes
}
