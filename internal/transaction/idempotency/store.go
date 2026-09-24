package idempotency

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Executor is the minimal database capability this package needs. A
// [dbport.Tx] and a [dbport.Conn] both satisfy it. [Guard] always passes a
// [dbport.Tx] it does not itself open or finish, so that its Reserve, the
// caller's effect and its Complete are three statements of one transaction
// the caller commits or rolls back as a whole.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// Store is the durable port TX-006 declares: Reserve, Complete, Lookup and
// Expire. [Guard] is built entirely on top of it and never reaches into
// PostgreSQL itself.
type Store interface {
	// Reserve attempts to reserve scope under digest and policy. It reports
	// created=true when this call's INSERT won the race: the returned Record
	// is the fresh RESERVED row, computed at now. It reports created=false
	// when scope was already reserved (by this call losing a concurrent
	// race, or by an earlier call entirely): the returned Record is
	// whatever already exists, unchanged, regardless of whether its digest
	// matches -- Reserve itself never decides conflict, it only ever reports
	// what is actually stored.
	//
	// Reserve refuses outright, before writing anything, when policy itself
	// cannot be honored ([CodeRetentionTooShort]) or when digest is not a
	// well-formed canonical digest ([CodeInvalidRecord]).
	Reserve(ctx context.Context, ex Executor, scope Scope, digest string, policy RetentionPolicy, now time.Time) (rec Record, created bool, err error)

	// Complete moves a RESERVED record to COMPLETED, attaching identity, at
	// now. It refuses with [CodeNotReserved] when no RESERVED record exists
	// for scope -- already completed, expired and swept, or never reserved.
	Complete(ctx context.Context, ex Executor, scope Scope, identity ResultIdentity, now time.Time) (Record, error)

	// Lookup reads the record for scope, if any. found is false when no
	// record exists; it is never used to distinguish "not found" from
	// "belongs to another tenant" -- row level security makes those the
	// same answer at the SQL level.
	Lookup(ctx context.Context, ex Executor, scope Scope) (rec Record, found bool, err error)

	// Expire processes every record for tenant whose expires_at is strictly
	// before cutoff. Expiring records are deleted; permanent-tombstone records
	// lose their result references and keep only the keyed request digest.
	// It is an explicit, policy-owned sweep and never reads a wall clock.
	Expire(ctx context.Context, ex Executor, tenant uuid.UUID, cutoff time.Time) (processed int64, err error)
}

// PostgresStore implements [Store] over the idempotency migrations. Its
// retention registry is immutable policy configuration; every method still
// takes its database [Executor] explicitly.
type PostgresStore struct {
	// RetentionRegistry controls class selection by capability. The zero value
	// uses [DefaultCapabilityRetentionRegistry].
	RetentionRegistry CapabilityRetentionRegistry
}

var _ Store = PostgresStore{}

const recordColumns = `tenant_id, capability_id, effect_scope, idempotency_key,
	request_digest, status, retention_class, result_ref, event_ref, effect_identity, evidence_id, action_plan_binding_ref, compensated_by_event_ref,
	created_at, expires_at`

// Reserve implements [Store.Reserve] with one statement: an
// INSERT ... ON CONFLICT (the table's own primary key) DO NOTHING RETURNING.
// PostgreSQL evaluates that exactly as it would a plain INSERT racing the
// same unique constraint -- a concurrent inserter for the same scope blocks
// until the first transaction commits or rolls back, then either finds the
// row now visible (and this INSERT returns no row) or finds it gone (and
// this INSERT proceeds normally) -- so the race is decided entirely by the
// database's own MVCC, never by a lock this package takes itself.
func (s PostgresStore) Reserve(
	ctx context.Context, ex Executor, scope Scope, digest string, policy RetentionPolicy, now time.Time,
) (Record, bool, error) {
	if err := scope.Validate(); err != nil {
		return Record{}, false, err
	}
	if !ValidDigest(digest) {
		return Record{}, false, refuse(CodeInvalidRecord, scope, "request digest %q is not a well-formed canonical digest", digest)
	}
	class, err := s.retentionClass(scope, policy.Class)
	if err != nil {
		return Record{}, false, err
	}
	policy.Class = class
	if err := policy.Validate(); err != nil {
		return Record{}, false, err
	}

	createdAt := now.UTC()
	expiresAt := createdAt.Add(policy.Retention)

	row := ex.QueryRow(ctx, `
		INSERT INTO idempotency_record (`+recordColumns+`)
		VALUES ($1, $2, $3, $4, $5, 'RESERVED', $6, NULL, NULL, NULL, NULL, NULL, NULL, $7, $8)
		ON CONFLICT (tenant_id, capability_id, effect_scope, idempotency_key) DO NOTHING
		RETURNING `+recordColumns,
		scope.Tenant, scope.Capability, scope.EffectScope, scope.Key,
		digest, class, createdAt, expiresAt)

	rec, err := scanRecord(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			// Lost the race. Whatever is there now is committed (that is
			// exactly what made our own INSERT see a conflict), so a plain
			// read finds it.
			existing, found, lookupErr := PostgresStore{}.Lookup(ctx, ex, scope)
			if lookupErr != nil {
				return Record{}, false, lookupErr
			}
			if !found {
				return Record{}, false, wrap(CodeStorageFailed, scope, err,
					"reservation lost its race but no existing record is visible")
			}
			return existing, false, nil
		}
		return Record{}, false, wrap(CodeStorageFailed, scope, err, "insert idempotency reservation")
	}
	return rec, true, nil
}

