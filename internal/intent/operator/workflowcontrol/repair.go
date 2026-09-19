package workflowcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	operationrepair "github.com/monstercameron/human-capital-management-suite/internal/operations/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// RepairKind is the governed operator kind a RepairPlan execution resolves to.
//
// It is DATABASE_REPAIR rather than a new kind of its own, because that is
// already the vocabulary's integrity-repair door: internal/operations/
// repairworkbench maps a RECONCILIATION_MISMATCH finding -- which is exactly
// what raises a RepairPlan -- onto it, and its policy is the one a corrective
// external effect needs (RoleIntegrityRepair, dual control, and a simulation
// over exactly the plan's scope). Inventing a parallel authority for repairs
// would mean a second place where JIT grants, second approvers, simulations
// and receipts are interpreted, and the two would drift.
const RepairKind = operator.KindDatabaseRepair

// RepairApprovalField is the grant field that narrows a JIT grant to one
// repair plan, the same way [InstanceApprovalField] narrows one to a workflow
// instance. A grant carrying it is the durable dual-control record for this
// exact plan: the grant store already requires its approver to differ from its
// requester, so that approver is the second person DATABASE_REPAIR demands.
func RepairApprovalField(planID string) string { return "repair_plan:" + planID }

// approvalField is the dual-control grant field a kind's second approval is
// recorded on. Repairs are scoped to a plan, every other governed control to a
// workflow instance.
func approvalField(kind operator.Kind, id string) string {
	if kind == RepairKind {
		return RepairApprovalField(id)
	}
	return InstanceApprovalField(id)
}

// RepairCommand is one governed RepairPlan execution.
//
// The plan and the current evidence travel together deliberately: the
// executor's last read before the corrective effect is a revalidation of the
// plan against independently loaded current truth, so a stale finding, a
// changed mapping or an expired approval refuses the repair instead of driving
// it.
type RepairCommand struct {
	TenantID uuid.UUID
	Tenant   values.TenantId
	// Plan is the immutable execution snapshot of the RepairPlan.
	Plan operationrepair.RepairPlan
	// Current is the independently loaded evidence the plan is revalidated
	// against.
	Current operationrepair.CurrentEvidence
	// Operator executes the repair; Author authored or approved the plan. The
	// two must differ where the plan requires approval.
	Operator string
	Author   string

	IdempotencyKey string
	ReasonRef      string
}

// RepairResult is what one governed repair produced.
type RepairResult struct {
	Outcome Outcome
	Code    string
	// Status is the REPAIR mode's own typed answer.
	Status           execute.RepairStatus
	PlanID           string
	PlanDigest       string
	FailedEffectKey  string
	FenceID          string
	Executed         bool
	ConsistencyState string

	IntentInstanceID string
	ReceiptDigest    string
	Replayed         bool
}

// RepairController runs governed RepairPlan executions.
//
// It is the same mechanism [Controller] uses -- an [operator.Gateway] over the
// same [operator.Journal], resolving authority through the same
// [AuthorityResolver] -- with one executor registered, for [RepairKind]. The
// gateway is what applies JIT authority, dual control, the simulation
// requirement and per-key idempotency, and what records the receipt; this type
// only decides what a repair is and hands the effect to
// internal/workflow/execute.RepairExecutor.
type RepairController struct {
	gateway  *operator.Gateway
	executor *execute.RepairExecutor
	// authority finds the operator's current grant for a repair.
	authority AuthorityResolver
	clock     func() time.Time
	// preflight dry-runs the simulation DATABASE_REPAIR requires when the
	// operator presents none; see WithRepairPreflightSimulation.
	preflight bool
	recorder  observe.Recorder
}

// RepairOption configures a [RepairController].
type RepairOption func(*RepairController)

// WithRepairPreflightSimulation makes the controller simulate a repair the
// operator presented no simulation for, and submit that simulation as the
// evidence. The simulation is [operationrepair.Simulate]: pure revalidation
// with no fence and no effect.
func WithRepairPreflightSimulation() RepairOption {
	return func(c *RepairController) { c.preflight = true }
}

// WithRepairRecorder records every repair, simulation and operator submission
// beneath it on r. A caller context that already carries a recorder keeps it.
func WithRepairRecorder(r observe.Recorder) RepairOption {
	return func(c *RepairController) { c.recorder = r }
}

