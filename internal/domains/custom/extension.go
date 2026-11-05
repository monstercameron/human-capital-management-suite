// WF-DATA-002: tenant extension fields on core HR and organizational
// entities. An extension binds a tenant-owned custom object definition to
// one core entity kind; extension records are effective-dated revisions
// keyed by the core entity reference and stored as custom-object events, so
// core tables never gain per-tenant columns. Reads ride the custom search
// projection, which withholds ungranted fields; writes resolve governed
// field kinds through the shared WF-DATA-031 helper.
package custom

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrExtensionDefinition refuses an extension that binds an unknown
	// entity kind or shadows a platform name.
	ErrExtensionDefinition = errors.New("custom: invalid extension definition")
	// ErrUnknownEntity refuses a record for an entity reference the core
	// cannot resolve for this tenant and kind.
	ErrUnknownEntity = errors.New("custom: unknown extended entity")
	// ErrIntervalViolation refuses a record whose effective span escapes
	// its entity's effective interval.
	ErrIntervalViolation = errors.New("custom: extension exceeds entity interval")
)

// EntityKind names one core HR or organizational entity kind an extension
// may bind. Values are canonical values.Kind strings shared with the
// aggregates registry (internal/data/aggregates) and the owning domains:
// person, worker, employment and assignment from the people aggregates,
// position from the position domain, organization_unit and legal_entity
// from the organization aggregates. Location and cost_center have no
// canonical kind yet; these spellings are their first use.
type EntityKind string

const (
	EntityPerson           EntityKind = "person"
	EntityWorker           EntityKind = "worker"
	EntityEmployment       EntityKind = "employment"
	EntityAssignment       EntityKind = "assignment"
	EntityPosition         EntityKind = "position"
	EntityOrganizationUnit EntityKind = "organization_unit"
	EntityLegalEntity      EntityKind = "legal_entity"
	EntityLocation         EntityKind = "location"
	EntityCostCenter       EntityKind = "cost_center"
)

// Valid reports whether k is one of the nine extendable core entity kinds.
func (k EntityKind) Valid() bool {
	switch k {
	case EntityPerson, EntityWorker, EntityEmployment, EntityAssignment,
		EntityPosition, EntityOrganizationUnit, EntityLegalEntity,
		EntityLocation, EntityCostCenter:
		return true
	}
	return false
}

// Kind returns k as a canonical entity kind for reference comparison.
func (k EntityKind) Kind() values.Kind { return values.Kind(k) }

// platformNamespaces are namespaces an extension definition may not claim:
// the core entity kinds plus the store's own infix, so an extension can
// bind to a core kind but never impersonate one.
var platformNamespaces = map[string]bool{
	"person": true, "worker": true, "employment": true, "assignment": true,
	"position": true, "organization": true, "organization_unit": true,
	"legal_entity": true, "location": true, "cost_center": true,
	"custom": true, "platform": true, "hcm": true, "system": true,
}

// platformFields are platform field names an extension field may not claim.
// The list mirrors the closed people field set (people.AllFields); the
// WF-DATA-002 tests pin the two together so a new platform field fails the
// suite until it is refused here too.
var platformFields = map[string]bool{
	"worker.worker_number": true, "worker.lifecycle_status": true,
	"person.legal_name": true, "person.preferred_name": true,
	"employment.employment_id": true, "employment.legal_entity": true,
	"employment.worker_type": true, "employment.hire_date": true,
	"employment.status": true, "employment.exemption_status": true,
	"employment.time_capture_mode": true, "employment.time_profile_ref": true,
	"assignment.assignment_id": true, "assignment.job_code": true,
	"assignment.grade": true, "assignment.org_unit": true,
	"assignment.position_id": true, "assignment.location": true,
	"assignment.pay_zone": true, "assignment.fte": true,
	"assignment.manager_relationship_ref": true, "assignment.cost_center": true,
}

