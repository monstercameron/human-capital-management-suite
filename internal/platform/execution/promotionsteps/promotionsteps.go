// Package promotionsteps supplies the executable promotion workflow's step
// runner. It owns dispatch only: successor scheduling, durable work items,
// timers and END handling remain the workflow driver/runtime's concern.
package promotionsteps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/rulethreshold"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/localcommit"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/revalidate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Failure classes are stable runtime classifications, not Go error text.
const (
	FailurePort      = "PORT_FAILURE"
	FailureNotWired  = "PORT_NOT_CONFIGURED"
	FailureBadOutput = "OUTPUT_DIGEST_MISSING"
)

// Semantic outcomes returned by the two observation port families. The
// current compiler normalizes these to PASS and PARTIAL route keys; the
// runner accepts either the semantic vocabulary or those compiled keys.
const (
	ObservationObserved      = "OBSERVED"
	ObservationFailed        = "FAIL"
	ReconciliationConsistent = "CONSISTENT"
	ReconciliationDegraded   = "DEGRADED"
)

// ErrWaitOwnedByDriver makes the WAIT boundary explicit. The execute driver
// derives TIMER_REQUIRED from a compiled WAIT successor and ResumeTimer feeds
// the fired result back into runtime.Advance; a step runner must not do either.
var ErrWaitOwnedByDriver = errors.New("promotionsteps: WAIT is owned by the workflow driver")

var errOutputDigestMissing = errors.New("promotionsteps: execute port returned no output digest")

// Artifact is the common typed output returned by a domain or governance
// port. Digest is recorded verbatim as frontier.NodeOutcome.OutputDigest.
// Refs are the durable references the node execution records alongside it.
type Artifact struct {
	OutputDigest string
	Refs         runtime.GovernanceRefs
}

// SnapshotPort runs the governed worker-state snapshot capability.
type SnapshotPort interface {
	Snapshot(context.Context, execute.StepRequest) (Artifact, error)
}

// CompensationPort runs the P1A compensation simulation service.
type CompensationPort interface {
	SimulateCompensation(context.Context, execute.StepRequest) (Artifact, error)
}

// BandPort runs the P1A pay-band position service.
type BandPort interface {
	EvaluateBand(context.Context, execute.StepRequest) (Artifact, error)
}

// ThresholdResult is the typed answer from the RULE-003 raise-threshold port.
// Route is normally derived from Tier by RulesThresholdPort, but is exposed
// so a composition root can bind a versioned rules adapter without duplicating
// the runner's route selection.
type ThresholdResult struct {
	Artifact
	Tier  rules.ApprovalTier
	Route workflow.Outcome
	// Inputs and Decision are the frozen evaluation the served recorder
	// freezes per proposal revision for RULE-004's commit-time
	// re-evaluation; ports that cannot supply them leave them zero and the
	// recorder skips (documented at [Runner.RunInTx]).
	Inputs   rules.PromotionApprovalInput
	Decision rules.PromotionApprovalDecision
}

// ThresholdPort evaluates rules.compensation.raise_threshold/v3.
type ThresholdPort interface {
	RaiseThreshold(context.Context, execute.StepRequest) (ThresholdResult, error)
}

// RevalidationResult is the typed GOVERN-003 answer. Requirement is used by
// the still_valid decision; Confirmed is the positive result.
type RevalidationResult struct {
	Artifact
	Confirmed   bool
	Requirement revalidate.RequirementKind
}

// RevalidatePort invokes governance/revalidate.
type RevalidatePort interface {
	Revalidate(context.Context, execute.StepRequest) (RevalidationResult, error)
}

// ValidityResult is the materialized input for still_valid. A port may read
// the durable revalidation artifact produced by the preceding node.
type ValidityResult struct {
	Artifact
	Status string
}

// ValidityPort evaluates the still_valid decision against the revalidation
// result. Its Status vocabulary is CONFIRMED, REAPPROVAL_REQUIRED, BLOCK or
// REPLAN_REQUIRED.
type ValidityPort interface {
	StillValid(context.Context, execute.StepRequest) (ValidityResult, error)
}

