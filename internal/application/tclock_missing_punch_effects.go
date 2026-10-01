package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/application/timeclockstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/clockrepair"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

var (
	errMissingPunchEffectsConfig = errors.New("missing punch effects: incomplete configuration")
	errMissingPunchGovernance    = errors.New("missing punch effects: governance evaluation is unavailable")
)

// MissingPunchGovernance evaluates the pinned policy before a time-plane
// effect is attempted. Implementations must return references to durable
// authorization and policy records; an adapter must never mint those values.
type MissingPunchGovernance interface {
	Evaluate(context.Context, MissingPunchGovernanceRequest) (runtime.GovernanceRefs, error)
}

// MissingPunchGovernanceRequest is the minimum immutable context required for
// a governance decision. The request payload is deliberately not copied into
// refs or logs.
type MissingPunchGovernanceRequest struct {
	Request clockservice.MissingPunchWorkflowRequest
	Node    string
}

// MissingPunchReopener performs the governed durable period reopen. A
// caller-supplied reopen reference is never treated as proof by itself.
type MissingPunchReopener interface {
	Reopen(context.Context, clockservice.MissingPunchWorkflowRequest, execute.StepRequest) (string, error)
}

// PostgresMissingPunchWorkflowEffects is the authoritative time-store
// implementation of MissingPunchWorkflowEffects. Store methods own the
// transaction and idempotency fence; this adapter only binds workflow context,
// governance, and durable evidence to their results.
type PostgresMissingPunchWorkflowEffects struct {
	Store         *timestore.Store
	Governance    MissingPunchGovernance
	Reopener      MissingPunchReopener
	Evidence      MissingPunchExecutionEvidence
	ResolveTenant func(string) (uuid.UUID, error)
}

var _ MissingPunchWorkflowEffects = (*PostgresMissingPunchWorkflowEffects)(nil)

// RejectDecision durably records a supervisor rejection after the approval
// node's completed execution has been verified by the workflow engine. The
// decision store's CAS and idempotency key fence retries and never creates a
// correction observation for a rejected request.
func (a *PostgresMissingPunchWorkflowEffects) RejectDecision(ctx context.Context, req clockservice.MissingPunchWorkflowRequest, proof MissingPunchExecutionProof) (clockservice.MissingPunchRecord, error) {
	if err := a.configured(); err != nil {
		return clockservice.MissingPunchRecord{}, err
	}
	tenant, err := a.ResolveTenant(req.TenantID)
	if err != nil || tenant == uuid.Nil || proof.TenantID != tenant || proof.CompletedState != "SUCCEEDED" || proof.NodeID != clockrepair.NodeApproval || proof.InstanceID == uuid.Nil || proof.Attempt < 1 || proof.PlanDigest == "" || proof.TraceID == "" || proof.CompletedAt.IsZero() {
		return clockservice.MissingPunchRecord{}, errors.New("missing punch effects: invalid approval proof")
	}
	if req.RequestID == "" || req.Actor == "" || req.IdempotencyKey == "" || req.ExpectedRevision < 1 || req.Approve {
		return clockservice.MissingPunchRecord{}, errors.New("missing punch effects: incomplete rejection")
	}
	decision, err := a.Store.CommitMissingPunchWorkflowDecision(ctx, timestore.MissingPunchWorkflowDecision{
		TenantID: req.TenantID, RequestID: req.RequestID, ActorRef: req.Actor, IdempotencyKey: req.IdempotencyKey,
		DecisionNote: req.DecisionNote, ReopenRef: req.ReopenRef, ExpectedRequestRevision: int64(req.ExpectedRevision),
		Approve: false, At: proof.CompletedAt, WorkflowInstanceID: proof.InstanceID, WorkflowTraceID: proof.TraceID,
		WorkflowNodeID: proof.NodeID, WorkflowPlanDigest: proof.PlanDigest, WorkflowAttempt: proof.Attempt, WorkflowInstanceVersion: proof.InstanceVersion,
	})
	if err != nil {
		return clockservice.MissingPunchRecord{}, err
	}
	return recordFromStore(decision.MissingPunchWorkflowRecord), nil
}