// ExtensionDefinition is one published tenant extension: the core entity
// kind it binds and the tenant-owned custom definition of its fields. The
// resolved policy and full field-domain grant are cached at publication so
// reads and writes never re-resolve them.
type ExtensionDefinition struct {
	EntityKind EntityKind
	Definition CustomObjectDefinition
	Policy     PolicyProjection

	grants map[string]bool
}

// Validate reports whether the extension binds a known entity kind and
// shadows no platform namespace, kind or field name.
func (d ExtensionDefinition) Validate() error {
	if !d.EntityKind.Valid() {
		return fmt.Errorf("%w: entity kind %q is not extendable", ErrExtensionDefinition, d.EntityKind)
	}
	if err := d.Definition.Validate(); err != nil {
		return err
	}
	if platformNamespaces[strings.ToLower(strings.TrimSpace(d.Definition.Namespace))] {
		return fmt.Errorf("%w: namespace %q is reserved", ErrExtensionDefinition, d.Definition.Namespace)
	}
	if platformNamespaces[strings.ToLower(strings.TrimSpace(d.Definition.Kind))] {
		return fmt.Errorf("%w: kind %q impersonates a core kind", ErrExtensionDefinition, d.Definition.Kind)
	}
	for name := range d.Definition.Fields {
		if platformFields[strings.ToLower(name)] {
			return fmt.Errorf("%w: field %q shadows a platform field", ErrExtensionDefinition, name)
		}
	}
	return nil
}

// PublishExtension publishes one tenant extension bound to a core entity
// kind. Publication resolves the definition policy, so an incompletely
// classified definition fails here, and requires the kind resolvers the
// governed fields need, mirroring custom-object publication.
func PublishExtension(def CustomObjectDefinition, entityKind EntityKind, deps FieldValueDeps) (ExtensionDefinition, error) {
	out := ExtensionDefinition{EntityKind: entityKind, Definition: def}
	if err := out.Validate(); err != nil {
		return ExtensionDefinition{}, err
	}
	policy, err := ResolvePolicy(def)
	if err != nil {
		return ExtensionDefinition{}, err
	}
	needRef, needArtifacts := false, false
	for _, fieldDef := range def.Fields {
		switch fieldDef.Kind {
		case FieldKindReference:
			needRef = true
		case FieldKindAttachment:
			needArtifacts = true
		}
	}
	if needRef && deps.RefReleases == nil {
		return ExtensionDefinition{}, fmt.Errorf("%w: reference fields require a release resolver", ErrInvalidFieldKind)
	}
	if needArtifacts && deps.Artifacts == nil {
		return ExtensionDefinition{}, fmt.Errorf("%w: attachment fields require an artifact registry", ErrInvalidFieldKind)
	}
	out.Policy = policy
	out.grants = make(map[string]bool, len(policy.Fields))
	for _, field := range policy.Fields {
		out.grants[field.AuthZDomain] = true
	}
	return out, nil
}

// EntityInterval is one core entity's effective span. End is zero for an
// entity that is still effective; Found is false when the entity reference
// resolves to nothing.
type EntityInterval struct {
	Start time.Time
	End   time.Time
	Found bool
}

// IntervalProvider resolves one core entity reference to its effective
// interval. Adapters over the people and organization aggregates provide
// it; tests stub it.
type IntervalProvider func(ctx context.Context, entity values.EntityRef) (EntityInterval, error)

// ExtensionDeps wires extension records to their event store, interval
// provider, clock, governed-field resolvers and record-rule dependencies.
type ExtensionDeps struct {
	Store     EventProjectionPort
	Access    *AccessDeps
	Clock     func() time.Time
	Intervals IntervalProvider
	Values    FieldValueDeps
}

// ExtensionRecords serves extension records: effective-dated revisions
// keyed by core entity reference, committed as custom-object events. The
// journal replays retried writes without reallocating governed values.
type ExtensionRecords struct {
	port      EventProjectionPort
	access    *AccessDeps
	clock     func() time.Time
	intervals IntervalProvider
	values    FieldValueDeps

	mu      sync.Mutex
	journal map[string]ExtensionRecord
}

