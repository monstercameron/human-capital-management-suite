package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockrepair"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// MissingPunchWorkflowEffects owns the authoritative time-plane effects for
// the clock-repair graph. Implementations must make each operation durable and
// idempotent before returning; the workflow engine records the matching node
// outcome after the effect succeeds.
type MissingPunchWorkflowEffects interface {
	CommitRequest(context.Context, clockservice.MissingPunchWorkflowRequest, execute.StepRequest, string) (MissingPunchEffectResult, error)
	ValidateRequest(context.Context, clockservice.MissingPunchWorkflowRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error)
	Eligibility(context.Context, clockservice.MissingPunchWorkflowRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error)
	ReopenPeriod(context.Context, clockservice.MissingPunchWorkflowRequest, execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error)
	AppendCorrection(context.Context, clockservice.MissingPunchWorkflowRequest, execute.StepRequest, string) (MissingPunchEffectResult, error)
	// RejectDecision persists the time-plane rejection using the already
	// committed approval-node evidence. It must CAS the request projection and
	// be idempotent for the same request/approval receipt.
	RejectDecision(context.Context, clockservice.MissingPunchWorkflowRequest, MissingPunchExecutionProof) (clockservice.MissingPunchRecord, error)
	OriginalWorkflowInstanceRef(context.Context, string, string) (string, error)
	LoadRequest(context.Context, string, string) (clockservice.MissingPunchRecord, error)
	RecordProof(context.Context, string, clockservice.MissingPunchWorkflowResult) (clockservice.MissingPunchRecord, error)
}

// MissingPunchEffectResult is the typed result returned by an authoritative
// capability. Its node identity, output digest and governance references must
// come from the same committed effect; the adapter never synthesizes them.
type MissingPunchEffectResult struct {
	Record  clockservice.MissingPunchRecord
	Outcome frontier.NodeOutcome
	Refs    runtime.GovernanceRefs
}

// MissingPunchExecutionProof is read back from durable node execution rows.
// It binds the accepted workflow result to the exact plan and attempt that
// the engine committed.
type MissingPunchExecutionProof struct {
	TenantID        uuid.UUID
	InstanceID      uuid.UUID
	PlanDigest      string
	TraceID         string
	NodeID          string
	Attempt         int
	CompletedState  string
	InstanceVersion int64
	CompletedAt     time.Time
}

// MissingPunchExecutionEvidence reads authoritative workflow execution
// evidence; implementations must use a tenant-scoped transaction.
type MissingPunchExecutionEvidence interface {
	LoadMissingPunchExecution(context.Context, uuid.UUID, uuid.UUID, string, int) (MissingPunchExecutionProof, error)
}

// PostgresMissingPunchExecutionEvidence reads the committed workflow node
// execution and instance rows. It is deliberately a reader only: the engine
// remains the sole writer of execution state.
type PostgresMissingPunchExecutionEvidence struct {
	DB execute.Beginner
}