// ExecutePromotionPort is the composition seam for the governed core commit.
// Its implementation is expected to invoke the existing
// execute.TerminalWriter inside the driver's transaction, using IdempotencyKey
// exactly once. The runner supplies the proposal material digest as that key.
type ExecutePromotionPort interface {
	ExecutePromotion(context.Context, execute.StepRequest, string) (Artifact, error)
}

// PreparePromotionPort resolves the immutable, domain-bound local commit plan
// immediately before execute_promotion. The terminal writer consumes this
// value; it is not allowed to reconstruct a broader participant set.
type PreparePromotionPort interface {
	PreparePromotion(context.Context, execute.StepRequest, string) (localcommit.PreparedPlan, error)
}

// ExecutePreparedPromotionPort is the prepared-plan terminal write seam.
type ExecutePreparedPromotionPort interface {
	ExecutePreparedPromotion(context.Context, execute.StepRequest, localcommit.PreparedPlan) (Artifact, error)
}

// ObservationResult is the typed answer from payroll or access observation.
type ObservationResult struct {
	Artifact
	Status string
}

// ObservationPort reads one outbound system through the INTG-009 observation
// boundary. It must not mutate the provider.
type ObservationPort interface {
	Observe(context.Context, execute.StepRequest) (ObservationResult, error)
}

// ReconciliationResult is the typed RECON-001 comparison answer.
type ReconciliationResult struct {
	Artifact
	Status string
}

// ReconciliationPort obtains the final reconciliation verdict.
type ReconciliationPort interface {
	Reconcile(context.Context, execute.StepRequest) (ReconciliationResult, error)
}

// HoldReleaseResult is the typed answer from the budget-hold compensation.
// Status names the COMPENSATE route the run takes: COMPENSATED, PARTIAL,
// FAILED or REPAIR_REQUIRED. Every route still lands on the bounded RepairPlan
// terminal; the route records whether the automatic correction succeeded, so
// repair starts from a known state instead of re-deriving it.
type HoldReleaseResult struct {
	Artifact
	Status string
}

// HoldReleasePort performs the bounded automatic correction when a downstream
// observation reports known-bad state: it releases the unit's
// compensation-pool hold under the proposal's original idempotency identity.
// It must not touch the committed promotion itself.
type HoldReleasePort interface {
	ReleaseHold(context.Context, execute.StepRequest) (HoldReleaseResult, error)
}

// Config binds the promotion workflow's narrow ports. Nil ports are allowed
// so a miswired node returns a typed, node-named failure rather than panics.
type Config struct {
	SnapshotWorker       SnapshotPort
	SimulateCompensation CompensationPort
	EvaluateBand         BandPort
	RaiseThreshold       ThresholdPort
	// RecordThresholdDecision freezes one threshold evaluation per
	// proposal revision inside the advancement transaction, for RULE-004's
	// commit-time re-evaluation. Nil skips recording: unit compositions
	// and custom threshold ports without freezable inputs run uncovered,
	// and the served composition sets the rulethreshold store write.
	RecordThresholdDecision  func(ctx context.Context, ex runtime.Executor, d rulethreshold.Decision) error
	Revalidate               RevalidatePort
	StillValid               ValidityPort
	ExecutePromotion         ExecutePromotionPort
	PreparePromotion         PreparePromotionPort
	ExecutePreparedPromotion ExecutePreparedPromotionPort
	ObservePayroll           ObservationPort
	ObserveAccess            ObservationPort
	ObserveReconciliation    ReconciliationPort
	CompensateHold           HoldReleasePort
}

// Runner implements execute.StepRunner for promotionexec's compiled plan.
type Runner struct {
	ports Config

	// successfulCommits is a process-local replay shield. The durable
	// execute.TerminalWriter/idempotency guard remains authoritative across
	// process boundaries; this shield also keeps a duplicate in-process runner
	// attempt from calling the commit port twice.
	mu                sync.Mutex
	successfulCommits map[string]Artifact
}

