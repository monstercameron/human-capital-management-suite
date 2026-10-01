package agentrunstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TenantTxRunner is satisfied by the isolated agentstore.Store. Each callback
// runs in a transaction bound to the agent database's local tenant projection.
type TenantTxRunner interface {
	RunTenantTx(context.Context, uuid.UUID, func(dbport.Tx) error) error
}

// AdmissionRepository appends one accepted or refused request into the
// immutable agent_run_request inbox for a single tenant.
type AdmissionRepository struct {
	runner     TenantTxRunner
	tenantID   uuid.UUID
	tenant     string
	sourceKeys AdmissionSourceKeyResolver
}

// AdmissionSourceKeyResolver recovers a native occurrence key from its owning
// durable source. The repository verifies it against the persisted digest.
type AdmissionSourceKeyResolver interface {
	ResolveSourceKey(context.Context, agentrun.Request) (string, error)
}

// NewAdmissionRepositoryWithSourceResolver binds a source-owner recovery port
// before the repository is shared with serving workers.
func NewAdmissionRepositoryWithSourceResolver(runner TenantTxRunner, tenantID uuid.UUID, tenantRef values.TenantId, resolver AdmissionSourceKeyResolver) (*AdmissionRepository, error) {
	if resolver == nil {
		return nil, agentrun.ErrInvalidRequest
	}
	repository, err := NewAdmissionRepository(runner, tenantID, tenantRef)
	if err != nil {
		return nil, err
	}
	repository.sourceKeys = resolver
	return repository, nil
}

var _ agentrun.Store = (*AdmissionRepository)(nil)

// NewAdmissionRepository creates a tenant-scoped request store. tenantRef is
// the canonical application tenant identifier used by agentrun.Request;
// tenantID is its UUID in the isolated agent store.
func NewAdmissionRepository(runner TenantTxRunner, tenantID uuid.UUID, tenantRef values.TenantId) (*AdmissionRepository, error) {
	ref := string(tenantRef)
	if runner == nil || tenantID == uuid.Nil || ref == "" || strings.TrimSpace(ref) != ref {
		return nil, fmt.Errorf("%w: runner, tenant UUID and canonical tenant reference are required", agentrun.ErrInvalidRequest)
	}
	return &AdmissionRepository{runner: runner, tenantID: tenantID, tenant: ref}, nil
}