// CloseCompensated binds a completed original effect to its verified inverse.
// The caller supplies the same transaction that wrote the compensation event.
func (PostgresStore) CloseCompensated(ctx context.Context, ex Executor, scope Scope, eventRef string) (Record, error) {
	if err := scope.Validate(); err != nil {
		return Record{}, err
	}
	if strings.TrimSpace(eventRef) == "" {
		return Record{}, refuse(CodeInvalidRecord, scope, "compensation event reference is required")
	}
	row := ex.QueryRow(ctx, `UPDATE idempotency_record SET status='COMPENSATED', compensated_by_event_ref=$1
		WHERE tenant_id=$2 AND capability_id=$3 AND effect_scope=$4 AND idempotency_key=$5 AND status='COMPLETED'
		RETURNING `+recordColumns, eventRef, scope.Tenant, scope.Capability, scope.EffectScope, scope.Key)
	rec, err := scanRecord(row)
	if err == nil {
		return rec, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return Record{}, wrap(CodeStorageFailed, scope, err, "close compensated idempotency record")
	}
	current, found, lookupErr := (PostgresStore{}).Lookup(ctx, ex, scope)
	if lookupErr != nil {
		return Record{}, lookupErr
	}
	if found && current.Status == StatusCompensated && current.CompensatedByRef == eventRef {
		return current, nil
	}
	return Record{}, refuse(CodeNotReserved, scope, "no completed original record to close or compensation event conflicts")
}

func (s PostgresStore) retentionClass(scope Scope, requested RetentionClass) (RetentionClass, error) {
	registry := s.RetentionRegistry
	if registry.exact == nil && registry.prefixes == nil {
		registry = DefaultCapabilityRetentionRegistry()
	}
	class, found := registry.classFor(scope.Capability)
	if !found {
		if protectedCapabilityName(scope.Capability) {
			return "", refuse(CodeRetentionPolicyMissing, scope,
				"capability requires an entry in the authoritative retention registry")
		}
		class = RetentionExpiring
	}
	if requested != "" && requested != class {
		return "", refuse(CodeRetentionPolicyConflict, scope,
			"per-request retention class conflicts with the authoritative capability policy")
	}
	return class, nil
}

// Complete implements [Store.Complete]. The UPDATE only ever matches a row
// that is still RESERVED, so a second Complete against an already-completed
// scope -- or a scope that expired and was swept -- refuses with
// [CodeNotReserved] rather than silently overwriting a stored identity.
func (PostgresStore) Complete(
	ctx context.Context, ex Executor, scope Scope, identity ResultIdentity, now time.Time,
) (Record, error) {
	if err := scope.Validate(); err != nil {
		return Record{}, err
	}
	if identity.Empty() {
		return Record{}, refuse(CodeInvalidRecord, scope, "completion carries no result/event/effect/evidence identity")
	}
	// now is accepted (rather than this package reading a wall clock) even
	// though the table has no separate completion timestamp column today,
	// so a future column never has to widen this signature.
	_ = now

	row := ex.QueryRow(ctx, `
		UPDATE idempotency_record SET
			status = 'COMPLETED',
			result_ref = $1,
			event_ref = $2,
			effect_identity = $3,
			evidence_id = $4,
			action_plan_binding_ref = $5
		WHERE tenant_id = $6 AND capability_id = $7 AND effect_scope = $8 AND idempotency_key = $9
		  AND status = 'RESERVED'
		RETURNING `+recordColumns,
		nullableText(identity.ResultRef), nullableText(identity.EventRef),
		nullableText(identity.EffectIdentity), nullableText(identity.EvidenceID), nullableText(identity.ActionPlanBindingRef),
		scope.Tenant, scope.Capability, scope.EffectScope, scope.Key)

	rec, err := scanRecord(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return Record{}, refuse(CodeNotReserved, scope,
				"no reserved record to complete (already completed, expired, or never reserved)")
		}
		return Record{}, wrap(CodeStorageFailed, scope, err, "complete idempotency record")
	}
	return rec, nil
}

