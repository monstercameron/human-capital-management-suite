package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// OBS-024's execution-evidence vocabulary entries [IntentService.ExecuteIntent]
// itself records. This is a deliberate, documented duplication of
// internal/workflow/execute's own EvidenceKindGateRefused/
// EvidenceKindGateAdmitted constants: this package must not import
// internal/workflow/execute (see execution.go's ProposalExecutor doc for
// why), so it carries its own copy of the two kinds it is the sole recorder
// of. A later phase that centralizes the vocabulary for the inspector
// (execute.EvidenceKind's own REFACTOR note) replaces both copies with one
// shared definition neither package currently depends on.
const (
	EvidenceKindGateRefused  = "GATE_REFUSED"
	EvidenceKindGateAdmitted = "GATE_ADMITTED"
)

// Reason references [IntentService.ExecuteIntent] owns.
const (
	reasonExecutionOutcomeAmbiguous = "intent.execution_outcome_ambiguous"
	// reasonExecutionRoleRequired reports a cell whose ExecutionAuthority is
	// configured and admits the intent type, but whose caller does not carry
	// the role the authority names.
	reasonExecutionRoleRequired = "p1b.execution_role_required"
	// reasonNoExecutablePlan reports an intent whose current preflight/
	// simulation produced no executable plan to run (the same condition
	// SimulateIntent expresses by minting no proposal revision at all).
	reasonNoExecutablePlan = "p1b.no_executable_plan"
	// reasonStaleProposal reports a caller-presented proposal revision or
	// material digest that does not match what this cell would itself
	// (re)compute for the named intent right now.
	reasonStaleProposal = "p1b.stale_proposal"
	// reasonUnapprovedProposal reports a proposal revision presented with no
	// recorded approval. Since WF-RUN-027 it reports two conditions that are
	// the same fact seen at two depths: an approval this cell's own gate
	// refuses to accept as presented, and one the workflow runtime could not
	// find a recorded decision for in intent_decision.
	reasonUnapprovedProposal = "p1b.unapproved_proposal"
	// reasonSupersededProposal reports a proposal revision the intent
	// relationship graph records as superseded, whatever currency the caller
	// asserted for it (WF-RUN-027).
	reasonSupersededProposal = "p1b.superseded_proposal"
	// reasonExecutionUnavailable reports an authorized, approved call this
	// cell still cannot run because it was not composed with the
	// driver-side wiring EXECUTE needs.
	reasonExecutionUnavailable = "workflow.execution_unavailable"
	// reasonNoActiveWorkflowVersion reports a start refused because no
	// published version of the workflow has been approved and activated in
	// this cell's registry (runtime.VERSION_NOT_ACTIVE). It is deliberately
	// not reasonExecutionUnavailable: that one says "this cell cannot execute
	// at all", travels as a retryable UNAVAILABLE and tells the reader to try
	// again, which this condition never resolves. Nothing the caller does to
	// the journey changes it; an operator releases a version.
	reasonNoActiveWorkflowVersion = "workflow.no_active_version"

	ruleExecutionAuthorityGate = "release.p1b_execution_authority_gate"
	// ruleWorkflowVersionRelease names the governed release the refusal above
	// is waiting on, so an operator reading the refusal knows which control it
	// points at rather than which component failed.
	ruleWorkflowVersionRelease = "release.workflow_version_activation"
)