// NewRepairController composes a governed repair door over journal.
func NewRepairController(journal operator.Journal, executor *execute.RepairExecutor, authority AuthorityResolver, clock func() time.Time, opts ...RepairOption) (*RepairController, error) {
	if executor == nil || authority == nil {
		return nil, fmt.Errorf("%w: a repair executor and an authority resolver are required", ErrInvalidCommand)
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	c := &RepairController{executor: executor, authority: authority, clock: clock}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	gw, err := operator.NewGateway(journal, map[operator.Kind]operator.Executor{
		RepairKind: operator.ExecutorFunc(c.applyRepair),
	}, clock)
	if err != nil {
		return nil, err
	}
	c.gateway = gw
	return c, nil
}

// observed returns ctx carrying the controller's recorder unless the caller
// already supplied one.
func (c *RepairController) observed(ctx context.Context) context.Context {
	if c == nil || c.recorder == nil || observe.RecorderFrom(ctx) != nil {
		return ctx
	}
	return observe.WithRecorder(ctx, c.recorder)
}

func (cmd RepairCommand) validate() error {
	switch {
	case cmd.TenantID == uuid.Nil || cmd.Tenant.Validate() != nil:
		return fmt.Errorf("%w: tenant", ErrInvalidCommand)
	case strings.TrimSpace(cmd.IdempotencyKey) == "":
		return fmt.Errorf("%w: idempotency_key", ErrInvalidCommand)
	case strings.TrimSpace(cmd.ReasonRef) == "":
		return fmt.Errorf("%w: reason_ref", ErrInvalidCommand)
	case strings.TrimSpace(cmd.Operator) == "":
		return fmt.Errorf("%w: operator", ErrInvalidCommand)
	}
	if err := cmd.Plan.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCommand, err)
	}
	return nil
}

// Scope is the exact operator scope a repair targets: the one plan and the one
// failed effect it is allowed to redrive. A simulation must cover exactly this.
func (cmd RepairCommand) Scope() operator.Scope {
	return operator.Scope{Resource: "repair_plan", IDs: []string{cmd.Plan.ID + "#" + cmd.Plan.FailedEffectKey}}
}

// Execute submits one RepairPlan execution through the governed operator door.
func (c *RepairController) Execute(ctx context.Context, cmd RepairCommand) (ret0 RepairResult, retErr error) {
	ctx, obsOp := observe.Begin(c.observed(ctx), "workflow.repair.execute", cmd)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if err := cmd.validate(); err != nil {
		return RepairResult{}, err
	}
	auth, err := c.authority.ResolveAuthority(ctx, cmd.Tenant, cmd.Operator, RepairKind, cmd.Plan.ID)
	if err != nil {
		return RepairResult{}, fmt.Errorf("workflowcontrol: resolve repair authority: %w", err)
	}
	if policy, _ := operator.PolicyFor(RepairKind); c.preflight && policy.SimulationRequired && auth.Simulation == nil {
		sim, _, err := c.Simulate(ctx, cmd)
		if err != nil {
			return RepairResult{}, fmt.Errorf("workflowcontrol: repair preflight simulation: %w", err)
		}
		auth.Simulation = sim
	}
	req := operator.Request{
		Kind: RepairKind, Tenant: cmd.Tenant, Operator: cmd.Operator, Scope: cmd.Scope(),
		// The plan pins the exact target version it revalidates against, and
		// its digest is the payload an idempotency key is bound to: the same
		// key re-presented for a different plan is a conflict, not a replay.
		ExpectedVersion: cmd.Plan.TargetVersion, PayloadDigest: cmd.Plan.Digest,
		IdempotencyKey: cmd.IdempotencyKey, Reason: cmd.ReasonRef, TicketRef: cmd.ReasonRef,
		JIT: auth.JIT, SecondApprover: auth.SecondApprover, Simulation: auth.Simulation, Emergency: auth.Emergency,
	}
	ctx = withRepairCommand(ctx, cmd)
	receipt, err := c.gateway.Submit(ctx, req)
	base := RepairResult{PlanID: cmd.Plan.ID, PlanDigest: cmd.Plan.Digest, FailedEffectKey: cmd.Plan.FailedEffectKey}
	switch code := operator.CodeOf(err); {
	case err == nil:
	case code == operator.CodeRepairRequired:
		// The effect boundary was reached and nothing clean came back. The
		// receipt records it; the key is never retried blindly.
		base.Outcome, base.Code = OutcomeRepairRequired, code
		base.Status, base.ConsistencyState = execute.RepairFailed, "DEGRADED"
		base.IntentInstanceID, base.ReceiptDigest = receipt.IntentInstanceID, receipt.Digest
		return base, nil
	case code == operator.CodeJournal || code == "":
		return RepairResult{}, err
	default:
		// Authority, separation of duties, simulation and idempotency refusals
		// are governed denials, not failures.
		base.Outcome, base.Code = OutcomeDenied, code
		return base, nil
	}
	res, parseErr := decodeRepairEffect(receipt.EffectRef)
	if parseErr != nil {
		return RepairResult{}, parseErr
	}
	res.IntentInstanceID, res.ReceiptDigest = receipt.IntentInstanceID, receipt.Digest
	res.Replayed = receipt.Outcome == operator.OutcomeDuplicate
	return res, nil
}