func (a *PostgresMissingPunchWorkflowEffects) configured() error {
	if a == nil || a.Store == nil || a.Governance == nil || a.ResolveTenant == nil {
		return errMissingPunchEffectsConfig
	}
	return nil
}

func (a *PostgresMissingPunchWorkflowEffects) refs(ctx context.Context, req clockservice.MissingPunchWorkflowRequest, node string) (runtime.GovernanceRefs, error) {
	if err := a.configured(); err != nil {
		return runtime.GovernanceRefs{}, err
	}
	refs, err := a.Governance.Evaluate(ctx, MissingPunchGovernanceRequest{Request: req, Node: node})
	if err != nil {
		return runtime.GovernanceRefs{}, err
	}
	if strings.TrimSpace(refs.AuthorizationDecisionID) == "" || strings.TrimSpace(refs.PolicyRef) == "" {
		return runtime.GovernanceRefs{}, errMissingPunchGovernance
	}
	return refs, nil
}

// CommitRequest persists the request and returns the version row's real
// identity as the effect reference.
func (a *PostgresMissingPunchWorkflowEffects) CommitRequest(ctx context.Context, req clockservice.MissingPunchWorkflowRequest, step execute.StepRequest, id string) (MissingPunchEffectResult, error) {
	if step.InstanceID == uuid.Nil || step.Attempt < 1 {
		return MissingPunchEffectResult{}, errors.New("missing punch effects: engine step binding is required")
	}
	refs, err := a.refs(ctx, req, clockrepair.NodeCommitRequest)
	if err != nil {
		return MissingPunchEffectResult{}, err
	}
	if err := a.Store.CaptureStateBase(ctx, req.TenantID, req.SessionID, req.ExpectedRevision); err != nil {
		return MissingPunchEffectResult{}, err
	}
	r, err := a.Store.CommitMissingPunchWorkflowRequest(ctx, timestore.MissingPunchWorkflowRequest{
		TenantID: req.TenantID, ID: id, SessionID: req.SessionID, OriginalObservationID: req.OriginalObservationID,
		WorkerRef: req.WorkerRef, ClaimedOutAt: req.ClaimedOutAt, At: req.At, Reason: req.Reason,
		RequestedBy: req.Actor, IdempotencyKey: req.IdempotencyKey, ExpectedSessionRevision: int64(req.ExpectedRevision),
		PeriodClosed: req.PeriodClosed, OriginalWorkflowInstanceRef: parseUUID(req.OriginalWorkflowInstanceRef),
		WorkflowInstanceID: step.InstanceID, WorkflowTraceID: step.TraceID, WorkflowNodeID: clockrepair.NodeCommitRequest,
		WorkflowPlanDigest: missingPunchStepPlanDigest(step), WorkflowAttempt: step.Attempt, WorkflowInstanceVersion: step.InstanceVersion,
	})
	if err != nil {
		return MissingPunchEffectResult{}, err
	}
	ref, err := a.requestVersionRef(ctx, req.TenantID, id, 1)
	if err != nil {
		return MissingPunchEffectResult{}, err
	}
	refs.EffectRefs = append(refs.EffectRefs, ref)
	refs.CapabilityExecutionID = runtime.NodeExecutionID(step.TenantID, step.InstanceID, clockrepair.NodeCommitRequest, step.Attempt).String()
	digest := missingPunchEffectDigest(ref)
	return MissingPunchEffectResult{Record: recordFromStore(r), Outcome: frontier.NodeOutcome{NodeID: clockrepair.NodeCommitRequest, Outcome: workflow.OutcomeSucceeded, OutputDigest: digest}, Refs: refs}, nil
}