// ExecuteIntent runs the caller-driven workflow driver for an intent whose
// current proposal revision is approved and immutable, and returns the
// resulting execution receipt.
//
// It is refused under the exact envelope every other governed write in this
// release already uses ([p1aRefusal]) unless three things are all true:
// this cell was composed with a non-nil [ExecutionAuthority] that admits the
// intent's own type, the authenticated caller carries the role that
// authority names, and the presented [intentsv1.ProposalApproval] names the
// exact proposal revision and material digest this cell would itself
// (re)compute for the intent right now. A cell composed with no
// ExecutionAuthority answers identically regardless of the caller, the
// approval presented, or whether a [ProposalExecutor] happens to be wired:
// P1A cells never execute.
func (s *IntentService) ExecuteIntent(ctx context.Context, req *intentsv1.ExecuteIntentRequest) (*intentsv1.ExecuteIntentResponse, error) {
	principal, inv, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}

	inst, rec, ownedErr := s.loadInstance(ctx, principal.Tenant().String(), req.GetIntentId())
	if ownedErr != nil {
		return nil, ownedErr
	}
	if want := req.GetExpectedInstanceVersion(); want != 0 && want != rec.InstanceVersion {
		return nil, envelope.New(envelope.CodeFailedPrecondition, reasonStaleRevision,
			"a precondition for the operation is not met").
			WithViolation("expected_instance_version", "the expected instance version is stale", "intent.expected_revision")
	}
	def, err := s.defs.Resolve(inst.Definition)
	if err != nil {
		return nil, envelope.New(envelope.CodeNotFound, reasonDefinitionUnknown,
			"the resource does not exist or is not visible").WithDiagnostic(err)
	}

	// The authority gate runs before this cell does anything else observable
	// (before it even re-simulates), so a caller who is refused here learns
	// nothing about whether the intent has an executable plan at all.
	//
	// OBS-024: the gate's own decision is recorded as evidence right here —
	// including a refusal, which is the whole point of this todo (RED:
	// "ExecuteIntent records evidence only for its re-simulation, never for
	// gate refusals, approvals, submissions or the terminal write") — before
	// this cell has looked at the presented approval or run a single node.
	if ownedErr := s.authorizeExecution(principal, def); ownedErr != nil {
		s.recordGateEvidence(ctx, principal.Tenant().String(), EvidenceKindGateRefused, inst.IntentID, ownedErr.ReasonRef())
		return nil, ownedErr
	}
	gateEvidenceID, evErr := s.recordGateEvidence(ctx, principal.Tenant().String(), EvidenceKindGateAdmitted, inst.IntentID, "")
	if evErr != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(evErr)
	}

	// Re-running the exact SimulateIntent path is what proves the presented
	// proposal_revision_id/material_proposal_digest name real, current
	// content: P1A mints a proposal revision id deterministically from the
	// intent's own identity and canonical request digest
	// ([derivedIDs]/[simulationRevision]), so simulating twice for the same
	// stored intent produces the same revision id and digest, never a
	// caller-invented one.
	simulated, ownedErr := s.simulateDetailed(ctx, principal, purposeOf(principal, inv), inst, def)
	artifact := simulated.Artifact
	if ownedErr != nil {
		return nil, ownedErr
	}
	if artifact.GetProposalRevisionId() == "" {
		return nil, envelope.New(envelope.CodeFailedPrecondition, reasonNoExecutablePlan,
			"a precondition for the operation is not met").
			WithViolation("intent_id", "the intent has no executable proposal to run", ruleExecutionAuthorityGate)
	}
	if simulated.Revision == nil || simulated.Revision.ProposalRevisionID == "" {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(fmt.Errorf("executable simulation returned no minted proposal revision"))
	}

	if ownedErr := checkApproval(req.GetApproval(), artifact); ownedErr != nil {
		return nil, ownedErr
	}

	if s.executor == nil || s.executionResolver == nil || s.executionVersions == nil || s.tenantUUID == nil ||
		s.executionFacts == nil {
		return nil, executionUnavailable()
	}

	start, ownedErr := s.executionStart(inst, artifact, req.GetApproval().GetApprovalRef(), *simulated.Revision)
	if ownedErr != nil {
		return nil, ownedErr
	}
	// HIPERF-004/005: a top-band subject under the execute plan pins the
	// high-performer variant digest; everyone else carries no pin and the
	// resolver serves the plan's own digest.
	if pin := s.highPerformerPin(ctx, inst.Tenant, employmentSubject(inst.Subjects)); pin != "" {
		start.PinnedCompiledPlanDigest = pin
	}
	// WF-RUN-034: the instance's later steps act as this verified principal,
	// re-authorized against current policy at each invocation.
	start.Delegation = executionDelegation(principal, purposeOf(principal, inv))

	result, err := s.executor.Execute(ctx, start)
	if err != nil {
		return nil, executionError(err)
	}
	if err := result.validate(); err != nil {
		return nil, executionError(err)
	}
	if outcomeErr := s.consumeExecutionResult(ctx, inst, def, rec, result); outcomeErr != nil {
		return nil, envelope.New(envelope.CodeFailedPrecondition, reasonStaleRevision,
			"the workflow outcome could not be bound to this intent").WithDiagnostic(outcomeErr)
	}
	// The GATE_ADMITTED entry recorded above precedes every evidence id the
	// driver itself records (APPROVAL_COMPLETED/TASK_SUBMITTED/
	// TERMINAL_WRITTEN), so it leads result.EvidenceIDs rather than trailing it.
	if gateEvidenceID != "" {
		result.EvidenceIDs = append([]string{gateEvidenceID}, result.EvidenceIDs...)
	}
	return &intentsv1.ExecuteIntentResponse{Execution: executionReceiptProto(result)}, nil
}

