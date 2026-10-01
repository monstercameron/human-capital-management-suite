// Package people owns the bounded business meaning of a worker's state: the
// bitemporal facts about a Person, Worker, Employment and Assignment, and the
// governed explanation of those facts that PEOPLE-005 exposes as
// hcmnext.people.explain_worker_state/v1.
//
// Semantic owner: People, Employment and Assignment domain. Phase: P1A.
//
// The package holds no storage. It defines the WorkerFacts read port and
// evaluates an authorization decision that someone else made; the intent
// kernel wires a real reader and a real AuthZ evaluator to it. That split is
// deliberate: a read model that could also decide its own authorization would
// be able to widen its own visibility, and an explanation that trusted
// caller-supplied facts would be an echo rather than an explanation.
package people

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Port and fact errors. All are matchable with errors.Is.
var (
	// ErrUnknownField is returned when a caller asks for a field this domain
	// does not define. It is not silently dropped: an unknown field name is
	// usually a stale client, and answering it as "absent" would be a lie.
	ErrUnknownField = errors.New("people: unknown worker field")
	// ErrNoFieldsRequested is returned for an explanation request with no fields.
	ErrNoFieldsRequested = errors.New("people: explanation requires at least one field")
	// ErrDuplicateField is returned when a field is requested twice.
	ErrDuplicateField = errors.New("people: field requested more than once")
	// ErrFactIncomplete is returned when the reader returns a fact that is
	// missing its bitemporal coordinates, source authority or provenance.
	ErrFactIncomplete = errors.New("people: fact is missing effective time, known time, authority or provenance")
	// ErrFactSubjectMismatch is returned when the reader answers about a
	// different worker than the one that was asked about.
	ErrFactSubjectMismatch = errors.New("people: reader answered about a different worker")
	// ErrDuplicateFact is returned when the reader returns two facts for one field.
	ErrDuplicateFact = errors.New("people: reader returned two facts for one field")
	// ErrReaderFailed wraps a failure from the WorkerFacts port.
	ErrReaderFailed = errors.New("people: worker facts reader failed")
)

// KindWorker is the entity kind of a worker reference.
const KindWorker values.Kind = "worker"

const (
	factSchema      = "hcmnext.domains.people.Fact"
	factSetSchema   = "hcmnext.domains.people.FactSet"
	peopleSchemaVer = 1
)

// FieldID names one worker-state field. Field identity is part of the AuthZ
// contract, so these are stable tokens rather than Go struct field names.
type FieldID string

// Worker-state fields covered by P1A. The set is closed: promotion preflight
// and simulation read exactly these, and nothing loads a compartmentalised
// field (identity claims, immigration, medical, payroll bank data) merely
// because a promotion targets the worker.
const (
	FieldWorkerNumber     FieldID = "worker.worker_number"
	FieldLifecycleStatus  FieldID = "worker.lifecycle_status"
	FieldLegalName        FieldID = "person.legal_name"
	FieldPreferredName    FieldID = "person.preferred_name"
	FieldEmploymentID     FieldID = "employment.employment_id"
	FieldLegalEntity      FieldID = "employment.legal_entity"
	FieldWorkerType       FieldID = "employment.worker_type"
	FieldHireDate         FieldID = "employment.hire_date"
	FieldEmploymentStatus FieldID = "employment.status"
	FieldAssignmentID     FieldID = "assignment.assignment_id"
	FieldJobCode          FieldID = "assignment.job_code"
	FieldGrade            FieldID = "assignment.grade"
	FieldOrgUnit          FieldID = "assignment.org_unit"
	FieldPositionID       FieldID = "assignment.position_id"
	FieldLocation         FieldID = "assignment.location"
	FieldPayZone          FieldID = "assignment.pay_zone"
	FieldFTE              FieldID = "assignment.fte"
	FieldManagerRelation  FieldID = "assignment.manager_relationship_ref"

	// FieldExemptionStatus, FieldTimeCaptureMode and FieldTimeProfileRef back
	// WTIME-001's per-assignment time profile: the overtime exemption
	// classification, the "hour tracking" capture mode and the named time
	// profile a worker's capture mode and exemption were resolved from. They
	// are employment facts, not assignment facts, for the same reason
	// FieldWorkerType is: exemption and capture mode describe the worker's
	// legal and payroll relationship, not the placement.
	FieldExemptionStatus FieldID = "employment.exemption_status"
	FieldTimeCaptureMode FieldID = "employment.time_capture_mode"
	FieldTimeProfileRef  FieldID = "employment.time_profile_ref"
)

