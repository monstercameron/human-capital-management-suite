package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// EP-INTENT-003: SubmitIntent, CancelIntent and SupersedeIntent.
//
// # The cancellation-outcome mapping
//
// CancelIntentResponse carries only an IntentInstance -- there is no
// CancellationOutcome enum on the wire, and none is added here. The four
// GREEN outcomes are told entirely through the returned instance's five
// lifecycle dimensions, exactly as [intent.CancelInstance] (the kernel
// function this method composes rather than reimplements) documents:
//
//	CANCELLED             RequestState->CANCELLED; ExecutionState moves to
//	                      NOT_PLANNED (never started) or BLOCKED (ran to a
//	                      clean safe point). Truthful because nothing further
//	                      will execute, and the record says so.
//	CANCELLATION_PENDING  No dimension moves. Truthful because nothing has
//	                      actually happened yet: the runtime has not reached
//	                      a safe point where this could be decided, so
//	                      claiming CANCELLED would be exactly this todo's RED
//	                      clause.
//	TOO_LATE              No dimension moves; ExecutionState is already
//	                      COMMITTED. RequestState is never set to CANCELLED
//	                      here -- the business effect already happened, and
//	                      saying otherwise falsifies the record.
//	REPAIR_REQUIRED       ExecutionState->REPAIR_REQUIRED; RequestState is
//	                      left exactly as it was, because the request was
//	                      neither cleanly cancelled nor completed.
//
// Reading the instance alone is enough to tell the four apart: ExecutionState
// is the discriminant between CANCELLATION_PENDING and TOO_LATE (EXECUTING
// vs. COMMITTED) and between CANCELLED and REPAIR_REQUIRED when execution was
// in flight (BLOCKED vs. REPAIR_REQUIRED); RequestState is the discriminant
// between CANCELLED and the other three (CANCELLED vs. anything else). The
// one appended [intentsv1.CancellationDecision] on the response additionally
// names the disposition by its own stable id (EffectDispositionRef), as
// supplementary evidence for the one caller who receives this exact
// response -- the kernel contract itself never depends on that field, and a
// caller must be able to prove the four outcomes apart from the five
// dimensions alone.
//
// Every dimensional move above is applied by [intent.CancelInstance],
// [intent.Submit] or [intent.SupersedeOriginal] through [lifecycle.Machine]
// against the intent's own definition profile; this file never assigns a
// lifecycle tuple directly.

const (
	reasonNotSimulated              = "intent.not_simulated"
	reasonAlreadyTerminal           = "intent.already_terminal"
	reasonRepairRefRequired         = "intent.repair_reference_required"
	reasonLifecycleWriteUnavailable = "intent.lifecycle_write_unavailable"
	reasonGovernanceRequired        = "intent.governance_decision_required"
	reasonIllegalTransition         = "intent.illegal_transition"
	reasonSupersedeSelf             = "intent.supersede_self"
	reasonSafePointUnavailable      = "intent.safe_point_unavailable"

	ruleLifecycleTransition = "kernel.lifecycle_transition"

	capabilitySubmit    = "intent.submit"
	capabilityCancel    = "intent.cancel"
	capabilitySupersede = "intent.supersede"
)

// lifecycleWriteUnavailable is the one typed answer for a cell composed with
// no idempotency coordinator or no [LifecycleMutator]-capable store: the
// method is published and authenticates, but this composition cannot durably
// perform the write.
func lifecycleWriteUnavailable() *envelope.Error {
	return envelope.New(envelope.CodeUnavailable, reasonLifecycleWriteUnavailable,
		"this cell is not composed to perform this governed write")
}

// requireField refuses a structurally empty request field. A zero value
// (empty string, zero version) never means "unset, so permissive" for a
// governed write: it fails closed.
func requireField(field string) *envelope.Error {
	return envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
		"the request is malformed or structurally invalid").
		WithViolation(field, "the field is required and may not be the zero value", rulePhaseCeiling)
}

// staleRevisionRefusal is the one typed answer for a presented
// expected_instance_version that does not name the intent's current version.
func staleRevisionRefusal() *envelope.Error {
	return envelope.New(envelope.CodeFailedPrecondition, reasonStaleRevision,
		"a precondition for the operation is not met").
		WithViolation("expected_instance_version", "the expected instance version is stale", "intent.expected_revision")
}