// LoadMissingPunchExecution returns the exact persisted trace and plan proof
// for one node attempt under a tenant-scoped transaction.
func (r PostgresMissingPunchExecutionEvidence) LoadMissingPunchExecution(ctx context.Context, tenantID, instanceID uuid.UUID, nodeID string, attempt int) (MissingPunchExecutionProof, error) {
	if r.DB == nil || tenantID == uuid.Nil || instanceID == uuid.Nil || strings.TrimSpace(nodeID) == "" || attempt < 1 {
		return MissingPunchExecutionProof{}, errors.New("missing punch evidence: invalid identity")
	}
	var tx dbport.Tx
	var err error
	if ro, ok := r.DB.(execute.ReadOnlyBeginner); ok {
		tx, err = ro.BeginReadOnly(ctx)
	} else {
		tx, err = r.DB.Begin(ctx)
	}
	if err != nil {
		return MissingPunchExecutionProof{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return MissingPunchExecutionProof{}, err
	}
	var proof MissingPunchExecutionProof
	var capabilityExecutionID string
	var storedAttempt int
	if err := tx.QueryRow(ctx, `
		SELECT n.node_id, n.attempt, n.status, COALESCE(n.trace_id, ''),
		       n.capability_execution_id, i.compiled_plan_hash, i.instance_version,
		       n.completed_at
		FROM workflow_node_execution n
		JOIN workflow_instance i ON i.tenant_id = n.tenant_id AND i.instance_id = n.instance_id
		WHERE n.tenant_id = $1 AND n.instance_id = $2 AND n.node_id = $3 AND n.attempt = $4`,
		tenantID, instanceID, nodeID, attempt).Scan(&proof.NodeID, &storedAttempt, &proof.CompletedState, &proof.TraceID, &capabilityExecutionID, &proof.PlanDigest, &proof.InstanceVersion, &proof.CompletedAt); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return MissingPunchExecutionProof{}, errors.New("missing punch evidence: node attempt not found")
		}
		return MissingPunchExecutionProof{}, err
	}
	proof.TenantID, proof.InstanceID, proof.Attempt = tenantID, instanceID, storedAttempt
	wantExecutionID := runtime.NodeExecutionID(tenantID, instanceID, nodeID, attempt).String()
	if capabilityExecutionID != wantExecutionID || proof.CompletedAt.IsZero() {
		return MissingPunchExecutionProof{}, errors.New("missing punch evidence: capability execution identity mismatch")
	}
	return proof, nil
}

// MissingPunchApproval is the store-owned resume binding. The store loads the
// real approval WorkItem and its pinned instance data; callers never assemble
// a WorkItem or choose an instance version from request data.
type MissingPunchApproval struct {
	TenantID        uuid.UUID
	RequestID       string
	InstanceID      uuid.UUID
	PlanDigest      string
	WorkItemID      uuid.UUID
	ItemVersion     int64
	InstanceVersion int64
	Start           runtime.StartRequest
	Outcome         frontier.NodeOutcome
	Refs            runtime.GovernanceRefs
	// Result is the receipt returned by execute.Driver.CompleteApproval. That
	// call already advances the approval and drains READY successors; the
	// executor must consume this receipt exactly once and never call Resume a
	// second time.
	Result execute.Result
}

// MissingPunchApprovalStore completes the durable supervisor WorkItem and
// returns the exact evidence needed by execute.Driver.Resume.
type MissingPunchApprovalStore interface {
	CompleteApproval(context.Context, clockservice.MissingPunchWorkflowRequest) (MissingPunchApproval, error)
}

// MissingPunchStepRunner is the governed capability dispatch for the repair
// graph. Compose one per request or decision with the same durable effects
// store that backs MissingPunchWorkflowExecutor, then pass it to
// execute.New. It never advances the graph itself.
type MissingPunchStepRunner struct {
	Effects MissingPunchWorkflowEffects
	Request clockservice.MissingPunchWorkflowRequest
}

// Run dispatches only the capability and terminal nodes owned by time. The
// APPROVAL node deliberately parks and is resumed through a stored WorkItem.
func (r MissingPunchStepRunner) Run(ctx context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if r.Effects == nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.New("missing punch workflow: effects are required")
	}
	id := r.Request.RequestID
	if id == "" {
		id = requestIdentity(r.Request.TenantID, r.Request.IdempotencyKey)
	}
	switch req.Node.ID {
	case clockrepair.NodeCommitRequest:
		result, err := r.Effects.CommitRequest(ctx, r.Request, req, id)
		return effectResult(req, result, err)
	case clockrepair.NodeValidate:
		return r.Effects.ValidateRequest(ctx, r.Request)
	case clockrepair.NodeEligibility:
		return r.Effects.Eligibility(ctx, r.Request)
	case clockrepair.NodeApproval:
		return frontier.NodeOutcome{NodeID: req.Node.ID, Await: frontier.AwaitWorkItem, AwaitRef: "approval.time.missing_punch.supervisor/v1"}, runtime.GovernanceRefs{}, nil
	case clockrepair.NodeReopen:
		outcome, refs, err := r.Effects.ReopenPeriod(ctx, r.Request, req)
		return effectResult(req, MissingPunchEffectResult{Outcome: outcome, Refs: refs}, err)
	case clockrepair.NodeCorrection:
		result, err := r.Effects.AppendCorrection(ctx, r.Request, req, id)
		return effectResult(req, result, err)
	case clockrepair.NodeApproved, clockrepair.NodeRejected, clockrepair.NodeRepair, clockrepair.NodeConflict, clockrepair.NodeClosedPeriod, clockrepair.NodeCancelled:
		return frontier.NodeOutcome{NodeID: req.Node.ID}, runtime.GovernanceRefs{}, nil
	default:
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("missing punch workflow: unsupported node %q", req.Node.ID)
	}
}

