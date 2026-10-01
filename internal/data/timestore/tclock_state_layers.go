package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
)

// StateLayerRecord is the durable audit envelope around one immutable layer.
type StateLayerRecord struct {
	Layer                                        timesession.StateCorrectionLayer
	LayerID, IdempotencyKey, OriginalObservation string
	CreatedAt                                    time.Time
}

// StateLayerProjection is the durable current cursor and immutable base.
type StateLayerProjection struct {
	Tenant, Subject, CurrentLayerID string
	BaseRevision, CurrentRevision   uint64
	BasePayload                     json.RawMessage
}

// StateLayerAppendRequest binds a correction to its governed workflow request.
type StateLayerAppendRequest struct {
	Tenant, RequestID, OriginalObservation string
	Layer                                  timesession.StateCorrectionLayer
	IdempotencyKey                         string
}

// AppendStateLayerRequest persists a layer with an exact completed-proof request binding.
func (s *Store) AppendStateLayerRequest(ctx context.Context, req StateLayerAppendRequest) (StateLayerRecord, bool, error) {
	tenant, layer, idempotencyKey, originalObservation := req.Tenant, req.Layer, req.IdempotencyKey, req.OriginalObservation
	if s == nil || tenant == "" || layer.Tenant != tenant || layer.Subject == "" || idempotencyKey == "" || originalObservation == "" || req.RequestID == "" || !layer.Approved {
		return StateLayerRecord{}, false, ErrInvalid
	}
	out := StateLayerRecord{Layer: layer, LayerID: uuid.NewString(), IdempotencyKey: idempotencyKey, OriginalObservation: originalObservation}
	duplicate := false
	fn := func(tx dbport.Tx) error { return appendStateLayerTx(ctx, tx, tenant, req.RequestID, &out, &duplicate) }

	if err := s.RunTenantTx(ctx, tenant, fn); err != nil {
		return out, duplicate, err
	}
	return out, duplicate, nil
}