// executionStart builds the [runtime.StartRequest] one approved, re-simulated
// promotion proposal is started with.
//
// It is a shared helper rather than an inline literal because a resume of the
// same instance has to present the identical Start the instance was created
// from (internal/workflow/execute.ResumeRequest.Start is context: it
// re-resolves the pinned plan and re-checks the durable WorkItem's tenant,
// correlation and proposal binding against it). A second, hand-copied literal
// somewhere else would be a second definition of "the same start", and the
// first divergence between them would surface as an unexplained
// WORK_ITEM_DRIFT rather than as a compile error.
//
// ExecuteIntent passes the already-minted revision through the optional
// argument, preserving its writes, baselines, effective interval and control
// context without a second simulation. Resume and older callers may omit it;
// those callers retain the compatibility shell until durable proposal
// persistence is introduced.
func (s *IntentService) executionStart(
	inst intent.Instance, artifact *intentsv1.SimulationArtifact, _ string, minted intent.ProposalRevision,
) (runtime.StartRequest, *envelope.Error) {
	materialDigest, digestErr := digest.FromProto(artifact.GetMaterialProposalDigest())
	if digestErr != nil {
		return runtime.StartRequest{}, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(digestErr)
	}
	subjectRefs := make([]string, 0, len(inst.Subjects))
	for _, subj := range inst.Subjects {
		subjectRefs = append(subjectRefs, subj.SubjectID)
	}
	cellID := s.executionCellID
	if cellID == "" {
		cellID = "cell-local"
	}
	// WF-RUN-027: the binding carries the revision and nothing else. Start
	// requires both durable-facts ports and resolves authorization and
	// supersession from intent_decision and intent_relationship itself, so an
	// Execute presenting Approved=true with no recorded decision is refused
	// (runtime.CodeUnapprovedProposal -> reasonUnapprovedProposal) and a
	// superseded revision is refused (runtime.CodeSupersededProposal) whatever
	// the caller said.
	revision := intent.ProposalRevision{
		ProposalRevisionID:  artifact.GetProposalRevisionId(),
		IntentID:            inst.IntentID,
		Revision:            simulationRevision,
		Tenant:              inst.Tenant,
		OrganizationScopeID: inst.OrganizationScopeID,
		Subjects:            inst.Subjects,
		MaterialDigest:      materialDigest,
	}
	if minted.ProposalRevisionID != revision.ProposalRevisionID || minted.IntentID != inst.IntentID || minted.Revision != simulationRevision || minted.Tenant != inst.Tenant || !reflect.DeepEqual(minted.MaterialDigest, materialDigest) {
		return runtime.StartRequest{}, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable, "the operation could not be completed").WithDiagnostic(fmt.Errorf("minted proposal revision does not match simulation artifact or intent binding"))
	}
	revision = minted
	return runtime.StartRequest{
		TenantID:            s.tenantUUID(inst.Tenant),
		CellID:              cellID,
		StartIdempotencyKey: acceptedExecutionIdempotencyKey(inst.IntentID, artifact.GetProposalRevisionId()),
		Resolver:            s.executionResolver,
		Versions:            s.executionVersions,
		Proposal:            runtime.ProposalBinding{Revision: revision},
		ProposalFacts:       proposalFactsOf(s.executionFacts),
		ApprovalFacts:       approvalFactsOf(s.executionFacts),
		ExpectedIntentID:    inst.IntentID,
		ExpectedTenant:      inst.Tenant,
		BusinessSubjectRefs: subjectRefs,
		ExecutionMode:       workflow.ModeExecute,
		CorrelationID:       inst.CorrelationID,
		CreatedAt:           s.clock().Time(),
	}, nil
}