func effectResult(req execute.StepRequest, result MissingPunchEffectResult, err error) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	outcome := result.Outcome
	if outcome.NodeID == "" {
		outcome.NodeID = req.Node.ID
	}
	if req.Plan == nil || req.InstanceID == uuid.Nil || req.Attempt < 1 || req.Plan.Digest() == "" || outcome.NodeID != req.Node.ID || outcome.Outcome != workflow.OutcomeSucceeded || outcome.OutputDigest == "" || result.Refs.CapabilityExecutionID != runtime.NodeExecutionID(req.TenantID, req.InstanceID, req.Node.ID, req.Attempt).String() || len(result.Refs.EffectRefs) == 0 {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.New("missing punch workflow: capability returned incomplete engine evidence")
	}
	return outcome, result.Refs, nil
}

// MissingPunchWorkflowExecutorOptions composes the published repair graph
// with the existing durable workflow engine. Effects and approvals are the
// only application-specific ports; execute.Driver remains the sole state
// machine and concurrency fence.
type MissingPunchWorkflowExecutorOptions struct {
	Driver *execute.Driver
	// DriverFactory creates a request-scoped driver whose StepRunner is bound
	// to the immutable request. It is required when one shared driver cannot
	// safely carry the effect payload for concurrent requests.
	DriverFactory func(context.Context, clockservice.MissingPunchWorkflowRequest) (*execute.Driver, error)
	Resolver      runtime.WorkflowResolver
	Versions      version.Store
	Tenant        func(string) (uuid.UUID, error)
	Effects       MissingPunchWorkflowEffects
	Approvals     MissingPunchApprovalStore
	// StartBinder persists and binds the immutable proposal revision and its
	// durable proposal/approval facts before the engine starts. A trigger-only
	// start is insufficient for the approval WorkItem path.
	StartBinder func(context.Context, clockservice.MissingPunchWorkflowRequest, runtime.StartRequest) (runtime.StartRequest, error)
	Clock       func() time.Time
	Evidence    MissingPunchExecutionEvidence
}

// MissingPunchWorkflowExecutor runs the hand-authored missing-punch graph.
// It is safe for concurrent use because all mutable state belongs to the
// durable store and execute.Driver's instance CAS.
type MissingPunchWorkflowExecutor struct {
	driver    *execute.Driver
	factory   func(context.Context, clockservice.MissingPunchWorkflowRequest) (*execute.Driver, error)
	resolver  runtime.WorkflowResolver
	versions  version.Store
	tenant    func(string) (uuid.UUID, error)
	effects   MissingPunchWorkflowEffects
	approvals MissingPunchApprovalStore
	startBind func(context.Context, clockservice.MissingPunchWorkflowRequest, runtime.StartRequest) (runtime.StartRequest, error)
	clock     func() time.Time
	evidence  MissingPunchExecutionEvidence
}

// NewMissingPunchWorkflowExecutor validates the concrete engine composition.
func NewMissingPunchWorkflowExecutor(opts MissingPunchWorkflowExecutorOptions) (*MissingPunchWorkflowExecutor, error) {
	if opts.Driver == nil && opts.DriverFactory == nil || opts.Resolver == nil || opts.Versions == nil || opts.Tenant == nil || opts.Effects == nil || opts.Approvals == nil || opts.StartBinder == nil || opts.Clock == nil || opts.Evidence == nil {
		return nil, errors.New("missing punch workflow: driver or factory, resolver, versions, tenant, effects, approvals, start binder, clock and execution evidence are required")
	}
	return &MissingPunchWorkflowExecutor{driver: opts.Driver, factory: opts.DriverFactory, resolver: opts.Resolver, versions: opts.Versions, tenant: opts.Tenant, effects: opts.Effects, approvals: opts.Approvals, startBind: opts.StartBinder, clock: opts.Clock, evidence: opts.Evidence}, nil
}

