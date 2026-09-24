// Package schedulingstore persists appointment requirements, resource types,
// worker availability facts, clock-device registrations, published schedule
// snapshots, and the durable fencing state for worker-initiated shift offers.
// It owns no scheduling decisions; it validates, tenant-scopes, and applies
// storage transitions.
package schedulingstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/appointment"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/availability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	CodeInvalid         = "SCHEDULING_INVALID_ROW"
	CodeDuplicate       = "SCHEDULING_DUPLICATE_REVISION"
	CodeVersionConflict = "SCHEDULING_STALE_CAS"
	CodeNotFound        = "SCHEDULING_NOT_FOUND"
	CodeTenant          = "SCHEDULING_TENANT_MISMATCH"
	CodeDatabase        = "SCHEDULING_DATABASE"
	CodeDuplicateDevice = "SCHEDULING_DUPLICATE_DEVICE_VERSION"
)

var (
	ErrInvalid         = errors.New("schedulingstore: invalid row")
	ErrDuplicate       = errors.New("schedulingstore: duplicate revision")
	ErrVersionConflict = errors.New("schedulingstore: stale revision")
	ErrNotFound        = errors.New("schedulingstore: row not found")
	ErrTenantMismatch  = errors.New("schedulingstore: tenant mismatch")
)

// CodedError is a typed persistence refusal. Code is stable for telemetry;
// errors.Is still exposes the more useful sentinel to callers.
type CodedError struct {
	code  string
	cause error
}

func (e *CodedError) Error() string { return e.code + ": " + e.cause.Error() }
func (e *CodedError) Unwrap() error { return e.cause }
func (e *CodedError) Code() string  { return e.code }

// CodeOf returns the scheduling storage code carried by err.
func CodeOf(err error) string {
	if err == nil {
		return ""
	}
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return ""
}

// DB is the transaction capability required by Store.
type DB interface{ dbport.Beginner }

// Executor is the explicit transaction capability used by the Tx helpers.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// Store is the PostgreSQL implementation of scheduling domain storage ports.
type Store struct{ db DB }

var _ appointment.Repository = (*Store)(nil)

// New constructs a scheduling store over db.
func New(db DB) *Store { return &Store{db: db} }

// NewStore is the descriptive constructor alias used by data-plane callers.
func NewStore(db DB) *Store { return New(db) }

func (s *Store) withTenant(ctx context.Context, tenant values.TenantId, fn func(dbport.Tx, uuid.UUID) error) error {
	if ctx == nil {
		return coded(CodeInvalid, ErrInvalid, "context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return coded(CodeInvalid, ErrInvalid, "database capability is required")
	}
	if err := tenant.Validate(); err != nil {
		return coded(CodeTenant, ErrTenantMismatch, err.Error())
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return coded(CodeDatabase, err, "begin transaction")
	}
	tenantID, err := resolveTenant(ctx, tx, tenant)
	if err == nil {
		err = tenancy.WithTenant(ctx, tx, tenantID)
	}
	if err == nil {
		err = fn(tx, tenantID)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return coded(CodeDatabase, err, "commit transaction")
	}
	return nil
}

func resolveTenant(ctx context.Context, q dbport.Querier, tenant values.TenantId) (uuid.UUID, error) {
	if id, err := uuid.Parse(tenant.String()); err == nil && id != uuid.Nil {
		return id, nil
	}
	var id uuid.UUID
	if err := q.QueryRow(ctx, `SELECT tenant_id FROM tenant WHERE tenant_key=$1`, tenant.String()).Scan(&id); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return uuid.Nil, coded(CodeNotFound, ErrNotFound, "tenant "+tenant.String())
		}
		return uuid.Nil, coded(CodeDatabase, err, "resolve tenant")
	}
	return id, nil
}

func coded(code string, cause error, detail string) error {
	if detail != "" {
		cause = fmt.Errorf("%w: %s", cause, detail)
	}
	return &CodedError{code: code, cause: cause}
}

func mapWriteError(code, name string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return coded(code, ErrDuplicate, name)
		case "23502", "23503", "23514", "22P02":
			return coded(CodeInvalid, ErrInvalid, name+": "+pgErr.Message)
		}
	}
	return coded(CodeDatabase, err, name)
}

// PutRequirement appends one requirement revision. The optional expected
// revision is a compare-and-set fence; omitted means the current revision.
func (s *Store) PutRequirement(ctx context.Context, tenant values.TenantId, in appointment.Requirement, expected ...uint64) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		return s.PutRequirementTx(ctx, tx, tenantID, in, expected...)
	})
}

// SaveRequirement is an alias for PutRequirement.
func (s *Store) SaveRequirement(ctx context.Context, tenant values.TenantId, in appointment.Requirement, expected ...uint64) error {
	return s.PutRequirement(ctx, tenant, in, expected...)
}