// StepRunner is the exported name for the promotion runner implementation.
// The alias keeps the package vocabulary aligned with execute.StepRunner.
type StepRunner = Runner

var _ execute.StepRunner = (*Runner)(nil)

var _ execute.TransactionalStepRunner = (*Runner)(nil)

// New constructs a deterministic promotion step runner. It reads no clock;
// all timestamps remain in execute.StepRequest.RecordedAt for ports that need
// them.
func New(cfg Config) *Runner {
	return &Runner{ports: cfg, successfulCommits: make(map[string]Artifact)}
}

// NewStepRunner is an explicit constructor alias for composition roots that
// name the runtime contract rather than the implementation role.
func NewStepRunner(cfg Config) *StepRunner { return New(cfg) }

// NewRunner is a descriptive constructor alias retained for callers that
// prefer the implementation name.
func NewRunner(cfg Config) *Runner { return New(cfg) }

// HandlesNode reports whether the runner has an explicit dispatch case for a
// published promotion node, including driver-owned END and WAIT cases.
func (r *Runner) HandlesNode(nodeID string) bool {
	switch nodeID {
	case promotionexec.NodeSnapshotWorker,
		promotionexec.NodeSimulateCompensation,
		promotionexec.NodeEvaluateBand,
		promotionexec.NodeRaiseThreshold,
		promotionexec.NodeApproveFinance,
		promotionexec.NodeApproveManager,
		promotionexec.NodeWaitEffectiveDate,
		promotionexec.NodeRevalidate,
		promotionexec.NodeStillValid,
		promotionexec.NodeReapproval,
		promotionexec.NodeExecutePromotion,
		promotionexec.NodeCompensateHold,
		promotionexec.NodeAcknowledgeRelease,
		promotionexec.NodeAwaitPayrollConfirmation,
		promotionexec.NodeAwaitAccessConfirmation,
		promotionexec.NodeObservePayroll,
		promotionexec.NodeObserveAccess,
		promotionexec.NodeObserveReconciliation,
		promotionexec.NodeEndComplete,
		promotionexec.NodeEndRepairPlan,
		promotionexec.NodeEndRejected,
		promotionexec.NodeEndInvalidated,
		promotionexec.NodeEndExpired,
		promotionexec.NodeEndCancelled,
		promotionexec.NodeEndBlocked:
		return true
	default:
		return false
	}
}