// ExecuteMissingPunch implements clockservice.MissingPunchWorkflowExecutor.
// REQUEST starts the actual published graph. DECIDE completes the durable
// approval item, resumes that same instance, and lets the graph invoke the
// correction capability before reaching its terminal.
func (e *MissingPunchWorkflowExecutor) ExecuteMissingPunch(ctx context.Context, req clockservice.MissingPunchWorkflowRequest) (clockservice.MissingPunchWorkflowResult, error) {
	if err := e.validateRequest(req); err != nil {
		return clockservice.MissingPunchWorkflowResult{}, err
	}
	tenantID, err := e.tenant(req.TenantID)
	if err != nil {
		return clockservice.MissingPunchWorkflowResult{}, fmt.Errorf("missing punch workflow: resolve tenant: %w", err)
	}
	if tenantID == uuid.Nil {
		return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: resolve tenant: empty tenant")
	}
	switch strings.ToUpper(strings.TrimSpace(req.Action)) {
	case "REQUEST":
		return e.executeRequest(ctx, tenantID, req)
	case "DECIDE":
		return e.executeDecision(ctx, tenantID, req)
	default:
		return clockservice.MissingPunchWorkflowResult{}, errMissingPunchInvalidAction
	}
}

func (e *MissingPunchWorkflowExecutor) executeRequest(ctx context.Context, tenantID uuid.UUID, req clockservice.MissingPunchWorkflowRequest) (clockservice.MissingPunchWorkflowResult, error) {
	plan, err := e.plan(ctx, runtime.StartRequest{TenantID: tenantID})
	if err != nil {
		return clockservice.MissingPunchWorkflowResult{}, err
	}
	requestID := requestIdentity(tenantID.String(), req.IdempotencyKey)
	req.RequestID = requestID
	existing, found, err := e.loadExistingRequest(ctx, req.TenantID, requestID)
	if err != nil {
		return clockservice.MissingPunchWorkflowResult{}, err
	}
	if found {
		if err := validateRequestReplay(existing, req); err != nil {
			return clockservice.MissingPunchWorkflowResult{}, err
		}
		if existing.CreatedAt.IsZero() {
			return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: replayed request has no persisted created time")
		}
		req.At = existing.CreatedAt
	}
	originalRef := existing.OriginalWorkflowInstanceRef
	if strings.TrimSpace(originalRef) == "" {
		originalRef, err = e.effects.OriginalWorkflowInstanceRef(ctx, req.TenantID, req.SessionID)
		if err != nil || strings.TrimSpace(originalRef) == "" {
			return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: original workflow instance reference is unavailable")
		}
	}
	if strings.TrimSpace(req.OriginalWorkflowInstanceRef) != "" && req.OriginalWorkflowInstanceRef != originalRef {
		return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: original workflow instance reference mismatch")
	}
	req.OriginalWorkflowInstanceRef = originalRef
	// The driver was composed with the application StepRunner. The request
	// effect is therefore reached by the registered capability handler; this
	// check keeps a resolver returning an unrelated plan from being accepted.
	if plan.WorkflowID != clockservice.MissingPunchWorkflowID || plan.Digest() == "" {
		return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: resolver returned the wrong published graph")
	}
	start := e.startRequest(tenantID, req, requestID, plan)
	if e.startBind == nil {
		return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: start binder is required")
	}
	start, err = e.startBind(ctx, req, start)
	if err != nil {
		return clockservice.MissingPunchWorkflowResult{}, fmt.Errorf("missing punch workflow: bind proposal start: %w", err)
	}
	driver, err := e.driverFor(ctx, req)
	if err != nil {
		return clockservice.MissingPunchWorkflowResult{}, err
	}
	result, err := driver.Execute(ctx, execute.ExecuteRequest{Start: start, Inputs: missingPunchInputs(req, originalRef)})
	if err != nil {
		return clockservice.MissingPunchWorkflowResult{}, err
	}
	advance, ok := findAdvance(result.Advances, clockrepair.NodeCommitRequest)
	if !ok {
		if found {
			return e.replayProof(ctx, existing, req.TenantID)
		}
		return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: request commit was not executed")
	}
	record, err := e.effects.LoadRequest(ctx, req.TenantID, requestID)
	if err != nil {
		return clockservice.MissingPunchWorkflowResult{}, err
	}
	return e.proof(ctx, record, result.Start.InstanceID, result.Start.WorkflowID, result.Start.CompiledPlanDigest, advance, result.InstanceVersion, req.TenantID)
}