// PutRequirementTx appends a requirement in an already tenant-scoped
// transaction.
func (s *Store) PutRequirementTx(ctx context.Context, ex Executor, tenantID uuid.UUID, in appointment.Requirement, expected ...uint64) error {
	if len(expected) > 1 {
		return coded(CodeInvalid, ErrInvalid, "at most one expected revision is allowed")
	}
	normalized, payload, start, end, err := encodeRequirement(in)
	if err != nil {
		return err
	}
	if requestTenant := normalized.RequestTenant(); requestTenant != "tenant-placeholder" && requestTenant != values.TenantId(tenantID.String()) {
		return coded(CodeTenant, ErrTenantMismatch, "requirement references another tenant")
	}
	if err := checkNextTenant(ctx, ex, tenantID, "appointment_requirement", "requirement_id", normalized.ID, normalized.Revision, expected); err != nil {
		return err
	}
	purpose := normalized.Purpose
	if purpose == "" {
		purpose = string(normalized.PurposeKind)
	}
	locationClass := string(normalized.LocationClass)
	if locationClass == "" {
		locationClass = normalized.Location.Mode
	}
	state := normalized.State
	if state == "" {
		state = "DRAFT"
	}
	_, err = ex.Exec(ctx, `
		INSERT INTO appointment_requirement (
			row_id, tenant_id, requirement_id, version, revision, purpose,
			participants, duration, window_start, window_end, location_class,
			privacy_class, lead_time, state, canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8 * interval '1 microsecond',$9,$10,$11,$12,$13 * interval '1 microsecond',$14,$15)`,
		uuid.New(), tenantID, normalized.ID, normalized.Version, int64(normalized.Revision), purpose,
		payload, normalized.Duration.Microseconds(), start, end, locationClass, normalized.PrivacyClass,
		normalized.LeadTime.Microseconds(), state, storageDigest(normalized.CanonicalDigest))
	if err != nil {
		return mapWriteError(CodeDuplicate, "appointment_requirement", err)
	}
	return nil
}

// GetRequirement loads one immutable requirement revision.
func (s *Store) GetRequirement(ctx context.Context, tenant values.TenantId, id string, revision uint64) (appointment.Requirement, error) {
	var out appointment.Requirement
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var rowID uuid.UUID
		var storedRevision int64
		var payload []byte
		var duration, lead time.Duration
		var start, end *time.Time
		var locationClass, purpose, state, digest string
		err := tx.QueryRow(ctx, `
			SELECT row_id,requirement_id,version,revision,purpose,participants,duration,
				window_start,window_end,location_class,privacy_class,lead_time,state,canonical_digest
			FROM appointment_requirement
			WHERE tenant_id=$1 AND requirement_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(
			&rowID, &out.ID, &out.Version, &storedRevision, &purpose, &payload, &duration,
			&start, &end, &locationClass, &out.PrivacyClass, &lead, &state, &digest)
		if errors.Is(err, dbport.ErrNoRows) {
			return coded(CodeNotFound, appointment.ErrRequirementNotFound, id)
		}
		if err != nil {
			return coded(CodeDatabase, err, "load appointment_requirement")
		}
		if err := decodeRequirementPayload(payload, &out); err != nil {
			return coded(CodeDatabase, err, "decode appointment_requirement")
		}
		out.Revision = uint64(storedRevision)
		out.Purpose = purpose
		out.LocationClass = appointment.LocationClass(locationClass)
		out.LeadTime = lead
		out.State = state
		out.CanonicalDigest = domainDigest(digest)
		if start != nil {
			var interval values.EffectiveInterval
			var intervalErr error
			if end == nil {
				interval, intervalErr = values.NewOpenInstantInterval(values.NewInstant(start.UTC()))
			} else {
				interval, intervalErr = values.NewInstantInterval(values.NewInstant(start.UTC()), values.NewInstant(end.UTC()))
			}
			if intervalErr != nil {
				return coded(CodeDatabase, intervalErr, "decode appointment window")
			}
			out.WindowInterval = interval
			out.Window.Start = start.UTC()
			if end != nil {
				out.Window.End = end.UTC()
			}
		}
		_ = rowID
		return nil
	})
	return out, err
}

// LoadRequirement is an alias for GetRequirement.
func (s *Store) LoadRequirement(ctx context.Context, tenant values.TenantId, id string, revision uint64) (appointment.Requirement, error) {
	return s.GetRequirement(ctx, tenant, id, revision)
}

// ListRequirementVersions returns a requirement's revisions in ascending order.
func (s *Store) ListRequirementVersions(ctx context.Context, tenant values.TenantId, id string) ([]appointment.Requirement, error) {
	var out []appointment.Requirement
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM appointment_requirement WHERE tenant_id=$1 AND requirement_id=$2 ORDER BY revision`, tenantID, id)
		if err != nil {
			return coded(CodeDatabase, err, "list appointment_requirement")
		}
		var revisions []int64
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return coded(CodeDatabase, err, "scan appointment_requirement revision")
			}
			revisions = append(revisions, revision)
		}
		if err := rows.Err(); err != nil {
			return coded(CodeDatabase, err, "list appointment_requirement")
		}
		rows.Close()
		if len(revisions) == 0 {
			return coded(CodeNotFound, appointment.ErrRequirementNotFound, id)
		}
		for _, revision := range revisions {
			requirement, err := s.getRequirementTx(ctx, tx, tenantID, id, uint64(revision))
			if err != nil {
				return err
			}
			out = append(out, requirement)
		}
		return nil
	})
	return out, err
}

