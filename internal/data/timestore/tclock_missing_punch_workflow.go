package timestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// MissingPunchWorkflowRequest is the fully bound request written by the
// governed workflow. All workflow receipt fields are required evidence.
type MissingPunchWorkflowRequest struct {
	TenantID, ID, SessionID, OriginalObservationID, WorkerRef string
	ClaimedOutAt, At                                          time.Time
	Reason, RequestedBy, IdempotencyKey                       string
	ExpectedSessionRevision                                   int64
	PeriodClosed                                              bool
	OriginalWorkflowInstanceRef, WorkflowInstanceID           uuid.UUID
	WorkflowTraceID, WorkflowNodeID, WorkflowPlanDigest       string
	WorkflowAttempt                                           int
	WorkflowInstanceVersion                                   int64
}

// MissingPunchWorkflowRecord is the current durable request projection.
type MissingPunchWorkflowRecord struct {
	MissingPunchWorkflowRequest
	Status                          string
	RequestRevision                 int64
	DecidedBy, Decision, ReopenRef  string
	DecidedAt, CreatedAt, UpdatedAt time.Time
	CompletedProofRevision          int64
	CompletedProofDigest            string
	CompletedProofInstanceID        uuid.UUID
	CompletedProofNodeID            string
	CompletedProofAttempt           int
	CompletedProofAt                time.Time
}

// MissingPunchWorkflowProof is immutable completion evidence emitted after
// the engine has successfully completed the correction effect. ProofRevision
// is independent from the request business revision.
type MissingPunchWorkflowProof struct {
	TenantID, RequestID             string
	ExpectedProofRevision           int64
	WorkflowInstanceID              uuid.UUID
	WorkflowTraceID, WorkflowNodeID string
	WorkflowPlanDigest              string
	WorkflowAttempt                 int
	WorkflowInstanceVersion         int64
	ProofDigest                     string
	ProofPayload                    json.RawMessage
	CompletedAt                     time.Time
}

// AppendCompletedMissingPunchProof persists final engine evidence with a
// monotonic projection CAS in a time-store-owned tenant transaction.
func (s *Store) AppendCompletedMissingPunchProof(ctx context.Context, proof MissingPunchWorkflowProof) (MissingPunchWorkflowRecord, error) {
	if err := validateMissingPunchProof(proof); err != nil {
		return MissingPunchWorkflowRecord{}, err
	}
	var out MissingPunchWorkflowRecord
	fn := func(tx dbport.Tx) error { return appendMissingPunchProofTx(ctx, tx, proof, &out) }
	err := s.RunTenantTx(ctx, proof.TenantID, fn)
	return out, err
}