func (e *MissingPunchWorkflowExecutor) loadExistingRequest(ctx context.Context, tenant, requestID string) (clockservice.MissingPunchRecord, bool, error) {
	record, err := e.effects.LoadRequest(ctx, tenant, requestID)
	if err == nil {
		return record, true, nil
	}
	if errors.Is(err, dbport.ErrNoRows) || errors.Is(err, timestore.ErrNotFound) {
		return clockservice.MissingPunchRecord{}, false, nil
	}
	return clockservice.MissingPunchRecord{}, false, err
}

func validateRequestReplay(record clockservice.MissingPunchRecord, req clockservice.MissingPunchWorkflowRequest) error {
	if record.ID != req.RequestID || record.TenantID != req.TenantID || record.SessionID != req.SessionID || record.WorkerRef != req.WorkerRef || record.OriginalObservationID != req.OriginalObservationID || record.RequestedBy != req.Actor || record.Reason != req.Reason || record.ExpectedSessionRevision != req.ExpectedRevision || record.Revision < 1 || !record.ClaimedOutAt.Equal(req.ClaimedOutAt.UTC()) {
		return errors.New("missing punch workflow: idempotency key is bound to different request content")
	}
	return nil
}

func (e *MissingPunchWorkflowExecutor) replayProof(ctx context.Context, record clockservice.MissingPunchRecord, tenant string) (clockservice.MissingPunchWorkflowResult, error) {
	instanceID, err := uuid.Parse(record.WorkflowInstanceID)
	if err != nil || instanceID == uuid.Nil || record.WorkflowNodeID == "" || record.WorkflowAttempt < 1 || record.WorkflowInstanceVersion < 1 || record.WorkflowPlanDigest == "" {
		return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: replay has incomplete engine proof")
	}
	advance := runtime.AdvanceReceipt{InstanceID: instanceID, NodeID: record.WorkflowNodeID, Attempt: record.WorkflowAttempt, NewInstanceVersion: record.WorkflowInstanceVersion}
	return e.proof(ctx, record, instanceID, clockservice.MissingPunchWorkflowID, record.WorkflowPlanDigest, advance, record.WorkflowInstanceVersion, tenant)
}

