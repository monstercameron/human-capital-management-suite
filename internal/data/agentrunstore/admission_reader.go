package agentrunstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// GetByID loads one admission from this repository's fixed tenant scope.
// The source key is reconstructed with the canonical server converter and
// checked against the stored digest before the record is returned.
func (r *AdmissionRepository) GetByID(ctx context.Context, id string) (agentrun.Record, error) {
	if r == nil || r.runner == nil || ctx == nil || id == "" {
		return agentrun.Record{}, fmt.Errorf("%w: repository and admission id are required", agentrun.ErrInvalidRequest)
	}
	var record agentrun.Record
	var payload, authority []byte
	var decision, refusal, sourceKeyDigest string
	err := r.runner.RunTenantTx(ctx, r.tenantID, func(tx dbport.Tx) error {
		var sourceKind, sourceRef string
		err := tx.QueryRow(ctx, `SELECT request_id,request_digest,request_payload,decision,COALESCE(refusal_code,''),
			COALESCE(authority_snapshot::text,'null'),admitted_at,source_kind,COALESCE(source_ref,''),source_key_digest
			FROM agent_run_request WHERE tenant_id=$1 AND request_id=$2`, r.tenantID, id).
			Scan(&record.ID, &record.RequestDigest, &payload, &decision, &refusal, &authority, &record.AdmittedAt, &sourceKind, &sourceRef, &sourceKeyDigest)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrAdmissionNotFound
		}
		if err != nil {
			return fmt.Errorf("agentrunstore: read admission: %w", err)
		}
		if err := json.Unmarshal(payload, &record.Request); err != nil {
			return fmt.Errorf("agentrunstore: decode admission request: %w", err)
		}
		record.Request.Source.Kind = agentrun.SourceKind(sourceKind)
		record.Request.Source.Ref = sourceRef
		if record.Request.Source.TenantID != r.tenant {
			return agentrun.ErrSourceConflict
		}
		if record.Request.Source.Kind != agentrun.SourcePersonaMention {
			return nil
		}
		if record.Request.Persona == nil {
			return fmt.Errorf("%w: persona identity is required", agentrun.ErrInvalidRequest)
		}
		err = tx.QueryRow(ctx, `SELECT invocation_id FROM persona_invocations WHERE tenant_id=$1 AND post_id=$2 AND persona_id=$3
			AND invoker_id=$4 AND conversation_id=$5 AND thread_id=$6 AND persona_version=$7 AND installation_id=$8 AND mode='ON_BEHALF_OF'`,
			r.tenantID, sourceRef, record.Request.Persona.ID, record.Request.Principal.InvokerID,
			record.Request.Audience.ID, record.Request.Context.ID, record.Request.Persona.Version, record.Request.InstallationID).Scan(&record.Request.Source.Key)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrAdmissionNotFound
		}
		if err != nil {
			return fmt.Errorf("agentrunstore: restore persona invocation identity: %w", err)
		}
		return nil
	})
	if err != nil {
		return agentrun.Record{}, err
	}
	// A source owner may need another database transaction to recover its
	// native occurrence key. Release the admission transaction first.
	if record.Request.Source.Kind != agentrun.SourcePersonaMention {
		if r.sourceKeys != nil {
			key, err := r.sourceKeys.ResolveSourceKey(ctx, record.Request)
			if err != nil {
				return agentrun.Record{}, err
			}
			record.Request.Source.Key = key
		} else {
			source, err := (agentrun.CanonicalSourceConverter{}).ConvertSource(ctx, record.Request)
			if err != nil {
				return agentrun.Record{}, err
			}
			record.Request.Source = source
		}
	}
	record.Decision = agentrun.Decision(decision)
	record.RefusalCode = refusal
	if string(authority) != "null" {
		if err := json.Unmarshal(authority, &record.Authority); err != nil {
			return agentrun.Record{}, fmt.Errorf("agentrunstore: decode admission authority: %w", err)
		}
	}
	keyDigest, err := agentrun.SourceKeyDigest(record.Request.Source)
	if err != nil || keyDigest != sourceKeyDigest {
		return agentrun.Record{}, agentrun.ErrSourceConflict
	}
	if record.ID != id {
		return agentrun.Record{}, agentrun.ErrSourceConflict
	}
	if err := agentrun.ValidateAdmissionRecord(record); err != nil {
		return agentrun.Record{}, fmt.Errorf("agentrunstore: stored admission is invalid: %w", err)
	}
	return record, nil
}