// lifecycleTransitionError projects a refusal from [intent.Submit],
// [intent.CancelInstance] or [intent.SupersedeOriginal] -- and the
// [lifecycle] package errors they can return -- onto the owned error model.
// Every sentinel this package's three new kernel functions can return is
// named here; nothing falls through to a generic default that would collapse
// a specific refusal into an unspecific one.
func lifecycleTransitionError(err error) *envelope.Error {
	switch {
	case errors.Is(err, intent.ErrSubmitNotSimulated):
		return envelope.New(envelope.CodeFailedPrecondition, reasonNotSimulated,
			"a precondition for the operation is not met").
			WithViolation("intent_id", "the intent has not been simulated and carries no proposal to submit", ruleLifecycleTransition).
			WithDiagnostic(err)
	case errors.Is(err, intent.ErrSubmitAlreadyTerminal),
		errors.Is(err, intent.ErrCancelAlreadyTerminal),
		errors.Is(err, intent.ErrSupersedeAlreadyTerminal):
		return envelope.New(envelope.CodeFailedPrecondition, reasonAlreadyTerminal,
			"a precondition for the operation is not met").
			WithViolation("intent_id", "the intent is already in a terminal request state", ruleLifecycleTransition).
			WithDiagnostic(err)
	case errors.Is(err, intent.ErrCancelRepairRefRequired):
		return envelope.New(envelope.CodeUnavailable, reasonRepairRefRequired,
			"the operation could not be completed").
			WithDiagnostic(err)
	case errors.Is(err, intent.ErrCancelUnrecognizedPoint):
		return envelope.New(envelope.CodeUnavailable, reasonSafePointUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	case errors.Is(err, intent.ErrSupersedeSelf):
		return envelope.New(envelope.CodeInvalidArgument, reasonSupersedeSelf,
			"the request is malformed or structurally invalid").
			WithViolation("definition", "an intent may not supersede itself", ruleLifecycleTransition).
			WithDiagnostic(err)
	case errors.Is(err, lifecycle.ErrGovernanceBypassed):
		return envelope.New(envelope.CodeInvalidArgument, reasonGovernanceRequired,
			"the request is malformed or structurally invalid").
			WithViolation("reason_ref", "this transition requires a governed reason reference", ruleLifecycleTransition).
			WithDiagnostic(err)
	case errors.Is(err, lifecycle.ErrUndeclaredTransition), errors.Is(err, lifecycle.ErrIllegalTuple),
		errors.Is(err, lifecycle.ErrDimensionLoss):
		return envelope.New(envelope.CodeFailedPrecondition, reasonIllegalTransition,
			"a precondition for the operation is not met").
			WithViolation("intent_id", "the intent is not in a state that accepts this action", ruleLifecycleTransition).
			WithDiagnostic(err)
	case errors.Is(err, intent.ErrInvalidInstance):
		return envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
			"the request is malformed or structurally invalid").WithDiagnostic(err)
	default:
		return envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(err)
	}
}

// idempotencyError projects an [endpoint.Coordinator] refusal onto the owned
// error model, exactly as internal/transport/humanwork's mutationError does
// for its own idempotent writes.
func idempotencyError(err error) *envelope.Error {
	var revConflict *endpoint.RevisionConflict
	if errors.As(err, &revConflict) {
		return staleRevisionRefusal()
	}
	var payloadConflict *endpoint.PayloadConflict
	if errors.As(err, &payloadConflict) {
		return envelope.New(envelope.CodeAborted, "intent.idempotency_key_reused",
			"the idempotency key was already used for a different request")
	}
	if errors.Is(err, endpoint.ErrIdempotencyKeyRequired) {
		return requireField("idempotency_key")
	}
	if errors.Is(err, endpoint.ErrIdempotencyKeyMismatch) || errors.Is(err, endpoint.ErrScopeRequired) ||
		errors.Is(err, endpoint.ErrRevisionRequired) {
		return envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
			"the request is malformed or structurally invalid").WithDiagnostic(err)
	}
	if errors.Is(err, ErrLifecycleWritesUnavailable) {
		return lifecycleWriteUnavailable()
	}
	return lifecycleTransitionError(err)
}

// mintInstance runs the same family-dispatched constructor CreateIntent uses:
// a mutation-capable ChangeRequest is drafted (preflighted and simulated
// later, never executable at creation); every other family is created ready.
func mintInstance(spec intent.InstanceSpec, def intent.Definition, digester intent.Digester, ids intent.IDSource, clock intent.Clock) (intent.Instance, error) {
	if def.Family == intent.FamilyChangeRequest {
		inst, _, err := intent.Draft(spec, def, digester, ids, clock)
		return inst, err
	}
	inst, _, err := intent.NewInstance(spec, def, digester, ids, clock)
	return inst, err
}

// ---------------------------------------------------------------------------
// SubmitIntent
// ---------------------------------------------------------------------------