// ValidateRequest checks the authoritative session, original observation and
// workflow lineage before any mutating capability is allowed to run.
func (a *PostgresMissingPunchWorkflowEffects) ValidateRequest(ctx context.Context, req clockservice.MissingPunchWorkflowRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	refs, err := a.refs(ctx, req, clockrepair.NodeValidate)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	if err := a.validateLineage(ctx, req); err != nil {
		return frontier.NodeOutcome{NodeID: clockrepair.NodeValidate, Outcome: workflow.OutcomeRejected, OutputDigest: missingPunchEffectDigest(err.Error())}, refs, err
	}
	return frontier.NodeOutcome{NodeID: clockrepair.NodeValidate, Outcome: workflow.OutcomeSucceeded, OutputDigest: missingPunchEffectDigest(req.SessionID + "\x00" + req.OriginalObservationID)}, refs, nil
}

// Eligibility rechecks closed-period and revision facts from the durable
// request projection. Closed periods are routed to the declared reopen node.
func (a *PostgresMissingPunchWorkflowEffects) Eligibility(ctx context.Context, req clockservice.MissingPunchWorkflowRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	refs, err := a.refs(ctx, req, clockrepair.NodeEligibility)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	r, err := (timeclockstore.Adapter{Store: a.Store}).GetMissingPunchRequest(ctx, req.TenantID, req.RequestID)
	if err != nil {
		return frontier.NodeOutcome{}, refs, err
	}
	if r.TenantID != req.TenantID || r.WorkerRef != req.WorkerRef || r.OriginalWorkflowInstanceRef != req.OriginalWorkflowInstanceRef {
		return frontier.NodeOutcome{NodeID: clockrepair.NodeEligibility, Outcome: workflow.Outcome("CONFLICT"), OutputDigest: missingPunchEffectDigest("conflict")}, refs, nil
	}
	route := workflow.Outcome(clockrepair.RouteEligible)
	if r.PeriodClosed {
		route = workflow.Outcome(clockrepair.RouteClosedPeriod)
	}
	return frontier.NodeOutcome{NodeID: clockrepair.NodeEligibility, Outcome: route, OutputDigest: missingPunchEffectDigest(r.ID + "\x00" + r.Decision)}, refs, nil
}

// ReopenPeriod permits the graph to proceed only when governance supplied a
// durable reopen reference. The actual correction transaction consumes that
// reference and remains the sole writer of the request/session facts.
func (a *PostgresMissingPunchWorkflowEffects) ReopenPeriod(ctx context.Context, req clockservice.MissingPunchWorkflowRequest, step execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if step.InstanceID == uuid.Nil || step.Attempt < 1 {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.New("missing punch effects: engine step binding is required")
	}
	if a.Reopener == nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.New("missing punch effects: durable period reopen is unavailable")
	}
	refs, err := a.refs(ctx, req, clockrepair.NodeReopen)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	reopenRef, err := a.Reopener.Reopen(ctx, req, step)
	if err != nil || strings.TrimSpace(reopenRef) == "" {
		if err == nil {
			err = errors.New("missing punch effects: reopen produced no durable reference")
		}
		return frontier.NodeOutcome{}, refs, err
	}
	if strings.TrimSpace(req.ReopenRef) != "" && req.ReopenRef != reopenRef {
		return frontier.NodeOutcome{}, refs, errors.New("missing punch effects: reopen reference binding mismatch")
	}
	refs.EffectRefs = append(refs.EffectRefs, reopenRef)
	refs.CapabilityExecutionID = runtime.NodeExecutionID(step.TenantID, step.InstanceID, clockrepair.NodeReopen, step.Attempt).String()
	return frontier.NodeOutcome{NodeID: clockrepair.NodeReopen, Outcome: workflow.OutcomeSucceeded, OutputDigest: missingPunchEffectDigest(reopenRef)}, refs, nil
}