func appendMissingPunchProofTx(ctx context.Context, tx dbport.Tx, proof MissingPunchWorkflowProof, out *MissingPunchWorkflowRecord) error {
	var priorDigest string
	err := tx.QueryRow(ctx, `SELECT proof_digest FROM missed_punch_workflow_execution_proof WHERE tenant_id=$1 AND request_id=$2 AND workflow_instance_id=$3 AND workflow_node_id=$4 AND workflow_attempt=$5`, proof.TenantID, proof.RequestID, proof.WorkflowInstanceID, proof.WorkflowNodeID, proof.WorkflowAttempt).Scan(&priorDigest)
	if err == nil {
		if priorDigest != proof.ProofDigest {
			return ErrMissingPunchWorkflowReplay
		}
		return loadMissingPunchRecord(ctx, tx, proof.TenantID, proof.RequestID, out)
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return err
	}
	var currentRevision int64
	if err := tx.QueryRow(ctx, `SELECT completed_proof_revision FROM missed_punch_workflow_request WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, proof.TenantID, proof.RequestID).Scan(&currentRevision); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if currentRevision != proof.ExpectedProofRevision {
		return ErrRevisionConflict
	}
	nextRevision := currentRevision + 1
	proofID := uuid.NewString()
	if _, err := tx.Exec(ctx, `INSERT INTO missed_punch_workflow_execution_proof(tenant_id,id,request_id,proof_revision,workflow_instance_id,workflow_trace_id,workflow_node_id,workflow_plan_digest,workflow_attempt,workflow_instance_version,proof_digest,proof_payload,completed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13)`, proof.TenantID, proofID, proof.RequestID, nextRevision, proof.WorkflowInstanceID, proof.WorkflowTraceID, proof.WorkflowNodeID, proof.WorkflowPlanDigest, proof.WorkflowAttempt, proof.WorkflowInstanceVersion, proof.ProofDigest, string(proof.ProofPayload), proof.CompletedAt.UTC()); err != nil {
		if isUniqueViolation(err) {
			return ErrRevisionConflict
		}
		return err
	}
	rows, err := tx.Exec(ctx, `UPDATE missed_punch_workflow_request SET completed_proof_revision=$1,completed_proof_digest=$2,completed_proof_instance_id=$3,completed_proof_node_id=$4,completed_proof_attempt=$5,completed_proof_at=$6,updated_at=now() WHERE tenant_id=$7 AND id=$8 AND completed_proof_revision=$9`, nextRevision, proof.ProofDigest, proof.WorkflowInstanceID, proof.WorkflowNodeID, proof.WorkflowAttempt, proof.CompletedAt.UTC(), proof.TenantID, proof.RequestID, proof.ExpectedProofRevision)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrRevisionConflict
	}
	body, _ := json.Marshal(map[string]any{"request_id": proof.RequestID, "proof_revision": nextRevision, "proof_digest": proof.ProofDigest, "workflow_node_id": proof.WorkflowNodeID})
	if err := appendOutbox(ctx, tx, proof.TenantID, "clock.missing_punch.proof_completed", int(nextRevision), body); err != nil {
		return err
	}
	return loadMissingPunchRecord(ctx, tx, proof.TenantID, proof.RequestID, out)
}

// MissingPunchWorkflowDecision carries the review result and workflow proof.
type MissingPunchWorkflowDecision struct {
	TenantID, RequestID, ActorRef, IdempotencyKey string
	DecisionNote, ReopenRef                       string
	ExpectedRequestRevision                       int64
	Approve                                       bool
	At                                            time.Time
	WorkflowInstanceID                            uuid.UUID
	WorkflowTraceID, WorkflowNodeID               string
	WorkflowPlanDigest                            string
	WorkflowAttempt                               int
	WorkflowInstanceVersion                       int64
}

// MissingPunchWorkflowCorrection contains the immutable correction output.
type MissingPunchWorkflowCorrection struct {
	MissingPunchWorkflowRecord
	CorrectionObservationID string
	CorrectionDigest        string
}

// ErrMissingPunchWorkflowReplay reports an idempotency key reused with
// different input. Replays with the same digest return the original record.
var ErrMissingPunchWorkflowReplay = errors.New("missing-punch workflow idempotency conflict")

// CommitMissingPunchWorkflowRequest appends a request and proof in one
// time-store-owned tenant transaction. Cross-database caller transactions are
// deliberately ignored at this boundary.
func (s *Store) CommitMissingPunchWorkflowRequest(ctx context.Context, req MissingPunchWorkflowRequest) (MissingPunchWorkflowRecord, error) {
	if err := validateMissingPunchRequest(req); err != nil {
		return MissingPunchWorkflowRecord{}, err
	}
	var out MissingPunchWorkflowRecord
	err := s.RunTenantTx(ctx, req.TenantID, func(tx dbport.Tx) error { return commitMissingPunchRequestTx(ctx, tx, req, &out) })
	return out, err
}

func commitMissingPunchRequestTx(ctx context.Context, tx dbport.Tx, req MissingPunchWorkflowRequest, out *MissingPunchWorkflowRecord) error {
	digest := missingPunchRequestDigest(req)
	var priorDigest string
	err := tx.QueryRow(ctx, `SELECT input_digest FROM missed_punch_workflow_version WHERE tenant_id=$1 AND request_id=$2 AND idempotency_key=$3`, req.TenantID, req.ID, req.IdempotencyKey).Scan(&priorDigest)
	if err == nil {
		if priorDigest != digest {
			return ErrMissingPunchWorkflowReplay
		}
		return loadMissingPunchRecord(ctx, tx, req.TenantID, req.ID, out)
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return err
	}
	var current int64
	var worker, assignment, status string
	if err := tx.QueryRow(ctx, `SELECT worker_ref,assignment_ref,status,revision FROM time_session WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, req.TenantID, req.SessionID).Scan(&worker, &assignment, &status, &current); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if worker != req.WorkerRef || status != "OPEN" || current != req.ExpectedSessionRevision {
		return ErrRevisionConflict
	}
	var originalWorker, originalAssignment, originalEvent string
	if err := tx.QueryRow(ctx, `SELECT worker_ref,assignment_ref,event_type FROM time_observation WHERE tenant_id=$1 AND id=$2`, req.TenantID, req.OriginalObservationID).Scan(&originalWorker, &originalAssignment, &originalEvent); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if originalWorker != req.WorkerRef || originalAssignment != assignment || originalEvent != "CLOCK_IN" {
		return ErrInvalid
	}
	var missingOutEvents int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM time_session_event WHERE tenant_id=$1 AND session_id=$2 AND (kind='MISSING_OUT' OR payload->>'terminal'='TIME_SESSION_MISSING_OUT')`, req.TenantID, req.SessionID).Scan(&missingOutEvents); err != nil {
		return err
	}
	if missingOutEvents == 0 {
		return ErrInvalid
	}
	var boundInstance uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT instance_id FROM time_workflow_session_run WHERE tenant_id=$1 AND session_id=$2`, req.TenantID, req.SessionID).Scan(&boundInstance); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if boundInstance != req.OriginalWorkflowInstanceRef {
		return ErrInvalid
	}
	if _, err := tx.Exec(ctx, `INSERT INTO missed_punch_workflow_request(tenant_id,id,session_id,original_observation_id,worker_ref,claimed_out_at,reason,requested_by,status,request_revision,expected_session_revision,period_closed,original_workflow_instance_ref,workflow_instance_id,workflow_trace_id,workflow_node_id,workflow_plan_digest,workflow_attempt,workflow_instance_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'PENDING',1,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, req.TenantID, req.ID, req.SessionID, req.OriginalObservationID, req.WorkerRef, req.ClaimedOutAt.UTC(), req.Reason, req.RequestedBy, req.ExpectedSessionRevision, req.PeriodClosed, req.OriginalWorkflowInstanceRef, req.WorkflowInstanceID, req.WorkflowTraceID, req.WorkflowNodeID, req.WorkflowPlanDigest, req.WorkflowAttempt, req.WorkflowInstanceVersion); err != nil {
		if isUniqueViolation(err) {
			return ErrRevisionConflict
		}
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO missed_punch_workflow_version(tenant_id,id,request_id,revision,action,status,actor_ref,idempotency_key,input_digest,workflow_instance_id,workflow_trace_id,workflow_node_id,workflow_plan_digest,workflow_attempt,workflow_instance_version) VALUES($1,$2,$3,1,'REQUEST','PENDING',$4,$5,$6,$7,$8,$9,$10,$11,$12)`, req.TenantID, uuid.NewString(), req.ID, req.RequestedBy, req.IdempotencyKey, digest, req.WorkflowInstanceID, req.WorkflowTraceID, req.WorkflowNodeID, req.WorkflowPlanDigest, req.WorkflowAttempt, req.WorkflowInstanceVersion); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"request_id": req.ID, "session_id": req.SessionID, "observation_id": req.OriginalObservationID, "input_digest": digest})
	if err := appendOutbox(ctx, tx, req.TenantID, "clock.missing_punch.requested", 1, body); err != nil {
		return err
	}
	return loadMissingPunchRecord(ctx, tx, req.TenantID, req.ID, out)
}