// SubmitIntent binds the exact proposal revision this cell would itself
// (re)simulate for the intent right now and starts it once.
//
// It re-runs the same SimulateIntent path ExecuteIntent already relies on to
// derive the one deterministic proposal revision a P1A intent can ever have,
// so a caller cannot bind a revision this cell never actually produced. A
// stale expected_instance_version and a presented proposal_revision_id that
// does not match are both refused before the idempotency coordinator ever
// runs the transition, and every actual dimensional move is
// [intent.Submit]'s, never this method's own assignment.
func (s *IntentService) SubmitIntent(ctx context.Context, req *intentsv1.SubmitIntentRequest) (*intentsv1.SubmitIntentResponse, error) {
	principal, inv, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	tenant := principal.Tenant().String()
	intentID := req.GetIntentId()

	switch {
	case intentID == "":
		return nil, requireField("intent_id")
	case req.GetIdempotencyKey() == "":
		return nil, requireField("idempotency_key")
	case req.GetProposalRevisionId() == "":
		return nil, requireField("proposal_revision_id")
	case req.GetExpectedInstanceVersion() == 0:
		return nil, requireField("expected_instance_version")
	}
	if s.idempotency == nil {
		return nil, lifecycleWriteUnavailable()
	}

	inst, rec, ownedErr := s.loadInstance(ctx, tenant, intentID)
	if ownedErr != nil {
		return nil, ownedErr
	}
	def, err := s.defs.Resolve(inst.Definition)
	if err != nil {
		return nil, envelope.New(envelope.CodeNotFound, reasonDefinitionUnknown,
			"the resource does not exist or is not visible").WithDiagnostic(err)
	}
	// RBAC-RT-003: submitting requires the capability for this intent type
	// and the subject relationship a read would require, before the
	// re-simulation or any write.
	if authErr := s.denyStoredIntentAction(ctx, principal, purposeOf(principal, inv), def, inst); authErr != nil {
		return nil, authErr
	}

	// Re-derive the one deterministic proposal revision this intent can ever
	// have, exactly like ExecuteIntent's own authority gate.
	simulated, ownedErr := s.simulateDetailed(ctx, principal, purposeOf(principal, inv), inst, def)
	if ownedErr != nil {
		return nil, ownedErr
	}
	if simulated.Artifact.GetProposalRevisionId() == "" {
		return nil, envelope.New(envelope.CodeFailedPrecondition, reasonNoExecutablePlan,
			"a precondition for the operation is not met").
			WithViolation("intent_id", "the intent has no simulated proposal to submit", ruleLifecycleTransition)
	}
	if req.GetProposalRevisionId() != simulated.Artifact.GetProposalRevisionId() {
		return nil, envelope.New(envelope.CodeFailedPrecondition, reasonStaleProposal,
			"a precondition for the operation is not met").
			WithViolation("proposal_revision_id",
				"the presented proposal revision does not match the revision this cell would simulate for this intent right now",
				ruleLifecycleTransition)
	}

	expected := req.GetExpectedInstanceVersion()
	idemReq := endpoint.Request{
		Scope:            endpoint.Scope{Principal: principal.Subject(), Tenant: tenant, Capability: capabilitySubmit},
		MessageKey:       req.GetIdempotencyKey(),
		Payload:          []byte(fmt.Sprintf("submit|%s|%d|%s", intentID, expected, req.GetProposalRevisionId())),
		ExpectedRevision: &expected,
		CurrentRevision:  rec.InstanceVersion,
	}
	if _, doErr := s.idempotency.Do(ctx, idemReq, func(ctx context.Context) (endpoint.Outcome, error) {
		working := inst
		if err := intent.Submit(&working, def, intent.SubmitRequest{
			ProposalRevisionID: req.GetProposalRevisionId(),
			RecordedAt:         s.clock().Time(),
		}); err != nil {
			return endpoint.Outcome{}, err
		}
		if working.InstanceVersion == inst.InstanceVersion {
			// Idempotent no-op inside Submit itself (already SUBMITTED under
			// the one valid revision): nothing to persist.
			return endpoint.Outcome{Status: "OK", ResultDigest: intentID}, nil
		}
		mutator, ok := s.store.(LifecycleMutator)
		if !ok {
			return endpoint.Outcome{}, ErrLifecycleWritesUnavailable
		}
		if _, mutateErr := mutator.MutateLifecycle(ctx, LifecycleMutation{
			Tenant: tenant, IntentID: intentID, ExpectedInstanceVersion: rec.InstanceVersion,
			Lifecycle: working.Lifecycle, CommitReceiptRef: inst.CommitReceiptRef, RepairRef: inst.RepairRef,
			RecordedAt: working.RecordedAt.Time(),
		}); mutateErr != nil {
			return endpoint.Outcome{}, mutateErr
		}
		return endpoint.Outcome{Status: "OK", ResultDigest: intentID}, nil
	}); doErr != nil {
		return nil, idempotencyError(doErr)
	}

	final, _, ownedErr := s.loadInstance(ctx, tenant, intentID)
	if ownedErr != nil {
		return nil, ownedErr
	}
	msg, convErr := protomap.InstanceToProto(final)
	if convErr != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(convErr)
	}
	return &intentsv1.SubmitIntentResponse{Intent: msg}, nil
}