// Run dispatches one READY promotion node. It never schedules a successor,
// creates human work or creates a timer; those are derived by execute.Driver
// and runtime.Advance from the pinned compiled plan.
func (r *Runner) Run(ctx context.Context, req execute.StepRequest) (ret0 frontier.NodeOutcome, ret1 runtime.GovernanceRefs, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_steps.run", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0, ret1) }()
	if !r.HandlesNode(req.Node.ID) {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("promotionsteps: unsupported node %q", req.Node.ID)
	}

	switch req.Node.ID {
	case promotionexec.NodeSnapshotWorker:
		if err := requireType(req, workflow.StepCapability); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		if r.ports.SnapshotWorker == nil {
			return failed(req, FailureNotWired)
		}
		artifact, err := r.ports.SnapshotWorker.Snapshot(ctx, req)
		return capabilityResult(req, artifact, err)

	case promotionexec.NodeSimulateCompensation:
		if err := requireType(req, workflow.StepCapability); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		if r.ports.SimulateCompensation == nil {
			return failed(req, FailureNotWired)
		}
		artifact, err := r.ports.SimulateCompensation.SimulateCompensation(ctx, req)
		return capabilityResult(req, artifact, err)

	case promotionexec.NodeEvaluateBand:
		if err := requireType(req, workflow.StepCapability); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		if r.ports.EvaluateBand == nil {
			return failed(req, FailureNotWired)
		}
		artifact, err := r.ports.EvaluateBand.EvaluateBand(ctx, req)
		return capabilityResult(req, artifact, err)

	case promotionexec.NodeRaiseThreshold:
		result, failClass, err := r.evaluateThreshold(ctx, req)
		if err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		if failClass != "" {
			return failed(req, failClass)
		}
		return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: result.Route, OutputDigest: result.OutputDigest}, result.Refs, nil

	case promotionexec.NodeApproveFinance, promotionexec.NodeApproveManager:
		if err := requireType(req, workflow.StepApproval); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		return approvalAwait(req)

	case promotionexec.NodeWaitEffectiveDate:
		if err := requireType(req, workflow.StepWait); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, ErrWaitOwnedByDriver

	case promotionexec.NodeRevalidate:
		if err := requireType(req, workflow.StepCapability); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		if r.ports.Revalidate == nil {
			return failed(req, FailureNotWired)
		}
		result, err := r.ports.Revalidate.Revalidate(ctx, req)
		if err != nil {
			return failed(req, FailurePort)
		}
		if strings.TrimSpace(result.OutputDigest) == "" {
			return failed(req, FailureBadOutput)
		}
		return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: workflow.OutcomeSucceeded, OutputDigest: result.OutputDigest}, result.Refs, nil

	case promotionexec.NodeStillValid:
		if err := requireType(req, workflow.StepDecision); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		if r.ports.StillValid == nil {
			return failed(req, FailureNotWired)
		}
		result, err := r.ports.StillValid.StillValid(ctx, req)
		if err != nil {
			return failed(req, FailurePort)
		}
		if strings.TrimSpace(result.OutputDigest) == "" {
			return failed(req, FailureBadOutput)
		}
		route, ok := validityRoute(result.Status)
		if !ok {
			return failed(req, FailureBadOutput)
		}
		return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: route, OutputDigest: result.OutputDigest}, result.Refs, nil

	case promotionexec.NodeReapproval:
		if err := requireType(req, workflow.StepTask); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		return frontier.NodeOutcome{NodeID: req.Node.ID, Await: frontier.AwaitWorkItem, AwaitRef: req.Node.ID}, runtime.GovernanceRefs{}, nil

	case promotionexec.NodeExecutePromotion:
		if err := requireType(req, workflow.StepCapability); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		if r.ports.ExecutePromotion == nil && (r.ports.PreparePromotion == nil || r.ports.ExecutePreparedPromotion == nil) {
			return failed(req, FailureNotWired)
		}
		key := req.Proposal.Revision.MaterialDigest.Digest
		if strings.TrimSpace(key) == "" {
			return failed(req, FailureBadOutput)
		}
		artifact, err := r.executePromotion(ctx, req, key)
		if err != nil {
			if errors.Is(err, errOutputDigestMissing) {
				return failed(req, FailureBadOutput)
			}
			return failedWithCause(req, FailurePort, err)
		}
		return capabilityResult(req, artifact, nil)

	case promotionexec.NodeObservePayroll, promotionexec.NodeObserveAccess:
		if err := requireType(req, workflow.StepObserve); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		var port ObservationPort
		if req.Node.ID == promotionexec.NodeObservePayroll {
			port = r.ports.ObservePayroll
		} else {
			port = r.ports.ObserveAccess
		}
		if port == nil {
			return failed(req, FailureNotWired)
		}
		result, err := port.Observe(ctx, req)
		if err != nil {
			return failed(req, FailurePort)
		}
		if strings.TrimSpace(result.OutputDigest) == "" {
			return failed(req, FailureBadOutput)
		}
		if result.Status == ObservationFailed && contains(req.Node.Routes, workflow.OutcomeFail) {
			// A real observation that did not see the committed change routes
			// the compiled FAIL edge; it is never folded into PASS.
			return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: workflow.OutcomeFail, OutputDigest: result.OutputDigest}, result.Refs, nil
		}
		if result.Status != ObservationObserved {
			return failed(req, FailureBadOutput)
		}
		route := observationRoute(req.Node)
		return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: route, OutputDigest: result.OutputDigest}, result.Refs, nil

	case promotionexec.NodeAcknowledgeRelease,
		promotionexec.NodeAwaitPayrollConfirmation,
		promotionexec.NodeAwaitAccessConfirmation:
		if err := requireType(req, workflow.StepSignal); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		// SIGNAL is driver-owned like WAIT: the runner only parks. The
		// execute driver's continuation sink opens the durable subscription
		// through the composed SignalSubscriptions adapter and resumes this
		// node from its matched receipt; an unconfigured cell fails the
		// advancement closed instead of completing. The 1.1.0 provider
		// waits park the same way: their resumed SUCCEEDED is not the
		// provider's verdict, the observation after each wait judges the
		// provider's receipt (ProviderReceiptReader).
		return frontier.NodeOutcome{NodeID: req.Node.ID, Await: frontier.AwaitSignal, AwaitRef: req.Node.ID}, runtime.GovernanceRefs{}, nil

	case promotionexec.NodeCompensateHold:
		if err := requireType(req, workflow.StepCompensate); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		if r.ports.CompensateHold == nil {
			return failed(req, FailureNotWired)
		}
		result, err := r.ports.CompensateHold.ReleaseHold(ctx, req)
		if err != nil {
			return failed(req, FailurePort)
		}
		if strings.TrimSpace(result.OutputDigest) == "" {
			return failed(req, FailureBadOutput)
		}
		route := workflow.Outcome(result.Status)
		if !contains(req.Node.Routes, route) {
			return failed(req, FailureBadOutput)
		}
		return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: route, OutputDigest: result.OutputDigest}, result.Refs, nil

	case promotionexec.NodeObserveReconciliation:
		if err := requireType(req, workflow.StepObserve); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		if r.ports.ObserveReconciliation == nil {
			return failed(req, FailureNotWired)
		}
		result, err := r.ports.ObserveReconciliation.Reconcile(ctx, req)
		if err != nil {
			return failed(req, FailurePort)
		}
		if strings.TrimSpace(result.OutputDigest) == "" {
			return failed(req, FailureBadOutput)
		}
		route, ok := reconciliationRoute(req.Node, result.Status)
		if !ok {
			return failed(req, FailureBadOutput)
		}
		return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: route, OutputDigest: result.OutputDigest}, result.Refs, nil

	case promotionexec.NodeEndComplete,
		promotionexec.NodeEndRepairPlan,
		promotionexec.NodeEndRejected,
		promotionexec.NodeEndInvalidated,
		promotionexec.NodeEndExpired,
		promotionexec.NodeEndCancelled,
		promotionexec.NodeEndBlocked:
		if err := requireType(req, workflow.StepEnd); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
		}
		// END is intentionally a bare result. execute.Driver's continuation
		// sink validates the compiled terminal and invokes TerminalWriter.
		return frontier.NodeOutcome{NodeID: req.Node.ID}, runtime.GovernanceRefs{}, nil
	}

	return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("promotionsteps: dispatch case for %q was not implemented", req.Node.ID)
}