// CommitMissingPunchWorkflowDecision atomically appends a decision and, for
// approval, the correction observation, session CAS/event, and outbox proof.
func (s *Store) CommitMissingPunchWorkflowDecision(ctx context.Context, d MissingPunchWorkflowDecision) (MissingPunchWorkflowCorrection, error) {
	if err := validateMissingPunchDecision(d); err != nil {
		return MissingPunchWorkflowCorrection{}, err
	}
	var out MissingPunchWorkflowCorrection
	fn := func(tx dbport.Tx) error { return commitMissingPunchDecisionTx(ctx, tx, d, &out) }
	err := s.RunTenantTx(ctx, d.TenantID, fn)
	return out, err
}

func commitMissingPunchDecisionTx(ctx context.Context, tx dbport.Tx, d MissingPunchWorkflowDecision, out *MissingPunchWorkflowCorrection) error {
	digest := missingPunchDecisionDigest(d)
	var priorDigest string
	err := tx.QueryRow(ctx, `SELECT input_digest FROM missed_punch_workflow_version WHERE tenant_id=$1 AND request_id=$2 AND idempotency_key=$3`, d.TenantID, d.RequestID, d.IdempotencyKey).Scan(&priorDigest)
	if err == nil {
		if priorDigest != digest {
			return ErrMissingPunchWorkflowReplay
		}
		if err := loadMissingPunchRecord(ctx, tx, d.TenantID, d.RequestID, &out.MissingPunchWorkflowRecord); err != nil {
			return err
		}
		if out.Status != "APPROVED" {
			return nil
		}
		return loadMissingPunchCorrection(ctx, tx, d.TenantID, d.RequestID, out)
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return err
	}
	var r MissingPunchWorkflowRecord
	if err := loadMissingPunchRecord(ctx, tx, d.TenantID, d.RequestID, &r); err != nil {
		return err
	}
	if d.ActorRef == r.RequestedBy || d.ActorRef == r.WorkerRef {
		return ErrInvalid
	}
	// Period status is read from the server-owned session projection at the
	// decision boundary. The request's copied value is display/audit context,
	// never authority for permitting a closed-period decision.
	var sessionPayload []byte
	if err := tx.QueryRow(ctx, `SELECT payload FROM time_session WHERE tenant_id=$1 AND id=$2`, d.TenantID, r.SessionID).Scan(&sessionPayload); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var sessionFacts struct {
		PeriodClosed bool `json:"period_closed"`
	}
	if err := json.Unmarshal(sessionPayload, &sessionFacts); err != nil {
		return ErrInvalid
	}
	if r.RequestRevision != d.ExpectedRequestRevision || r.Status != "PENDING" || (sessionFacts.PeriodClosed && d.ReopenRef == "") {
		return ErrRevisionConflict
	}
	status := "REJECTED"
	if d.Approve {
		status = "APPROVED"
	}
	var correctionID, correctionDigest string
	if d.Approve {
		correctionID = d.RequestID + ":correction"
		correctionDigest = sha256Hex(strings.Join([]string{r.OriginalObservationID, r.ClaimedOutAt.UTC().Format(time.RFC3339Nano), d.IdempotencyKey}, "\x00"))
		payload, _ := json.Marshal(map[string]string{"request_id": d.RequestID, "corrects_id": r.OriginalObservationID, "input_digest": digest})
		inserted, err := tx.Exec(ctx, `INSERT INTO time_observation(tenant_id,id,worker_ref,assignment_ref,device_ref,source,event_type,project_ref,timezone,occurred_at,received_at,idempotency_key,digest,corrects_id,payload) SELECT r.tenant_id,$2,o.worker_ref,o.assignment_ref,'','MISSING_PUNCH','CLOCK_OUT',o.project_ref,o.timezone,$3,$4,$5,$6,r.original_observation_id,$7::jsonb FROM missed_punch_workflow_request r JOIN time_observation o ON o.tenant_id=r.tenant_id AND o.id=r.original_observation_id WHERE r.tenant_id=$1 AND r.id=$8`, d.TenantID, correctionID, r.ClaimedOutAt.UTC(), d.At.UTC(), d.IdempotencyKey, correctionDigest, string(payload), d.RequestID)
		if err != nil {
			return err
		}
		if inserted != 1 {
			return ErrNotFound
		}
		rows, err := tx.Exec(ctx, `UPDATE time_session SET status='CLOSED',revision=revision+1,closed_at=$3,payload=jsonb_set(jsonb_set(payload,'{open_exceptions}',COALESCE((SELECT jsonb_agg(item) FROM jsonb_array_elements(COALESCE(payload->'open_exceptions','[]'::jsonb)) item WHERE item->>'kind' <> 'MISSING_OUT'),'[]'::jsonb)),'{correction_observation_id}',to_jsonb($5::text)),updated_at=now() WHERE tenant_id=$1 AND id=$2 AND revision=$4 AND status='OPEN'`, d.TenantID, r.SessionID, r.ClaimedOutAt.UTC(), r.ExpectedSessionRevision, correctionID)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrRevisionConflict
		}
		if _, err := tx.Exec(ctx, `INSERT INTO time_session_event(tenant_id,id,session_id,sequence,revision,kind,actor_ref,idempotency_key,digest,payload) SELECT tenant_id,gen_random_uuid()::text,id,revision,revision,'MISSING_PUNCH_CORRECTION',$1,$2,$3,$4::jsonb FROM time_session WHERE tenant_id=$5 AND id=$6`, d.ActorRef, d.IdempotencyKey, correctionDigest, string(payload), d.TenantID, r.SessionID); err != nil {
			return err
		}
		if err := appendOutbox(ctx, tx, d.TenantID, "clock.session.missing_punch_correction", 1, payload); err != nil {
			return err
		}
	}
	newRevision := r.RequestRevision + 1
	rows, err := tx.Exec(ctx, `UPDATE missed_punch_workflow_request SET status=$1,request_revision=$2,updated_at=now() WHERE tenant_id=$3 AND id=$4 AND request_revision=$5 AND status='PENDING'`, status, newRevision, d.TenantID, d.RequestID, r.RequestRevision)
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrRevisionConflict
	}
	if _, err := tx.Exec(ctx, `INSERT INTO missed_punch_workflow_version(tenant_id,id,request_id,revision,action,status,actor_ref,idempotency_key,input_digest,decision_note,reopen_ref,workflow_instance_id,workflow_trace_id,workflow_node_id,workflow_plan_digest,workflow_attempt,workflow_instance_version,correction_observation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`, d.TenantID, uuid.NewString(), d.RequestID, newRevision, map[bool]string{true: "APPROVE", false: "REJECT"}[d.Approve], status, d.ActorRef, d.IdempotencyKey, digest, d.DecisionNote, d.ReopenRef, d.WorkflowInstanceID, d.WorkflowTraceID, d.WorkflowNodeID, d.WorkflowPlanDigest, d.WorkflowAttempt, d.WorkflowInstanceVersion, nullIfEmpty(correctionID)); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"request_id": d.RequestID, "status": status, "correction_observation_id": correctionID, "input_digest": digest})
	if err := appendOutbox(ctx, tx, d.TenantID, "clock.missing_punch."+strings.ToLower(status), 1, body); err != nil {
		return err
	}
	if !d.Approve {
		return loadMissingPunchRecord(ctx, tx, d.TenantID, d.RequestID, &out.MissingPunchWorkflowRecord)
	}
	return loadMissingPunchCorrection(ctx, tx, d.TenantID, d.RequestID, out)
}