// ---------------------------------------------------------------------------
// CancelIntent
// ---------------------------------------------------------------------------

// CancelIntent requests governed cancellation and reports the truthful
// disposition through the returned instance's dimensional state (see this
// file's own doc comment for the exact mapping).
func (s *IntentService) CancelIntent(ctx context.Context, req *intentsv1.CancelIntentRequest) (*intentsv1.CancelIntentResponse, error) {
	principal, inv, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	tenant := principal.Tenant().String()
	intentID := req.GetIntentId()
	reasonRef := strings.TrimSpace(req.GetReasonRef())

	switch {
	case intentID == "":
		return nil, requireField("intent_id")
	case req.GetIdempotencyKey() == "":
		return nil, requireField("idempotency_key")
	case reasonRef == "":
		return nil, requireField("reason_ref")
	case req.GetExpectedInstanceVersion() == 0:
		return nil, requireField("expected_instance_version")
	}
	if s.idempotency == nil {
		return nil, lifecycleWriteUnavailable()
	}

	inst, rec, ownedErr := s.loadInstance(ctx, tenant, intentID)
	if ownedErr != nil {
		return nil, ownedErr
	}
	def, err := s.defs.Resolve(inst.Definition)
	if err != nil {
		return nil, envelope.New(envelope.CodeNotFound, reasonDefinitionUnknown,
			"the resource does not exist or is not visible").WithDiagnostic(err)
	}
	// RBAC-RT-003: cancelling requires the capability for this intent type
	// and the subject relationship a read would require, before the
	// safe-point read or any write.
	if authErr := s.denyStoredIntentAction(ctx, principal, purposeOf(principal, inv), def, inst); authErr != nil {
		return nil, authErr
	}

	// The safe-point question is asked at most once, before the idempotency
	// coordinator, and only when it actually applies: an intent that is not
	// EXECUTING has no safe-point fact to read.
	point := intent.CancellationPointUnknown
	repairRef := ""
	if inst.Lifecycle.Execution == lifecycle.ExecutionExecuting && s.safePoints != nil {
		p, rr, spErr := s.safePoints.At(ctx, tenant, intentID)
		if spErr != nil {
			return nil, envelope.New(envelope.CodeUnavailable, reasonSafePointUnavailable,
				"the operation could not be completed").WithDiagnostic(spErr)
		}
		point, repairRef = p, rr
	}

	expected := req.GetExpectedInstanceVersion()
	idemReq := endpoint.Request{
		Scope:            endpoint.Scope{Principal: principal.Subject(), Tenant: tenant, Capability: capabilityCancel},
		MessageKey:       req.GetIdempotencyKey(),
		Payload:          []byte(fmt.Sprintf("cancel|%s|%d|%s", intentID, expected, reasonRef)),
		ExpectedRevision: &expected,
		CurrentRevision:  rec.InstanceVersion,
	}
	outcome, doErr := s.idempotency.Do(ctx, idemReq, func(ctx context.Context) (endpoint.Outcome, error) {
		working := inst
		point, repairRef := point, repairRef
		if s.workflowCancel != nil {
			// WF-RUN-010: when the kernel would cancel this intent, a bound
			// workflow instance is cancelled through the governed decision
			// first and the disposition follows the recorded verdict (see
			// workflow_cancellation.go). The probe runs on a copy, so a
			// refused or terminal intent never reaches the workflow.
			probe := inst
			probed, probeErr := intent.CancelInstance(&probe, def, intent.CancelRequest{
				ReasonRef: reasonRef, Point: intent.CancellationPointClean, RecordedAt: s.clock().Time(),
			})
			if probeErr == nil && probed == intent.DispositionCancelled {
				verdict, wfErr := s.workflowCancel.CancelBound(ctx, BoundCancellation{
					Tenant: principal.Tenant(), IntentID: intentID, CorrelationID: inst.CorrelationID,
					Reason: reasonRef, RequestedBy: principal.Subject(), RecordedAt: s.clock().Time(),
				})
				if wfErr != nil {
					return endpoint.Outcome{}, wfErr
				}
				if verdict.Bound {
					if verdict.HasSettlement && verdict.Decision != workflow.CannotCancel &&
						verdict.TerminalStatus != runtime.InstanceCompleted {
						if err := applyCancellationSettlement(&working, def, verdict, s.clock().Time()); err != nil {
							return endpoint.Outcome{}, err
						}
					}
					var decided intent.CancellationDisposition
					point, repairRef, decided = verdict.governedDisposition(inst.Lifecycle.Execution == lifecycle.ExecutionExecuting)
					if decided != "" {
						if working.Lifecycle != inst.Lifecycle {
							mutator, ok := s.store.(LifecycleMutator)
							if !ok {
								return endpoint.Outcome{}, ErrLifecycleWritesUnavailable
							}
							if _, mutateErr := mutator.MutateLifecycle(ctx, LifecycleMutation{
								Tenant: tenant, IntentID: intentID, ExpectedInstanceVersion: rec.InstanceVersion,
								Lifecycle: working.Lifecycle, RecordedAt: s.clock().Time(),
							}); mutateErr != nil {
								return endpoint.Outcome{}, mutateErr
							}
						}
						s.recordCancellationEvidence(ctx, tenant, intentID, string(decided), reasonRef)
						return endpoint.Outcome{Status: string(decided), ResultDigest: intentID}, nil
					}
				}
			}
		}
		d, cancelErr := intent.CancelInstance(&working, def, intent.CancelRequest{
			ReasonRef: reasonRef, Point: point, RepairRef: repairRef, RecordedAt: s.clock().Time(),
		})
		if cancelErr != nil {
			return endpoint.Outcome{}, cancelErr
		}
		switch d {
		case intent.DispositionCancelled, intent.DispositionRepairRequired:
			mutator, ok := s.store.(LifecycleMutator)
			if !ok {
				return endpoint.Outcome{}, ErrLifecycleWritesUnavailable
			}
			if _, mutateErr := mutator.MutateLifecycle(ctx, LifecycleMutation{
				Tenant: tenant, IntentID: intentID, ExpectedInstanceVersion: rec.InstanceVersion,
				Lifecycle: working.Lifecycle, CommitReceiptRef: working.CommitReceiptRef, RepairRef: working.RepairRef,
				RecordedAt: working.RecordedAt.Time(),
			}); mutateErr != nil {
				return endpoint.Outcome{}, mutateErr
			}
		case intent.DispositionCancellationPending, intent.DispositionTooLate:
			// Nothing durable to compare-and-swap: the record is that a
			// cancellation was requested and evaluated, kept as evidence
			// rather than a store mutation neither dimension needs.
			s.recordCancellationEvidence(ctx, tenant, intentID, string(d), reasonRef)
		}
		return endpoint.Outcome{Status: string(d), ResultDigest: intentID}, nil
	})
	if doErr != nil {
		return nil, idempotencyError(doErr)
	}
	disposition := intent.CancellationDisposition(outcome.Status)
	if disposition == intent.DispositionCancelled && s.releaseAdmission != nil {
		// A cleanly cancelled intent no longer holds its admission window
		// (PROMOUX-002). Best-effort like the journey's own release: the
		// cancellation already landed, and a missed release costs
		// availability of the window, never correctness; a replay retries it.
		_ = s.releaseAdmission(ctx, principal.Tenant(), intentID, s.clock().Time())
	}

	final, _, ownedErr := s.loadInstance(ctx, tenant, intentID)
	if ownedErr != nil {
		return nil, ownedErr
	}
	msg, convErr := protomap.InstanceToProto(final)
	if convErr != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(convErr)
	}
	msg.CancellationDecisions = append(msg.CancellationDecisions,
		cancellationDecisionProto(final.IntentID, principal, reasonRef, disposition, s.clock().Time()))
	return &intentsv1.CancelIntentResponse{Intent: msg}, nil
}