// evaluateThreshold runs the RULE-003 port and selects the route, keeping
// the exact failure-outcome contract the Run dispatch had before the
// in-transaction recording split it out: requireType mismatches are errors,
// every other failure is a stable failure outcome with a nil error.
func (r *Runner) evaluateThreshold(ctx context.Context, req execute.StepRequest) (ThresholdResult, string, error) {
	if err := requireType(req, workflow.StepDecision); err != nil {
		return ThresholdResult{}, "", err
	}
	if r.ports.RaiseThreshold == nil {
		return ThresholdResult{}, FailureNotWired, nil
	}
	result, err := r.ports.RaiseThreshold.RaiseThreshold(ctx, req)
	if err != nil {
		return ThresholdResult{}, FailurePort, nil
	}
	if strings.TrimSpace(result.OutputDigest) == "" {
		return ThresholdResult{}, FailureBadOutput, nil
	}
	route := result.Route
	if route == "" {
		switch result.Tier {
		case rules.ApprovalTierStandard:
			route = workflow.Outcome("WITHIN_THRESHOLD")
		case rules.ApprovalTierFinanceRequired, rules.ApprovalTierExecutiveRequired:
			route = workflow.Outcome("ABOVE_THRESHOLD")
		case rules.ApprovalTierUnknownBlocked:
			route = workflow.OutcomeUnknown
		default:
			return ThresholdResult{}, FailureBadOutput, nil
		}
	}
	if !thresholdRoute(route) {
		return ThresholdResult{}, FailureBadOutput, nil
	}
	result.Route = route
	return result, "", nil
}