func (e *MissingPunchWorkflowExecutor) executeDecision(ctx context.Context, tenantID uuid.UUID, req clockservice.MissingPunchWorkflowRequest) (clockservice.MissingPunchWorkflowResult, error) {
	approval, err := e.approvals.CompleteApproval(ctx, req)
	if err != nil {
		return clockservice.MissingPunchWorkflowResult{}, err
	}
	if approval.TenantID != tenantID || approval.RequestID != req.RequestID || approval.InstanceID == uuid.Nil || approval.WorkItemID == uuid.Nil || approval.ItemVersion < 1 || approval.InstanceVersion < 1 || approval.PlanDigest == "" || approval.Start.Resolver == nil || approval.Start.Versions == nil {
		return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: incomplete durable approval binding")
	}
	if approval.Start.TenantID != tenantID || approval.Start.PinnedCompiledPlanDigest != approval.PlanDigest {
		return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: approval plan binding mismatch")
	}
	result := approval.Result
	if result.InstanceVersion < approval.InstanceVersion || result.Start.InstanceID != uuid.Nil && result.Start.InstanceID != approval.InstanceID {
		return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: approval completion returned an invalid engine receipt")
	}
	want := clockrepair.NodeApproval
	if req.Approve {
		want = clockrepair.NodeCorrection
	}
	advance, ok := findAdvance(result.Advances, want)
	if !ok {
		return clockservice.MissingPunchWorkflowResult{}, fmt.Errorf("missing punch workflow: resume did not execute %s", want)
	}
	record, err := e.effects.LoadRequest(ctx, req.TenantID, req.RequestID)
	if err != nil {
		return clockservice.MissingPunchWorkflowResult{}, err
	}
	if !req.Approve {
		if advance.RouteKey != string(workflow.OutcomeRejected) && advance.RouteKey != "REJECTED" {
			return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: approval receipt is not a rejection")
		}
		core, err := e.evidence.LoadMissingPunchExecution(ctx, tenantID, approval.InstanceID, advance.NodeID, advance.Attempt)
		if err != nil {
			return clockservice.MissingPunchWorkflowResult{}, err
		}
		if core.CompletedState != "SUCCEEDED" || core.CompletedAt.IsZero() || core.PlanDigest != approval.PlanDigest || core.TraceID == "" || core.InstanceVersion < advance.NewInstanceVersion {
			return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: rejection approval evidence is incomplete")
		}
		record, err = e.effects.RejectDecision(ctx, req, core)
		if err != nil {
			return clockservice.MissingPunchWorkflowResult{}, err
		}
	}
	return e.proof(ctx, record, approval.InstanceID, clockservice.MissingPunchWorkflowID, approval.PlanDigest, advance, result.InstanceVersion, req.TenantID)
}

func (e *MissingPunchWorkflowExecutor) plan(ctx context.Context, req runtime.StartRequest) (*workflow.CompiledWorkflow, error) {
	selection, err := e.resolver.ResolveWorkflow(ctx, req)
	if err != nil {
		return nil, err
	}
	if selection.WorkflowID != clockservice.MissingPunchWorkflowID || selection.Plan == nil {
		return nil, errors.New("missing punch workflow: no published repair plan")
	}
	return selection.Plan, nil
}

func (e *MissingPunchWorkflowExecutor) startRequest(tenantID uuid.UUID, req clockservice.MissingPunchWorkflowRequest, key string, plan *workflow.CompiledWorkflow) runtime.StartRequest {
	now := req.At.UTC()
	return runtime.StartRequest{TenantID: tenantID, CellID: "time", StartIdempotencyKey: key, Resolver: e.resolver, Versions: e.versions, Source: &runtime.StartSource{Kind: runtime.StartSourceTrigger, IntentType: clockrepair.IntentType, Trigger: &runtime.TriggerStartSource{TriggerID: key, TriggerType: "hcmnext.time.fix_missing_punch", Key: req.SessionID, Facts: map[string]string{"session_id": req.SessionID, "observation_id": req.OriginalObservationID, "worker_ref": req.WorkerRef}}}, BusinessSubjectRefs: []string{"time_session:" + req.SessionID, "worker:" + req.WorkerRef}, ExecutionMode: workflow.ModeExecute, CorrelationID: key, CreatedAt: now, PinnedCompiledPlanDigest: plan.Digest()}
}