// applyCancellationSettlement moves only the existing business and consistency
// dimensions through the definition's lifecycle machine. The target values
// come from the persisted cancellation settlement, never from the request or
// the cancellation disposition alone.
func applyCancellationSettlement(inst *intent.Instance, def intent.Definition, verdict BoundCancellationVerdict, at time.Time) error {
	if !verdict.HasSettlement || inst == nil {
		return nil
	}
	ctx := inst.LifecycleContext(def)
	machine, err := lifecycle.NewMachine(def.LifecycleProfiles(), inst.Lifecycle, ctx)
	if err != nil {
		return fmt.Errorf("app: seed lifecycle settlement: %w", err)
	}
	decisionRef := "workflow-cancellation-settlement:" + verdict.SettlementDecision.String()
	apply := func(next lifecycle.Dimensions) error {
		if err := machine.Apply(next, ctx, lifecycle.TransitionRecord{
			At: values.NewInstant(at), ReasonRef: decisionRef, GovernanceDecisionRef: decisionRef,
		}); err != nil {
			return err
		}
		return nil
	}

	next := machine.Current()
	if next.Business != verdict.BusinessState {
		next.Business = verdict.BusinessState
		if err := apply(next); err != nil {
			return fmt.Errorf("app: apply cancellation business settlement: %w", err)
		}
	}
	if err := applyConsistencySettlement(machine, verdict.ConsistencyState, apply); err != nil {
		return fmt.Errorf("app: apply cancellation consistency settlement: %w", err)
	}
	if machine.Current() != inst.Lifecycle {
		inst.Lifecycle = machine.Current()
		inst.RecordedAt = values.NewInstant(at)
		inst.LastTransitionAt = values.NewInstant(at)
	}
	return nil
}

