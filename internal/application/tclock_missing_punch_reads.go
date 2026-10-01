package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
	transporttimeclock "github.com/monstercameron/human-capital-management-suite/internal/transport/timeclock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TimestoreMissingPunchReadStore is the production read adapter. Queries are
// tenant-bound by RunTenantTx and pending rows are additionally constrained to
// the worker scope resolved by MissingPunchReadAuthorization.
type TimestoreMissingPunchReadStore struct{ Store *timestore.Store }

// GetMissingPunchCorrectionContext loads a session, its immutable original IN
// observation, and pending requests bound to that session.
func (s TimestoreMissingPunchReadStore) GetMissingPunchCorrectionContext(ctx context.Context, tenant, sessionID string) (transporttimeclock.MissingPunchCorrectionContext, []clockservice.MissingPunchRecord, error) {
	if s.Store == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(sessionID) == "" {
		return transporttimeclock.MissingPunchCorrectionContext{}, nil, clockservice.ErrMissingPunchUnavailable
	}
	var out transporttimeclock.MissingPunchCorrectionContext
	var rows []clockservice.MissingPunchRecord
	err := s.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var payload []byte
		var assignment string
		if err := tx.QueryRow(ctx, `SELECT id,worker_ref,assignment_ref,revision,payload FROM time_session WHERE tenant_id=$1 AND id=$2`, tenant, sessionID).Scan(&out.SessionID, &out.WorkerRef, &assignment, &out.Revision, &payload); err != nil {
			return mapMissingPunchReadError(err)
		}
		_, out.OriginalWorkflowInstanceRef, out.Timezone, out.PeriodRef, out.PeriodClosed = sessionReadFacts(payload)
		if err := tx.QueryRow(ctx, `SELECT workflow_id,instance_id FROM time_workflow_session_run WHERE tenant_id=$1 AND session_id=$2`, tenant, sessionID).Scan(&out.OriginalWorkflowID, &out.OriginalWorkflowInstanceRef); err != nil {
			return mapMissingPunchReadError(err)
		}
		var requestID, observationID string
		if err := tx.QueryRow(ctx, `SELECT id,original_observation_id FROM missed_punch_workflow_request WHERE tenant_id=$1 AND session_id=$2 AND status='PENDING' ORDER BY updated_at DESC,id LIMIT 1`, tenant, sessionID).Scan(&requestID, &observationID); err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return mapMissingPunchReadError(err)
		}
		if observationID == "" {
			if err := tx.QueryRow(ctx, `SELECT payload->>'observation_id' FROM time_session_event WHERE tenant_id=$1 AND session_id=$2 AND payload ? 'observation_id' AND kind <> 'MISSING_PUNCH_CORRECTION' ORDER BY sequence LIMIT 1`, tenant, sessionID).Scan(&observationID); err != nil && !errors.Is(err, dbport.ErrNoRows) {
				return mapMissingPunchReadError(err)
			}
		}
		if observationID == "" {
			return clockservice.ErrMissingPunchUnavailable
		}
		if observationID != "" {
			var eventType string
			var occurredAt time.Time
			if err := tx.QueryRow(ctx, `SELECT event_type,occurred_at,timezone FROM time_observation WHERE tenant_id=$1 AND id=$2 AND worker_ref=$3 AND assignment_ref=$4`, tenant, observationID, out.WorkerRef, assignment).Scan(&eventType, &occurredAt, &out.Timezone); err != nil {
				return mapMissingPunchReadError(err)
			}
			eventType = normalizeClockInType(eventType)
			if eventType == "" {
				return clockservice.ErrMissingPunchUnavailable
			}
			out.OriginalIn = &transporttimeclock.MissingPunchPunchFact{EventType: eventType, ObservationID: observationID, OccurredAt: occurredAt.UTC()}
			if requestID != "" {
				row, err := readMissingPunchRecord(ctx, tx, tenant, requestID)
				if err != nil {
					return err
				}
				rows = append(rows, row)
			}
		}
		return nil
	})
	if err != nil {
		return transporttimeclock.MissingPunchCorrectionContext{}, nil, err
	}
	return out, rows, nil
}