// AppendCorrection atomically records the decision and, when approved, the
// correction observation and closed session through timestore.
func (a *PostgresMissingPunchWorkflowEffects) AppendCorrection(ctx context.Context, req clockservice.MissingPunchWorkflowRequest, step execute.StepRequest, id string) (MissingPunchEffectResult, error) {
	if !req.Approve {
		return MissingPunchEffectResult{}, errors.New("missing punch effects: rejected decision has no correction effect")
	}
	if step.InstanceID == uuid.Nil || step.Attempt < 1 {
		return MissingPunchEffectResult{}, errors.New("missing punch effects: engine step binding is required")
	}
	refs, err := a.refs(ctx, req, clockrepair.NodeCorrection)
	if err != nil {
		return MissingPunchEffectResult{}, err
	}
	current, loadErr := (timeclockstore.Adapter{Store: a.Store}).GetMissingPunchRequest(ctx, req.TenantID, req.RequestID)
	if loadErr != nil {
		return MissingPunchEffectResult{}, loadErr
	}
	if err := a.Store.CaptureStateBase(ctx, req.TenantID, req.SessionID, current.ExpectedSessionRevision); err != nil {
		return MissingPunchEffectResult{}, err
	}
	r, err := a.Store.CommitMissingPunchWorkflowDecision(ctx, timestore.MissingPunchWorkflowDecision{
		TenantID: req.TenantID, RequestID: req.RequestID, ActorRef: req.Actor, IdempotencyKey: req.IdempotencyKey,
		DecisionNote: req.DecisionNote, ReopenRef: req.ReopenRef, ExpectedRequestRevision: int64(req.ExpectedRevision),
		Approve: req.Approve, At: req.At, WorkflowInstanceID: step.InstanceID, WorkflowTraceID: step.TraceID,
		WorkflowNodeID: clockrepair.NodeCorrection, WorkflowPlanDigest: missingPunchStepPlanDigest(step), WorkflowAttempt: step.Attempt, WorkflowInstanceVersion: step.InstanceVersion,
	})
	if err != nil {
		return MissingPunchEffectResult{}, err
	}
	refsFromStore, err := a.correctionRefs(ctx, req.TenantID, req.RequestID)
	if err != nil {
		return MissingPunchEffectResult{}, err
	}
	refs.EffectRefs = append(refs.EffectRefs, refsFromStore...)
	refs.CapabilityExecutionID = runtime.NodeExecutionID(step.TenantID, step.InstanceID, clockrepair.NodeCorrection, step.Attempt).String()
	record := recordFromStore(r.MissingPunchWorkflowRecord)
	record.CorrectionObservationID = r.CorrectionObservationID
	record.CorrectionDigest = r.CorrectionDigest
	return MissingPunchEffectResult{Record: record, Outcome: frontier.NodeOutcome{NodeID: clockrepair.NodeCorrection, Outcome: workflow.OutcomeSucceeded, OutputDigest: missingPunchEffectDigest(strings.Join(refsFromStore, "\x00"))}, Refs: refs}, nil
}