func (s *Store) getRequirementTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, id string, revision uint64) (appointment.Requirement, error) {
	var out appointment.Requirement
	var rowID uuid.UUID
	var storedRevision int64
	var payload []byte
	var duration, lead time.Duration
	var start, end *time.Time
	var purpose, locationClass, state, digest string
	err := tx.QueryRow(ctx, `SELECT row_id,requirement_id,version,revision,purpose,participants,duration,window_start,window_end,location_class,privacy_class,lead_time,state,canonical_digest FROM appointment_requirement WHERE tenant_id=$1 AND requirement_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(&rowID, &out.ID, &out.Version, &storedRevision, &purpose, &payload, &duration, &start, &end, &locationClass, &out.PrivacyClass, &lead, &state, &digest)
	if err != nil {
		return out, err
	}
	if err := decodeRequirementPayload(payload, &out); err != nil {
		return out, err
	}
	out.Revision, out.Purpose, out.LocationClass, out.LeadTime, out.State = uint64(storedRevision), purpose, appointment.LocationClass(locationClass), lead, state
	out.CanonicalDigest = domainDigest(digest)
	if start != nil {
		if end == nil {
			out.WindowInterval, err = values.NewOpenInstantInterval(values.NewInstant(start.UTC()))
		} else {
			out.WindowInterval, err = values.NewInstantInterval(values.NewInstant(start.UTC()), values.NewInstant(end.UTC()))
		}
		if err != nil {
			return out, err
		}
		out.Window.Start = start.UTC()
		if end != nil {
			out.Window.End = end.UTC()
		}
	}
	_ = rowID
	return out, nil
}

// PutResourceType appends one immutable resource type revision.
func (s *Store) PutResourceType(ctx context.Context, tenant values.TenantId, in appointment.ResourceType, expected ...uint64) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		return s.PutResourceTypeTx(ctx, tx, tenantID, in, expected...)
	})
}

// SaveResourceType is an alias for PutResourceType.
func (s *Store) SaveResourceType(ctx context.Context, tenant values.TenantId, in appointment.ResourceType, expected ...uint64) error {
	return s.PutResourceType(ctx, tenant, in, expected...)
}

// PutResourceTypeTx appends a resource type in an existing tenant transaction.
func (s *Store) PutResourceTypeTx(ctx context.Context, ex Executor, tenantID uuid.UUID, in appointment.ResourceType, expected ...uint64) error {
	if len(expected) > 1 {
		return coded(CodeInvalid, ErrInvalid, "at most one expected revision is allowed")
	}
	normalized, err := appointment.NewResourceType(in)
	if err != nil {
		return coded(CodeInvalid, ErrInvalid, err.Error())
	}
	if err := checkNextTenant(ctx, ex, tenantID, "resource_type", "resource_type_id", normalized.ID, normalized.Revision, expected); err != nil {
		return err
	}
	mode, limit, err := normalized.CapacitySemantics()
	if err != nil {
		return coded(CodeInvalid, ErrInvalid, err.Error())
	}
	_, err = ex.Exec(ctx, `INSERT INTO resource_type (row_id,tenant_id,resource_type_id,version,revision,name,kind,capacity_mode,capacity_limit,privacy_class,canonical_digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, uuid.New(), tenantID, normalized.ID, normalized.Version, int64(normalized.Revision), normalized.Name, string(normalized.Kind), string(mode), limit, nullableText(normalized.PrivacyClass), storageDigest(normalized.CanonicalDigest))
	if err != nil {
		return mapWriteError(CodeDuplicate, "resource_type", err)
	}
	return nil
}

// GetResourceType loads a resource type projection. Qualification is not a
// column in migration 00114 and therefore remains empty in this projection.
func (s *Store) GetResourceType(ctx context.Context, tenant values.TenantId, id string, revision uint64) (appointment.ResourceType, error) {
	var out appointment.ResourceType
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var rowID uuid.UUID
		var storedRevision int64
		var capacityText string
		var privacy, digest string
		err := tx.QueryRow(ctx, `SELECT row_id,resource_type_id,version,revision,name,kind,capacity_mode,capacity_limit::text,privacy_class,canonical_digest FROM resource_type WHERE tenant_id=$1 AND resource_type_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(&rowID, &out.ID, &out.Version, &storedRevision, &out.Name, &out.Kind, &out.CapacityMode, &capacityText, &privacy, &digest)
		if errors.Is(err, dbport.ErrNoRows) {
			return coded(CodeNotFound, appointment.ErrResourceTypeNotFound, id)
		}
		if err != nil {
			return coded(CodeDatabase, err, "load resource_type")
		}
		out.Revision = uint64(storedRevision)
		out.PrivacyClass = privacy
		out.CanonicalDigest = domainDigest(digest)
		if capacityText != "" {
			capacity, parseErr := strconv.ParseFloat(capacityText, 64)
			if parseErr != nil || capacity < 0 || capacity > math.MaxInt64 || math.Trunc(capacity) != capacity {
				return coded(CodeDatabase, ErrInvalid, "resource_type capacity is not an integer")
			}
			out.CapacityLimit = int64(capacity)
			out.Capacity = int(out.CapacityLimit)
		}
		_ = rowID
		return nil
	})
	return out, err
}

// LoadResourceType is an alias for GetResourceType.
func (s *Store) LoadResourceType(ctx context.Context, tenant values.TenantId, id string, revision uint64) (appointment.ResourceType, error) {
	return s.GetResourceType(ctx, tenant, id, revision)
}

// ListResourceTypeVersions returns a resource type's revisions in order.
func (s *Store) ListResourceTypeVersions(ctx context.Context, tenant values.TenantId, id string) ([]appointment.ResourceType, error) {
	var out []appointment.ResourceType
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM resource_type WHERE tenant_id=$1 AND resource_type_id=$2 ORDER BY revision`, tenantID, id)
		if err != nil {
			return coded(CodeDatabase, err, "list resource_type")
		}
		var revisions []int64
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return coded(CodeDatabase, err, "scan resource_type revision")
			}
			revisions = append(revisions, revision)
		}
		if err := rows.Err(); err != nil {
			return coded(CodeDatabase, err, "list resource_type")
		}
		rows.Close()
		if len(revisions) == 0 {
			return coded(CodeNotFound, appointment.ErrResourceTypeNotFound, id)
		}
		for _, revision := range revisions {
			resource, err := s.getResourceTypeTx(ctx, tx, tenantID, id, uint64(revision))
			if err != nil {
				return err
			}
			out = append(out, resource)
		}
		return nil
	})
	return out, err
}