// Lookup implements [Store.Lookup].
func (PostgresStore) Lookup(ctx context.Context, ex Executor, scope Scope) (Record, bool, error) {
	if err := scope.Validate(); err != nil {
		return Record{}, false, err
	}
	row := ex.QueryRow(ctx, `
		SELECT `+recordColumns+` FROM idempotency_record
		WHERE tenant_id = $1 AND capability_id = $2 AND effect_scope = $3 AND idempotency_key = $4`,
		scope.Tenant, scope.Capability, scope.EffectScope, scope.Key)
	rec, err := scanRecord(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return Record{}, false, nil
		}
		return Record{}, false, wrap(CodeStorageFailed, scope, err, "read idempotency record")
	}
	return rec, true, nil
}

// Expire implements [Store.Expire].
func (PostgresStore) Expire(ctx context.Context, ex Executor, tenant uuid.UUID, cutoff time.Time) (int64, error) {
	if tenant == uuid.Nil {
		return 0, refuse(CodeInvalidScope, Scope{}, "expire requires a tenant")
	}
	compensated, err := ex.Exec(ctx, `UPDATE idempotency_record SET
		result_ref = NULL, event_ref = NULL, effect_identity = NULL, evidence_id = NULL, expires_at = NULL
		WHERE tenant_id = $1 AND retention_class = 'PERMANENT_TOMBSTONE'
		  AND expires_at < $2 AND status = 'COMPENSATED'`, tenant, cutoff.UTC())
	if err != nil {
		return 0, wrap(CodeStorageFailed, Scope{Tenant: tenant}, err, "compact expired compensated idempotency records")
	}
	tombstones, err := ex.Exec(ctx, `UPDATE idempotency_record SET
		status = 'TOMBSTONE', result_ref = NULL, event_ref = NULL, compensated_by_event_ref = NULL,
		effect_identity = NULL, evidence_id = NULL, expires_at = NULL
		WHERE tenant_id = $1 AND retention_class = 'PERMANENT_TOMBSTONE'
		  AND expires_at < $2 AND status IN ('RESERVED', 'COMPLETED')`, tenant, cutoff.UTC())
	if err != nil {
		return 0, wrap(CodeStorageFailed, Scope{Tenant: tenant}, err, "compact expired idempotency records")
	}
	removed, err := ex.Exec(ctx, `DELETE FROM idempotency_record
		WHERE tenant_id = $1 AND retention_class = 'EXPIRING' AND expires_at < $2`,
		tenant, cutoff.UTC())
	if err != nil {
		return 0, wrap(CodeStorageFailed, Scope{Tenant: tenant}, err, "expire idempotency records")
	}
	return compensated + tombstones + removed, nil
}

func scanRecord(row dbport.Row) (Record, error) {
	var (
		rec                                                                                          Record
		status                                                                                       string
		resultRef, eventRef, effectIdentity, evidenceID, actionPlanBindingRef, compensatedByEventRef *string
		expiresAt                                                                                    *time.Time
	)
	err := row.Scan(
		&rec.Scope.Tenant, &rec.Scope.Capability, &rec.Scope.EffectScope, &rec.Scope.Key,
		&rec.RequestDigest, &status, &rec.RetentionClass, &resultRef, &eventRef, &effectIdentity, &evidenceID, &actionPlanBindingRef, &compensatedByEventRef,
		&rec.CreatedAt, &expiresAt)
	if err != nil {
		return Record{}, err
	}
	rec.Status = Status(status)
	rec.Identity = ResultIdentity{
		ResultRef:            derefText(resultRef),
		EventRef:             derefText(eventRef),
		EffectIdentity:       derefText(effectIdentity),
		EvidenceID:           derefText(evidenceID),
		ActionPlanBindingRef: derefText(actionPlanBindingRef),
	}
	rec.CompensatedByRef = derefText(compensatedByEventRef)
	rec.CreatedAt = rec.CreatedAt.UTC()
	if expiresAt != nil {
		rec.ExpiresAt = expiresAt.UTC()
	}
	return rec, nil
}

// nullableText binds "" as SQL NULL, so an unset optional reference is
// absent rather than an empty string that later reads as a real value.
func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func derefText(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