// OriginalWorkflowInstanceRef reads the authoritative session binding.
func (a *PostgresMissingPunchWorkflowEffects) OriginalWorkflowInstanceRef(ctx context.Context, tenant, session string) (string, error) {
	if err := a.configured(); err != nil {
		return "", err
	}
	var id uuid.UUID
	err := a.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT instance_id FROM time_workflow_session_run WHERE tenant_id=$1 AND session_id=$2`, tenant, session).Scan(&id)
	})
	if err != nil || id == uuid.Nil {
		return "", errors.Join(timestore.ErrNotFound, err)
	}
	return id.String(), nil
}

// LoadRequest returns the current durable request projection.
func (a *PostgresMissingPunchWorkflowEffects) LoadRequest(ctx context.Context, tenant, id string) (clockservice.MissingPunchRecord, error) {
	if err := a.configured(); err != nil {
		return clockservice.MissingPunchRecord{}, err
	}
	r, err := (timeclockstore.Adapter{Store: a.Store}).GetMissingPunchRequest(ctx, tenant, id)
	if err != nil {
		return clockservice.MissingPunchRecord{}, err
	}
	return r, nil
}

// RecordProof stores the engine's completed node proof after the engine has
// durably recorded the node. Replays update no state and return the projection.
func (a *PostgresMissingPunchWorkflowEffects) RecordProof(ctx context.Context, id string, proof clockservice.MissingPunchWorkflowResult) (clockservice.MissingPunchRecord, error) {
	if err := a.configured(); err != nil {
		return clockservice.MissingPunchRecord{}, err
	}
	if proof.Record.ID != id || proof.Record.TenantID == "" || proof.InstanceID == uuid.Nil || proof.PlanDigest == "" || proof.TraceID == "" || proof.NodeID == "" || proof.Attempt < 1 || proof.InstanceVersion < 1 {
		return clockservice.MissingPunchRecord{}, errMissingPunchEffectsConfig
	}
	tenantID, err := a.ResolveTenant(proof.Record.TenantID)
	if err != nil || tenantID == uuid.Nil || a.Evidence == nil {
		return clockservice.MissingPunchRecord{}, errors.New("missing punch effects: core execution evidence is unavailable")
	}
	core, err := a.Evidence.LoadMissingPunchExecution(ctx, tenantID, proof.InstanceID, proof.NodeID, proof.Attempt)
	if err != nil || core.TenantID != tenantID || core.InstanceID != proof.InstanceID || core.NodeID != proof.NodeID || core.Attempt != proof.Attempt || core.CompletedState != "SUCCEEDED" || core.PlanDigest != proof.PlanDigest || core.TraceID != proof.TraceID || core.InstanceVersion != proof.InstanceVersion {
		if err == nil {
			err = errors.New("missing punch effects: node proof is not durable")
		}
		return clockservice.MissingPunchRecord{}, err
	}
	var expected int64
	err = a.Store.RunTenantTx(ctx, proof.Record.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT completed_proof_revision FROM missed_punch_workflow_request WHERE tenant_id=$1 AND id=$2`, proof.Record.TenantID, id).Scan(&expected)
	})
	if err != nil {
		return clockservice.MissingPunchRecord{}, err
	}
	payload, err := json.Marshal(proof)
	if err != nil {
		return clockservice.MissingPunchRecord{}, err
	}
	out, err := a.Store.AppendCompletedMissingPunchProof(ctx, timestore.MissingPunchWorkflowProof{TenantID: proof.Record.TenantID, RequestID: id, ExpectedProofRevision: expected, WorkflowInstanceID: proof.InstanceID, WorkflowTraceID: proof.TraceID, WorkflowNodeID: proof.NodeID, WorkflowPlanDigest: proof.PlanDigest, WorkflowAttempt: proof.Attempt, WorkflowInstanceVersion: proof.InstanceVersion, ProofDigest: missingPunchEffectDigest(string(payload)), ProofPayload: payload, CompletedAt: core.CompletedAt})
	if err != nil {
		return clockservice.MissingPunchRecord{}, err
	}
	if proof.NodeID == clockrepair.NodeCorrection && out.Status == "APPROVED" {
		current, loadErr := (timeclockstore.Adapter{Store: a.Store}).GetMissingPunchRequest(ctx, proof.Record.TenantID, id)
		if loadErr != nil {
			return clockservice.MissingPunchRecord{}, loadErr
		}
		out = timestoreRecordFromClock(current, out)
		correctionID, correctionDigest, identityErr := a.correctionIdentity(ctx, proof.Record.TenantID, id)
		if identityErr != nil {
			return clockservice.MissingPunchRecord{}, identityErr
		}
		stateKey := id + ":state-layer"
		projection, err := a.Store.ReadStateLayerProjection(ctx, proof.Record.TenantID, out.SessionID)
		if errors.Is(err, timestore.ErrNotFound) {
			projection = timestore.StateLayerProjection{Tenant: proof.Record.TenantID, Subject: out.SessionID, BaseRevision: uint64(out.ExpectedSessionRevision)}
		} else if err != nil {
			return clockservice.MissingPunchRecord{}, err
		}
		if out.DecidedBy == "" || out.Reason == "" {
			return clockservice.MissingPunchRecord{}, errors.New("missing punch effects: approved layer provenance is incomplete")
		}
		layer := timesession.StateCorrectionLayer{Tenant: proof.Record.TenantID, Subject: out.SessionID, BaseRevision: uint64(out.ExpectedSessionRevision), ParentRevision: projection.CurrentRevision, Revision: projection.CurrentRevision + 1, Approved: true, Provenance: timesession.LayerProvenance{Actor: out.DecidedBy, Reason: out.Reason, WorkflowProof: timesession.WorkflowProof{InstanceID: proof.InstanceID.String(), PlanDigest: proof.PlanDigest, TraceID: proof.TraceID, NodeID: proof.NodeID, Attempt: uint64(proof.Attempt), Version: uint64(proof.InstanceVersion)}}, Patches: []timesession.StateLayerPatch{{Path: "clock_out_at", Value: timesession.TimeValue(out.ClaimedOutAt)}, {Path: "reason", Value: timesession.StringValue(out.Reason)}}}
		layer.Digest = layer.DigestValue()
		if existing, found, err := a.Store.LookupStateLayer(ctx, proof.Record.TenantID, out.SessionID, stateKey); err != nil {
			return clockservice.MissingPunchRecord{}, err
		} else if found {
			candidate := layer
			candidate.BaseRevision = existing.Layer.BaseRevision
			candidate.ParentRevision = existing.Layer.ParentRevision
			candidate.Revision = existing.Layer.Revision
			candidate.Digest = candidate.DigestValue()
			if existing.Layer.Digest != candidate.Digest || existing.Layer.Provenance != candidate.Provenance || len(existing.Layer.Patches) != len(candidate.Patches) {
				return clockservice.MissingPunchRecord{}, errors.New("missing punch effects: state-layer replay mismatch")
			}
			record := recordFromStore(out)
			record.CorrectionObservationID = correctionID
			record.CorrectionDigest = correctionDigest
			return record, nil
		}
		if _, _, err := a.Store.AppendStateLayerRequest(ctx, timestore.StateLayerAppendRequest{Tenant: proof.Record.TenantID, RequestID: id, OriginalObservation: out.OriginalObservationID, Layer: layer, IdempotencyKey: stateKey}); err != nil {
			return clockservice.MissingPunchRecord{}, err
		}
		record := recordFromStore(out)
		record.CorrectionObservationID = correctionID
		record.CorrectionDigest = correctionDigest
		return record, nil
	}
	return recordFromStore(out), nil
}