func (s *Store) getResourceTypeTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, id string, revision uint64) (appointment.ResourceType, error) {
	var out appointment.ResourceType
	var rowID uuid.UUID
	var storedRevision int64
	var capacityText, privacy, digest string
	err := tx.QueryRow(ctx, `SELECT row_id,resource_type_id,version,revision,name,kind,capacity_mode,capacity_limit::text,privacy_class,canonical_digest FROM resource_type WHERE tenant_id=$1 AND resource_type_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(&rowID, &out.ID, &out.Version, &storedRevision, &out.Name, &out.Kind, &out.CapacityMode, &capacityText, &privacy, &digest)
	if err != nil {
		return out, err
	}
	out.Revision, out.PrivacyClass, out.CanonicalDigest = uint64(storedRevision), privacy, domainDigest(digest)
	if capacityText != "" {
		capacity, err := strconv.ParseFloat(capacityText, 64)
		if err != nil || math.Trunc(capacity) != capacity {
			return out, errors.New("resource_type capacity is not an integer")
		}
		out.CapacityLimit, out.Capacity = int64(capacity), int(capacity)
	}
	_ = rowID
	return out, nil
}

// PutWorkerAvailability appends one availability stream revision.
func (s *Store) PutWorkerAvailability(ctx context.Context, tenant values.TenantId, in availability.WorkerAvailability) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		return s.PutWorkerAvailabilityTx(ctx, tx, tenantID, in)
	})
}

// SaveAvailability is an alias for PutWorkerAvailability.
func (s *Store) SaveAvailability(ctx context.Context, tenant values.TenantId, in availability.WorkerAvailability) error {
	return s.PutWorkerAvailability(ctx, tenant, in)
}

// AppendWorkerAvailability derives the tenant from the domain fact.
func (s *Store) AppendWorkerAvailability(ctx context.Context, in availability.WorkerAvailability) error {
	return s.PutWorkerAvailability(ctx, in.Worker.Tenant, in)
}

// PutWorkerAvailabilityTx appends availability in an existing tenant transaction.
func (s *Store) PutWorkerAvailabilityTx(ctx context.Context, ex Executor, tenantID uuid.UUID, in availability.WorkerAvailability) error {
	if err := in.Validate(); err != nil {
		return coded(CodeInvalid, ErrInvalid, err.Error())
	}
	if in.Worker.Tenant != values.TenantId(tenantID.String()) {
		return coded(CodeTenant, ErrTenantMismatch, "availability references another tenant")
	}
	if in.AvailabilityID.Tenant != values.TenantId(tenantID.String()) {
		return coded(CodeTenant, ErrTenantMismatch, "availability id references another tenant")
	}
	revision, ok := in.Revision.Sequence()
	if !ok || revision == 0 || revision > math.MaxInt64 {
		return coded(CodeInvalid, ErrInvalid, "availability revision must be a positive sequence")
	}
	start, end, err := instantBounds(in.Effective)
	if err != nil {
		return err
	}
	worker, err := uuidField(in.Worker, "worker")
	if err != nil {
		return err
	}
	employment, err := uuidField(in.Employment, "employment")
	if err != nil {
		return err
	}
	var assignment any
	if in.Assignment.Id != "" {
		assignmentID, parseErr := uuidField(in.Assignment, "assignment")
		if parseErr != nil {
			return parseErr
		}
		assignment = assignmentID
	}
	source, err := json.Marshal(sourcePayload{Authority: in.Source.Authority.String(), Revision: in.Source.Revision.String(), TimezoneID: in.Source.TimezoneID, CalendarRef: in.Source.CalendarRef, ConsentRef: in.Source.ConsentRef})
	if err != nil {
		return coded(CodeInvalid, ErrInvalid, err.Error())
	}
	if err := checkNextTenant(ctx, ex, tenantID, "worker_availability", "availability_id", in.AvailabilityID.Id, revision, nil); err != nil {
		return err
	}
	_, err = ex.Exec(ctx, `INSERT INTO worker_availability (row_id,tenant_id,availability_id,revision,worker_ref,employment_ref,assignment_ref,scope,state,reason,effective_from,effective_to,source) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, uuid.New(), tenantID, in.AvailabilityID.Id, int64(revision), worker, employment, assignment, string(in.Scope), string(in.State), in.Reason, start, end, string(source))
	if err != nil {
		return mapWriteError(CodeDuplicate, "worker_availability", err)
	}
	return nil
}

