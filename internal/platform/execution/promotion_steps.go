package execution

// WF-RUN-034: the served EXECUTE path runs internal/platform/execution/
// promotionsteps for every node of the executable promotion graph. This file
// supplies the production adapter behind every promotionsteps port:
//
//   - the governed reads (snapshot, compensation, band, threshold inputs,
//     revalidation, the three observations) invoke the capability gateway
//     through the application's PromotionStepServices, as the delegation
//     runtime.Start pinned for the instance, with a purpose, a deadline, an
//     idempotency key and the node's declared effect set;
//   - execute_promotion runs inside the advance transaction
//     (execute.TransactionalStepRunner): it re-authorizes the commit through
//     the gateway, resolves the approved proposal to its plan-bound command
//     with PROMOUX-016's promotionterminal.Resolver and applies it with
//     internal/data/promotioncommit, so the domain facts and the node's
//     recorded outcome commit together or not at all, exactly once. The END
//     node then records only the terminal ledger fact.
//
// The application's step services are composed after the cell (the cell
// builds the gateway the services need, and the cell needs this execution
// wiring first), so they are bound late through
// [PromotionExecution.BindStepServices]. Until they are bound every port is
// absent, and promotionsteps fails each governed node closed with
// PORT_NOT_CONFIGURED; nothing is fabricated in the meantime.

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

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotionbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	domaincommit "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/revalidate"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionsteps"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionterminal"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// PromotionStepServices is the application's governed step service
// (*app.PromotionStepServices in production).
type PromotionStepServices interface {
	SnapshotWorker(context.Context, app.PromotionStepCall) (app.PromotionStepAnswer, error)
	SimulateCompensation(context.Context, app.PromotionStepCall) (app.PromotionStepAnswer, error)
	EvaluateBand(context.Context, app.PromotionStepCall) (app.PromotionStepAnswer, error)
	ThresholdInputs(context.Context, app.PromotionStepCall) (rules.PromotionApprovalInput, app.PromotionStepAnswer, error)
	AuthorizeCommit(context.Context, app.PromotionStepCall) (app.PromotionStepAnswer, error)
	GovernanceStanding(context.Context, app.PromotionStepCall) (app.GovernanceStanding, error)
	GovernedRead(ctx context.Context, call app.PromotionStepCall, capabilityID string, read func(context.Context) (any, error)) (any, app.PromotionStepAnswer, error)
}

var _ PromotionStepServices = (*app.PromotionStepServices)(nil)

// stepInvocationBudget bounds every capability invocation one step makes,
// measured from the step's recorded instant.
const stepInvocationBudget = 60 * time.Second

// ErrStepServicesBound refuses a second binding: the step services a running
// driver invokes are fixed once composed.
var ErrStepServicesBound = errors.New("platform execution: promotion step services are already bound")

// errNoDelegation fails a governed step whose instance pinned no delegation.
var errNoDelegation = errors.New("platform execution: the workflow instance pinned no execution delegation")

// promotionStepPorts is the production adapter behind every promotionsteps
// port. services is bound late; db opens the read transactions the
// out-of-transaction ports use.
type promotionStepPorts struct {
	db              execute.Beginner
	cellID          string
	authorityDigest string
	// planDigest is the compiled promotion plan this composition runs. The
	// approval record and its revalidation are bound to it, so a plan
	// recompiled after approval refuses revalidation on that binding alone.
	planDigest string
	// clock stamps the governance records and revalidation results.
	clock func() time.Time

	mu       sync.RWMutex
	services PromotionStepServices
}

// BindStepServices binds the application's governed step services to the
// composed promotion execution. It may be called once; before it is called
// every governed node fails closed.
func (p *PromotionExecution) BindStepServices(services PromotionStepServices) error {
	if p == nil || p.steps == nil {
		return fmt.Errorf("platform execution: this composition has no promotion step ports")
	}
	if services == nil {
		return fmt.Errorf("platform execution: bind promotion step services: nil services")
	}
	p.steps.mu.Lock()
	defer p.steps.mu.Unlock()
	if p.steps.services != nil {
		return ErrStepServicesBound
	}
	p.steps.services = services
	return nil
}

func (p *promotionStepPorts) bound() PromotionStepServices {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.services
}