func loadMissingPunchRecord(ctx context.Context, tx dbport.Tx, tenant, id string, out *MissingPunchWorkflowRecord) error {
	var proofInstance *uuid.UUID
	var proofAt *time.Time
	err := tx.QueryRow(ctx, `SELECT tenant_id,id,session_id,original_observation_id,worker_ref,claimed_out_at,reason,requested_by,status,request_revision,expected_session_revision,period_closed,original_workflow_instance_ref,workflow_instance_id,workflow_trace_id,workflow_node_id,workflow_plan_digest,workflow_attempt,workflow_instance_version,created_at,updated_at,completed_proof_revision,completed_proof_digest,completed_proof_instance_id,completed_proof_node_id,completed_proof_attempt,completed_proof_at FROM missed_punch_workflow_request WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&out.TenantID, &out.ID, &out.SessionID, &out.OriginalObservationID, &out.WorkerRef, &out.ClaimedOutAt, &out.Reason, &out.RequestedBy, &out.Status, &out.RequestRevision, &out.ExpectedSessionRevision, &out.PeriodClosed, &out.OriginalWorkflowInstanceRef, &out.WorkflowInstanceID, &out.WorkflowTraceID, &out.WorkflowNodeID, &out.WorkflowPlanDigest, &out.WorkflowAttempt, &out.WorkflowInstanceVersion, &out.CreatedAt, &out.UpdatedAt, &out.CompletedProofRevision, &out.CompletedProofDigest, &proofInstance, &out.CompletedProofNodeID, &out.CompletedProofAttempt, &proofAt)
	if err == nil {
		if proofInstance != nil {
			out.CompletedProofInstanceID = *proofInstance
		}
		if proofAt != nil {
			out.CompletedProofAt = *proofAt
		}
	}
	return err
}

func loadMissingPunchCorrection(ctx context.Context, tx dbport.Tx, tenant, id string, out *MissingPunchWorkflowCorrection) error {
	if err := loadMissingPunchRecord(ctx, tx, tenant, id, &out.MissingPunchWorkflowRecord); err != nil {
		return err
	}
	var action string
	if err := tx.QueryRow(ctx, `SELECT action,actor_ref,reopen_ref,workflow_instance_id,workflow_trace_id,workflow_node_id,workflow_plan_digest,workflow_attempt,workflow_instance_version,created_at FROM missed_punch_workflow_version WHERE tenant_id=$1 AND request_id=$2 ORDER BY revision DESC LIMIT 1`, tenant, id).Scan(&action, &out.DecidedBy, &out.ReopenRef, &out.WorkflowInstanceID, &out.WorkflowTraceID, &out.WorkflowNodeID, &out.WorkflowPlanDigest, &out.WorkflowAttempt, &out.WorkflowInstanceVersion, &out.DecidedAt); err != nil {
		return err
	}
	if action == "REQUEST" {
		return ErrNotFound
	}
	if action == "APPROVE" {
		out.Decision = "APPROVED"
	} else {
		out.Decision = "REJECTED"
	}
	return tx.QueryRow(ctx, `SELECT correction_observation_id,digest FROM missed_punch_workflow_version LEFT JOIN time_observation ON time_observation.tenant_id=missed_punch_workflow_version.tenant_id AND time_observation.id=missed_punch_workflow_version.correction_observation_id WHERE missed_punch_workflow_version.tenant_id=$1 AND request_id=$2 AND correction_observation_id IS NOT NULL ORDER BY revision DESC LIMIT 1`, tenant, id).Scan(&out.CorrectionObservationID, &out.CorrectionDigest)
}

func validateMissingPunchRequest(r MissingPunchWorkflowRequest) error {
	if r.TenantID == "" || r.ID == "" || r.SessionID == "" || r.OriginalObservationID == "" || r.WorkerRef == "" || r.ClaimedOutAt.IsZero() || r.At.IsZero() || r.Reason == "" || r.RequestedBy == "" || r.IdempotencyKey == "" || r.ExpectedSessionRevision < 1 || r.OriginalWorkflowInstanceRef == uuid.Nil || r.WorkflowInstanceID == uuid.Nil || r.WorkflowTraceID == "" || r.WorkflowNodeID == "" || r.WorkflowPlanDigest == "" || r.WorkflowAttempt < 1 || r.WorkflowInstanceVersion < 1 {
		return ErrInvalid
	}
	return nil
}

func validateMissingPunchDecision(d MissingPunchWorkflowDecision) error {
	if d.TenantID == "" || d.RequestID == "" || d.ActorRef == "" || d.IdempotencyKey == "" || d.ExpectedRequestRevision < 1 || d.At.IsZero() || d.WorkflowInstanceID == uuid.Nil || d.WorkflowTraceID == "" || d.WorkflowNodeID == "" || d.WorkflowPlanDigest == "" || d.WorkflowAttempt < 1 || d.WorkflowInstanceVersion < 1 {
		return ErrInvalid
	}
	return nil
}

func validateMissingPunchProof(p MissingPunchWorkflowProof) error {
	if p.TenantID == "" || p.RequestID == "" || p.ExpectedProofRevision < 0 || p.WorkflowInstanceID == uuid.Nil || p.WorkflowTraceID == "" || p.WorkflowNodeID == "" || p.WorkflowPlanDigest == "" || p.WorkflowAttempt < 1 || p.WorkflowInstanceVersion < 1 || p.ProofDigest == "" || len(p.ProofPayload) == 0 || p.CompletedAt.IsZero() {
		return ErrInvalid
	}
	var value any
	if err := json.Unmarshal(p.ProofPayload, &value); err != nil || value == nil {
		return ErrInvalid
	}
	return nil
}

func missingPunchRequestDigest(r MissingPunchWorkflowRequest) string {
	// At and execution receipt coordinates are ingestion/recovery metadata. The
	// remaining typed fields are the request's semantic input and are encoded as
	// JSON so delimiters cannot create equivalent digests for distinct values.
	input := struct {
		TenantID, ID, SessionID, OriginalObservationID, WorkerRef string
		ClaimedOutAt, Reason, RequestedBy, IdempotencyKey         string
		ExpectedSessionRevision                                   int64
		PeriodClosed                                              bool
		OriginalWorkflowInstanceRef, WorkflowInstanceID           uuid.UUID
	}{r.TenantID, r.ID, r.SessionID, r.OriginalObservationID, r.WorkerRef, r.ClaimedOutAt.UTC().Format(time.RFC3339Nano), r.Reason, r.RequestedBy, r.IdempotencyKey, r.ExpectedSessionRevision, r.PeriodClosed, r.OriginalWorkflowInstanceRef, r.WorkflowInstanceID}
	b, _ := json.Marshal(input)
	return sha256Hex(string(b))
}

func missingPunchDecisionDigest(d MissingPunchWorkflowDecision) string {
	input := struct {
		TenantID, RequestID, ActorRef, IdempotencyKey string
		DecisionNote, ReopenRef                       string
		ExpectedRequestRevision                       int64
		Approve                                       bool
		WorkflowInstanceID                            uuid.UUID
	}{d.TenantID, d.RequestID, d.ActorRef, d.IdempotencyKey, d.DecisionNote, d.ReopenRef, d.ExpectedRequestRevision, d.Approve, d.WorkflowInstanceID}
	b, _ := json.Marshal(input)
	return sha256Hex(string(b))
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