func (e *MissingPunchWorkflowExecutor) proof(ctx context.Context, record clockservice.MissingPunchRecord, instanceID uuid.UUID, workflowID, planDigest string, advance runtime.AdvanceReceipt, instanceVersion int64, tenant string) (clockservice.MissingPunchWorkflowResult, error) {
	tenantID, err := e.tenant(tenant)
	if err != nil || tenantID == uuid.Nil || instanceID == uuid.Nil || workflowID != clockservice.MissingPunchWorkflowID || planDigest == "" || advance.NodeID == "" || advance.Attempt < 1 || advance.NewInstanceVersion < 1 || record.TenantID != tenant {
		return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: engine proof is incomplete")
	}
	evidence, err := e.evidence.LoadMissingPunchExecution(ctx, tenantID, instanceID, advance.NodeID, advance.Attempt)
	if err != nil {
		return clockservice.MissingPunchWorkflowResult{}, err
	}
	if evidence.TenantID != tenantID || evidence.InstanceID != instanceID || evidence.PlanDigest != planDigest || evidence.NodeID != advance.NodeID || evidence.Attempt != advance.Attempt || strings.TrimSpace(evidence.TraceID) == "" || evidence.CompletedState != "SUCCEEDED" || evidence.InstanceVersion < advance.NewInstanceVersion || instanceVersion < advance.NewInstanceVersion {
		return clockservice.MissingPunchWorkflowResult{}, errors.New("missing punch workflow: durable execution evidence does not match engine receipt")
	}
	proof := clockservice.MissingPunchWorkflowResult{Record: record, InstanceID: instanceID, WorkflowID: workflowID, PlanDigest: planDigest, TraceID: evidence.TraceID, NodeID: advance.NodeID, Attempt: advance.Attempt, InstanceVersion: instanceVersion, Committed: true}
	stored, err := e.effects.RecordProof(ctx, record.ID, proof)
	if err != nil {
		return clockservice.MissingPunchWorkflowResult{}, err
	}
	proof.Record = stored
	return proof, nil
}

func requestIdentity(tenant, key string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(tenant+"\x00"+clockservice.MissingPunchWorkflowID+"\x00"+key)).String()
}

func missingPunchInputs(req clockservice.MissingPunchWorkflowRequest, originalRef string) []workflow.TypedOutput {
	return []workflow.TypedOutput{
		{Path: "session_id", Value: workflow.TypedValue{Type: workflow.ValueType{Kind: workflow.KindString, Brand: "TimeSessionID"}, Text: req.SessionID}},
		{Path: "original_observation_id", Value: workflow.TypedValue{Type: workflow.ValueType{Kind: workflow.KindString, Brand: "TimeObservationID"}, Text: req.OriginalObservationID}},
		{Path: "expected_revision", Value: workflow.TypedValue{Type: workflow.ValueType{Kind: workflow.KindInteger}, Text: strconv.FormatUint(req.ExpectedRevision, 10)}},
		{Path: "proposed_clock_out_at", Value: workflow.TypedValue{Type: workflow.ValueType{Kind: workflow.KindInstant}, Text: req.ClaimedOutAt.UTC().Format(time.RFC3339Nano)}},
		{Path: "reason", Value: workflow.TypedValue{Type: workflow.ValueType{Kind: workflow.KindString, Brand: "CorrectionReason"}, Text: req.Reason}},
		{Path: "original_workflow_instance_ref", Value: workflow.TypedValue{Type: workflow.ValueType{Kind: workflow.KindString, Brand: "WorkflowInstanceRef"}, Text: originalRef}},
	}
}

func findAdvance(records []runtime.AdvanceReceipt, node string) (runtime.AdvanceReceipt, bool) {
	for _, record := range records {
		if record.NodeID == node {
			return record, true
		}
	}
	return runtime.AdvanceReceipt{}, false
}

var _ clockservice.MissingPunchWorkflowExecutor = (*MissingPunchWorkflowExecutor)(nil)

var errMissingPunchInvalidAction = errors.New("missing punch workflow: action must be REQUEST or DECIDE")

func (e *MissingPunchWorkflowExecutor) validateRequest(req clockservice.MissingPunchWorkflowRequest) error {
	if e == nil || e.driver == nil && e.factory == nil || strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || req.At.IsZero() {
		return errors.New("missing punch workflow: invalid request")
	}
	return nil
}

func (e *MissingPunchWorkflowExecutor) driverFor(ctx context.Context, req clockservice.MissingPunchWorkflowRequest) (*execute.Driver, error) {
	if e.factory != nil {
		driver, err := e.factory(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("missing punch workflow: build request driver: %w", err)
		}
		if driver == nil {
			return nil, errors.New("missing punch workflow: driver factory returned nil")
		}
		return driver, nil
	}
	if e.driver == nil {
		return nil, errors.New("missing punch workflow: driver is required")
	}
	return e.driver, nil
}