// runner builds the promotionsteps runner for one step. A runner is built per
// step on purpose: promotionsteps keeps an in-process replay shield for
// execute_promotion, and a shield that outlived a rolled-back advance
// transaction would report a commit that never became durable. The durable
// fence is the transaction itself plus the commit's own replay refusal.
func (p *promotionStepPorts) runner() *promotionsteps.Runner {
	if p.bound() == nil {
		return promotionsteps.New(promotionsteps.Config{})
	}
	threshold := promotionsteps.RulesThresholdPort{Inputs: p.thresholdInputs}
	return promotionsteps.New(promotionsteps.Config{
		SnapshotWorker: p, SimulateCompensation: p, EvaluateBand: p, RaiseThreshold: threshold,
		Revalidate: p, StillValid: p, ExecutePromotion: p,
		ObservePayroll:        observationPort{ports: p, capabilityID: promotionexec.CapabilityObservePayroll, observe: p.observePayroll},
		ObserveAccess:         observationPort{ports: p, capabilityID: promotionexec.CapabilityObserveAccess, observe: p.observeAccess},
		ObserveReconciliation: p,
		CompensateHold:        p,
	})
}

// txKey carries the advance transaction to the execute_promotion port.
type txKey struct{}

func withStepTx(ctx context.Context, tx dbport.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

func stepTx(ctx context.Context) (dbport.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(dbport.Tx)
	return tx, ok && tx != nil
}

// declaredEffects is the effect set a node may cause: its compiled effect
// class and every weaker class. A DECISION node computes over inputs it
// sources through governed reads, so it declares READ_ONLY.
func declaredEffects(node workflow.CompiledNode) []capability.EffectClass {
	ladder := []capability.EffectClass{
		capability.EffectPure, capability.EffectReadOnly, capability.EffectInternalMutation,
		capability.EffectExternalMutation, capability.EffectIrreversibleExternalMutation,
	}
	bound := node.EffectClass
	if node.Type == workflow.StepDecision && (bound == "" || bound == capability.EffectPure) {
		bound = capability.EffectReadOnly
	}
	for i, class := range ladder {
		if class == bound {
			return append([]capability.EffectClass(nil), ladder[:i+1]...)
		}
	}
	return []capability.EffectClass{capability.EffectPure}
}

// inTenantRead runs fn in a tenant-scoped transaction that is always rolled
// back: every out-of-transaction port only reads.
func (p *promotionStepPorts) inTenantRead(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if p.db == nil {
		return fmt.Errorf("platform execution: promotion step ports have no database")
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("platform execution: begin promotion step read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	return fn(tx)
}

// WF-RUN-034: there is deliberately no committing helper beside inTenantRead.
// The GOVERN-002 record an approval produces is appended through the approval
// kernel's Record hook, inside the vote's own transaction, so a recorded
// approval and the governance its revalidation recomposes commit or roll back
// together.

// call loads the instance's delegation and builds the governed call for req.
// ex, when non-nil, is the advance transaction to read it through.
func (p *promotionStepPorts) call(ctx context.Context, ex runtime.Executor, req execute.StepRequest) (app.PromotionStepCall, PromotionStepServices, error) {
	services := p.bound()
	if services == nil {
		return app.PromotionStepCall{}, nil, fmt.Errorf("platform execution: promotion step services are not bound")
	}
	var delegation runtime.ExecutionDelegation
	var found bool
	load := func(q runtime.Executor) error {
		var err error
		delegation, found, err = runtime.LoadExecutionDelegation(ctx, q, req.TenantID, req.InstanceID)
		return err
	}
	var err error
	if ex != nil {
		err = load(ex)
	} else {
		err = p.inTenantRead(ctx, req.TenantID, func(tx dbport.Tx) error { return load(tx) })
	}
	if err != nil {
		return app.PromotionStepCall{}, nil, err
	}
	if !found {
		return app.PromotionStepCall{}, nil, errNoDelegation
	}
	return app.PromotionStepCall{
		Delegation: delegation, IntentID: req.Proposal.Revision.IntentID, NodeID: req.Node.ID,
		IdempotencyKey:  fmt.Sprintf("workflow:%s:%s:%s:%d", req.TenantID, req.InstanceID, req.Node.ID, req.Attempt),
		Deadline:        req.RecordedAt.UTC().Add(stepInvocationBudget),
		DeclaredEffects: declaredEffects(req.Node),
	}, services, nil
}

// refs records the gateway evidence and the delegated subject beside the
// node's outcome.
func refs(req execute.StepRequest, answer app.PromotionStepAnswer) runtime.GovernanceRefs {
	out := runtime.GovernanceRefs{ProposalRef: req.Proposal.Revision.MaterialDigest.Digest, AuthorizationDecisionID: answer.Subject}
	if len(answer.EvidenceIDs) > 0 {
		out.CapabilityExecutionID = answer.EvidenceIDs[len(answer.EvidenceIDs)-1]
		out.EffectRefs = append([]string(nil), answer.EvidenceIDs...)
	}
	return out
}

func digestOf(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Snapshot implements promotionsteps.SnapshotPort.
func (p *promotionStepPorts) Snapshot(ctx context.Context, req execute.StepRequest) (ret0 promotionsteps.Artifact, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_ports.snapshot", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	return p.governed(ctx, req, PromotionStepServices.SnapshotWorker)
}

// SimulateCompensation implements promotionsteps.CompensationPort.
func (p *promotionStepPorts) SimulateCompensation(ctx context.Context, req execute.StepRequest) (ret0 promotionsteps.Artifact, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_ports.simulate_compensation", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	return p.governed(ctx, req, PromotionStepServices.SimulateCompensation)
}

// EvaluateBand implements promotionsteps.BandPort.
func (p *promotionStepPorts) EvaluateBand(ctx context.Context, req execute.StepRequest) (ret0 promotionsteps.Artifact, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_ports.evaluate_band", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	return p.governed(ctx, req, PromotionStepServices.EvaluateBand)
}

func (p *promotionStepPorts) governed(ctx context.Context, req execute.StepRequest, invoke func(PromotionStepServices, context.Context, app.PromotionStepCall) (app.PromotionStepAnswer, error)) (promotionsteps.Artifact, error) {
	call, services, err := p.call(ctx, nil, req)
	if err != nil {
		return promotionsteps.Artifact{}, err
	}
	answer, err := invoke(services, ctx, call)
	if err != nil {
		return promotionsteps.Artifact{}, err
	}
	return promotionsteps.Artifact{OutputDigest: answer.Digest, Refs: refs(req, answer)}, nil
}

// thresholdInputs sources RULE-003's inputs for promotionsteps'
// RulesThresholdPort through governed reads.
func (p *promotionStepPorts) thresholdInputs(ctx context.Context, req execute.StepRequest) (ret0 rules.PromotionApprovalInput, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_ports.threshold_inputs", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	call, services, err := p.call(ctx, nil, req)
	if err != nil {
		return rules.PromotionApprovalInput{}, err
	}
	in, _, err := services.ThresholdInputs(ctx, call)
	return in, err
}

// revalidation is the typed GOVERN-003 answer the revalidate read produces.
type revalidation struct {
	requirement revalidate.RequirementKind
	confirmed   bool
	sourced     []string
	digest      string
}

// evaluateRevalidation recomposes the promotion's governance from current
// facts and compares it with the decision its approval recorded
// (migration 00309). An instance with no durable record, a record that does
// not reproduce from its own inputs, a record that never allowed, or a plan
// recompiled since it was taken all block: none of them is a confirmation.
func (p *promotionStepPorts) evaluateRevalidation(ctx context.Context, req execute.StepRequest, standing app.GovernanceStanding) (revalidation, error) {
	out := revalidation{requirement: revalidate.RequirementBlock}
	err := p.inTenantRead(ctx, req.TenantID, func(tx dbport.Tx) error {
		record, err := loadApprovalGovernance(ctx, tx, req.TenantID, req.InstanceID)
		if errors.Is(err, ErrNoApprovalGovernance) {
			out.sourced = append(out.sourced, "governance.record=absent")
			return nil
		}
		if err != nil {
			return err
		}
		out.sourced = append(out.sourced, "governance.record="+record.Historical.Decision.Digest, "governance.state="+string(record.Historical.Decision.State))
		facts, err := p.governanceFacts(ctx, tx, governanceInputs{
			tenantID: req.TenantID, instanceID: req.InstanceID, proposal: req.Proposal,
			planDigest: p.planDigest, standing: standing,
		})
		if err != nil {
			return err
		}
		result, err := revalidate.Revalidate(p.instant, record.Historical, facts, p.planDigest)
		if err != nil {
			out.sourced = append(out.sourced, "revalidation.refused="+err.Error())
			return nil
		}
		out.sourced = append(out.sourced, "revalidation.recomposed="+result.RecomposedDigest, "revalidation.state="+string(result.RecomposedState))
		for _, changed := range result.ChangedInputs {
			out.sourced = append(out.sourced, "revalidation.changed="+string(changed))
		}
		switch {
		case result.Confirmed && record.Historical.Allows():
			out.confirmed, out.requirement = true, revalidate.RequirementNone
		case result.Confirmed:
			// The world is unchanged and the recorded decision never allowed.
			out.sourced = append(out.sourced, "revalidation.confirmed_but_blocking=true")
			out.requirement = revalidate.RequirementBlock
		default:
			out.requirement = result.Requirement
		}
		return nil
	})
	if err != nil {
		return revalidation{}, err
	}
	out.digest = digestOf(append([]string{"govern-003.revalidation/v1", string(out.requirement), fmt.Sprintf("confirmed=%t", out.confirmed)}, out.sourced...)...)
	return out, nil
}

// instant is the clock revalidation stamps its result with.
func (p *promotionStepPorts) instant() values.Instant {
	now := time.Now().UTC()
	if p.clock != nil {
		now = p.clock().UTC()
	}
	return values.NewInstant(now)
}

func (p *promotionStepPorts) revalidationRead(ctx context.Context, req execute.StepRequest) (revalidation, app.PromotionStepAnswer, error) {
	call, services, err := p.call(ctx, nil, req)
	if err != nil {
		return revalidation{}, app.PromotionStepAnswer{}, err
	}
	standing, err := services.GovernanceStanding(ctx, call)
	if err != nil {
		return revalidation{}, app.PromotionStepAnswer{}, err
	}
	got, answer, err := services.GovernedRead(ctx, call, promotionexec.CapabilityRevalidate, func(ctx context.Context) (any, error) {
		return p.evaluateRevalidation(ctx, req, standing)
	})
	if err != nil {
		return revalidation{}, answer, err
	}
	result, ok := got.(revalidation)
	if !ok {
		return revalidation{}, answer, fmt.Errorf("platform execution: revalidation read returned %T", got)
	}
	return result, answer, nil
}

// Revalidate implements promotionsteps.RevalidatePort.
func (p *promotionStepPorts) Revalidate(ctx context.Context, req execute.StepRequest) (ret0 promotionsteps.RevalidationResult, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_ports.revalidate", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	result, answer, err := p.revalidationRead(ctx, req)
	if err != nil {
		return promotionsteps.RevalidationResult{}, err
	}
	return promotionsteps.RevalidationResult{
		Artifact:  promotionsteps.Artifact{OutputDigest: result.digest, Refs: refs(req, answer)},
		Confirmed: result.confirmed, Requirement: result.requirement,
	}, nil
}

// StillValid implements promotionsteps.ValidityPort. It re-reads the same
// durable facts the revalidate node read, through the gateway, rather than
// trusting a remembered answer.
func (p *promotionStepPorts) StillValid(ctx context.Context, req execute.StepRequest) (ret0 promotionsteps.ValidityResult, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_ports.still_valid", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	result, answer, err := p.revalidationRead(ctx, req)
	if err != nil {
		return promotionsteps.ValidityResult{}, err
	}
	status := string(result.requirement)
	if result.confirmed {
		status = "CONFIRMED"
	}
	return promotionsteps.ValidityResult{
		Artifact: promotionsteps.Artifact{OutputDigest: result.digest, Refs: refs(req, answer)},
		Status:   status,
	}, nil
}

// commitResolver is PROMOUX-016's resolver bound to this tenant's local
// consistency boundary and the delegated actor.
func (p *promotionStepPorts) commitResolver(tenantID uuid.UUID, actor string) promotionterminal.Resolver {
	return promotionterminal.Resolver{
		Revisions: intentcontrol.RevisionStore{}, Decisions: promotionterminal.WorkItemDecisions{},
		People: aggregates.PeopleStore{}, Organization: aggregates.OrganizationStore{}, Compensation: aggregates.CompensationStore{},
		Boundary: transaction.ConsistencyBoundary{
			BoundaryID: "boundary:" + p.cellID + ":local", Tenant: values.TenantId(tenantID.String()), CellID: p.cellID,
			CoordinatorID: "coordinator:" + p.cellID,
			Admitted:      []transaction.AdmissionSelector{{StorageClass: "LOCAL_POSTGRES"}},
			Isolation:     transaction.IsolationSerializable, Protocol: transaction.CommitProtocolSingleDatabaseACID,
			CoordinatorEpoch: 1, CrossBoundaryDisposition: transaction.CrossBoundaryDispositionSplitIntoEffects,
		},
		AuthorityDigest: p.authorityDigest, ActorPrincipalID: actor,
	}
}

func commitRequest(req execute.StepRequest, idempotencyKey string) execute.TerminalWriteRequest {
	out := execute.TerminalWriteRequest{
		TenantID: req.TenantID, InstanceID: req.InstanceID, Proposal: req.Proposal,
		CorrelationID: req.CorrelationID, IdempotencyKey: idempotencyKey, RecordedAt: req.RecordedAt,
	}
	if req.Plan != nil {
		out.WorkflowID, out.PlanDigest = req.Plan.WorkflowID, req.Plan.Digest()
	}
	return out
}

// ExecutePromotion implements promotionsteps.ExecutePromotionPort. It runs
// only inside the advance transaction: the commit is authorized through the
// gateway as the delegated principal, resolved from the approved proposal and
// applied in that transaction.
func (p *promotionStepPorts) ExecutePromotion(ctx context.Context, req execute.StepRequest, idempotencyKey string) (ret0 promotionsteps.Artifact, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_ports.execute_promotion", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	tx, ok := stepTx(ctx)
	if !ok {
		return promotionsteps.Artifact{}, fmt.Errorf("platform execution: execute_promotion runs only inside the advance transaction")
	}
	call, services, err := p.call(ctx, tx, req)
	if err != nil {
		return promotionsteps.Artifact{}, err
	}
	answer, err := services.AuthorizeCommit(ctx, call)
	if err != nil {
		return promotionsteps.Artifact{}, err
	}
	twr := commitRequest(req, idempotencyKey)
	cmd, err := p.commitResolver(req.TenantID, call.Delegation.Subject).Resolve(ctx, tx, twr)
	if err != nil {
		return promotionsteps.Artifact{}, fmt.Errorf("platform execution: resolve the promotion commit: %w", err)
	}
	if cmd.TenantID != req.TenantID.String() || cmd.ProposalRevisionID != req.Proposal.Revision.ProposalRevisionID ||
		cmd.ProposalDigest != idempotencyKey || cmd.WorkflowPlanDigest != twr.PlanDigest {
		return promotionsteps.Artifact{}, fmt.Errorf("%w: the resolved command does not bind this step", domaincommit.ErrInvalidCommand)
	}
	receipt, err := (promotioncommit.Writer{}).Write(ctx, tx, cmd)
	if err != nil {
		return promotionsteps.Artifact{}, err
	}
	stepRefs := refs(req, answer)
	stepRefs.EffectRefs = append(stepRefs.EffectRefs, receipt.OutboxIDs...)
	return promotionsteps.Artifact{
		OutputDigest: digestOf("promotion.core_commit/v1", cmd.ProposalDigest, receipt.AssignmentRowID, receipt.OccupancyRowID, receipt.BasePayRowID, receipt.BudgetRowID),
		Refs:         stepRefs,
	}, nil
}

// CompensateHold implements promotionsteps.HoldReleasePort: the bounded
// automatic correction behind compensate_budget_hold. It releases the unit's
// compensation-pool hold under the proposal's original idempotency identity,
// inside the advance transaction, so the release and the node's recorded
// outcome commit together or not at all. The committed promotion itself is
// never touched: honest partial completion stands while only the encumbrance
// is unwound for the RepairPlan that follows.
//
// The step reports COMPENSATED whether a hold was open or not. Both leave
// the same postcondition (no outstanding hold for this proposal); the output
// digest records which was true, so history never claims a release that did
// not happen. Like execute_promotion it runs only inside the advance
// transaction and only for the delegation runtime.Start pinned.
func (p *promotionStepPorts) ReleaseHold(ctx context.Context, req execute.StepRequest) (ret0 promotionsteps.HoldReleaseResult, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_ports.compensate_hold", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	tx, ok := stepTx(ctx)
	if !ok {
		return promotionsteps.HoldReleaseResult{}, fmt.Errorf("platform execution: compensate_budget_hold runs only inside the advance transaction")
	}
	proposalDigest := req.Proposal.Revision.MaterialDigest.Digest
	if strings.TrimSpace(proposalDigest) == "" {
		return promotionsteps.HoldReleaseResult{}, fmt.Errorf("platform execution: compensate_budget_hold needs the proposal material digest")
	}
	intentID, err := uuid.Parse(req.Proposal.Revision.IntentID)
	if err != nil {
		return promotionsteps.HoldReleaseResult{}, fmt.Errorf("platform execution: compensate_budget_hold needs a UUID intent identity: %w", err)
	}
	call, _, err := p.call(ctx, tx, req)
	if err != nil {
		return promotionsteps.HoldReleaseResult{}, err
	}
	released, err := promotionbudget.ReleaseForIntent(ctx, tx, req.TenantID, intentID, req.RecordedAt.UTC())
	if err != nil {
		return promotionsteps.HoldReleaseResult{}, err
	}
	held := "held=false"
	if released {
		held = "held=true"
	}
	return promotionsteps.HoldReleaseResult{
		Artifact: promotionsteps.Artifact{
			OutputDigest: digestOf("promotion.compensation.hold_release/v1", proposalDigest, held, req.RecordedAt.UTC().Format(time.RFC3339Nano)),
			Refs: runtime.GovernanceRefs{
				ProposalRef:             proposalDigest,
				AuthorizationDecisionID: call.Delegation.Subject,
			},
		},
		Status: "COMPENSATED",
	}, nil
}

// observation is one local-store observation of the committed promotion.
type observation struct {
	status string
	facts  []string
}

// observed resolves the committed command and hands it to check in one read
// transaction. A promotion whose command cannot be resolved is observed FAIL.
func (p *promotionStepPorts) observed(ctx context.Context, req execute.StepRequest, check func(context.Context, dbport.Tx, domaincommit.Command) (observation, error)) (observation, error) {
	var out observation
	err := p.inTenantRead(ctx, req.TenantID, func(tx dbport.Tx) error {
		twr := commitRequest(req, req.Proposal.Revision.MaterialDigest.Digest)
		cmd, resolveErr := p.commitResolver(req.TenantID, "system:promotion-observation").Resolve(ctx, tx, twr)
		if resolveErr != nil {
			out = observation{status: promotionsteps.ObservationFailed, facts: []string{"command.resolved=false"}}
			return nil
		}
		var err error
		out, err = check(ctx, tx, cmd)
		return err
	})
	return out, err
}

func outboxHolds(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, effectID string) (bool, error) {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE tenant_id = $1 AND effect_identity = $2`, tenantID, effectID).Scan(&count); err != nil {
		return false, fmt.Errorf("platform execution: read outbox effect %s: %w", effectID, err)
	}
	return count == 1, nil
}

// observePayroll reports OBSERVED only when the compensation projection payroll
// consumes shows the committed base pay at the effective date and the payroll
// sync leg is enqueued. internal/data/payrollstore records pay runs, not a
// worker's pay revision, so it cannot reflect a single promotion and is not
// read.
func (p *promotionStepPorts) observePayroll(ctx context.Context, req execute.StepRequest) (observation, error) {
	return p.observed(ctx, req, payrollObservation)
}

func payrollObservation(ctx context.Context, tx dbport.Tx, cmd domaincommit.Command) (observation, error) {
	out := observation{status: promotionsteps.ObservationFailed}
	tenantID, tenantErr := uuid.Parse(cmd.TenantID)
	baseID, err := uuid.Parse(cmd.BasePayComponentID)
	if err != nil || tenantErr != nil {
		return out, nil
	}
	payMatches := false
	if base, readErr := (aggregates.CompensationStore{}).CurrentCompensationComponent(ctx, tx, tenantID, baseID, cmd.EffectiveAt); readErr == nil {
		if amount, parseErr := values.NewDecimal(base.Amount, 4, values.RoundingExactRequired); parseErr == nil {
			payMatches = base.Currency == cmd.BasePay.Currency() && amount.Cmp(cmd.BasePay.Amount()) == 0
		}
	}
	enqueued, err := outboxHolds(ctx, tx, tenantID, "payroll:"+cmd.ProposalRevisionID)
	if err != nil {
		return out, err
	}
	out.facts = []string{fmt.Sprintf("compensation.base_pay.committed=%t", payMatches), fmt.Sprintf("outbox.payroll_sync.enqueued=%t", enqueued)}
	if payMatches && enqueued {
		out.status = promotionsteps.ObservationObserved
	}
	return out, nil
}

// observeAccess reports OBSERVED only when the assignment projection access
// provisioning reads shows the target job and grade at the effective date
// and the IAM sync leg is enqueued.
func (p *promotionStepPorts) observeAccess(ctx context.Context, req execute.StepRequest) (observation, error) {
	return p.observed(ctx, req, accessObservation)
}

func accessObservation(ctx context.Context, tx dbport.Tx, cmd domaincommit.Command) (observation, error) {
	out := observation{status: promotionsteps.ObservationFailed}
	tenantID, tenantErr := uuid.Parse(cmd.TenantID)
	assignmentID, err := uuid.Parse(cmd.AssignmentID)
	if err != nil || tenantErr != nil {
		return out, nil
	}
	placed := false
	if assignment, readErr := (aggregates.PeopleStore{}).CurrentAssignment(ctx, tx, tenantID, assignmentID, cmd.EffectiveAt); readErr == nil {
		placed = assignment.JobCode == cmd.TargetJobCode && assignment.Grade == cmd.TargetGrade
	}
	enqueued, err := outboxHolds(ctx, tx, tenantID, "iam:"+cmd.ProposalRevisionID)
	if err != nil {
		return out, err
	}
	out.facts = []string{fmt.Sprintf("assignment.target_placement.committed=%t", placed), fmt.Sprintf("outbox.iam_sync.enqueued=%t", enqueued)}
	if placed && enqueued {
		out.status = promotionsteps.ObservationObserved
	}
	return out, nil
}

// occupancyObservation reports OBSERVED when the committed position
// occupancy names the promoted worker at the effective date.
func occupancyObservation(ctx context.Context, tx dbport.Tx, cmd domaincommit.Command) (observation, error) {
	out := observation{status: promotionsteps.ObservationFailed}
	tenantID, tenantErr := uuid.Parse(cmd.TenantID)
	occupancyID, err := uuid.Parse(cmd.PositionOccupancyID)
	if err != nil || tenantErr != nil {
		return out, nil
	}
	if occupancy, readErr := (aggregates.OrganizationStore{}).CurrentPositionOccupancy(ctx, tx, tenantID, occupancyID, cmd.EffectiveAt); readErr == nil &&
		occupancy.WorkerRef != nil && occupancy.WorkerRef.String() == cmd.WorkerID {
		out.status = promotionsteps.ObservationObserved
	}
	return out, nil
}

// observationPort adapts one local-store observation to
// promotionsteps.ObservationPort through the gateway.
type observationPort struct {
	ports        *promotionStepPorts
	capabilityID string
	observe      func(context.Context, execute.StepRequest) (observation, error)
}

// Observe implements promotionsteps.ObservationPort.
func (o observationPort) Observe(ctx context.Context, req execute.StepRequest) (ret0 promotionsteps.ObservationResult, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_ports.observe", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	got, answer, err := o.ports.governedObservation(ctx, req, o.capabilityID, o.observe)
	if err != nil {
		return promotionsteps.ObservationResult{}, err
	}
	return promotionsteps.ObservationResult{
		Artifact: promotionsteps.Artifact{OutputDigest: digestOf(append([]string{o.capabilityID, got.status}, got.facts...)...), Refs: refs(req, answer)},
		Status:   got.status,
	}, nil
}

func (p *promotionStepPorts) governedObservation(ctx context.Context, req execute.StepRequest, capabilityID string, read func(context.Context, execute.StepRequest) (observation, error)) (observation, app.PromotionStepAnswer, error) {
	call, services, err := p.call(ctx, nil, req)
	if err != nil {
		return observation{}, app.PromotionStepAnswer{}, err
	}
	got, answer, err := services.GovernedRead(ctx, call, capabilityID, func(ctx context.Context) (any, error) {
		return read(ctx, req)
	})
	if err != nil {
		return observation{}, answer, err
	}
	result, ok := got.(observation)
	if !ok {
		return observation{}, answer, fmt.Errorf("platform execution: %s returned %T", capabilityID, got)
	}
	return result, answer, nil
}

// Reconcile implements promotionsteps.ReconciliationPort: CONSISTENT only
// when the payroll-side and access-side observations both hold and the
// committed position occupancy names the promoted worker; DEGRADED otherwise.
func (p *promotionStepPorts) Reconcile(ctx context.Context, req execute.StepRequest) (ret0 promotionsteps.ReconciliationResult, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_ports.reconcile", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	got, answer, err := p.governedObservation(ctx, req, promotionexec.CapabilityObserveReconciliation, func(ctx context.Context, req execute.StepRequest) (observation, error) {
		payroll, err := p.observePayroll(ctx, req)
		if err != nil {
			return observation{}, err
		}
		access, err := p.observeAccess(ctx, req)
		if err != nil {
			return observation{}, err
		}
		occupied, err := p.observed(ctx, req, occupancyObservation)
		if err != nil {
			return observation{}, err
		}
		return reconcile(payroll, access, occupied), nil
	})
	if err != nil {
		return promotionsteps.ReconciliationResult{}, err
	}
	return promotionsteps.ReconciliationResult{
		Artifact: promotionsteps.Artifact{OutputDigest: digestOf(append([]string{promotionexec.CapabilityObserveReconciliation, got.status}, got.facts...)...), Refs: refs(req, answer)},
		Status:   got.status,
	}, nil
}

// reconcile compares the three local observations: CONSISTENT only when all
// hold, DEGRADED otherwise.
func reconcile(payroll, access, occupied observation) observation {
	out := observation{status: promotionsteps.ReconciliationDegraded, facts: []string{
		"payroll=" + payroll.status, "access=" + access.status, "occupancy=" + occupied.status,
	}}
	if payroll.status == promotionsteps.ObservationObserved && access.status == promotionsteps.ObservationObserved && occupied.status == promotionsteps.ObservationObserved {
		out.status = promotionsteps.ReconciliationConsistent
	}
	return out
}

// promotionStepRunner runs the composed promotion graph. The prototype plan
// parks on APPROVAL and completes on END; the executable plan runs
// promotionsteps for every node, with execute_promotion inside the advance
// transaction.
type promotionStepRunner struct {
	plan           PromotionPlan
	effectiveDates *sync.Map
	ports          *promotionStepPorts
}

var _ execute.TransactionalStepRunner = promotionStepRunner{}

// Run implements execute.StepRunner.
func (r promotionStepRunner) Run(ctx context.Context, req execute.StepRequest) (ret0 frontier.NodeOutcome, ret1 runtime.GovernanceRefs, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_runner.run", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0, ret1) }()
	if r.plan == "" || r.plan == PLAN_PROTOTYPE {
		return runPrototypeStep(req)
	}
	if r.effectiveDates != nil {
		if date, ok := req.Proposal.Revision.EffectiveTime.StartDate(); ok {
			r.effectiveDates.Store(req.InstanceID.String(), date)
		}
	}
	return r.ports.runner().Run(ctx, req)
}

// RunsInTransaction implements execute.TransactionalStepRunner: only the
// executable plan's governed core commit runs inside the advance transaction.
func (r promotionStepRunner) RunsInTransaction(node workflow.CompiledNode) bool {
	return r.plan == PLAN_EXECUTE && node.ID == promotionexec.NodeExecutePromotion
}

// RunInTx implements execute.TransactionalStepRunner.
func (r promotionStepRunner) RunInTx(ctx context.Context, ex runtime.Executor, req execute.StepRequest) (ret0 frontier.NodeOutcome, ret1 runtime.GovernanceRefs, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.promotion_step_runner.run_in_tx", req)
	defer func() { observe.DoneWith(obsOp, retErr, ret0, ret1) }()
	tx, ok := ex.(dbport.Tx)
	if !ok {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("platform execution: execute_promotion needs the advance transaction, got %T", ex)
	}
	if strings.TrimSpace(req.Node.ID) == "" {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("platform execution: in-transaction step names no node")
	}
	return r.ports.runner().Run(withStepTx(ctx, tx), req)
}