// NewExtensionRecords binds the dependencies. A nil clock reads wall time
// and a nil sequence allocator starts fresh, mirroring publication.
func NewExtensionRecords(deps ExtensionDeps) *ExtensionRecords {
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}
	sequences := deps.Values.Sequences
	if sequences == nil {
		sequences = NewSequences()
	}
	deps.Values.Sequences = sequences
	return &ExtensionRecords{
		port: deps.Store, access: deps.Access, clock: clock,
		intervals: deps.Intervals, values: deps.Values,
		journal: map[string]ExtensionRecord{},
	}
}

// WriteOpts carries one extension write: its effective span, revision
// precondition, provenance and idempotency key. Principal and Scopes
// authorize the revision a change revises.
type WriteOpts struct {
	Effective        values.EffectiveInterval
	ExpectedRevision uint64
	Actor            string
	Purpose          string
	EvidenceDigest   string
	IdempotencyKey   string
	Principal        string
	Scopes           []string
}

// ReadOpts carries one extension read: the caller, the authz domains they
// hold and the purpose. Fields narrows the projection; empty reads every
// granted field and nothing else.
type ReadOpts struct {
	Principal string
	Scopes    []string
	Grants    map[string]bool
	Purpose   string
	Fields    []string
}

// ExtensionRecord is one committed extension revision: its receipt, entity,
// revision number and fields. Reads carry no receipt.
type ExtensionRecord struct {
	Receipt  CommitReceipt
	Entity   values.EntityRef
	Revision uint64
	Fields   map[string]TypedValue
	Replayed bool
}

// Create commits the first extension revision for one entity.
func (s *ExtensionRecords) Create(ctx context.Context, tenant string, def ExtensionDefinition, entity values.EntityRef, fields map[string]TypedValue, opts WriteOpts) (ExtensionRecord, error) {
	return s.commit(ctx, tenant, def, entity, fields, opts, OperationCreate)
}

// Change commits the next extension revision for one entity.
func (s *ExtensionRecords) Change(ctx context.Context, tenant string, def ExtensionDefinition, entity values.EntityRef, fields map[string]TypedValue, opts WriteOpts) (ExtensionRecord, error) {
	return s.commit(ctx, tenant, def, entity, fields, opts, OperationChange)
}

// Read returns the current extension revision for one entity. Fields the
// caller holds no grant for are absent, never empty.
func (s *ExtensionRecords) Read(ctx context.Context, tenant string, def ExtensionDefinition, entity values.EntityRef, opts ReadOpts) (ExtensionRecord, error) {
	if err := def.Validate(); err != nil {
		return ExtensionRecord{}, err
	}
	if err := checkEntityRef(tenant, def.EntityKind, entity); err != nil {
		return ExtensionRecord{}, err
	}
	if s.port == nil {
		return ExtensionRecord{}, fmt.Errorf("%w: event store is required", ErrInvalidMutation)
	}
	rec, err := s.searchOne(ctx, tenant, def, entity.String(), opts.Grants, opts.Fields, opts.Principal, opts.Scopes, opts.Purpose)
	if err != nil {
		return ExtensionRecord{}, err
	}
	return ExtensionRecord{Entity: entity, Revision: rec.Revision, Fields: cloneFields(rec.Fields)}, nil
}