// pinnedStart binds start to the compiled plan instance pinned, so a
// resolver serving more than one version of the workflow continues the
// instance on the exact version it started on (a 1.0.0 promotion keeps
// resuming on 1.0.0 after 1.1.0 is activated). A new start carries no pin.
func pinnedStart(start runtime.StartRequest, instance runtime.Instance) runtime.StartRequest {
	start.PinnedCompiledPlanDigest = instance.CompiledPlanHash
	return start
}

// recordGateEvidence records one OBS-024 GATE_REFUSED/GATE_ADMITTED entry
// through this service's own evidence sink — the same capability evidence
// sink mechanism CAP-002's gateway already writes invocation/refusal
// evidence through (a fresh [MemoryEvidenceSink] private to this service
// when no cell-wide sink was configured; [NewCell] wires the cell's own
// gateway sink instead, so [Cell.Evidence] reads both back from one place).
// No workflow instance exists yet at this call: nodeID is always empty.
func (s *IntentService) recordGateEvidence(ctx context.Context, tenant, kind, intentID, reason string) (string, error) {
	return s.evidence.RecordInvocation(ctx, capability.InvocationEvidence{
		CapabilityID:      "workflow.execution_authority_gate",
		CapabilityVersion: 1,
		SubjectRef:        intentID,
		Tenant:            tenant,
		Decision:          kind,
		ReasonCode:        reason,
		OccurredAt:        s.clock().Time(),
	})
}

// authorizeExecution is the P1B execution authority gate. It returns nil only
// when this cell was composed with an [ExecutionAuthority] that admits def's
// own intent type and principal carries the role that authority names.
func (s *IntentService) authorizeExecution(principal *trust.Principal, def intent.Definition) *envelope.Error {
	if !s.executionAuthority.admitsType(def.Ref.TypeID) {
		return p1aRefusal("ExecuteIntent", "executing an approved promotion proposal")
	}
	if !s.executionAuthority.admitsCaller(principal) {
		return envelope.New(envelope.CodePermissionDenied, reasonExecutionRoleRequired,
			"the caller is not authorized to execute this proposal").
			WithViolation("(caller)",
				"the caller does not carry the role this cell's execution authority requires",
				ruleExecutionAuthorityGate)
	}
	return nil
}

// checkApproval refuses an [intentsv1.ProposalApproval] that does not name
// the exact revision and digest artifact carries, or that asserts no
// recorded approval.
func checkApproval(approval *intentsv1.ProposalApproval, artifact *intentsv1.SimulationArtifact) *envelope.Error {
	if approval.GetProposalRevisionId() == "" || approval.GetProposalRevisionId() != artifact.GetProposalRevisionId() {
		return envelope.New(envelope.CodeFailedPrecondition, reasonStaleProposal,
			"a precondition for the operation is not met").
			WithViolation("approval.proposal_revision_id",
				"the presented proposal revision does not match the revision this cell would simulate for this intent right now",
				ruleExecutionAuthorityGate)
	}
	presentedDigest, presentedErr := digest.FromProto(approval.GetMaterialProposalDigest())
	simulatedDigest, simulatedErr := digest.FromProto(artifact.GetMaterialProposalDigest())
	if presentedErr != nil || simulatedErr != nil || !reflect.DeepEqual(presentedDigest, simulatedDigest) {
		return envelope.New(envelope.CodeFailedPrecondition, reasonStaleProposal,
			"a precondition for the operation is not met").
			WithViolation("approval.material_proposal_digest",
				"the presented material proposal digest does not match the revision this cell would simulate for this intent right now",
				ruleExecutionAuthorityGate)
	}
	if !approval.GetApproved() || approval.GetApprovalRef() == "" {
		return envelope.New(envelope.CodeFailedPrecondition, reasonUnapprovedProposal,
			"a precondition for the operation is not met").
			WithViolation("approval.approved",
				"the presented proposal revision carries no recorded approval", ruleExecutionAuthorityGate)
	}
	return nil
}