func appendStateLayerTx(ctx context.Context, tx dbport.Tx, tenant, requestID string, out *StateLayerRecord, duplicate *bool) error {
	l := out.Layer
	p := l.Provenance.WorkflowProof
	if !validDigest(l.Digest) || l.Digest != l.DigestValue() || l.BaseRevision == 0 || l.Revision == 0 || l.Revision != l.ParentRevision+1 || l.Provenance.Actor == "" || l.Provenance.Reason == "" || p.InstanceID == "" || p.PlanDigest == "" || p.TraceID == "" || p.NodeID == "" || p.Attempt == 0 || p.Version == 0 {
		return ErrInvalid
	}
	var bound timesession.WorkflowProof
	var actor, reason string
	if p.NodeID != "append_correction" {
		return ErrInvalid
	}
	err := tx.QueryRow(ctx, `SELECT p.workflow_instance_id,p.workflow_plan_digest,p.workflow_trace_id,p.workflow_node_id,p.workflow_attempt,p.workflow_instance_version,(SELECT v.actor_ref FROM missed_punch_workflow_version v WHERE v.tenant_id=r.tenant_id AND v.request_id=r.id AND v.action='APPROVE' ORDER BY v.revision DESC LIMIT 1),r.reason FROM missed_punch_workflow_execution_proof p JOIN missed_punch_workflow_request r ON r.tenant_id=p.tenant_id AND r.id=p.request_id WHERE p.tenant_id=$1 AND r.id=$2 AND r.session_id=$3 AND r.original_observation_id=$4 AND r.status='APPROVED' AND p.workflow_instance_id=$5 AND p.workflow_plan_digest=$6 AND p.workflow_trace_id=$7 AND p.workflow_node_id='append_correction' AND p.workflow_attempt=$8 AND p.workflow_instance_version=$9`, tenant, requestID, l.Subject, out.OriginalObservation, p.InstanceID, p.PlanDigest, p.TraceID, p.Attempt, p.Version).Scan(&bound.InstanceID, &bound.PlanDigest, &bound.TraceID, &bound.NodeID, &bound.Attempt, &bound.Version, &actor, &reason)
	if errors.Is(err, dbport.ErrNoRows) {
		return ErrInvalid
	}
	if err != nil {
		return err
	}
	if actor != l.Provenance.Actor || reason != l.Provenance.Reason {
		return ErrInvalid
	}
	var claimed time.Time
	if err := tx.QueryRow(ctx, `SELECT claimed_out_at FROM missed_punch_workflow_request WHERE tenant_id=$1 AND id=$2`, tenant, requestID).Scan(&claimed); err != nil {
		return err
	}
	for _, patch := range l.Patches {
		switch patch.Path {
		case "clock_out_at":
			if patch.Value.Kind != timesession.LayerValueTime || !patch.Value.Time.Equal(claimed) {
				return ErrInvalid
			}
		case "reason":
			if patch.Value.Kind != timesession.LayerValueString || patch.Value.String != reason {
				return ErrInvalid
			}
		}
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenant+"|state-layer|"+l.Subject); err != nil {
		return err
	}
	var priorDigest string
	err = tx.QueryRow(ctx, `SELECT id,digest,created_at FROM time_state_layer WHERE tenant_id=$1 AND subject_id=$2 AND idempotency_key=$3`, tenant, l.Subject, out.IdempotencyKey).Scan(&out.LayerID, &priorDigest, &out.CreatedAt)
	if err == nil {
		if priorDigest != l.Digest {
			return ErrIdempotencyConflict
		}
		*duplicate = true
		return nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT o.id FROM time_observation o JOIN missed_punch_workflow_request r ON r.tenant_id=o.tenant_id AND r.original_observation_id=o.id JOIN time_session s ON s.tenant_id=r.tenant_id AND s.id=r.session_id WHERE o.tenant_id=$1 AND o.id=$2 AND r.id=$3 AND s.id=$4 AND o.worker_ref=s.worker_ref AND o.assignment_ref=s.assignment_ref AND o.event_type IN ('IN','CLOCK_IN')`, tenant, out.OriginalObservation, requestID, l.Subject).Scan(new(string)); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var basePayload []byte
	var storedBaseRevision int64
	if err := tx.QueryRow(ctx, `SELECT payload,revision FROM time_session WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, l.Subject).Scan(&basePayload, &storedBaseRevision); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var capturedRevision int64
	captureErr := tx.QueryRow(ctx, `SELECT base_revision,base_payload FROM time_state_base WHERE tenant_id=$1 AND subject_id=$2`, tenant, l.Subject).Scan(&capturedRevision, &basePayload)
	if captureErr == nil {
		storedBaseRevision = capturedRevision
	} else if !errors.Is(captureErr, dbport.ErrNoRows) {
		return captureErr
	}
	if l.ParentRevision == 0 && storedBaseRevision != int64(l.BaseRevision) {
		return ErrRevisionConflict
	}
	if _, err := tx.Exec(ctx, `INSERT INTO time_state_base(tenant_id,subject_id,base_revision,base_payload) VALUES($1,$2,$3,$4::jsonb) ON CONFLICT (tenant_id,subject_id) DO NOTHING`, tenant, l.Subject, l.BaseRevision, basePayload); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO time_state_layer_registry(tenant_id,subject_id,base_revision,current_revision,current_layer_id,base_payload,updated_at) VALUES($1,$2,$3,0,NULL,$4::jsonb,now()) ON CONFLICT (tenant_id,subject_id) DO NOTHING`, tenant, l.Subject, l.BaseRevision, basePayload); err != nil {
		return err
	}
	var baseRevision, current int64
	if err := tx.QueryRow(ctx, `SELECT base_revision,current_revision FROM time_state_layer_registry WHERE tenant_id=$1 AND subject_id=$2 FOR UPDATE`, tenant, l.Subject).Scan(&baseRevision, &current); err != nil {
		return err
	}
	if baseRevision != int64(l.BaseRevision) || uint64(current) != l.ParentRevision {
		return ErrRevisionConflict
	}
	base, err := timesession.NewLayeredState(timesession.TimeSessionBaseFacts{Tenant: tenant, Subject: l.Subject, BaseRevision: l.BaseRevision})
	if err != nil {
		return ErrInvalid
	}
	rows, err := tx.Query(ctx, `SELECT revision,parent_revision,actor_ref,reason,workflow_instance_id,workflow_plan_id,workflow_plan_digest,workflow_trace_id,workflow_node_id,workflow_attempt,workflow_version,digest,patches FROM time_state_layer WHERE tenant_id=$1 AND subject_id=$2 AND approved=true ORDER BY revision`, tenant, l.Subject)
	if err != nil {
		return err
	}
	historyCount := 0
	for rows.Next() {
		historyCount++
		var prior timesession.StateCorrectionLayer
		var proof timesession.WorkflowProof
		var patches []byte
		if err := rows.Scan(&prior.Revision, &prior.ParentRevision, &prior.Provenance.Actor, &prior.Provenance.Reason, &proof.InstanceID, &proof.PlanID, &proof.PlanDigest, &proof.TraceID, &proof.NodeID, &proof.Attempt, &proof.Version, &prior.Digest, &patches); err != nil {
			rows.Close()
			return err
		}
		prior.Tenant, prior.Subject, prior.BaseRevision, prior.Approved, prior.Provenance.WorkflowProof = tenant, l.Subject, l.BaseRevision, true, proof
		if err := json.Unmarshal(patches, &prior.Patches); err != nil {
			rows.Close()
			return err
		}
		var next timesession.LayeredState
		next, err = base.AppendLayer(prior)
		if err != nil {
			rows.Close()
			return fmt.Errorf("%w: persisted history: %v", ErrInvalid, err)
		}
		base = next
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if current > 0 && historyCount != int(current) {
		return fmt.Errorf("%w: history rows=%d current=%d", ErrInvalid, historyCount, current)
	}
	if _, err := base.AppendLayer(l); err != nil {
		return fmt.Errorf("%w: candidate: %v", ErrInvalid, err)
	}
	patches, err := json.Marshal(l.Patches)
	if err != nil {
		return ErrInvalid
	}
	err = tx.QueryRow(ctx, `INSERT INTO time_state_layer(tenant_id,id,subject_id,base_revision,revision,parent_revision,approved,actor_ref,reason,workflow_instance_id,workflow_plan_id,workflow_plan_digest,workflow_trace_id,workflow_node_id,workflow_attempt,workflow_version,original_observation_id,idempotency_key,digest,patches) VALUES($1,$2,$3,$4,$5,$6,true,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19::jsonb) RETURNING created_at`, tenant, out.LayerID, l.Subject, l.BaseRevision, l.Revision, l.ParentRevision, l.Provenance.Actor, l.Provenance.Reason, p.InstanceID, p.PlanID, p.PlanDigest, p.TraceID, p.NodeID, p.Attempt, p.Version, out.OriginalObservation, out.IdempotencyKey, l.Digest, patches).Scan(&out.CreatedAt)
	if err != nil {
		return err
	}
	affected, err := tx.Exec(ctx, `UPDATE time_state_layer_registry SET current_revision=$4,current_layer_id=$5,updated_at=now() WHERE tenant_id=$1 AND subject_id=$2 AND base_revision=$3 AND current_revision=$6`, tenant, l.Subject, l.BaseRevision, l.Revision, out.LayerID, l.ParentRevision)
	if err != nil || affected != 1 {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: projection update affected %d rows", ErrRevisionConflict, affected)
	}
	_, err = tx.Exec(ctx, `INSERT INTO time_state_layer_ledger(tenant_id,id,subject_id,revision,layer_id,event_type,digest,created_at) VALUES($1,$2,$3,$4,$5,'ACTIVATED',$6,now())`, tenant, out.LayerID+":activated", l.Subject, l.Revision, out.LayerID, l.Digest)
	return err
}

func validDigest(v string) bool {
	return strings.HasPrefix(v, "sha256:") && len(v) == len("sha256:")+64
}

// ReadStateLayerProjection reads the tenant-scoped current cursor and base.
func (s *Store) ReadStateLayerProjection(ctx context.Context, tenant, subject string) (StateLayerProjection, error) {
	if s == nil || tenant == "" || subject == "" {
		return StateLayerProjection{}, ErrInvalid
	}
	var out StateLayerProjection
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var base, current int64
		var payload []byte
		err := tx.QueryRow(ctx, `SELECT r.tenant_id,r.subject_id,b.base_revision,r.current_revision,r.current_layer_id,b.base_payload FROM time_state_layer_registry r JOIN time_state_base b ON b.tenant_id=r.tenant_id AND b.subject_id=r.subject_id WHERE r.tenant_id=$1 AND r.subject_id=$2`, tenant, subject).Scan(&out.Tenant, &out.Subject, &base, &current, &out.CurrentLayerID, &payload)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		out.BaseRevision, out.CurrentRevision, out.BasePayload = uint64(base), uint64(current), append(json.RawMessage(nil), payload...)
		return nil
	})
	return out, err
}

// ListStateLayers returns only approved layers at or before revision.
func (s *Store) ListStateLayers(ctx context.Context, tenant, subject string, revision uint64) ([]StateLayerRecord, error) {
	if s == nil || tenant == "" || subject == "" {
		return nil, ErrInvalid
	}
	out := []StateLayerRecord{}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,base_revision,revision,parent_revision,actor_ref,reason,workflow_instance_id,workflow_plan_id,workflow_plan_digest,workflow_trace_id,workflow_node_id,workflow_attempt,workflow_version,original_observation_id,idempotency_key,digest,patches,created_at FROM time_state_layer WHERE tenant_id=$1 AND subject_id=$2 AND approved=true AND revision <= $3 ORDER BY revision`, tenant, subject, revision)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r StateLayerRecord
			var proof timesession.WorkflowProof
			var patches []byte
			if err := rows.Scan(&r.LayerID, &r.Layer.BaseRevision, &r.Layer.Revision, &r.Layer.ParentRevision, &r.Layer.Provenance.Actor, &r.Layer.Provenance.Reason, &proof.InstanceID, &proof.PlanID, &proof.PlanDigest, &proof.TraceID, &proof.NodeID, &proof.Attempt, &proof.Version, &r.OriginalObservation, &r.IdempotencyKey, &r.Layer.Digest, &patches, &r.CreatedAt); err != nil {
				return err
			}
			r.Layer.Tenant, r.Layer.Subject, r.Layer.Approved, r.Layer.Provenance.WorkflowProof = tenant, subject, true, proof
			if err := json.Unmarshal(patches, &r.Layer.Patches); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// GetStateAsOf rebuilds the pure layered state from its immutable base and