// RunsInTransaction claims only the threshold node into the advancement
// transaction, so the threshold decision it freezes and the outcome that
// decision produced commit or roll back together. Every other node keeps
// its existing pre-transaction evaluation.
func (r *Runner) RunsInTransaction(node workflow.CompiledNode) bool {
	return node.ID == promotionexec.NodeRaiseThreshold
}

// RunInTx evaluates the threshold node inside the advancement transaction
// and freezes its decision per proposal revision for RULE-004's commit-time
// re-evaluation. Ports that cannot supply freezable inputs or a served
// recorder leave no record and behave exactly as Run; the served
// composition wires both, so a served threshold always freezes.
func (r *Runner) RunInTx(ctx context.Context, ex runtime.Executor, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	result, failClass, err := r.evaluateThreshold(ctx, req)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	if failClass != "" {
		return failed(req, failClass)
	}
	if err := r.recordThresholdDecision(ctx, ex, req, result); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: result.Route, OutputDigest: result.OutputDigest}, result.Refs, nil
}

// recordThresholdDecision freezes the evaluation the threshold branch just
// produced. It is a no-op without a served recorder, with freezable inputs
// missing from a custom port, or outside an EXECUTE advance: simulate,
// replay and shadow runs never freeze approval-time evidence, and a unit
// composition without the store write runs uncovered exactly as before.
func (r *Runner) recordThresholdDecision(ctx context.Context, ex runtime.Executor, req execute.StepRequest, result ThresholdResult) error {
	if r.ports.RecordThresholdDecision == nil {
		return nil
	}
	if req.Context.ExecutionMode != workflow.ModeExecute {
		return nil
	}
	if err := result.Inputs.Validate(); err != nil {
		return nil
	}
	decision := result.Decision
	if decision.MatchedRowID == "" || decision.TableID == "" || decision.TableVersion == "" || decision.TableDigest == "" {
		return nil
	}
	intentID, err := uuid.Parse(req.Proposal.Revision.IntentID)
	if err != nil {
		return fmt.Errorf("promotionsteps: threshold record needs an intent-keyed revision: %w", err)
	}
	digest, err := rules.InputDigest(result.Inputs)
	if err != nil {
		return fmt.Errorf("promotionsteps: threshold record input digest: %w", err)
	}
	return r.ports.RecordThresholdDecision(ctx, ex, rulethreshold.Decision{
		TenantID: req.TenantID, IntentID: intentID, Revision: req.Proposal.Revision.Revision,
		Attempt: req.Attempt, InstanceID: req.InstanceID,
		Tier: string(result.Tier), MatchedRow: decision.MatchedRowID,
		TableID: decision.TableID, TableVersion: decision.TableVersion, TableDigest: decision.TableDigest,
		InputDigest: digest, Input: result.Inputs, RecordedAt: req.RecordedAt,
	})
}

func requireType(req execute.StepRequest, want workflow.StepType) error {
	if req.Node.Type != want {
		return fmt.Errorf("promotionsteps: node %q is %s, want %s", req.Node.ID, req.Node.Type, want)
	}
	return nil
}

func capabilityResult(req execute.StepRequest, artifact Artifact, err error) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if err != nil {
		return failed(req, FailurePort)
	}
	if strings.TrimSpace(artifact.OutputDigest) == "" {
		return failed(req, FailureBadOutput)
	}
	return frontier.NodeOutcome{NodeID: req.Node.ID, Outcome: workflow.OutcomeSucceeded, OutputDigest: artifact.OutputDigest}, artifact.Refs, nil
}