// executionUnavailable is returned when the authority gate and the presented
// approval both admit the call, but this cell was not composed with the
// driver-side wiring ([ProposalExecutor] plus its workflow resolver, version
// store and tenant-identity mapper) EXECUTE needs to actually run.
func executionUnavailable() *envelope.Error {
	return envelope.New(
		envelope.CodeFailedPrecondition,
		reasonExecutionUnavailable,
		"workflow execution is not configured for this service",
	)
}

// executionError projects a caller-driven driver refusal onto the owned
// error model. [runtime.Error] is the one typed refusal shape that package
// exposes; anything else is reported as this cell's own fault.
func executionError(err error) *envelope.Error {
	if errors.Is(err, transactioncommit.ErrCommitAmbiguous) {
		return envelope.New(envelope.CodeUnavailable, reasonExecutionOutcomeAmbiguous,
			"the workflow start outcome could not be determined").
			WithRetryable(false).
			WithDiagnostic(err)
	}
	code := runtime.CodeOf(err)
	if code == "" {
		return envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}
	switch code {
	case runtime.CodeInstanceNotFound, runtime.CodeNodeExecutionNotFound:
		return envelope.New(envelope.CodeNotFound, reasonIntentNotFound,
			"the resource does not exist or is not visible").WithDiagnostic(err)
	case runtime.CodeStaleInstance:
		return envelope.New(envelope.CodeFailedPrecondition, reasonStaleRevision,
			"a precondition for the operation is not met").
			WithViolation("expected_instance_version", "the workflow instance version is stale", ruleExecutionAuthorityGate).
			WithDiagnostic(err)
	// WF-RUN-027: the runtime's two derived refusals get their own reason
	// references rather than sharing the generic stale-proposal one. A caller
	// that presented Approved=true and was told "stale proposal" cannot tell
	// "no decision was ever recorded for this revision" from "this revision
	// has been superseded", and those are different things to do about.
	case runtime.CodeUnapprovedProposal:
		return envelope.New(envelope.CodeFailedPrecondition, reasonUnapprovedProposal,
			"a precondition for the operation is not met").
			WithViolation("approval.approved",
				"no approval decision is recorded against this proposal revision", ruleExecutionAuthorityGate).
			WithDiagnostic(err)
	case runtime.CodeSupersededProposal:
		return envelope.New(envelope.CodeFailedPrecondition, reasonSupersededProposal,
			"a precondition for the operation is not met").
			WithViolation("approval.proposal_revision_id",
				"this proposal revision has been superseded and may no longer be executed",
				ruleExecutionAuthorityGate).
			WithDiagnostic(err)
	// No ACTIVE workflow version is a release that has not happened, not a
	// stage and not a transient fault: the published version is a DRAFT until
	// an operator approves and activates it. It gets its own reason so the
	// reader is told what is actually wrong instead of being invited to retry
	// something retrying can never fix.
	case runtime.CodeVersionNotActive:
		return envelope.New(envelope.CodeFailedPrecondition, reasonNoActiveWorkflowVersion,
			"no published workflow version is active for this tenant").
			WithRetryable(false).
			WithViolation("workflow_version",
				"no published version of this workflow has been approved and activated",
				ruleWorkflowVersionRelease).
			WithDiagnostic(err)
	// A version that exists but does not resolve is this cell's configuration
	// rather than a missing release, and stays on the generic refusal.
	case runtime.CodeVersionResolutionFailed, runtime.CodeWorkflowResolutionFailed:
		return executionUnavailable().WithDiagnostic(err)
	case runtime.CodeMutableProposal, runtime.CodeApprovalBindingMismatch:
		return envelope.New(envelope.CodeFailedPrecondition, reasonStaleProposal,
			"a precondition for the operation is not met").
			WithViolation("approval", "the proposal revision is no longer executable", ruleExecutionAuthorityGate).
			WithDiagnostic(err)
	default:
		return envelope.New(envelope.CodeFailedPrecondition, reasonDomainUnavailable,
			"a precondition for the operation is not met").WithDiagnostic(err)
	}
}