func (s *ExtensionRecords) commit(ctx context.Context, tenant string, def ExtensionDefinition, entity values.EntityRef, fields map[string]TypedValue, opts WriteOpts, op Operation) (ExtensionRecord, error) {
	if err := def.Validate(); err != nil {
		return ExtensionRecord{}, err
	}
	if err := checkEntityRef(tenant, def.EntityKind, entity); err != nil {
		return ExtensionRecord{}, err
	}
	if s.port == nil || s.intervals == nil || s.clock == nil {
		return ExtensionRecord{}, fmt.Errorf("%w: store, interval provider and clock are required", ErrInvalidMutation)
	}
	if strings.TrimSpace(opts.IdempotencyKey) == "" {
		return ExtensionRecord{}, fmt.Errorf("%w: idempotency key is required", ErrInvalidMutation)
	}
	objectID := entity.String()
	key := tenant + "\x00" + objectID + "\x00" + string(op) + "\x00" + opts.IdempotencyKey
	s.mu.Lock()
	if prior, ok := s.journal[key]; ok {
		s.mu.Unlock()
		prior.Replayed = true
		prior.Receipt.Replayed = true
		return prior, nil
	}
	s.mu.Unlock()
	start, end, open, err := effectiveBounds(opts.Effective)
	if err != nil {
		return ExtensionRecord{}, err
	}
	interval, err := s.intervals(ctx, entity)
	if err != nil {
		return ExtensionRecord{}, err
	}
	if !interval.Found {
		return ExtensionRecord{}, fmt.Errorf("%w: %s", ErrUnknownEntity, entity.String())
	}
	if start.Before(interval.Start) || (!open && !interval.End.IsZero() && interval.End.Before(end)) || (open && !interval.End.IsZero()) {
		return ExtensionRecord{}, fmt.Errorf("%w: record %s escapes entity interval %s",
			ErrIntervalViolation, intervalSpan(start, end, open), entitySpan(interval))
	}
	var stored map[string]string
	head := int64(0)
	if op == OperationChange {
		if opts.ExpectedRevision == 0 {
			return ExtensionRecord{}, fmt.Errorf("%w: change requires an expected revision", ErrInvalidMutation)
		}
		current, err := s.searchOne(ctx, tenant, def, objectID, def.grants, nil, opts.Principal, opts.Scopes, opts.Purpose)
		if err != nil {
			return ExtensionRecord{}, err
		}
		if current.Revision != opts.ExpectedRevision {
			return ExtensionRecord{}, fmt.Errorf("%w: entity=%s expected=%d actual=%d",
				ErrStaleRevision, objectID, opts.ExpectedRevision, current.Revision)
		}
		stored, err = governedTexts(def.Definition, current.Fields)
		if err != nil {
			return ExtensionRecord{}, err
		}
		// Stream heads equal object revisions: every committed mutation
		// appends exactly one event and advances the revision by one.
		head = int64(current.Revision)
	} else if opts.ExpectedRevision != 0 {
		return ExtensionRecord{}, fmt.Errorf("%w: create carries no expected revision", ErrInvalidMutation)
	}
	now := s.clock()
	texts, err := governedTexts(def.Definition, fields)
	if err != nil {
		return ExtensionRecord{}, err
	}
	resolved, err := ResolveGovernedFields(ctx, def.Definition, tenant, op, texts, stored, now, s.values)
	if err != nil {
		return ExtensionRecord{}, err
	}
	for name, text := range resolved {
		if fieldDef, ok := def.Definition.Fields[name]; ok && fieldDef.Kind != FieldKindPlain {
			fields[name] = TypedValue{FieldName: name, Type: fieldDef.Type, Value: text}
		}
	}
	record := CustomRecordRevision{
		ObjectID: objectID, ObjectKind: def.Definition.Kind, Namespace: def.Definition.Namespace,
		DefinitionVersion: def.Definition.Version, Effective: opts.Effective, FieldValues: fields,
	}
	receipt, err := s.port.Commit(ctx, MutationRequest{
		Tenant: values.TenantId(tenant), ObjectID: objectID, Definition: def.Definition,
		Record: record, Operation: op, ExpectedHead: head, ExpectedRevision: opts.ExpectedRevision,
		Actor: opts.Actor, Purpose: opts.Purpose, EvidenceDigest: opts.EvidenceDigest,
		GrantedDomains: def.grants, OccurredAt: now, EffectiveAt: now,
	})
	if err != nil {
		return ExtensionRecord{}, err
	}
	result := ExtensionRecord{
		Receipt: receipt, Entity: entity, Revision: receipt.Event.Revision,
		Fields: cloneFields(receipt.Event.Record.FieldValues), Replayed: receipt.Replayed,
	}
	s.mu.Lock()
	s.journal[key] = result
	s.mu.Unlock()
	return result, nil
}