func failed(req execute.StepRequest, class string) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	return frontier.NodeOutcome{NodeID: req.Node.ID, Failed: true, ErrorClass: class}, runtime.GovernanceRefs{}, nil
}

// failedWithCause keeps the stable failure class while retaining the
// operation's concrete cause on the durable node execution.  A class alone
// turns every commit problem into an unqueryable PORT_FAILURE, which leaves a
// repair operator unable to distinguish authorization, resolution, baseline,
// or participant failures.
func failedWithCause(req execute.StepRequest, class string, cause error) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	message := strings.TrimSpace(cause.Error())
	if len(message) > 512 {
		message = message[:512]
	}
	if message == "" {
		return failed(req, class)
	}
	return frontier.NodeOutcome{NodeID: req.Node.ID, Failed: true, ErrorClass: class + ": " + message}, runtime.GovernanceRefs{}, nil
}

func approvalAwait(req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	ref := promotionexec.ApprovalManager
	if req.Node.ID == promotionexec.NodeApproveFinance {
		ref = promotionexec.ApprovalFinance
	}
	return frontier.NodeOutcome{NodeID: req.Node.ID, Await: frontier.AwaitWorkItem, AwaitRef: ref}, runtime.GovernanceRefs{}, nil
}

func thresholdRoute(route workflow.Outcome) bool {
	return route == workflow.Outcome("ABOVE_THRESHOLD") || route == workflow.Outcome("WITHIN_THRESHOLD") || route == workflow.OutcomeUnknown
}

func validityRoute(status string) (workflow.Outcome, bool) {
	switch status {
	case "CONFIRMED":
		return workflow.Outcome("VALID"), true
	case string(revalidate.RequirementReapprovalRequired):
		return workflow.Outcome("REAPPROVAL_REQUIRED"), true
	case string(revalidate.RequirementBlock), string(revalidate.RequirementReplanRequired):
		return workflow.Outcome("BLOCKED"), true
	default:
		return "", false
	}
}

func observationRoute(node workflow.CompiledNode) workflow.Outcome {
	if len(node.Routes) == 0 {
		return workflow.Outcome(ObservationObserved)
	}
	if contains(node.Routes, workflow.Outcome(ObservationObserved)) {
		return workflow.Outcome(ObservationObserved)
	}
	return workflow.OutcomePass
}

func reconciliationRoute(node workflow.CompiledNode, status string) (workflow.Outcome, bool) {
	switch status {
	case ReconciliationConsistent:
		if len(node.Routes) == 0 {
			return workflow.Outcome(ReconciliationConsistent), true
		}
		if contains(node.Routes, workflow.Outcome(ReconciliationConsistent)) {
			return workflow.Outcome(ReconciliationConsistent), true
		}
		return workflow.OutcomePass, true
	case ReconciliationDegraded:
		if len(node.Routes) == 0 {
			return workflow.Outcome(ReconciliationDegraded), true
		}
		if contains(node.Routes, workflow.Outcome(ReconciliationDegraded)) {
			return workflow.Outcome(ReconciliationDegraded), true
		}
		return workflow.OutcomePartial, true
	default:
		return "", false
	}
}

func contains(routes []string, want workflow.Outcome) bool {
	for _, route := range routes {
		if route == string(want) {
			return true
		}
	}
	return false
}

func (r *Runner) commitKey(req execute.StepRequest, proposalDigest string) string {
	return req.TenantID.String() + "\x00" + req.InstanceID.String() + "\x00" + req.Node.ID + "\x00" + proposalDigest
}