// executionReceiptProto projects one [ExecutionResult] onto the wire receipt.
//
// WF-RUN-032: parked_continuation_refs and work_items are rendered as
// separate typed lists from result.ParkedContinuationRefs and
// result.ParkedWorkItems respectively -- a continuation is never named as a
// work item here, and a work item never as a continuation. The deprecated
// parked_continuations string field is still populated, from the same
// ParkedContinuations the port has always carried, for a caller that has not
// migrated off it yet.
func executionReceiptProto(result ExecutionResult) *intentsv1.ExecutionReceipt {
	continuations := make([]*intentsv1.ParkedContinuation, 0, len(result.ParkedContinuationRefs))
	for _, c := range result.ParkedContinuationRefs {
		continuations = append(continuations, &intentsv1.ParkedContinuation{
			ContinuationId: c.ContinuationID, Kind: c.Kind, TargetNodeId: c.TargetNodeID,
		})
	}
	workItems := make([]*intentsv1.ParkedWorkItem, 0, len(result.ParkedWorkItems))
	for _, w := range result.ParkedWorkItems {
		workItems = append(workItems, &intentsv1.ParkedWorkItem{
			WorkItemId: w.WorkItemID, Kind: w.Kind, NodeId: w.NodeID,
		})
	}
	receipt := &intentsv1.ExecutionReceipt{
		InstanceId:   result.InstanceID,
		VisitedNodes: append([]string(nil), result.VisitedNodes...),
		//lint:ignore SA1019 wire compatibility: parked_continuations stays populated from the port field until the wire format migrates. owner=workflow-platform expires=2027-03-24
		ParkedContinuations:    append([]string(nil), result.ParkedContinuations...),
		InstanceVersion:        uint64(result.InstanceVersion),
		ReceiptDigest:          receiptDigestFor(result),
		ParkedContinuationRefs: continuations,
		WorkItems:              workItems,
	}
	switch result.effectiveStatus() {
	case ExecutionResultParked:
		receipt.Status = intentsv1.ExecutionReceiptStatus_EXECUTION_RECEIPT_STATUS_PARKED
	case ExecutionResultComplete:
		receipt.Status = intentsv1.ExecutionReceiptStatus_EXECUTION_RECEIPT_STATUS_COMPLETE
	case ExecutionResultResolved:
		receipt.Status = intentsv1.ExecutionReceiptStatus_EXECUTION_RECEIPT_STATUS_RESOLVED
		if state := result.ResolvedStart; state != nil {
			receipt.ResolvedStart = &intentsv1.ResolvedStartState{
				RuntimeStatus: string(state.RuntimeStatus), CurrentNodeIds: append([]string(nil), state.CurrentNodeIDs...),
				WorkflowId: state.WorkflowID, WorkflowVersion: state.WorkflowVersion,
				CompiledPlanDigest: state.CompiledPlanDigest, SemanticVersion: state.SemanticVersion,
				Lifecycle: runtimeDimensionsProto(state.Lifecycle),
			}
		}
	}
	return receipt
}

func runtimeDimensionsProto(d runtime.Dimensions) *intentsv1.LifecycleDimensions {
	if d.Empty() {
		return nil
	}
	return &intentsv1.LifecycleDimensions{
		Request:     intentsv1.RequestState(intentsv1.RequestState_value["REQUEST_STATE_"+d.RequestState]),
		Execution:   intentsv1.ExecutionState(intentsv1.ExecutionState_value["EXECUTION_STATE_"+d.ExecutionState]),
		Business:    intentsv1.BusinessState(intentsv1.BusinessState_value["BUSINESS_STATE_"+d.BusinessState]),
		Consistency: intentsv1.ConsistencyState(intentsv1.ConsistencyState_value["CONSISTENCY_STATE_"+d.ConsistencyState]),
		Obligation:  intentsv1.ObligationState(intentsv1.ObligationState_value["OBLIGATION_STATE_"+d.ObligationState]),
	}
}

// receiptDigestFor mints a stable, human-inspectable tag binding an
// execution receipt's identity together. It is not a cryptographic digest
// over canonicalized content the way [intent.ProposalRevision.MaterialDigest]
// is; nothing here is approval-bound material, only a receipt of what
// already ran.
func receiptDigestFor(result ExecutionResult) string {
	status := string(result.effectiveStatus())
	if status == string(ExecutionResultResolved) {
		return fmt.Sprintf("execution:%s:RESOLVED:%d", result.InstanceID, result.InstanceVersion)
	}
	return "execution:" + result.InstanceID + ":" + status
}