// ListBySource returns up to limit immutable admissions from this tenant's
// exact source kind. Every record passes the same identity checks as GetByID.
func (r *AdmissionRepository) ListBySource(ctx context.Context, kind agentrun.SourceKind, limit int) ([]agentrun.Record, error) {
	return r.listBySource(ctx, kind, limit, false, time.Time{}, time.Time{}, "")
}

// ListPendingBySource bounds recovery work after excluding settled executions.
func (r *AdmissionRepository) ListPendingBySource(ctx context.Context, kind agentrun.SourceKind, limit int) ([]agentrun.Record, error) {
	return r.listBySource(ctx, kind, limit, true, time.Time{}, time.Time{}, "")
}

// ListRunnableBySource excludes live leases, reconciliation and elapsed deadlines.
func (r *AdmissionRepository) ListRunnableBySource(ctx context.Context, kind agentrun.SourceKind, limit int, at time.Time) ([]agentrun.Record, error) {
	if at.IsZero() {
		return nil, agentrun.ErrInvalidRequest
	}
	return r.ListRunnableBySourceAfter(ctx, kind, limit, at, time.Time{}, "")
}

// ListRunnableBySourceAfter advances across records whose current authority has changed.
func (r *AdmissionRepository) ListRunnableBySourceAfter(ctx context.Context, kind agentrun.SourceKind, limit int, at, afterAt time.Time, afterID string) ([]agentrun.Record, error) {
	if at.IsZero() || afterAt.IsZero() != (afterID == "") {
		return nil, agentrun.ErrInvalidRequest
	}
	return r.listBySource(ctx, kind, limit, true, at, afterAt, afterID)
}

func (r *AdmissionRepository) listBySource(ctx context.Context, kind agentrun.SourceKind, limit int, pending bool, runnableAt, afterAt time.Time, afterID string) ([]agentrun.Record, error) {
	if r == nil || r.runner == nil || ctx == nil || limit < 1 || limit > 1000 {
		return nil, agentrun.ErrInvalidRequest
	}
	switch kind {
	case agentrun.SourceChat, agentrun.SourcePersonaMention, agentrun.SourceAPI, agentrun.SourceEvent, agentrun.SourceSchedule, agentrun.SourceWorkflow:
	default:
		return nil, agentrun.ErrInvalidRequest
	}
	ids := make([]string, 0, limit)
	err := r.runner.RunTenantTx(ctx, r.tenantID, func(tx dbport.Tx) error {
		query := `SELECT request_id FROM agent_run_request WHERE tenant_id=$1 AND source_kind=$2
			ORDER BY admitted_at,request_id COLLATE "C" LIMIT $3`
		if pending {
			query = `SELECT r.request_id FROM agent_run_request r
				LEFT JOIN agent_run_execution e ON e.tenant_id=r.tenant_id AND e.admission_id=r.request_id
				WHERE r.tenant_id=$1 AND r.source_kind=$2 AND r.decision='ACCEPTED'
				  AND (e.admission_id IS NULL OR e.state IN ('READY','RUNNING','RECONCILING'))
				ORDER BY r.admitted_at,r.request_id COLLATE "C" LIMIT $3`
		}
		args := []any{r.tenantID, string(kind), limit}
		if !runnableAt.IsZero() {
			query = `SELECT r.request_id FROM agent_run_request r
				LEFT JOIN agent_run_execution e ON e.tenant_id=r.tenant_id AND e.admission_id=r.request_id
				WHERE r.tenant_id=$1 AND r.source_kind=$2 AND r.decision='ACCEPTED' AND r.deadline>$4
				  AND (e.admission_id IS NULL OR e.state='READY' OR (e.state='RUNNING' AND e.lease_until<=$4))
				AND ($5::timestamptz IS NULL OR (r.admitted_at,r.request_id COLLATE "C") > ($5,$6::text COLLATE "C"))
				ORDER BY r.admitted_at,r.request_id COLLATE "C" LIMIT $3`
			args = append(args, runnableAt.UTC())
			var cursorAt any
			if !afterAt.IsZero() {
				cursorAt = afterAt.UTC()
			}
			args = append(args, cursorAt, afterID)
		}
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	records := make([]agentrun.Record, 0, len(ids))
	for _, id := range ids {
		record, err := r.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// ErrAdmissionNotFound marks an admission absent from a tenant-scoped reader.
var ErrAdmissionNotFound = errors.New("agentrunstore: admission not found")
