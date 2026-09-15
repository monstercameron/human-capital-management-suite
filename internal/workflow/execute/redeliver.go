package execute

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// ErrRedeliveryStale reports a READY redelivery whose instance is no longer at
// the version, status or frontier the recovery sweep claimed it at. Nothing is
// run: the instance moved, so whatever the sweep observed is no longer the
// work that was lost (WF-RUN-003).
var ErrRedeliveryStale = errors.New("workflow execute: redelivery is stale")

// RedeliverRequest names an instance whose driver died mid-drain and asks for
// its durable READY frontier to be drained again.
//
// Start is context only, exactly as for [ResumeTimerRequest]: its resolver and
// version store re-resolve the pinned plan, and its proposal, correlation and
// subject fields are the ones every step and continuation of the instance
// receives. ExpectedInstanceVersion is the version the caller observed when it
// claimed the instance; a redelivery against any other version is refused
// [ErrRedeliveryStale] before a step runs.
type RedeliverRequest struct {
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
}

// FenceFromContext returns the WORKFLOW_INSTANCE fence ctx carries -- the one
// a dispatcher or recovery sweep attached with [WithFence], or the one the
// driver attached after acquiring its own lease -- so a step that writes an
// effect outside the advance transaction can verify it before writing
// (WF-RUN-003).
func FenceFromContext(ctx context.Context) (runtime.Fence, bool) {
	return fenceFromContext(ctx)
}

// redeliverableStatuses are the instance statuses a lost drain can leave
// behind with READY work still on the frontier.
var redeliverableStatuses = map[runtime.InstanceStatus]bool{
	runtime.InstanceCreated:        true,
	runtime.InstanceRunning:        true,
	runtime.InstanceWaiting:        true,
	runtime.InstancePauseRequested: true,
}

// RedeliverReady drains the durable READY frontier of an instance whose driver
// died after committing that READY work and before running it (WF-RUN-003).
//
// It reads which nodes are READY from the committed node executions rather
// than from any process memory, and it runs them through the same bounded
// drain [Driver.Execute] uses, so a node whose advancement already committed
// is never run again (it is no longer READY) and a READY successor whose
// predecessor committed is run exactly as the dead driver would have run it.
// Every advancement is fenced: ctx carries the WORKFLOW_INSTANCE fence the
// recovery sweep took over ([WithFence]), or the driver acquires its own.
func (d *Driver) RedeliverReady(ctx context.Context, req RedeliverRequest) (ret0 Result, retErr error) {
	ctx, obsOp := observe.Begin(d.observed(ctx), "workflow.execute.redeliver_ready", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if req.Start.TenantID == uuid.Nil || req.InstanceID == uuid.Nil || req.ExpectedInstanceVersion < 1 {
		return Result{}, invalid("ready redelivery requires tenant, instance and a positive expected instance version")
	}
	selection, err := resolvePinnedPlan(ctx, req.Start, "ready redelivery")
	if err != nil {
		return Result{}, err
	}
	run := runContext{
		start: req.Start, selection: selection, instanceID: req.InstanceID,
		traceID: d.opts.Instrumentation.TraceID(ctx),
	}
	ctx, release, err := d.acquireInstanceLease(ctx, req.Start.TenantID, req.InstanceID)
	if err != nil {
		return Result{}, err
	}
	defer func() { retErr = releasing(retErr, release) }()

	var (
		inst  runtime.Instance
		ready []string
	)
	if err := d.inTenantTx(ctx, req.Start.TenantID, func(tx runtime.Executor) error {
		var lerr error
		inst, ready, lerr = redeliverableFrontier(ctx, tx, run, req.ExpectedInstanceVersion)
		return lerr
	}); err != nil {
		return Result{}, err
	}
	result := Result{
		InstanceVersion: inst.InstanceVersion,
		Frontier:        append([]string(nil), inst.CurrentNodeIDs...),
	}
	return d.drainReady(ctx, run, result, ready)
}

// redeliverableFrontier loads the instance and returns the nodes on its
// frontier whose latest attempt is READY or RUNNING. It refuses, before any
// step runs, an instance that is pinned to another plan, is not in a live
// status, has moved past the expected version or has nothing to redeliver.
func redeliverableFrontier(ctx context.Context, ex runtime.Executor, run runContext, expected int64) (runtime.Instance, []string, error) {
	store := runtime.Store{}
	inst, err := store.LoadInstance(ctx, ex, run.start.TenantID, run.instanceID)
	if err != nil {
		return runtime.Instance{}, nil, err
	}
	if inst.CompiledPlanHash != run.selection.Plan.Digest() {
		return runtime.Instance{}, nil, invalid("instance %s pins plan %s, redelivery resolved %s",
			run.instanceID, inst.CompiledPlanHash, run.selection.Plan.Digest())
	}
	if !redeliverableStatuses[inst.RuntimeStatus] {
		return runtime.Instance{}, nil, fmt.Errorf("%w: instance %s is %s", ErrRedeliveryStale, run.instanceID, inst.RuntimeStatus)
	}
	if inst.InstanceVersion != expected {
		return runtime.Instance{}, nil, fmt.Errorf("%w: instance %s is at version %d, the claim observed %d",
			ErrRedeliveryStale, run.instanceID, inst.InstanceVersion, expected)
	}
	rows, err := store.LoadNodeExecutions(ctx, ex, run.start.TenantID, run.instanceID)
	if err != nil {
		return runtime.Instance{}, nil, err
	}
	latest := map[string]runtime.NodeExecution{}
	for _, row := range rows {
		if cur, ok := latest[row.NodeID]; !ok || row.Attempt > cur.Attempt {
			latest[row.NodeID] = row
		}
	}
	ready := []string{}
	for _, nodeID := range inst.CurrentNodeIDs {
		row, ok := latest[nodeID]
		if ok && (row.Status == runtime.NodeReady || row.Status == runtime.NodeRunning) {
			ready = append(ready, nodeID)
		}
	}
	if len(ready) == 0 {
		return runtime.Instance{}, nil, fmt.Errorf("%w: instance %s has no READY or RUNNING node on its frontier", ErrRedeliveryStale, run.instanceID)
	}
	sort.Strings(ready)
	return inst, ready, nil
}