func (r *Runner) executePromotion(ctx context.Context, req execute.StepRequest, proposalDigest string) (Artifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var artifact Artifact
	var ok bool
	artifact, ok = r.successfulCommits[r.commitKey(req, proposalDigest)]
	if ok {
		return artifact, nil
	}
	var err error
	if r.ports.PreparePromotion != nil && r.ports.ExecutePreparedPromotion != nil {
		prepared, prepareErr := r.ports.PreparePromotion.PreparePromotion(ctx, req, proposalDigest)
		if prepareErr != nil {
			return Artifact{}, prepareErr
		}
		artifact, err = r.ports.ExecutePreparedPromotion.ExecutePreparedPromotion(ctx, req, prepared)
	} else {
		artifact, err = r.ports.ExecutePromotion.ExecutePromotion(ctx, req, proposalDigest)
	}
	if err != nil {
		return Artifact{}, err
	}
	if strings.TrimSpace(artifact.OutputDigest) == "" {
		return Artifact{}, errOutputDigestMissing
	}
	r.successfulCommits[r.commitKey(req, proposalDigest)] = artifact
	return artifact, nil
}

// CompileApprovalRequirement compiles the exact requirement already published
// by promotionexec. It is kept here as the work-item composition seam so an
// execution adapter does not rebuild finance or manager requirements itself.
func CompileApprovalRequirement(nodeID, approver string, decideBy time.Time) (humanwork.ApprovalRequirement, error) {
	switch nodeID {
	case promotionexec.NodeApproveFinance:
		return promotionexec.CompileFinanceApprovalRequirement(approver, decideBy)
	case promotionexec.NodeApproveManager:
		return promotionexec.CompileManagerApprovalRequirement(approver, decideBy)
	default:
		return humanwork.ApprovalRequirement{}, fmt.Errorf("promotionsteps: node %q has no approval requirement", nodeID)
	}
}

// RulesThresholdPort is the default RULE-003 adapter. Input resolution stays
// behind Inputs, so this package can be tested with fakes and a composition
// root can source the typed values from the preceding domain artifacts.
type RulesThresholdPort struct {
	Inputs func(context.Context, execute.StepRequest) (rules.PromotionApprovalInput, error)
}

var _ ThresholdPort = RulesThresholdPort{}

// RaiseThreshold evaluates the published promotion threshold table.
func (p RulesThresholdPort) RaiseThreshold(ctx context.Context, req execute.StepRequest) (ret0 ThresholdResult, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_steps.raise_threshold", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if p.Inputs == nil {
		return ThresholdResult{}, errors.New("promotionsteps: RULE-003 input resolver is not configured")
	}
	in, err := p.Inputs(ctx, req)
	if err != nil {
		return ThresholdResult{}, err
	}
	table := rules.PromotionApprovalThresholdTable()
	decision, err := rules.EvaluatePromotionApproval(table, in)
	if err != nil {
		return ThresholdResult{}, err
	}
	route := workflow.OutcomeUnknown
	switch decision.Tier {
	case rules.ApprovalTierStandard:
		route = workflow.Outcome("WITHIN_THRESHOLD")
	case rules.ApprovalTierFinanceRequired, rules.ApprovalTierExecutiveRequired:
		route = workflow.Outcome("ABOVE_THRESHOLD")
	}
	return ThresholdResult{
		Artifact: Artifact{
			OutputDigest: digestParts("rules.compensation.raise_threshold/v3", decision.TableDigest, decision.MatchedRowID, string(decision.Tier)),
			Refs:         runtime.GovernanceRefs{DecisionID: decision.MatchedRowID, PolicyRef: decision.TableDigest},
		},
		Tier: decision.Tier, Route: route, Inputs: in, Decision: decision,
	}, nil
}

func digestParts(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// PromotionInputSnapshotDigest is a small helper for adapters that need to
// bind a snapshot output to the same canonical identity used by the domain.
// It does not inspect or invent input values.
func PromotionInputSnapshotDigest(snapshotDigest string) string {
	return digestParts("promotion.snapshot/v1", snapshotDigest)
}

// NewRecordedAt returns the request's supplied instant in UTC. It exists for
// adapters that need the same normalization as the runtime and deliberately
// does not fall back to a clock.
func NewRecordedAt(req execute.StepRequest) (values.Instant, error) {
	if req.RecordedAt.IsZero() {
		return values.Instant{}, errors.New("promotionsteps: RecordedAt is required")
	}
	return values.NewInstant(req.RecordedAt.UTC()), nil
}