func applyConsistencySettlement(machine *lifecycle.Machine, target lifecycle.ConsistencyState, apply func(lifecycle.Dimensions) error) error {
	current := machine.Current().Consistency
	if current == target {
		return nil
	}
	move := func(state lifecycle.ConsistencyState) error {
		next := machine.Current()
		next.Consistency = state
		return apply(next)
	}
	switch current {
	case lifecycle.ConsistencyUnspecified, lifecycle.ConsistencyNotApplicable, lifecycle.ConsistencyUnknown:
		if err := move(lifecycle.ConsistencyPendingObservation); err != nil {
			return err
		}
		current = lifecycle.ConsistencyPendingObservation
	}
	if current == target {
		return nil
	}
	if current == lifecycle.ConsistencyDegraded && target == lifecycle.ConsistencyConsistent {
		if err := move(lifecycle.ConsistencyRepairing); err != nil {
			return err
		}
	}
	return move(target)
}

// recordCancellationEvidence durably records that a cancellation was
// requested and evaluated, through the same evidence sink OBS-024's
// GATE_REFUSED/GATE_ADMITTED entries already use, for the two dispositions
// ([intent.DispositionCancellationPending], [intent.DispositionTooLate]) that
// make no durable store mutation of their own.
func (s *IntentService) recordCancellationEvidence(ctx context.Context, tenant, intentID, disposition, reasonRef string) {
	_, _ = s.evidence.RecordInvocation(ctx, capability.InvocationEvidence{
		CapabilityID:      "intent.cancellation_disposition",
		CapabilityVersion: 1,
		SubjectRef:        intentID,
		Tenant:            tenant,
		Decision:          disposition,
		ReasonCode:        reasonRef,
		OccurredAt:        s.clock().Time(),
	})
}

// cancellationDecisionProto renders the one [intentsv1.CancellationDecision]
// CancelIntent appends to its own response. The identifier is derived, not
// freshly minted: recording the same disposition for the same intent twice
// (an exact idempotent replay) names the same evidence, not two.
func cancellationDecisionProto(intentID string, principal *trust.Principal, reasonRef string, disposition intent.CancellationDisposition, at time.Time) *intentsv1.CancellationDecision {
	id := uuid.NewSHA1(artifactNamespace, []byte(intentID+"|cancellation|"+string(disposition))).String()
	return &intentsv1.CancellationDecision{
		CancellationDecisionId: id,
		IntentId:               intentID,
		RequestedBy: &intentsv1.PrincipalReference{
			PrincipalId: principal.Subject(),
			Kind:        intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN,
		},
		ReasonRef:            reasonRef,
		PolicyDecisionRef:    reasonRef,
		EffectDispositionRef: string(disposition),
		DecidedAt:            protomap.InstantToProto(values.NewInstant(at)),
	}
}

// ---------------------------------------------------------------------------
// SupersedeIntent
// ---------------------------------------------------------------------------

// specForSupersede projects the successor's typed request plus the caller's
// own trusted context onto the kernel's creation spec, exactly as
// [IntentService.specFor] does for CreateIntent -- tenant, organization
// scope, purpose and origin all come from the authenticated principal, never
// from the original intent or from anything the caller asserts. The
// successor inherits only subjects and execution mode from original: enough
// to keep referring to the same business subjects under the same mode
// contract, and nothing that could smuggle authority the caller's own
// admission did not already grant. CausationID points at original so the two
// are one chain and two intents, never one counted twice.
func (s *IntentService) specForSupersede(
	req *intentsv1.SupersedeIntentRequest,
	def intent.Definition,
	original intent.Instance,
	principal *trust.Principal,
	inv *transport.Invocation,
) (intent.InstanceSpec, *envelope.Error) {
	payload, err := protomap.PayloadFromProto(req.GetRequest())
	if err != nil {
		return intent.InstanceSpec{}, envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
			"the request is malformed or structurally invalid").
			WithViolation("request", "the typed payload is not a valid schema-bearing payload", rulePhaseCeiling).
			WithDiagnostic(err)
	}
	origin, err := deriveOrigin(principal, inv)
	if err != nil {
		return intent.InstanceSpec{}, envelope.New(envelope.CodeInvalidArgument, reasonRequestRejected,
			"the request is malformed or structurally invalid").
			WithViolation("(origin)", "the trusted origin of the request could not be established", rulePhaseCeiling).
			WithDiagnostic(err)
	}
	correlation := principal.EvidenceID()
	trace := principal.EvidenceID()
	if inv != nil {
		correlation = inv.RequestID()
		trace = inv.TrustedFingerprint()
	}
	causation := original.IntentID
	spec := intent.InstanceSpec{
		Tenant:              principal.Tenant(),
		OrganizationScopeID: principal.OrganizationScopeID(),
		Initiator: intent.PrincipalReference{
			PrincipalID:          principal.Subject(),
			Kind:                 initiatorKindOf(principal.SubjectKind()),
			IdentityAssuranceRef: principal.EvidenceID(),
		},
		Purpose:                       purposeOf(principal, inv),
		Subjects:                      append([]intent.SubjectReference(nil), original.Subjects...),
		Request:                       payload,
		IdempotencyKey:                req.GetIdempotencyKey(),
		CorrelationID:                 correlation,
		TraceID:                       trace,
		CausationID:                   &causation,
		Classification:                def.DataClassificationFloor,
		RetentionClass:                def.RetentionClass,
		ControlSnapshots:              s.controls.Snapshots,
		ExecutionMode:                 original.ExecutionMode,
		SourceAuthoritySnapshotDigest: s.controls.SourceAuthorityDigest,
		RiskContextDigest:             s.controls.RiskContextDigest,
		Origin:                        origin,
	}
	if at, effErr := requestedEffectiveAt(payload); effErr == nil && at != nil {
		spec.RequestedEffectiveAt = at
	}
	return spec, nil
}