// approved history at the requested revision.
func (s *Store) GetStateAsOf(ctx context.Context, tenant, subject string, revision uint64) (timesession.LayeredState, error) {
	projection, err := s.ReadStateLayerProjection(ctx, tenant, subject)
	if err != nil {
		return timesession.LayeredState{}, err
	}
	if revision > projection.CurrentRevision {
		return timesession.LayeredState{}, ErrRevisionConflict
	}
	var baseFacts struct {
		ClockOutAt time.Time `json:"clock_out_at"`
		Reason     string    `json:"reason"`
	}
	if err := json.Unmarshal(projection.BasePayload, &baseFacts); err != nil {
		return timesession.LayeredState{}, ErrInvalid
	}
	base, err := timesession.NewLayeredState(timesession.TimeSessionBaseFacts{Tenant: tenant, Subject: subject, BaseRevision: projection.BaseRevision, ClockOutAt: baseFacts.ClockOutAt, Reason: baseFacts.Reason})
	if err != nil {
		return timesession.LayeredState{}, ErrInvalid
	}
	rows, err := s.ListStateLayers(ctx, tenant, subject, revision)
	if err != nil {
		return timesession.LayeredState{}, err
	}
	for _, row := range rows {
		base, err = base.AppendLayer(row.Layer)
		if err != nil {
			return timesession.LayeredState{}, ErrInvalid
		}
	}
	return base, nil
}