// GetWorkerAvailability loads one availability revision.
func (s *Store) GetWorkerAvailability(ctx context.Context, tenant values.TenantId, id string, revision uint64) (availability.WorkerAvailability, error) {
	var out availability.WorkerAvailability
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var rowID, workerID, employmentID uuid.UUID
		var assignmentID *uuid.UUID
		var availabilityID string
		var storedRevision int64
		var scope, state, reason string
		var start, end *time.Time
		var source string
		err := tx.QueryRow(ctx, `SELECT row_id,availability_id,revision,worker_ref,employment_ref,assignment_ref,scope,state,reason,effective_from,effective_to,source FROM worker_availability WHERE tenant_id=$1 AND availability_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(&rowID, &availabilityID, &storedRevision, &workerID, &employmentID, &assignmentID, &scope, &state, &reason, &start, &end, &source)
		if errors.Is(err, dbport.ErrNoRows) {
			return coded(CodeNotFound, ErrNotFound, id)
		}
		if err != nil {
			return coded(CodeDatabase, err, "load worker_availability")
		}
		out.AvailabilityID = values.EntityRef{Tenant: values.TenantId(tenantID.String()), Kind: "availability", Id: availabilityID}
		out.Revision, err = values.NewSequenceRevision("availability."+id, uint64(storedRevision))
		if err != nil {
			return coded(CodeDatabase, err, "decode availability revision")
		}
		out.Worker = values.EntityRef{Tenant: values.TenantId(tenantID.String()), Kind: "worker", Id: workerID.String()}
		out.Employment = values.EntityRef{Tenant: values.TenantId(tenantID.String()), Kind: "employment", Id: employmentID.String()}
		if assignmentID != nil {
			out.Assignment = values.EntityRef{Tenant: values.TenantId(tenantID.String()), Kind: "assignment", Id: assignmentID.String()}
		}
		out.Scope, out.State, out.Reason = availability.Scope(scope), availability.State(state), reason
		if start == nil {
			return coded(CodeDatabase, ErrInvalid, "availability effective_from is null")
		}
		if end == nil {
			out.Effective, err = values.NewOpenInstantInterval(values.NewInstant(start.UTC()))
		} else {
			out.Effective, err = values.NewInstantInterval(values.NewInstant(start.UTC()), values.NewInstant(end.UTC()))
		}
		if err != nil {
			return coded(CodeDatabase, err, "decode availability interval")
		}
		var payload sourcePayload
		if err := json.Unmarshal([]byte(source), &payload); err != nil {
			return coded(CodeDatabase, err, "decode availability source")
		}
		if err := json.Unmarshal([]byte(payload.Authority), &out.Source.Authority); err != nil {
			_ = out.Source.Authority.UnmarshalText([]byte(payload.Authority))
		}
		if err := out.Source.Revision.UnmarshalText([]byte(payload.Revision)); err != nil {
			return coded(CodeDatabase, err, "decode availability source revision")
		}
		out.Source.TimezoneID, out.Source.CalendarRef, out.Source.ConsentRef = payload.TimezoneID, payload.CalendarRef, payload.ConsentRef
		_ = rowID
		return nil
	})
	return out, err
}

// ListWorkerAvailability returns a stream in revision order.
func (s *Store) ListWorkerAvailability(ctx context.Context, tenant values.TenantId, id string) ([]availability.WorkerAvailability, error) {
	var out []availability.WorkerAvailability
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM worker_availability WHERE tenant_id=$1 AND availability_id=$2 ORDER BY revision`, tenantID, id)
		if err != nil {
			return coded(CodeDatabase, err, "list worker_availability")
		}
		var revisions []int64
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return coded(CodeDatabase, err, "scan worker_availability revision")
			}
			revisions = append(revisions, revision)
		}
		if err := rows.Err(); err != nil {
			return coded(CodeDatabase, err, "list worker_availability")
		}
		rows.Close()
		if len(revisions) == 0 {
			return coded(CodeNotFound, ErrNotFound, id)
		}
		for _, revision := range revisions {
			row, err := s.getWorkerAvailabilityTx(ctx, tx, tenantID, id, uint64(revision))
			if err != nil {
				return err
			}
			out = append(out, row)
		}
		return nil
	})
	return out, err
}

func (s *Store) getWorkerAvailabilityTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, id string, revision uint64) (availability.WorkerAvailability, error) {
	// Reuse the public decoder without opening another transaction.
	var rowID, workerID, employmentID uuid.UUID
	var assignmentID *uuid.UUID
	var availabilityID string
	var storedRevision int64
	var scope, state, reason string
	var start, end *time.Time
	var source string
	var out availability.WorkerAvailability
	err := tx.QueryRow(ctx, `SELECT row_id,availability_id,revision,worker_ref,employment_ref,assignment_ref,scope,state,reason,effective_from,effective_to,source FROM worker_availability WHERE tenant_id=$1 AND availability_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(&rowID, &availabilityID, &storedRevision, &workerID, &employmentID, &assignmentID, &scope, &state, &reason, &start, &end, &source)
	if err != nil {
		return out, err
	}
	out.AvailabilityID = values.EntityRef{Tenant: values.TenantId(tenantID.String()), Kind: "availability", Id: availabilityID}
	out.Revision, err = values.NewSequenceRevision("availability."+id, uint64(storedRevision))
	if err != nil {
		return out, err
	}
	out.Worker = values.EntityRef{Tenant: values.TenantId(tenantID.String()), Kind: "worker", Id: workerID.String()}
	out.Employment = values.EntityRef{Tenant: values.TenantId(tenantID.String()), Kind: "employment", Id: employmentID.String()}
	if assignmentID != nil {
		out.Assignment = values.EntityRef{Tenant: values.TenantId(tenantID.String()), Kind: "assignment", Id: assignmentID.String()}
	}
	out.Scope, out.State, out.Reason = availability.Scope(scope), availability.State(state), reason
	if start == nil {
		return out, errors.New("availability effective_from is null")
	}
	if end == nil {
		out.Effective, err = values.NewOpenInstantInterval(values.NewInstant(start.UTC()))
	} else {
		out.Effective, err = values.NewInstantInterval(values.NewInstant(start.UTC()), values.NewInstant(end.UTC()))
	}
	if err != nil {
		return out, err
	}
	var payload sourcePayload
	if err := json.Unmarshal([]byte(source), &payload); err != nil {
		return out, err
	}
	if err := out.Source.Authority.UnmarshalText([]byte(payload.Authority)); err != nil {
		return out, err
	}
	if err := out.Source.Revision.UnmarshalText([]byte(payload.Revision)); err != nil {
		return out, err
	}
	out.Source.TimezoneID, out.Source.CalendarRef, out.Source.ConsentRef = payload.TimezoneID, payload.CalendarRef, payload.ConsentRef
	_ = rowID
	return out, nil
}

// PutRegistration appends one immutable device/source registration version.
func (s *Store) PutRegistration(ctx context.Context, tenant values.TenantId, in clock.Registration) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		if err := in.Validate(); err != nil {
			return coded(CodeInvalid, ErrInvalid, err.Error())
		}
		state := in.State
		if state == "" {
			state = clock.Active
		}
		owner, err := uuid.Parse(in.OwnerRef)
		if err != nil || owner == uuid.Nil {
			return coded(CodeInvalid, ErrInvalid, "owner_ref must be a non-nil UUID")
		}
		location, err := uuid.Parse(in.LocationRef)
		if err != nil || location == uuid.Nil {
			return coded(CodeInvalid, ErrInvalid, "location_ref must be a non-nil UUID")
		}
		policies := make([][]byte, 0, 6)
		for _, policy := range []string{in.ClockTrustPolicy, in.OfflinePolicy, in.ReplayPolicy, in.SignaturePolicy, in.FirmwarePolicy, in.CertificatePolicy} {
			encoded, encodeErr := policyJSON(policy)
			if encodeErr != nil {
				return coded(CodeInvalid, ErrInvalid, encodeErr.Error())
			}
			policies = append(policies, encoded)
		}
		retention, err := policyJSON(in.RetentionPolicy)
		if err != nil {
			return coded(CodeInvalid, ErrInvalid, err.Error())
		}
		_, err = tx.Exec(ctx, `INSERT INTO time_device_registration (row_id,tenant_id,device_id,version,state,owner_ref,location_ref,clock_trust_policy,offline_policy,replay_policy,signature_policy,firmware_policy,certificate_policy,retention_policy) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10::jsonb,$11::jsonb,$12::jsonb,$13::jsonb,$14::jsonb)`, uuid.New(), tenantID, in.ID, in.Version, string(state), owner, location, policies[0], policies[1], policies[2], policies[3], policies[4], policies[5], retention)
		if err != nil {
			if isUnique(err) {
				return coded(CodeDuplicateDevice, ErrDuplicate, in.ID+"/"+in.Version)
			}
			return mapWriteError(CodeDuplicateDevice, "time_device_registration", err)
		}
		return nil
	})
}

// SaveRegistration is an alias for PutRegistration.
func (s *Store) SaveRegistration(ctx context.Context, tenant values.TenantId, in clock.Registration) error {
	return s.PutRegistration(ctx, tenant, in)
}

// GetRegistration loads one immutable registration version.
func (s *Store) GetRegistration(ctx context.Context, tenant values.TenantId, id, version string) (clock.Registration, error) {
	var out clock.Registration
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var rowID, owner, location uuid.UUID
		var state string
		var policies [7][]byte
		err := tx.QueryRow(ctx, `SELECT row_id,device_id,version,state,owner_ref,location_ref,clock_trust_policy,offline_policy,replay_policy,signature_policy,firmware_policy,certificate_policy,retention_policy FROM time_device_registration WHERE tenant_id=$1 AND device_id=$2 AND version=$3`, tenantID, id, version).Scan(&rowID, &out.ID, &out.Version, &state, &owner, &location, &policies[0], &policies[1], &policies[2], &policies[3], &policies[4], &policies[5], &policies[6])
		if errors.Is(err, dbport.ErrNoRows) {
			return coded(CodeNotFound, ErrNotFound, id+"/"+version)
		}
		if err != nil {
			return coded(CodeDatabase, err, "load time_device_registration")
		}
		out.State = clock.State(state)
		out.OwnerRef, out.LocationRef = owner.String(), location.String()
		fields := []*string{&out.ClockTrustPolicy, &out.OfflinePolicy, &out.ReplayPolicy, &out.SignaturePolicy, &out.FirmwarePolicy, &out.CertificatePolicy, &out.RetentionPolicy}
		for i := range fields {
			value, decodeErr := policyText(policies[i])
			if decodeErr != nil {
				return coded(CodeDatabase, decodeErr, "decode time device policy")
			}
			*fields[i] = value
		}
		_ = rowID
		return nil
	})
	return out, err
}

// LoadRegistration is an alias for GetRegistration.
func (s *Store) LoadRegistration(ctx context.Context, tenant values.TenantId, id, version string) (clock.Registration, error) {
	return s.GetRegistration(ctx, tenant, id, version)
}

func checkNextTenant(ctx context.Context, q dbport.Querier, tenantID uuid.UUID, table, keyColumn, key string, revision uint64, expected []uint64) error {
	if revision == 0 || revision > math.MaxInt64 {
		return coded(CodeInvalid, ErrInvalid, "revision must be a positive int64")
	}
	var latest int64
	if err := q.QueryRow(ctx, fmt.Sprintf("SELECT COALESCE(MAX(revision),0) FROM %s WHERE tenant_id=$1 AND %s=$2", table, keyColumn), tenantID, key).Scan(&latest); err != nil {
		return coded(CodeDatabase, err, "read current revision")
	}
	if len(expected) == 1 && expected[0] != uint64(latest) {
		return coded(CodeVersionConflict, ErrVersionConflict, fmt.Sprintf("%s %s expected %d, current %d", table, key, expected[0], latest))
	}
	if revision <= uint64(latest) {
		return coded(CodeDuplicate, ErrDuplicate, fmt.Sprintf("%s %s revision %d", table, key, revision))
	}
	if revision != uint64(latest)+1 {
		return coded(CodeVersionConflict, ErrVersionConflict, fmt.Sprintf("%s %s requires revision %d, got %d", table, key, latest+1, revision))
	}
	return nil
}

func instantBounds(interval values.EffectiveInterval) (time.Time, *time.Time, error) {
	if err := interval.Validate(); err != nil || interval.Kind() != values.IntervalKindInstant {
		return time.Time{}, nil, coded(CodeInvalid, ErrInvalid, "effective interval must use instants")
	}
	start, _ := interval.StartInstant()
	end, ok := interval.EndInstant()
	if !ok {
		return start.Time(), nil, nil
	}
	value := end.Time()
	return start.Time(), &value, nil
}

func uuidField(ref values.EntityRef, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(ref.Id)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, coded(CodeInvalid, ErrInvalid, field+" reference must carry a non-nil UUID")
	}
	return id, nil
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func storageDigest(value string) string {
	return strings.TrimPrefix(value, canonicalbytes.DigestAlgorithm+":")
}
func domainDigest(value string) string {
	if value == "" || strings.Contains(value, ":") {
		return value
	}
	return canonicalbytes.DigestAlgorithm + ":" + value
}
func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

type resourceRequirementPayload struct {
	ResourceTypeRef values.EntityRef `json:"resource_type_ref,omitempty"`
	ResourceTypeID  string           `json:"resource_type_id,omitempty"`
	QuantityValue   string           `json:"quantity_value,omitempty"`
	QuantityUnit    string           `json:"quantity_unit,omitempty"`
	Count           int              `json:"count,omitempty"`
}

type requirementPayload struct {
	Participants      []appointment.ParticipantRole      `json:"participants,omitempty"`
	ParticipantRefs   []values.EntityRef                 `json:"participant_refs,omitempty"`
	Resources         []appointment.ResourceType         `json:"resources,omitempty"`
	RequiredResources []resourceRequirementPayload       `json:"required_resources,omitempty"`
	Qualification     string                             `json:"qualification,omitempty"`
	QualificationRefs []values.EntityRef                 `json:"qualification_refs,omitempty"`
	WindowTimeZone    string                             `json:"window_time_zone,omitempty"`
	Location          appointment.Location               `json:"location,omitempty"`
	Cancellation      appointment.CancellationPolicy     `json:"cancellation,omitempty"`
	CancellationRules appointment.CancellationRules      `json:"cancellation_rules,omitempty"`
	ExternalCalendar  appointment.ExternalCalendarPolicy `json:"external_calendar,omitempty"`
	PurposeKind       appointment.PurposeKind            `json:"purpose_kind,omitempty"`
}

func encodeRequirement(in appointment.Requirement) (appointment.Requirement, []byte, time.Time, *time.Time, error) {
	if in.Revision == 0 {
		published, err := appointment.Publish(in)
		if err != nil {
			return appointment.Requirement{}, nil, time.Time{}, nil, coded(CodeInvalid, ErrInvalid, err.Error())
		}
		in = published.Requirement
		in.Revision = 1
	} else {
		normalized, err := appointment.NewAppointmentRequirement(in)
		if err != nil {
			return appointment.Requirement{}, nil, time.Time{}, nil, coded(CodeInvalid, ErrInvalid, err.Error())
		}
		in = normalized
	}
	window := in.WindowInterval
	if window.Validate() != nil {
		start := values.NewInstant(in.Window.Start)
		end := values.NewInstant(in.Window.End)
		var err error
		window, err = values.NewInstantInterval(start, end)
		if err != nil {
			return appointment.Requirement{}, nil, time.Time{}, nil, coded(CodeInvalid, ErrInvalid, err.Error())
		}
	}
	if window.Kind() != values.IntervalKindInstant {
		return appointment.Requirement{}, nil, time.Time{}, nil, coded(CodeInvalid, ErrInvalid, "requirement window must use instants")
	}
	start, _ := window.StartInstant()
	end, ok := window.EndInstant()
	if !ok {
		return appointment.Requirement{}, nil, time.Time{}, nil, coded(CodeInvalid, ErrInvalid, "requirement window must be closed")
	}
	endTime := end.Time()
	payload := requirementPayload{
		Participants: in.Participants, ParticipantRefs: in.ParticipantRefs, Resources: in.Resources,
		Qualification: in.Qualification, QualificationRefs: in.QualificationRefs, WindowTimeZone: in.Window.TimeZone,
		Location: in.Location, Cancellation: in.Cancellation, CancellationRules: in.CancellationRules,
		ExternalCalendar: in.ExternalCalendar, PurposeKind: in.PurposeKind,
	}
	for _, resource := range in.RequiredResources {
		item := resourceRequirementPayload{ResourceTypeRef: resource.ResourceTypeRef, ResourceTypeID: resource.ResourceTypeID, Count: resource.Count}
		if resource.Quantity.Validate() == nil {
			item.QuantityValue, item.QuantityUnit = resource.Quantity.Value().String(), resource.Quantity.Unit()
		}
		payload.RequiredResources = append(payload.RequiredResources, item)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return appointment.Requirement{}, nil, time.Time{}, nil, coded(CodeInvalid, ErrInvalid, err.Error())
	}
	digest := in.CanonicalDigest
	if digest == "" {
		digest, err = in.Digest()
		if err != nil {
			return appointment.Requirement{}, nil, time.Time{}, nil, coded(CodeInvalid, ErrInvalid, err.Error())
		}
		in.CanonicalDigest = digest
	}
	return in, encoded, start.Time(), &endTime, nil
}

func decodeRequirementPayload(raw []byte, out *appointment.Requirement) error {
	var payload requirementPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	out.Participants, out.ParticipantRefs, out.Resources = payload.Participants, payload.ParticipantRefs, payload.Resources
	out.Qualification, out.QualificationRefs, out.Window.TimeZone = payload.Qualification, payload.QualificationRefs, payload.WindowTimeZone
	out.Location, out.Cancellation, out.CancellationRules = payload.Location, payload.Cancellation, payload.CancellationRules
	out.ExternalCalendar, out.PurposeKind = payload.ExternalCalendar, payload.PurposeKind
	for _, item := range payload.RequiredResources {
		resource := appointment.ResourceRequirement{ResourceTypeRef: item.ResourceTypeRef, ResourceTypeID: item.ResourceTypeID, Count: item.Count}
		if item.QuantityValue != "" && item.QuantityUnit != "" {
			quantity, err := values.NewQuantity(item.QuantityValue, item.QuantityUnit, 0, values.RoundingHalfEven)
			if err != nil {
				return err
			}
			resource.Quantity = quantity
		}
		out.RequiredResources = append(out.RequiredResources, resource)
	}
	return nil
}

type sourcePayload struct {
	Authority   string `json:"authority"`
	Revision    string `json:"revision"`
	TimezoneID  string `json:"timezone_id"`
	CalendarRef string `json:"calendar_ref"`
	ConsentRef  string `json:"consent_ref,omitempty"`
}

func policyJSON(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("policy is required")
	}
	if json.Valid([]byte(value)) {
		return []byte(value), nil
	}
	return json.Marshal(value)
}

func policyText(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	if !json.Valid(raw) {
		return "", errors.New("policy JSON is invalid")
	}
	return string(raw), nil
}