// deterministicSuccessorID derives the successor's identity from the exact
// tuple that names one logical supersede request, the same reasoning
// [derivedIDs] documents for a simulation's artifacts: superseding the same
// original under the same idempotency key twice must mint one successor, not
// two, and a freshly-minted random id would defeat that the moment a replay
// skipped the closure that would have minted it.
func deterministicSuccessorID(originalID, idempotencyKey string) string {
	return uuid.NewSHA1(artifactNamespace, []byte("supersede|"+originalID+"|"+idempotencyKey)).String()
}

// SupersedeIntent creates an authorized successor and links it to the
// original, without mutating anything about the original except its own
// request state moving to SUPERSEDED and the new lineage link.
//
// The successor is minted through exactly the same admission path
// CreateIntent uses ([specForSupersede] plus [mintInstance]): the caller's
// own trusted tenant, organization scope and authorization decide what the
// successor may be, never the original's. The successor never receives
// authority the original did not already carry, because it receives no
// authority from the original at all -- only its subjects and execution mode
// are inherited, and everything else is admitted fresh.
func (s *IntentService) SupersedeIntent(ctx context.Context, req *intentsv1.SupersedeIntentRequest) (*intentsv1.SupersedeIntentResponse, error) {
	principal, inv, ownedErr := caller(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	tenant := principal.Tenant().String()
	originalID := req.GetSupersededIntentId()
	reasonRef := strings.TrimSpace(req.GetReasonRef())

	switch {
	case originalID == "":
		return nil, requireField("superseded_intent_id")
	case req.GetIdempotencyKey() == "":
		return nil, requireField("idempotency_key")
	case reasonRef == "":
		return nil, requireField("reason_ref")
	case req.GetExpectedInstanceVersion() == 0:
		return nil, requireField("expected_instance_version")
	}
	if s.idempotency == nil {
		return nil, lifecycleWriteUnavailable()
	}

	original, rec, ownedErr := s.loadInstance(ctx, tenant, originalID)
	if ownedErr != nil {
		return nil, ownedErr
	}
	originalDef, err := s.defs.Resolve(original.Definition)
	if err != nil {
		return nil, envelope.New(envelope.CodeNotFound, reasonDefinitionUnknown,
			"the resource does not exist or is not visible").WithDiagnostic(err)
	}
	successorDef, ownedErr := s.resolveDefinition(req.GetDefinition(), true)
	if ownedErr != nil {
		return nil, ownedErr
	}
	// RBAC-RT-003: superseding requires the capability for the original's
	// intent type and the subject relationship a read would require, before
	// the successor is minted or any write lands.
	if authErr := s.denyStoredIntentAction(ctx, principal, purposeOf(principal, inv), originalDef, original); authErr != nil {
		return nil, authErr
	}
	spec, ownedErr := s.specForSupersede(req, successorDef, original, principal, inv)
	if ownedErr != nil {
		return nil, ownedErr
	}
	successorID := deterministicSuccessorID(originalID, req.GetIdempotencyKey())

	expected := req.GetExpectedInstanceVersion()
	idemReq := endpoint.Request{
		Scope:      endpoint.Scope{Principal: principal.Subject(), Tenant: tenant, Capability: capabilitySupersede},
		MessageKey: req.GetIdempotencyKey(),
		Payload: []byte(fmt.Sprintf("supersede|%s|%d|%s|%s",
			originalID, expected, successorDef.Ref.String(), reasonRef)),
		ExpectedRevision: &expected,
		CurrentRevision:  rec.InstanceVersion,
	}
	// PROMOUX-013: the original's own transition is validated and durably
	// applied FIRST, and the successor is minted and appended only once that
	// has actually landed. This order is deliberate and load-bearing.
	//
	// AppendIntent and MutateLifecycle are each their own PostgreSQL
	// transaction (pgstore has no call that spans both), so this closure can
	// never make both writes atomic against each other; it can only choose
	// which one is allowed to be the durable trace of a request that fails
	// partway through. Minting the successor first (the original order) let
	// a caller who lost an optimistic-concurrency race, or whose original
	// was in a lifecycle tuple SupersedeOriginal legally refuses (Rule 1:
	// RequestSuperseded cannot coexist with ExecutionState=EXECUTING;
	// see internal/intent/lifecycle/rules.go), leave a durable orphan
	// successor intent behind that references an original which was never
	// actually superseded -- a partial domain write this todo's GREEN
	// clause refuses. Validating and compare-and-swapping the original
	// first means a losing or illegal request writes nothing at all: the
	// idempotency coordinator's closure returns before any store mutation.
	// The residual failure window (the original durably moves to SUPERSEDED
	// but the successor's own AppendIntent then fails) is an infrastructure
	// fault, not a race outcome, and it leaves the strictly safer trace: an
	// original correctly marked SUPERSEDED with no successor yet, rather
	// than a successor implying a supersession that never happened.
	if _, doErr := s.idempotency.Do(ctx, idemReq, func(ctx context.Context) (endpoint.Outcome, error) {
		working := original
		if _, supErr := intent.SupersedeOriginal(&working, originalDef, successorID, reasonRef, s.clock); supErr != nil {
			return endpoint.Outcome{}, supErr
		}
		mutator, ok := s.store.(LifecycleMutator)
		if !ok {
			return endpoint.Outcome{}, ErrLifecycleWritesUnavailable
		}
		if _, mutateErr := mutator.MutateLifecycle(ctx, LifecycleMutation{
			Tenant: tenant, IntentID: originalID, ExpectedInstanceVersion: rec.InstanceVersion,
			Lifecycle: working.Lifecycle, CommitReceiptRef: working.CommitReceiptRef, RepairRef: working.RepairRef,
			RecordedAt: working.RecordedAt.Time(),
		}); mutateErr != nil {
			return endpoint.Outcome{}, mutateErr
		}

		oneShotID := func() (string, error) { return successorID, nil }
		successor, mintErr := mintInstance(spec, successorDef, s.digester, oneShotID, s.clock)
		if mintErr != nil {
			return endpoint.Outcome{}, mintErr
		}
		successorBytes, encErr := encodeEnvelope(successor)
		if encErr != nil {
			return endpoint.Outcome{}, encErr
		}
		if _, appendErr := s.store.AppendIntent(ctx, IntentRecord{
			Tenant:            string(successor.Tenant),
			IntentID:          successor.IntentID,
			Definition:        successor.Definition,
			IdempotencyKey:    successor.IdempotencyKey,
			CorrelationID:     successor.CorrelationID,
			Lifecycle:         successor.Lifecycle,
			InstanceVersion:   successor.InstanceVersion,
			RequestDigest:     successor.CanonicalRequestDigest,
			CreatedAt:         successor.CreatedAt.Time(),
			RecordedAt:        successor.RecordedAt.Time(),
			LastTransitionAt:  successor.LastTransitionAt.Time(),
			Envelope:          successorBytes,
			EnvelopeSchemaRef: EnvelopeSchemaRef,
		}); appendErr != nil {
			return endpoint.Outcome{}, appendErr
		}
		return endpoint.Outcome{Status: "OK", ResultDigest: successorID}, nil
	}); doErr != nil {
		return nil, idempotencyError(doErr)
	}

	// successorID is deterministic from this request's own identity, so it
	// is read back from the store the same way whether this call actually
	// ran the closure above or replayed a cached outcome: either way the
	// durable fact behind it is the same one successor.
	successorRecord, _, ownedErr := s.loadInstance(ctx, tenant, successorID)
	if ownedErr != nil {
		return nil, ownedErr
	}
	msg, convErr := protomap.InstanceToProto(successorRecord)
	if convErr != nil {
		return nil, envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
			"the operation could not be completed").WithDiagnostic(convErr)
	}
	msg.SupersessionReferences = append(msg.SupersessionReferences, &intentsv1.SupersessionReference{
		SupersessionId:      uuid.NewSHA1(artifactNamespace, []byte(originalID+"|supersedes|"+successorID)).String(),
		SupersedingIntentId: successorID,
		SupersededIntentId:  originalID,
		ReasonRef:           reasonRef,
		RecordedAt:          protomap.InstantToProto(values.NewInstant(s.clock().Time())),
	})
	return &intentsv1.SupersedeIntentResponse{SupersedingIntent: msg}, nil
}