// ListPendingMissingPunch returns only pending rows for the server-resolved
// worker scope and bounded page size.
func (s TimestoreMissingPunchReadStore) ListPendingMissingPunch(ctx context.Context, tenant string, workers []string, limit uint32) ([]clockservice.MissingPunchRecord, error) {
	if s.Store == nil || strings.TrimSpace(tenant) == "" || len(workers) == 0 || limit == 0 || limit > 100 {
		return nil, clockservice.ErrMissingPunchUnavailable
	}
	var rows []clockservice.MissingPunchRecord
	err := s.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		r, err := tx.Query(ctx, `SELECT id FROM missed_punch_workflow_request WHERE tenant_id=$1 AND worker_ref=ANY($2) AND status='PENDING' ORDER BY updated_at DESC,id LIMIT $3`, tenant, workers, limit)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var id string
			if err := r.Scan(&id); err != nil {
				return err
			}
			row, err := readMissingPunchRecord(ctx, tx, tenant, id)
			if err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return r.Err()
	})
	return rows, err
}

func readMissingPunchRecord(ctx context.Context, tx dbport.Tx, tenant, id string) (clockservice.MissingPunchRecord, error) {
	var row clockservice.MissingPunchRecord
	var expected int64
	err := tx.QueryRow(ctx, `SELECT id,tenant_id,worker_ref,session_id,original_observation_id,claimed_out_at,reason,requested_by,status,request_revision,expected_session_revision,period_closed,original_workflow_instance_ref,workflow_instance_id,workflow_trace_id,workflow_node_id,workflow_plan_digest,workflow_attempt,workflow_instance_version,created_at,updated_at FROM missed_punch_workflow_request WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&row.ID, &row.TenantID, &row.WorkerRef, &row.SessionID, &row.OriginalObservationID, &row.ClaimedOutAt, &row.Reason, &row.RequestedBy, &row.Decision, &row.Revision, &expected, &row.PeriodClosed, &row.OriginalWorkflowInstanceRef, &row.WorkflowInstanceID, &row.WorkflowTraceID, &row.WorkflowNodeID, &row.WorkflowPlanDigest, &row.WorkflowAttempt, &row.WorkflowInstanceVersion, &row.CreatedAt, &row.DecidedAt)
	row.ExpectedSessionRevision = uint64(expected)
	if err != nil {
		return clockservice.MissingPunchRecord{}, mapMissingPunchReadError(err)
	}
	return row, nil
}

func mapMissingPunchReadError(err error) error {
	if errors.Is(err, dbport.ErrNoRows) {
		return timestore.ErrNotFound
	}
	return err
}

func sessionReadFacts(payload []byte) (workflow, instance, timezone, period string, closed bool) {
	var raw struct {
		WorkflowRef                 string `json:"workflow_ref"`
		OriginalWorkflowInstanceRef string `json:"original_workflow_instance_ref"`
		Timezone                    string `json:"timezone"`
		PeriodRef                   string `json:"period_ref"`
		PeriodClosed                bool   `json:"period_closed"`
	}
	if json.Unmarshal(payload, &raw) != nil {
		return "", "", "", "", false
	}
	if raw.WorkflowRef == "" {
		return "", raw.OriginalWorkflowInstanceRef, raw.Timezone, raw.PeriodRef, raw.PeriodClosed
	}
	return "", raw.WorkflowRef, raw.Timezone, raw.PeriodRef, raw.PeriodClosed
}

func normalizeClockInType(eventType string) string {
	switch strings.ToUpper(strings.TrimSpace(eventType)) {
	case "IN", "CLOCK_IN", "PUNCH_IN":
		return "CLOCK_IN"
	default:
		return ""
	}
}

// MissingPunchReadStore supplies immutable, already-projected correction
// reads. Implementations must apply tenant and current authority scope in the
// query; callers never provide a worker or supervisor filter.
type MissingPunchReadStore interface {
	GetMissingPunchCorrectionContext(context.Context, string, string) (transporttimeclock.MissingPunchCorrectionContext, []clockservice.MissingPunchRecord, error)
	ListPendingMissingPunch(context.Context, string, []string, uint32) ([]clockservice.MissingPunchRecord, error)
}

// MissingPunchReadAuthorization checks current authority before any read
// lookup. Implementations must fail closed and avoid existence disclosure.
type MissingPunchReadAuthorization interface {
	AuthorizeMissingPunchContext(context.Context, *trust.Principal, string, string) error
	AuthorizeMissingPunchPending(context.Context, *trust.Principal, string) ([]string, error)
}

// GetCorrectionContext returns the server-scoped correction context and its
// pending requests for the authenticated principal.
func (b *MissingPunchBridge) GetCorrectionContext(ctx context.Context, p *trust.Principal, sessionID string) (transporttimeclock.MissingPunchCorrectionContext, []transporttimeclock.MissingPunchCorrection, error) {
	if b == nil || b.ReadStore == nil || b.ReadAuthorization == nil {
		return transporttimeclock.MissingPunchCorrectionContext{}, nil, clockservice.ErrMissingPunchUnavailable
	}
	if p == nil || p.SubjectKind() != trust.SubjectKindHuman || strings.TrimSpace(sessionID) == "" {
		return transporttimeclock.MissingPunchCorrectionContext{}, nil, clockservice.ErrInvalidPrincipal
	}
	tenant := p.Tenant().String()
	if err := b.ReadAuthorization.AuthorizeMissingPunchContext(ctx, p, tenant, sessionID); err != nil {
		return transporttimeclock.MissingPunchCorrectionContext{}, nil, err
	}
	projection, rows, err := b.ReadStore.GetMissingPunchCorrectionContext(ctx, tenant, sessionID)
	if err != nil {
		return transporttimeclock.MissingPunchCorrectionContext{}, nil, err
	}
	if projection.SessionID != sessionID || strings.TrimSpace(projection.WorkerRef) == "" || strings.TrimSpace(projection.OriginalWorkflowID) == "" || strings.TrimSpace(projection.OriginalWorkflowInstanceRef) == "" {
		return transporttimeclock.MissingPunchCorrectionContext{}, nil, clockservice.ErrMissingPunchUnavailable
	}
	for _, row := range rows {
		if row.SessionID != sessionID || row.WorkerRef != projection.WorkerRef || row.TenantID != tenant {
			return transporttimeclock.MissingPunchCorrectionContext{}, nil, clockservice.ErrMissingPunchUnavailable
		}
	}
	corrections, err := missingPunchReadCorrections(rows)
	if err != nil {
		return transporttimeclock.MissingPunchCorrectionContext{}, nil, err
	}
	return projection, corrections, nil
}

// ListPendingCorrections returns only rows selected by the state-layer's
// current supervisor scope and bounded page size.
func (b *MissingPunchBridge) ListPendingCorrections(ctx context.Context, p *trust.Principal, pageSize uint32) ([]transporttimeclock.MissingPunchCorrection, error) {
	if b == nil || b.ReadStore == nil || b.ReadAuthorization == nil {
		return nil, clockservice.ErrMissingPunchUnavailable
	}
	if p == nil || p.SubjectKind() != trust.SubjectKindHuman {
		return nil, clockservice.ErrInvalidPrincipal
	}
	if pageSize == 0 || pageSize > 100 {
		return nil, clockservice.ErrMissingPunchInvalid
	}
	tenant := p.Tenant().String()
	workers, err := b.ReadAuthorization.AuthorizeMissingPunchPending(ctx, p, tenant)
	if err != nil {
		return nil, err
	}
	if len(workers) == 0 {
		return []transporttimeclock.MissingPunchCorrection{}, nil
	}
	rows, err := b.ReadStore.ListPendingMissingPunch(ctx, tenant, workers, pageSize)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(workers))
	for _, worker := range workers {
		allowed[worker] = struct{}{}
	}
	for _, row := range rows {
		if row.TenantID != tenant || row.Decision != "PENDING" {
			return nil, clockservice.ErrMissingPunchUnavailable
		}
		if _, ok := allowed[row.WorkerRef]; !ok {
			return nil, clockservice.ErrMissingPunchUnavailable
		}
	}
	corrections, err := missingPunchReadCorrections(rows)
	if err != nil {
		return nil, err
	}
	return corrections, nil
}

func missingPunchReadCorrections(rows []clockservice.MissingPunchRecord) ([]transporttimeclock.MissingPunchCorrection, error) {
	result := make([]transporttimeclock.MissingPunchCorrection, 0, len(rows))
	for _, row := range rows {
		state := strings.TrimSpace(row.Decision)
		if state == "" {
			state = "PENDING"
		}
		if state == "PENDING" {
			if row.ID == "" || row.WorkerRef == "" || row.SessionID == "" || row.ClaimedOutAt.IsZero() || row.Revision == 0 {
				return nil, clockservice.ErrMissingPunchUnavailable
			}
			result = append(result, transporttimeclock.MissingPunchCorrection{RequestID: row.ID, WorkerRef: row.WorkerRef, SessionID: row.SessionID, ClaimedEventType: "CLOCK_OUT", ClaimedOccurredAt: row.ClaimedOutAt, RequestReason: row.Reason, Status: state, DecisionReason: row.Decision, Revision: row.Revision})
			continue
		}
		correction, err := missingPunchCorrection(row, state, row.Decision)
		if err != nil {
			return nil, err
		}
		result = append(result, correction)
	}
	return result, nil
}