func timestoreRecordFromClock(current clockservice.MissingPunchRecord, fallback timestore.MissingPunchWorkflowRecord) timestore.MissingPunchWorkflowRecord {
	fallback.DecidedBy, fallback.ReopenRef, fallback.DecidedAt = current.DecidedBy, current.ReopenRef, current.DecidedAt
	fallback.Status = current.Decision
	return fallback
}

func (a *PostgresMissingPunchWorkflowEffects) validateLineage(ctx context.Context, req clockservice.MissingPunchWorkflowRequest) error {
	row, err := (timeclockstore.Adapter{Store: a.Store}).GetMissingPunchSession(ctx, req.TenantID, req.SessionID)
	if err != nil {
		return err
	}
	if row.TenantID != req.TenantID || row.ID != req.SessionID || row.WorkerRef != req.WorkerRef || row.Status != "OPEN" || !row.MissingOut || row.Revision != req.ExpectedRevision || row.OriginalWorkflowInstanceRef != req.OriginalWorkflowInstanceRef {
		return timestore.ErrRevisionConflict
	}
	obs, err := (timeclockstore.Adapter{Store: a.Store}).GetMissingPunchObservation(ctx, req.TenantID, req.OriginalObservationID)
	if err != nil {
		return err
	}
	if obs.TenantID != req.TenantID || obs.Observation.WorkerRef != req.WorkerRef || !obs.Observation.Accepted {
		return timestore.ErrInvalid
	}
	return nil
}