// CaptureStateBase captures authoritative session facts before its first correction.
// The immutable snapshot survives later mutable projection updates.
func (s *Store) CaptureStateBase(ctx context.Context, tenant, subject string, expectedRevision uint64) error {
	if s == nil || tenant == "" || subject == "" || expectedRevision == 0 {
		return ErrInvalid
	}
	return s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenant+"|state-layer|"+subject); err != nil {
			return err
		}
		var prior int64
		err := tx.QueryRow(ctx, `SELECT base_revision FROM time_state_base WHERE tenant_id=$1 AND subject_id=$2`, tenant, subject).Scan(&prior)
		if err == nil {
			if prior != int64(expectedRevision) {
				return ErrRevisionConflict
			}
			return nil
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		var payload []byte
		var revision int64
		if err := tx.QueryRow(ctx, `SELECT revision,payload FROM time_session WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, subject).Scan(&revision, &payload); err != nil {
			return err
		}
		if revision != int64(expectedRevision) {
			return ErrRevisionConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO time_state_base(tenant_id,subject_id,base_revision,base_payload) VALUES($1,$2,$3,$4::jsonb)`, tenant, subject, revision, payload)
		return err
	})
}

// LookupStateLayer returns the immutable layer bound to one idempotency key.
func (s *Store) LookupStateLayer(ctx context.Context, tenant, subject, key string) (StateLayerRecord, bool, error) {
	if s == nil || tenant == "" || subject == "" || key == "" {
		return StateLayerRecord{}, false, ErrInvalid
	}
	var revision uint64
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT revision FROM time_state_layer WHERE tenant_id=$1 AND subject_id=$2 AND idempotency_key=$3`, tenant, subject, key).Scan(&revision)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return StateLayerRecord{}, false, nil
	}
	if err != nil {
		return StateLayerRecord{}, false, err
	}
	rows, err := s.ListStateLayers(ctx, tenant, subject, revision)
	if err != nil {
		return StateLayerRecord{}, false, err
	}
	for _, row := range rows {
		if row.IdempotencyKey == key {
			return row, true, nil
		}
	}
	return StateLayerRecord{}, false, ErrNotFound
}