// searchOne returns the projected record for one object id through the
// custom search, which withholds every field the grants do not authorize.
func (s *ExtensionRecords) searchOne(ctx context.Context, tenant string, def ExtensionDefinition, objectID string, grants map[string]bool, fields []string, principal string, scopes []string, purpose string) (SearchRecord, error) {
	report, err := s.port.Search(ctx, SearchRequest{
		Tenant: values.TenantId(tenant), Definition: def.Definition,
		RequestedFields: fields, GrantedDomains: grants, Purpose: purpose,
		Principal: Principal{ID: principal, Scopes: scopes}, Access: s.access,
	})
	if err != nil {
		return SearchRecord{}, err
	}
	for _, rec := range report.Records {
		if rec.ObjectID == objectID {
			return rec, nil
		}
	}
	return SearchRecord{}, fmt.Errorf("%w: extension for %s", ErrNotFound, objectID)
}

// checkEntityRef proves the reference is well formed, tenant-scoped and of
// the bound kind, mirroring the people read-request validation.
func checkEntityRef(tenant string, kind EntityKind, entity values.EntityRef) error {
	if err := entity.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrUnknownEntity, err)
	}
	if string(entity.Tenant) != tenant {
		return fmt.Errorf("%w: %s is outside tenant %s", ErrUnknownEntity, entity.String(), tenant)
	}
	if EntityKind(entity.Kind) != kind {
		return fmt.Errorf("%w: kind is %q, want %q", ErrUnknownEntity, entity.Kind, kind)
	}
	return nil
}

// governedTexts extracts the kind-field texts of one field set. Governed
// values are canonical text; anything else is refused rather than coerced.
func governedTexts(def CustomObjectDefinition, fields map[string]TypedValue) (map[string]string, error) {
	out := map[string]string{}
	for name, fieldDef := range def.Fields {
		if fieldDef.Kind == FieldKindPlain {
			continue
		}
		value, ok := fields[name]
		if !ok {
			continue
		}
		text, ok := value.Value.(string)
		if !ok {
			return nil, fmt.Errorf("%w: governed field %q holds non-text", ErrInvalidFieldKind, name)
		}
		out[name] = text
	}
	return out, nil
}

// effectiveBounds extracts the instant span of one effective interval. Only
// instant intervals bound extension records; anything else is refused
// rather than converted across calendars.
func effectiveBounds(iv values.EffectiveInterval) (start, end time.Time, open bool, err error) {
	startInstant, ok := iv.StartInstant()
	if !ok {
		return time.Time{}, time.Time{}, false, fmt.Errorf("%w: instant effective start is required", ErrInvalidMutation)
	}
	endInstant, hasEnd := iv.EndInstant()
	if !hasEnd {
		return startInstant.Time(), time.Time{}, true, nil
	}
	return startInstant.Time(), endInstant.Time(), false, nil
}

func intervalSpan(start, end time.Time, open bool) string {
	if open {
		return start.UTC().Format(time.RFC3339) + "/open"
	}
	return start.UTC().Format(time.RFC3339) + "/" + end.UTC().Format(time.RFC3339)
}

func entitySpan(interval EntityInterval) string {
	if interval.End.IsZero() {
		return interval.Start.UTC().Format(time.RFC3339) + "/open"
	}
	return interval.Start.UTC().Format(time.RFC3339) + "/" + interval.End.UTC().Format(time.RFC3339)
}