func (a *PostgresMissingPunchWorkflowEffects) requestVersionRef(ctx context.Context, tenant, id string, revision int64) (string, error) {
	var ref string
	err := a.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM missed_punch_workflow_version WHERE tenant_id=$1 AND request_id=$2 AND revision=$3`, tenant, id, revision).Scan(&ref)
	})
	return "missing-punch-version:" + ref, err
}
func (a *PostgresMissingPunchWorkflowEffects) correctionRefs(ctx context.Context, tenant, id string) ([]string, error) {
	var observation, sessionEvent string
	err := a.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT correction_observation_id FROM missed_punch_workflow_version WHERE tenant_id=$1 AND request_id=$2 AND correction_observation_id IS NOT NULL ORDER BY revision DESC LIMIT 1`, tenant, id).Scan(&observation); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT e.id::text FROM time_session_event e JOIN missed_punch_workflow_request r ON r.tenant_id=e.tenant_id AND r.session_id=e.session_id WHERE r.tenant_id=$1 AND r.id=$2 AND e.kind='MISSING_PUNCH_CORRECTION' ORDER BY e.sequence DESC LIMIT 1`, tenant, id).Scan(&sessionEvent)
	})
	if err != nil {
		return nil, err
	}
	// Terminal proof binds the session aggregate identity. The event identity
	// remains internal audit evidence and is intentionally not presented as a
	// session effect reference.
	var sessionID string
	err = a.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT session_id FROM missed_punch_workflow_request WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&sessionID)
	})
	if err != nil {
		return nil, err
	}
	if sessionID == "" || sessionEvent == "" {
		return nil, errors.New("missing punch effects: incomplete correction evidence")
	}
	return []string{"time_observation:" + observation, "time_session:" + sessionID}, nil
}

func (a *PostgresMissingPunchWorkflowEffects) correctionIdentity(ctx context.Context, tenant, id string) (string, string, error) {
	var observation, digest string
	err := a.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT v.correction_observation_id,o.digest FROM missed_punch_workflow_version v JOIN time_observation o ON o.tenant_id=v.tenant_id AND o.id=v.correction_observation_id WHERE v.tenant_id=$1 AND v.request_id=$2 AND v.correction_observation_id IS NOT NULL ORDER BY v.revision DESC LIMIT 1`, tenant, id).Scan(&observation, &digest)
	})
	return observation, digest, err
}
func missingPunchEffectDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func parseUUID(value string) uuid.UUID { id, _ := uuid.Parse(value); return id }
func missingPunchStepPlanDigest(step execute.StepRequest) string {
	if step.Plan == nil {
		return ""
	}
	return step.Plan.Digest()
}
func recordFromStore(r timestore.MissingPunchWorkflowRecord) clockservice.MissingPunchRecord {
	return clockservice.MissingPunchRecord{ID: r.ID, TenantID: r.TenantID, SessionID: r.SessionID, OriginalObservationID: r.OriginalObservationID, WorkerRef: r.WorkerRef, ClaimedOutAt: r.ClaimedOutAt, CreatedAt: r.CreatedAt, Reason: r.Reason, RequestedBy: r.RequestedBy, Decision: r.Status, Revision: uint64(r.RequestRevision), ExpectedSessionRevision: uint64(r.ExpectedSessionRevision), PeriodClosed: r.PeriodClosed, OriginalWorkflowInstanceRef: r.OriginalWorkflowInstanceRef.String(), WorkflowInstanceID: r.WorkflowInstanceID.String(), WorkflowTraceID: r.WorkflowTraceID, WorkflowNodeID: r.WorkflowNodeID, WorkflowPlanDigest: r.WorkflowPlanDigest, WorkflowAttempt: r.WorkflowAttempt, WorkflowInstanceVersion: r.WorkflowInstanceVersion, DecidedBy: r.DecidedBy, ReopenRef: r.ReopenRef, DecidedAt: r.DecidedAt}
}