// CreateOrGet atomically persists or replays a source-key decision. The raw
// source key is hashed for the unique index and omitted from request_payload.
func (r *AdmissionRepository) CreateOrGet(ctx context.Context, candidate agentrun.Record) (record agentrun.Record, created bool, err error) {
	if r == nil || r.runner == nil {
		return agentrun.Record{}, false, fmt.Errorf("%w: nil admission repository", agentrun.ErrInvalidRequest)
	}
	if candidate.Request.Source.TenantID != r.tenant {
		return agentrun.Record{}, false, fmt.Errorf("%w: request tenant %q is outside repository scope %q", agentrun.ErrInvalidRequest, candidate.Request.Source.TenantID, r.tenant)
	}
	if err := agentrun.ValidateAdmissionRecord(candidate); err != nil {
		return agentrun.Record{}, false, err
	}
	sourceHash, err := agentrun.SourceKeyDigest(candidate.Request.Source)
	if err != nil {
		return agentrun.Record{}, false, err
	}
	payload, principal, audience, contextScope, budget, authority, err := encodeAdmission(candidate)
	if err != nil {
		return agentrun.Record{}, false, err
	}
	request := candidate.Request
	err = r.runner.RunTenantTx(ctx, r.tenantID, func(tx dbport.Tx) error {
		n, execErr := tx.Exec(ctx, `INSERT INTO agent_run_request (
			tenant_id, request_id, source_kind, source_key_digest, source_ref, request_digest, request_payload,
			decision, refusal_code, authority_snapshot, admitted_at, deadline, agent_id, agent_version, agent_digest,
			installation_id, legal_entity_id, principal_chain, purpose, audience, context_scope, budget, cause_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10::jsonb,$11,$12,$13,$14,$15,$16,$17,$18::jsonb,$19,$20::jsonb,$21::jsonb,$22::jsonb,$23)
			ON CONFLICT DO NOTHING`, r.tenantID, candidate.ID, string(request.Source.Kind), sourceHash, nullableText(request.Source.Ref),
			candidate.RequestDigest, string(payload), string(candidate.Decision), nullableText(candidate.RefusalCode), nullableJSON(authority),
			candidate.AdmittedAt.UTC(), request.Deadline.UTC(), request.Agent.AgentID, request.Agent.Version, request.Agent.Digest,
			request.InstallationID, request.LegalEntity, string(principal), request.Purpose, string(audience), string(contextScope), string(budget), request.CauseID)
		if execErr != nil {
			return fmt.Errorf("agentrunstore: insert admission request: %w", execErr)
		}
		if n == 1 {
			record, created = cloneAdmissionRecord(candidate), true
			return nil
		}
		var existingID, existingDigest, decision, refusal string
		var authorityJSON []byte
		var admittedAt time.Time
		readErr := tx.QueryRow(ctx, `SELECT request_id, request_digest, decision, COALESCE(refusal_code,''),
			COALESCE(authority_snapshot::text,'null'), admitted_at FROM agent_run_request
			WHERE tenant_id=$1 AND source_kind=$2 AND source_key_digest=$3`, r.tenantID, string(request.Source.Kind), sourceHash).
			Scan(&existingID, &existingDigest, &decision, &refusal, &authorityJSON, &admittedAt)
		if errors.Is(readErr, dbport.ErrNoRows) {
			return agentrun.ErrSourceConflict
		}
		if readErr != nil {
			return fmt.Errorf("agentrunstore: read admission replay: %w", readErr)
		}
		if existingID != candidate.ID || existingDigest != candidate.RequestDigest {
			return agentrun.ErrSourceConflict
		}
		prior := cloneAdmissionRecord(candidate)
		prior.Decision, prior.RefusalCode, prior.AdmittedAt = agentrun.Decision(decision), refusal, admittedAt.UTC()
		prior.Authority = agentrun.AuthoritySnapshot{}
		if string(authorityJSON) != "null" {
			if err := json.Unmarshal(authorityJSON, &prior.Authority); err != nil {
				return fmt.Errorf("agentrunstore: decode admission authority: %w", err)
			}
		}
		if err := agentrun.ValidateAdmissionRecord(prior); err != nil {
			return fmt.Errorf("agentrunstore: stored admission record is invalid: %w", err)
		}
		record = prior
		return nil
	})
	if err != nil {
		return agentrun.Record{}, false, err
	}
	return record, created, nil
}

func encodeAdmission(record agentrun.Record) (payload, principal, audience, contextScope, budget, authority []byte, err error) {
	request := record.Request
	request.Source.Key = ""
	if payload, err = json.Marshal(request); err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("agentrunstore: encode request payload: %w", err)
	}
	if principal, err = json.Marshal(request.Principal); err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("agentrunstore: encode principal chain: %w", err)
	}
	if audience, err = json.Marshal(request.Audience); err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("agentrunstore: encode audience: %w", err)
	}
	if contextScope, err = json.Marshal(request.Context); err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("agentrunstore: encode context scope: %w", err)
	}
	if budget, err = json.Marshal(request.Budget); err != nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("agentrunstore: encode budget: %w", err)
	}
	if record.Decision == agentrun.DecisionAccepted {
		if authority, err = json.Marshal(record.Authority); err != nil {
			return nil, nil, nil, nil, nil, nil, fmt.Errorf("agentrunstore: encode authority snapshot: %w", err)
		}
	}
	return payload, principal, audience, contextScope, budget, authority, nil
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableJSON(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return string(value)
}

func cloneAdmissionRecord(record agentrun.Record) agentrun.Record {
	record.Request.Deadline = record.Request.Deadline.UTC()
	record.AdmittedAt = record.AdmittedAt.UTC()
	return record
}
