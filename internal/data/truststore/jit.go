package truststore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
)

// JITGrantRecord is the lossless storage projection of the columns in
// jit_grant. Scope contains the non-column grant request coordinates.
type JITGrantRecord struct {
	TenantID  uuid.UUID
	RowID     uuid.UUID
	GrantID   string
	Revision  uint64
	State     string
	Requester string
	Approver  string
	Scope     json.RawMessage
	NotBefore time.Time
	ExpiresAt time.Time
	Revoked   bool
}

// JITGrantScope is the JSON shape written into jit_grant.scope.
type JITGrantScope struct {
	Role          string   `json:"role"`
	TicketRef     string   `json:"ticket_ref"`
	Justification string   `json:"justification"`
	Capabilities  []string `json:"capabilities"`
	Fields        []string `json:"fields,omitempty"`
	Purpose       string   `json:"purpose"`
}

// PutJITGrant appends the initial grant revision. A grant identity cannot be
// silently replaced: revision 1 is the only accepted initial revision and a
// repeated revision is returned as a typed duplicate.
func (s *Store) PutJITGrant(ctx context.Context, tenantID uuid.UUID, in JITGrantRecord) error {
	if tenantID == uuid.Nil || in.TenantID != uuid.Nil && in.TenantID != tenantID {
		return failure(CodeTenantRequired, "jit_grant", in.GrantID, errors.New("tenant is missing or mismatched"))
	}
	if in.Revision == 0 {
		return invalid("jit_grant", "revision", "revision starts at 1")
	}
	if in.Revision != 1 {
		return failure(CodeVersionConflict, "jit_grant", in.GrantID, errors.New("the initial revision must be 1"))
	}
	if err := validateJITGrant(in); err != nil {
		return err
	}
	return s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO jit_grant
				(tenant_id,row_id,grant_id,revision,state,requester,approver,scope,not_before,expires_at,revoked)
			VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10,$11)`,
			tenantID, nonNilUUID(in.RowID), in.GrantID, int64(in.Revision), in.State,
			in.Requester, in.Approver, in.Scope, in.NotBefore.UTC(), in.ExpiresAt.UTC(), in.Revoked)
		if err != nil {
			classified := classifyJITWriteError(in.GrantID, err)
			if CodeOf(classified) == CodeDuplicate {
				return failure(CodeDuplicateRevision, "jit_grant", in.GrantID, err)
			}
			return classified
		}
		return nil
	})
}

// SaveJITGrant is an adapter from the pure jit domain object to its durable
// control projection. The domain object remains the authority for the grant's
// policy validity; this method only records the already-approved shape.
func (s *Store) SaveJITGrant(ctx context.Context, grant *jit.Grant, revision uint64, state string) error {
	if grant == nil {
		return invalid("jit_grant", "grant", "grant is required")
	}
	tenantID, err := parseTenant(grant.Tenant.String())
	if err != nil {
		return err
	}
	scope, err := jsonValue(JITGrantScope{
		Role: string(grant.Role), TicketRef: grant.TicketRef, Justification: grant.Justification,
		Capabilities: append([]string(nil), grant.Capabilities...), Fields: append([]string(nil), grant.Fields...),
		Purpose: grant.Purpose,
	})
	if err != nil {
		return failure(CodeInvalid, "jit_grant", grant.ID, err)
	}
	_, _, _, revoked := grant.Revocation()
	return s.PutJITGrant(ctx, tenantID, JITGrantRecord{
		TenantID: tenantID, RowID: uuid.New(), GrantID: grant.ID, Revision: revision,
		State: state, Requester: grant.Principal, Approver: grant.Approver, Scope: scope,
		NotBefore: grant.IssuedAt, ExpiresAt: grant.ExpiresAt, Revoked: revoked,
	})
}

// LoadJITGrant loads one exact immutable revision projection.
func (s *Store) LoadJITGrant(ctx context.Context, tenantID uuid.UUID, grantID string, revision uint64) (JITGrantRecord, error) {
	if tenantID == uuid.Nil {
		return JITGrantRecord{}, failure(CodeTenantRequired, "jit_grant", grantID, errors.New("tenant is required"))
	}
	var out JITGrantRecord
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		var storedRevision int64
		err := tx.QueryRow(ctx, `
			SELECT tenant_id,row_id,grant_id,revision,state,requester,COALESCE(approver,''),scope,not_before,expires_at,revoked
			FROM jit_grant WHERE tenant_id=$1 AND grant_id=$2 AND revision=$3`,
			tenantID, grantID, int64(revision)).Scan(
			&out.TenantID, &out.RowID, &out.GrantID, &storedRevision, &out.State, &out.Requester,
			&out.Approver, &out.Scope, &out.NotBefore, &out.ExpiresAt, &out.Revoked)
		if errors.Is(err, dbport.ErrNoRows) {
			return failure(CodeNotFound, "jit_grant", grantID, errors.New("revision not found"))
		}
		if err != nil {
			return failure(CodeDatabase, "jit_grant", grantID, err)
		}
		out.Revision = uint64(storedRevision)
		out.NotBefore = out.NotBefore.UTC()
		out.ExpiresAt = out.ExpiresAt.UTC()
		return nil
	})
	return out, err
}

// UpdateJITGrant appends one revision-fenced state change. The previous row is
// never overwritten, so the grant's control history remains recoverable while
// a retry with the same expected revision receives CodeVersionConflict.
func (s *Store) UpdateJITGrant(ctx context.Context, tenantID uuid.UUID, grantID string, expectedRevision uint64, state string, revoked bool) (JITGrantRecord, error) {
	if tenantID == uuid.Nil {
		return JITGrantRecord{}, failure(CodeTenantRequired, "jit_grant", grantID, errors.New("tenant is required"))
	}
	if expectedRevision == 0 || state == "" {
		return JITGrantRecord{}, invalid("jit_grant", grantID, "expected revision and state are required")
	}
	var out JITGrantRecord
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `
			SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
			tenantID.String()+":trust-jit-grant:"+grantID); err != nil {
			return failure(CodeDatabase, "jit_grant", grantID, err)
		}
		var current JITGrantRecord
		var storedRevision int64
		err := tx.QueryRow(ctx, `
			SELECT tenant_id,row_id,grant_id,revision,state,requester,
				COALESCE(approver,''),scope,not_before,expires_at,revoked
			FROM jit_grant
			WHERE tenant_id=$1 AND grant_id=$2
			ORDER BY revision DESC LIMIT 1`, tenantID, grantID).Scan(
			&current.TenantID, &current.RowID, &current.GrantID, &storedRevision, &current.State,
			&current.Requester, &current.Approver, &current.Scope, &current.NotBefore,
			&current.ExpiresAt, &current.Revoked)
		if errors.Is(err, dbport.ErrNoRows) {
			return failure(CodeVersionConflict, "jit_grant", grantID, fmt.Errorf("expected revision %d is stale", expectedRevision))
		}
		if err != nil {
			return failure(CodeDatabase, "jit_grant", grantID, err)
		}
		current.Revision = uint64(storedRevision)
		if current.Revision != expectedRevision {
			return failure(CodeVersionConflict, "jit_grant", grantID, fmt.Errorf("expected revision %d is stale; current revision is %d", expectedRevision, current.Revision))
		}
		out = current
		out.RowID = uuid.New()
		out.Revision = expectedRevision + 1
		out.State = state
		out.Revoked = revoked
		_, err = tx.Exec(ctx, `
			INSERT INTO jit_grant
				(tenant_id,row_id,grant_id,revision,state,requester,approver,scope,not_before,expires_at,revoked)
			VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10,$11)`,
			out.TenantID, out.RowID, out.GrantID, int64(out.Revision), out.State,
			out.Requester, out.Approver, out.Scope, out.NotBefore.UTC(), out.ExpiresAt.UTC(), out.Revoked)
		if err != nil {
			return classifyJITWriteError(grantID, err)
		}
		out.NotBefore = out.NotBefore.UTC()
		out.ExpiresAt = out.ExpiresAt.UTC()
		return nil
	})
	return out, err
}