// Simulate dry-runs a repair. It is [operationrepair.Simulate]: the same
// current-truth revalidation the execution performs, with no fence, no effect
// and no durable record, sealed over exactly the command's scope so the
// gateway can require it and the receipt names what was predicted.
func (c *RepairController) Simulate(ctx context.Context, cmd RepairCommand) (ret0 *operator.Simulation, ret1 execute.RepairStatus, retErr error) {
	// The dry run makes no downstream calls, so the observed scope has no
	// context to propagate into; only the operation handle is retained.
	_, obsOp := observe.Begin(c.observed(ctx), "workflow.repair.simulate", cmd)
	defer func() { observe.DoneWith(obsOp, retErr, ret1) }()
	if err := cmd.validate(); err != nil {
		return nil, "", err
	}
	at := c.clock().UTC()
	simulation, err := operationrepair.Simulate(operationrepair.RevalidationRequest{
		Plan: cmd.Plan, Current: cmd.Current, Now: at, Actor: cmd.Operator,
	})
	if err != nil {
		return nil, "", err
	}
	scope := cmd.Scope()
	sum := sha256.Sum256([]byte(repairSimulationPrefix + "|" + strings.Join(scope.IDs, ",") + "|" +
		string(simulation.Status) + "|" + simulation.PlanDigest + "|" + simulation.ResultDigest + "|" +
		strconv.Itoa(simulation.EffectCount)))
	return &operator.Simulation{Digest: "sha256:" + hex.EncodeToString(sum[:]), Scope: scope, At: at},
		execute.RepairStatus(simulation.Status), nil
}

// applyRepair is the gateway-invoked effect. It refuses unless the gateway
// minted its authorization, so it cannot be driven around the door.
func (c *RepairController) applyRepair(ctx context.Context, auth operator.Authorization, _ operator.Request) (string, error) {
	cmd, ok := repairCommandFrom(ctx)
	if !ok {
		return "", fmt.Errorf("%w: repair executor invoked without a command", ErrInvalidCommand)
	}
	if err := auth.Require(RepairKind, cmd.Tenant); err != nil {
		return "", err
	}
	result, err := c.executor.Execute(ctx, execute.RepairExecutionRequest{
		TenantID: cmd.TenantID, Plan: cmd.Plan, Current: cmd.Current,
		Now: c.clock().UTC(), Actor: cmd.Operator, Author: cmd.Author,
	})
	if err != nil {
		if result.Status == execute.RepairFailed {
			// The corrective effect was reached. Whether the provider accepted
			// it is unknown, so this is a repair-required receipt, never an
			// aborted key the next caller may reuse.
			return "", fmt.Errorf("workflowcontrol: repair: %w", err)
		}
		// Nothing reached the effect boundary: the key stays retryable.
		return "", fmt.Errorf("workflowcontrol: repair: %w: %w", operator.ErrNoEffect, err)
	}
	return encodeRepairEffect(cmd, result), nil
}

type repairCommandKey struct{}

func withRepairCommand(ctx context.Context, cmd RepairCommand) context.Context {
	return context.WithValue(ctx, repairCommandKey{}, cmd)
}

func repairCommandFrom(ctx context.Context) (RepairCommand, bool) {
	cmd, ok := ctx.Value(repairCommandKey{}).(RepairCommand)
	return cmd, ok
}

// repairEffectPrefix versions the effect reference the gateway journals, so a
// replayed receipt decodes to exactly the original repair result.
const (
	repairEffectPrefix     = "workflowcontrol/repair/v1"
	repairSimulationPrefix = "workflowcontrol/repair/simulation/v1"
)

func encodeRepairEffect(cmd RepairCommand, result execute.RepairExecutionResult) string {
	return strings.Join([]string{
		repairEffectPrefix, string(result.Status), cmd.Plan.ID, result.PlanDigest,
		result.FailedEffectKey, result.Fence.FenceID, strconv.FormatBool(result.Executed),
		result.ConsistencyState,
	}, "|")
}

func decodeRepairEffect(ref string) (RepairResult, error) {
	parts := strings.Split(ref, "|")
	if len(parts) != 8 || parts[0] != repairEffectPrefix {
		return RepairResult{}, fmt.Errorf("workflowcontrol: unreadable recorded repair effect %q", ref)
	}
	executed, err := strconv.ParseBool(parts[6])
	if err != nil {
		return RepairResult{}, fmt.Errorf("workflowcontrol: recorded repair effect execution flag: %w", err)
	}
	status := execute.RepairStatus(parts[1])
	outcome := OutcomeApplied
	if status != execute.RepairCompleted {
		// A repair that did not close is not an applied change; it is a
		// governed refusal to claim one.
		outcome = OutcomeDenied
	}
	return RepairResult{
		Outcome: outcome, Code: string(status), Status: status, PlanID: parts[2], PlanDigest: parts[3],
		FailedEffectKey: parts[4], FenceID: parts[5], Executed: executed, ConsistencyState: parts[7],
	}, nil
}