// knownFields is the closed set of defined fields.
var knownFields = map[FieldID]struct{}{
	FieldWorkerNumber:     {},
	FieldLifecycleStatus:  {},
	FieldLegalName:        {},
	FieldPreferredName:    {},
	FieldEmploymentID:     {},
	FieldLegalEntity:      {},
	FieldWorkerType:       {},
	FieldHireDate:         {},
	FieldEmploymentStatus: {},
	FieldAssignmentID:     {},
	FieldJobCode:          {},
	FieldGrade:            {},
	FieldOrgUnit:          {},
	FieldPositionID:       {},
	FieldLocation:         {},
	FieldPayZone:          {},
	FieldFTE:              {},
	FieldManagerRelation:  {},
	FieldExemptionStatus:  {},
	FieldTimeCaptureMode:  {},
	FieldTimeProfileRef:   {},
}

// Validate reports whether f is a defined worker-state field.
func (f FieldID) Validate() error {
	if _, ok := knownFields[f]; !ok {
		return fmt.Errorf("%w: %q", ErrUnknownField, string(f))
	}
	return nil
}

// String returns the field token.
func (f FieldID) String() string { return string(f) }

// AllFields returns every defined field in sorted order. It is the default
// projection for an explanation that does not narrow its own request.
func AllFields() []FieldID {
	out := make([]FieldID, 0, len(knownFields))
	for f := range knownFields {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// AsOf is the bitemporal coordinate of a question. Both halves are mandatory:
// "what is true now" and "what did we know then" are different questions, and
// an explanation that answers only the first cannot explain a correction.
type AsOf struct {
	// EffectiveOn is the business date the facts are asked about.
	EffectiveOn values.LocalDate
	// KnownAt is the knowledge cut-off: facts recorded after it are invisible.
	KnownAt values.KnownAt
}

// Validate reports whether both coordinates are set.
func (a AsOf) Validate() error {
	if err := a.EffectiveOn.Validate(); err != nil {
		return fmt.Errorf("people: as-of effective date: %w", err)
	}
	if a.KnownAt.Canonical() == nil {
		return fmt.Errorf("people: as-of known-at is unset")
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (a AsOf) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.people.AsOf", peopleSchemaVer).
		Value("effective_on", a.EffectiveOn).
		Value("known_at", a.KnownAt).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Fact is one effective-dated, recorded assertion about a worker.
//
// The value is a Presence rather than a bare string so that "no value",
// "explicitly null", "not known", "withheld" and "not applicable to this
// worker" stay five distinct answers all the way to the caller. Collapsing
// them into an empty string is how a read model quietly starts asserting that
// a worker has no manager when in truth nobody asked the graph.
type Fact struct {
	Field FieldID
	Value values.Presence[string]

	// Effective is the half-open business interval the assertion covers.
	Effective values.EffectiveInterval
	// KnownAt is when the assertion became available to its authority.
	KnownAt values.KnownAt
	// Revision names the stream position the assertion was read at.
	Revision values.RevisionToken
	// Authority is the source-authority decision for this field.
	Authority evidence.SourceAuthority
	// Provenance is where the assertion came from and when it was recorded.
	Provenance evidence.Provenance
}

// Validate reports whether the fact is complete enough to disclose. A fact
// missing any of its four evidence coordinates is rejected rather than
// disclosed with the gap left implicit.
func (f Fact) Validate() error {
	if err := f.Field.Validate(); err != nil {
		return err
	}
	if err := f.Value.Validate(); err != nil {
		return fmt.Errorf("people: field %s: %w", f.Field, err)
	}
	if err := f.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: field %s effective interval: %w", ErrFactIncomplete, f.Field, err)
	}
	if f.KnownAt.Canonical() == nil {
		return fmt.Errorf("%w: field %s has no known-at", ErrFactIncomplete, f.Field)
	}
	if !f.Revision.IsSpecified() {
		return fmt.Errorf("%w: field %s has no revision", ErrFactIncomplete, f.Field)
	}
	if err := f.Authority.Validate(); err != nil {
		return fmt.Errorf("%w: field %s: %w", ErrFactIncomplete, f.Field, err)
	}
	if err := f.Provenance.Validate(); err != nil {
		return fmt.Errorf("%w: field %s: %w", ErrFactIncomplete, f.Field, err)
	}
	return values.ValidateKnowledgeOrder(f.KnownAt, f.Provenance.RecordedAt, false)
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (f Fact) Canonical() []byte {
	if f.Validate() != nil {
		return nil
	}
	encoded, err := values.MarshalPresence(f.Value, values.StringCodec{})
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New(factSchema, peopleSchemaVer).
		String("field", string(f.Field)).
		Field("value", encoded).
		Value("effective", f.Effective).
		Value("known_at", f.KnownAt).
		Value("revision", f.Revision).
		Value("authority", f.Authority).
		Value("provenance", f.Provenance).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// FactSet is everything the reader could say about one worker at one
// bitemporal coordinate.
//
// Exists is separate from len(Facts) because "this worker does not exist" and
// "this worker exists and has no facts you asked for" are different answers,
// and only one of them is safe to disclose to an unauthorized caller.
type FactSet struct {
	Worker values.EntityRef
	Exists bool
	Facts  []Fact
	// Watermark is the read position the whole set was taken at, so a caller
	// can prove two fields were read from the same consistent snapshot.
	Watermark values.RevisionToken
}

// Validate reports whether the set is internally consistent.
func (s FactSet) Validate() error {
	if err := s.Worker.Validate(); err != nil {
		return fmt.Errorf("people: fact set worker: %w", err)
	}
	if !s.Exists {
		if len(s.Facts) != 0 {
			return fmt.Errorf("people: fact set says the worker does not exist but carries %d facts", len(s.Facts))
		}
		return nil
	}
	if !s.Watermark.IsSpecified() {
		return fmt.Errorf("%w: fact set has no read watermark", ErrFactIncomplete)
	}
	seen := make(map[FieldID]struct{}, len(s.Facts))
	for _, f := range s.Facts {
		if err := f.Validate(); err != nil {
			return err
		}
		if _, dup := seen[f.Field]; dup {
			return fmt.Errorf("%w: %s", ErrDuplicateFact, f.Field)
		}
		seen[f.Field] = struct{}{}
	}
	return nil
}

// Lookup returns the fact for a field, if the set carries one.
func (s FactSet) Lookup(field FieldID) (Fact, bool) {
	for _, f := range s.Facts {
		if f.Field == field {
			return f, true
		}
	}
	return Fact{}, false
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (s FactSet) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	sorted := append([]Fact(nil), s.Facts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Field < sorted[j].Field })
	w := canonicalbytes.New(factSetSchema, peopleSchemaVer).
		Value("worker", s.Worker).
		Bool("exists", s.Exists).
		Count("facts", len(sorted))
	for _, f := range sorted {
		w.Value("fact", f)
	}
	if s.Exists {
		w.Value("watermark", s.Watermark)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// FactQuery is what the read port is asked for.
type FactQuery struct {
	Tenant values.TenantId
	Worker values.EntityRef
	AsOf   AsOf
	// Fields is the exact projection to read. The port must not widen it.
	Fields []FieldID
}

// Validate reports whether the query is well formed.
func (q FactQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("people: query tenant: %w", err)
	}
	if err := q.Worker.Validate(); err != nil {
		return fmt.Errorf("people: query worker: %w", err)
	}
	if q.Worker.Tenant != q.Tenant {
		return fmt.Errorf("people: query worker %s is outside tenant %s", q.Worker, q.Tenant)
	}
	if q.Worker.Kind != KindWorker {
		return fmt.Errorf("people: query subject kind is %q, want %q", q.Worker.Kind, KindWorker)
	}
	if err := q.AsOf.Validate(); err != nil {
		return err
	}
	if len(q.Fields) == 0 {
		return ErrNoFieldsRequested
	}
	seen := make(map[FieldID]struct{}, len(q.Fields))
	for _, f := range q.Fields {
		if err := f.Validate(); err != nil {
			return err
		}
		if _, dup := seen[f]; dup {
			return fmt.Errorf("%w: %s", ErrDuplicateField, f)
		}
		seen[f] = struct{}{}
	}
	return nil
}

// WorkerFacts is the read port the intent kernel wires to a real repository.
//
// It is the only way this package learns anything about a worker. There is
// deliberately no variant that accepts facts from the caller: an explanation
// whose inputs the caller supplied cannot certify what the record says.
type WorkerFacts interface {
	// WorkerFactsAt returns the facts visible at the query's bitemporal
	// coordinate, restricted to the requested fields. It returns an error only
	// for a read failure; a worker that does not exist is a FactSet with
	// Exists false, because non-existence is an answer, not a fault.
	WorkerFactsAt(ctx context.Context, q FactQuery) (FactSet, error)
}