// JITEvidenceRecord is one immutable jit_evidence_record row.
type JITEvidenceRecord struct {
	TenantID      uuid.UUID
	RowID         uuid.UUID
	GrantID       string
	EvidenceKind  string
	Detail        json.RawMessage
	At            time.Time
	EventSequence uint64
}

// AppendJITEvidence records the next event for a grant. Event sequence is
// caller-owned and must be strictly next, which makes gaps and duplicate
// evidence visible rather than silently accepted.
func (s *Store) AppendJITEvidence(ctx context.Context, in JITEvidenceRecord) error {
	if in.TenantID == uuid.Nil || in.RowID == uuid.Nil {
		return failure(CodeTenantRequired, "jit_evidence_record", in.GrantID, errors.New("tenant and row id are required"))
	}
	if in.EventSequence == 0 || in.GrantID == "" || in.EvidenceKind == "" || in.At.IsZero() {
		return invalid("jit_evidence_record", in.GrantID, "grant, kind, timestamp and positive sequence are required")
	}
	return s.withTenant(ctx, in.TenantID, func(tx dbport.Tx) error {
		var latest int64
		err := tx.QueryRow(ctx, `
			SELECT COALESCE(MAX(event_sequence),0) FROM jit_evidence_record
			WHERE tenant_id=$1 AND grant_id=$2`, in.TenantID, in.GrantID).Scan(&latest)
		if err != nil {
			return failure(CodeDatabase, "jit_evidence_record", in.GrantID, err)
		}
		if in.EventSequence <= uint64(latest) {
			return failure(CodeDuplicateEvent, "jit_evidence_record", in.GrantID, errors.New("event sequence already recorded"))
		}
		if in.EventSequence != uint64(latest)+1 {
			return failure(CodeVersionConflict, "jit_evidence_record", in.GrantID, errors.New("event sequence is not the next event"))
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO jit_evidence_record
				(tenant_id,row_id,grant_id,evidence_kind,detail,at,event_sequence)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			in.TenantID, in.RowID, in.GrantID, in.EvidenceKind, nullableJSON(in.Detail), in.At.UTC(), int64(in.EventSequence))
		if err != nil {
			return classifyJITWriteError(in.GrantID, err)
		}
		return nil
	})
}

func validateJITGrant(in JITGrantRecord) error {
	if in.GrantID == "" || in.State == "" || in.Requester == "" || in.NotBefore.IsZero() || in.ExpiresAt.IsZero() {
		return invalid("jit_grant", in.GrantID, "grant id, state, requester and validity window are required")
	}
	if !in.ExpiresAt.After(in.NotBefore) {
		return invalid("jit_grant", in.GrantID, "expires_at must be after not_before")
	}
	if len(in.Scope) == 0 {
		return invalid("jit_grant", in.GrantID, "scope is required")
	}
	return nil
}

func classifyJITWriteError(key string, err error) error {
	return classifyWriteError("jit_grant", key, err)
}

func nonNilUUID(value uuid.UUID) uuid.UUID {
	if value == uuid.Nil {
		return uuid.New()
	}
	return value
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}

// ActiveJITGrants returns the requester's grants that are current at now and
// name capability, rebuilt through jit.Restore so a durable record is only
// authority when it still satisfies every grant rule. Only the latest
// revision of each grant counts; a revoked, expired or not-yet-valid grant is
// omitted. tenantKey is the tenant identity the restored grants carry.
func (s *Store) ActiveJITGrants(ctx context.Context, tenantID uuid.UUID, tenantKey values.TenantId, requester, capability string, now time.Time) ([]*jit.Grant, error) {
	if requester == "" || capability == "" || now.IsZero() {
		return nil, invalid("jit_grant", "requester", "requester, capability and instant are required")
	}
	var out []*jit.Grant
	err := s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT DISTINCT ON (grant_id) grant_id, state, requester, COALESCE(approver,''), scope, not_before, expires_at, revoked
			FROM jit_grant
			WHERE tenant_id = $1 AND requester = $2
			ORDER BY grant_id, revision DESC`, tenantID, requester)
		if err != nil {
			return failure(CodeDatabase, "jit_grant", requester, err)
		}
		defer rows.Close()
		for rows.Next() {
			var id, state, req, approver string
			var scope json.RawMessage
			var notBefore, expires time.Time
			var revoked bool
			if err := rows.Scan(&id, &state, &req, &approver, &scope, &notBefore, &expires, &revoked); err != nil {
				return failure(CodeDatabase, "jit_grant", requester, err)
			}
			if revoked || now.Before(notBefore) || !now.Before(expires) {
				continue
			}
			var sc JITGrantScope
			if err := json.Unmarshal(scope, &sc); err != nil || !slices.Contains(sc.Capabilities, capability) {
				continue
			}
			grant, err := jit.Restore(jit.Stored{ID: id, Principal: req, Tenant: tenantKey, Role: jit.Role(sc.Role), TicketRef: sc.TicketRef,
				Justification: sc.Justification, Capabilities: sc.Capabilities, Fields: sc.Fields, Purpose: sc.Purpose,
				Approver: approver, IssuedAt: notBefore, ExpiresAt: expires})
			if err != nil {
				// A record that no longer satisfies the grant rules is not authority.
				continue
			}
			out = append(out, grant)
		}
		return rows.Err()
	})
	return out, err
}
