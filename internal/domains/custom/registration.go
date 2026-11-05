// Tenant capability registration publishes one custom type's generated
// manifest as live registry capabilities (WF-DATA-008): create, update,
// read and query under the tenant-owned namespace, request and response
// typed by the pinned definition, effect classes taken from the manifest,
// writes carrying caller idempotency keys with journaled replay and a
// reversal descriptor on every receipt.
//
// Tenancy is enforced twice: the capability id and scope name the tenant,
// and every handler refuses a payload naming another tenant. The P1A
// gateway refuses write effects, so workflow creates travel the bound
// handler through the exact runner call the served write gateway will
// front; reads and queries run through the gateway today. Correct and
// retire stay manifest-only until their governed compositions land.
package custom

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/documentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrRegistrationInvalid reports an unusable publish input: a bad
	// tenant, definition, payload or dependency.
	ErrRegistrationInvalid = errors.New("custom: invalid capability registration")
	// ErrRegistrationConflict reports a same-version definition that
	// differs from the published one.
	ErrRegistrationConflict = errors.New("custom: capability registration conflict")
	// ErrTenantMismatch reports a payload naming another tenant than the
	// capability's bound one.
	ErrTenantMismatch = errors.New("custom: tenant mismatch")
	// ErrCapabilityShadow reports a capability id already owned by another
	// domain, including the platform namespace.
	ErrCapabilityShadow = errors.New("custom: capability id is owned by another domain")
	// ErrReversalUnavailable reports a reversal with no journaled basis.
	ErrReversalUnavailable = errors.New("custom: reversal unavailable")
)

// TestRefWFData008 is the conformance test proving every tenant capability
// contract this file publishes.
const TestRefWFData008 = "TestTodo_WF_DATA_008"

// Runner-call input keys. A workflow maps these as constant or computed
// inputs; the handler decodes the runner call through InputTexts without
// importing the runner.
const (
	callTenant           = "tenant"
	callPrincipal        = "principal"
	callScopes           = "scopes"
	callObjectID         = "object_id"
	callIdempotencyKey   = "idempotency_key"
	callActor            = "actor"
	callPurpose          = "purpose"
	callEvidenceDigest   = "evidence_digest"
	callExpectedRevision = "expected_revision"
	callFields           = "fields"
	callLimit            = "limit"
	callOffset           = "offset"
	callFieldPrefix      = "field."
	callFilterPrefix     = "filter."
)

// runnerCall is the structural shape of a workflow runner capability call.
// capabilityrunner.CapabilityCall satisfies it via InputTexts.
type runnerCall interface {
	InputTexts() map[string]string
}

// RecordPayload is one create, update or read request. Tenant must name the
// capability's bound tenant; writes require IdempotencyKey; update requires
// the ExpectedRevision the write targets.
type RecordPayload struct {
	Tenant           string
	Principal        string
	Scopes           []string
	ObjectID         string
	Fields           map[string]string
	IdempotencyKey   string
	Actor            string
	Purpose          string
	EvidenceDigest   string
	ExpectedRevision uint64
}

// QueryPayload is one collection read request.
type QueryPayload struct {
	Tenant    string
	Principal string
	Scopes    []string
	Purpose   string
	Filters   map[string]string
	Fields    []string
	Limit     int
	Offset    int
}

// Reversal names the inverse of one committed write: the operation, object
// and revision it targets, and the key its replay would carry. Reversals
// are terminal: executing one never produces a further reversal.
type Reversal struct {
	Operation        Operation
	ObjectID         string
	ExpectedRevision uint64
	IdempotencyKey   string
	Reason           string
}

// RecordResult is one executed record operation.
type RecordResult struct {
	Receipt  CommitReceipt
	ObjectID string
	Revision uint64
	Fields   map[string]TypedValue
	Retired  bool
	Reversal *Reversal
	Replayed bool
}

// QueryResult is one executed collection read.
type QueryResult struct {
	Report SearchReport
}

// PublishDeps wires a publication to its store, record-rule dependencies,
// clock and runner-answer builder. Answer translates local results for
// runner-originated calls; nil serves direct calls with local results and
// refuses runner calls explicitly. Sequences allocates record numbers and
// defaults to a fresh allocator; definitions using reference or attachment
// fields additionally require RefReleases and Artifacts.
type PublishDeps struct {
	Store  EventProjectionPort
	Access *AccessDeps
	Clock  func() time.Time
	Answer func(op Operation, record RecordResult, query QueryResult, isQuery bool) (any, error)

	Sequences   *Sequences
	RefReleases RefReleaseResolver
	Artifacts   *documentsecurity.Registry
}

// Registration is one published type: its tenant, definition, generated
// manifest, four registered capability definitions and bound handlers.
type Registration struct {
	Tenant       string
	Definition   CustomObjectDefinition
	Manifest     CapabilityManifest
	Capabilities []capability.Definition

	store    EventProjectionPort
	access   *AccessDeps
	clock    func() time.Time
	answer   func(op Operation, record RecordResult, query QueryResult, isQuery bool) (any, error)
	granted  map[string]bool
	handlers map[Operation]capability.Handler

	sequences   *Sequences
	refReleases RefReleaseResolver
	artifacts   *documentsecurity.Registry

	mu      sync.Mutex
	journal map[string]journalEntry
}

type journalEntry struct {
	result   RecordResult
	preWrite map[string]TypedValue
}

// publishedOps is the registered surface in publication order: create,
// update (CHANGE), read and query.
var publishedOps = []struct {
	op     Operation
	name   string
	effect capability.EffectClass
}{
	{OperationCreate, "create", capability.EffectInternalMutation},
	{OperationChange, "update", capability.EffectInternalMutation},
	{OperationRead, "read", capability.EffectReadOnly},
	{OperationQuery, "query", capability.EffectReadOnly},
}

// CapabilityID builds the tenant-owned capability id for one operation. The
// `tenant.` root keeps tenant capabilities out of the platform `hcmnext.`
// namespace by construction.
func CapabilityID(tenant string, def CustomObjectDefinition, op Operation) (string, error) {
	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		return "", fmt.Errorf("%w: tenant is required", ErrRegistrationInvalid)
	}
	if strings.ContainsAny(tenant, " \t\n\r\x00.") {
		return "", fmt.Errorf("%w: tenant %q carries whitespace or dots", ErrRegistrationInvalid, tenant)
	}
	name := ""
	for _, published := range publishedOps {
		if published.op == op {
			name = published.name
		}
	}
	if name == "" {
		return "", fmt.Errorf("%w: operation %s is not published", ErrRegistrationInvalid, op)
	}
	id := "tenant." + tenant + ".custom." + def.Namespace + "." + strings.ToLower(def.Kind) + "." + name
	if strings.HasPrefix(id, "hcmnext.") {
		return "", fmt.Errorf("%w: %q", ErrCapabilityShadow, id)
	}
	return id, nil
}

func ownerDomain(tenant string) string { return "tenant." + tenant + ".custom" }

func typedSchema(capID, slot, kind string, def CustomObjectDefinition) capability.SchemaRef {
	titles := map[string]string{"request": "Request", "response": "Response", "error": "Error"}
	return capability.SchemaRef{
		SchemaID:         capID + "/" + slot + "@" + def.Digest(),
		Version:          uint32(def.Version),
		ProtobufFullName: "hcmnext.custom.v1." + kind + titles[slot],
	}
}

// definitionsFor converts the generated manifest to registry definitions.
// Effect classes come from the manifest's own side effects: an operation
// appending ledger events is an internal mutation, anything else is a read.
func definitionsFor(tenant string, def CustomObjectDefinition, manifest CapabilityManifest) ([]capability.Definition, error) {
	if def.Version > math.MaxUint32 {
		return nil, fmt.Errorf("%w: definition version %d exceeds the capability version range", ErrRegistrationInvalid, def.Version)
	}
	manifestByOp := map[Operation]Capability{}
	for _, c := range manifest.Capabilities {
		manifestByOp[c.Operation] = c
	}
	domains := []string{}
	seen := map[string]bool{}
	for _, c := range manifest.Capabilities {
		for _, d := range c.AuthZDomains {
			if !seen[d] {
				seen[d] = true
				domains = append(domains, d)
			}
		}
	}
	sort.Strings(domains)
	out := make([]capability.Definition, 0, len(publishedOps))
	for _, published := range publishedOps {
		id, err := CapabilityID(tenant, def, published.op)
		if err != nil {
			return nil, err
		}
		effect := capability.EffectReadOnly
		if mc, ok := manifestByOp[published.op]; ok {
			effect = capability.EffectReadOnly
			for _, sideEffect := range mc.SideEffects {
				if sideEffect == "APPEND_LEDGER_EVENT" {
					effect = capability.EffectInternalMutation
				}
			}
			if effect != published.effect {
				return nil, fmt.Errorf("%w: manifest effect for %s disagrees with the published surface", ErrRegistrationInvalid, published.op)
			}
		} else if published.op != OperationQuery {
			return nil, fmt.Errorf("%w: manifest names no %s capability", ErrRegistrationInvalid, published.op)
		}
		idempotency := "idempotency.read-safe.v1"
		var writeData capability.DataDomainFieldSet
		if effect.IsWrite() {
			idempotency = "idempotency.key-required.v1"
			writeData = capability.DataDomainFieldSet{DataDomains: append([]string(nil), domains...)}
		}
		out = append(out, capability.Definition{
			ID:                   id,
			Version:              uint32(def.Version),
			OwnerDomain:          ownerDomain(tenant),
			RequestSchema:        typedSchema(id, "request", def.Kind, def),
			ResponseSchema:       typedSchema(id, "response", def.Kind, def),
			ErrorSchema:          typedSchema(id, "error", def.Kind, def),
			EffectClass:          effect,
			ReadData:             capability.DataDomainFieldSet{DataDomains: append([]string(nil), domains...)},
			WriteData:            writeData,
			RiskClass:            "MEDIUM",
			IdempotencyPolicyRef: idempotency,
			AuthZScopeRef:        "scope:" + id,
			LegalBasisRef:        "legal.tenant.custom.v1",
			EntitlementRef:       "entitlement.tenant.custom.v1",
			SLOClassRef:          "slo.interactive.p95-2s.v1",
			TestRef:              TestRefWFData008,
		})
	}
	return out, nil
}

// RegisterGeneratedCapabilities publishes one custom type: it generates the
// manifest, converts it to tenant-namespaced registry definitions and binds
// store-backed handlers. Republishing the identical definition is a no-op
// success; a conflicting same-version definition, another owner's id, or an
// unusable input fails before anything is registered.
func RegisterGeneratedCapabilities(reg *capability.Registry, tenant string, def CustomObjectDefinition, purpose string, deps PublishDeps) (*Registration, error) {
	if reg == nil {
		return nil, fmt.Errorf("%w: registry is required", ErrRegistrationInvalid)
	}
	if deps.Store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrRegistrationInvalid)
	}
	if deps.Clock == nil {
		return nil, fmt.Errorf("%w: clock is required", ErrRegistrationInvalid)
	}
	if err := def.Validate(); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("%w: reference fields require a release resolver", ErrInvalidFieldKind)
	}
	if needArtifacts && deps.Artifacts == nil {
		return nil, fmt.Errorf("%w: attachment fields require an artifact registry", ErrInvalidFieldKind)
	}
	sequences := deps.Sequences
	if sequences == nil {
		sequences = NewSequences()
	}
	manifest, err := GenerateCapabilities(def, purpose)
	if err != nil {
		return nil, err
	}
	defs, err := definitionsFor(tenant, def, manifest)
	if err != nil {
		return nil, err
	}
	policy, err := ResolvePolicy(def)
	if err != nil {
		return nil, err
	}
	granted := make(map[string]bool, len(policy.Fields))
	for _, field := range policy.Fields {
		granted[field.AuthZDomain] = true
	}
	r := &Registration{
		Tenant: tenant, Definition: def, Manifest: manifest, Capabilities: defs,
		store: deps.Store, access: deps.Access, clock: deps.Clock, answer: deps.Answer,
		granted: granted, handlers: map[Operation]capability.Handler{}, journal: map[string]journalEntry{},
		sequences: sequences, refReleases: deps.RefReleases, artifacts: deps.Artifacts,
	}
	// Two phases so a refused publish registers nothing.
	skip := make([]bool, len(defs))
	for i, defn := range defs {
		rec, found := reg.Lookup(defn.Key())
		if !found {
			continue
		}
		if rec.Definition.OwnerDomain != defn.OwnerDomain {
			return nil, fmt.Errorf("%w: %s is owned by %s", ErrCapabilityShadow, defn.ID, rec.Definition.OwnerDomain)
		}
		want, err := capability.Digest(defn)
		if err != nil {
			return nil, err
		}
		if rec.Digest != want {
			return nil, fmt.Errorf("%w: %s is already published with a different definition", ErrRegistrationConflict, defn.ID)
		}
		skip[i] = true
	}
	for i, defn := range defs {
		op := publishedOps[i].op
		handler := r.Handler(op)
		if !skip[i] {
			if err := reg.Register(defn, handler); err != nil {
				return nil, err
			}
		}
		r.handlers[op] = handler
	}
	return r, nil
}

// Handler returns the bound handler for one published operation. It decodes
// local record and query payloads directly, and workflow runner calls
// through their input texts, translating the latter with the PublishDeps
// answer builder. Unpublished operations get a handler that refuses.
func (r *Registration) Handler(op Operation) capability.Handler {
	published := false
	for _, candidate := range publishedOps {
		if candidate.op == op {
			published = true
		}
	}
	if !published {
		return func(context.Context, any) (any, error) {
			return nil, fmt.Errorf("%w: operation %s is not published", ErrRegistrationInvalid, op)
		}
	}
	return func(ctx context.Context, payload any) (any, error) {
		switch p := payload.(type) {
		case RecordPayload:
			return r.serveRecord(ctx, op, p, nil)
		case QueryPayload:
			if op != OperationQuery {
				return nil, fmt.Errorf("%w: query payload for %s", ErrRegistrationInvalid, op)
			}
			return r.serveQuery(ctx, p, nil)
		case runnerCall:
			return r.serveRunnerCall(ctx, op, p.InputTexts())
		default:
			return nil, fmt.Errorf("%w: unexpected payload %T for %s", ErrRegistrationInvalid, payload, op)
		}
	}
}

func cloneFields(in map[string]TypedValue) map[string]TypedValue {
	out := make(map[string]TypedValue, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (r *Registration) checkTenant(tenant string) error {
	if tenant != r.Tenant {
		return fmt.Errorf("%w: payload tenant %q is outside %q", ErrTenantMismatch, tenant, r.Tenant)
	}
	return nil
}

func (r *Registration) journalKey(objectID, key string) string {
	return r.Tenant + "\x00" + objectID + "\x00" + key
}

// ExecuteRecord runs one create or change request.
func (r *Registration) ExecuteRecord(ctx context.Context, op Operation, p RecordPayload) (RecordResult, error) {
	if op != OperationCreate && op != OperationChange {
		return RecordResult{}, fmt.Errorf("%w: %s is not a record write", ErrRegistrationInvalid, op)
	}
	if err := r.checkTenant(p.Tenant); err != nil {
		return RecordResult{}, err
	}
	if strings.TrimSpace(p.IdempotencyKey) == "" {
		return RecordResult{}, fmt.Errorf("%w: writes carry an idempotency key", ErrRegistrationInvalid)
	}
	if op == OperationCreate && p.ExpectedRevision != 0 {
		return RecordResult{}, fmt.Errorf("%w: create expected revision must be zero", ErrRegistrationInvalid)
	}
	if op == OperationChange && p.ExpectedRevision == 0 {
		return RecordResult{}, fmt.Errorf("%w: change names the revision it targets", ErrRegistrationInvalid)
	}
	if strings.TrimSpace(p.ObjectID) == "" {
		return RecordResult{}, fmt.Errorf("%w: object id is required", ErrRegistrationInvalid)
	}
	key := r.journalKey(p.ObjectID, p.IdempotencyKey)
	r.mu.Lock()
	if prior, ok := r.journal[key]; ok {
		r.mu.Unlock()
		replayed := prior.result
		replayed.Replayed = true
		replayed.Receipt.Replayed = true
		return replayed, nil
	}
	r.mu.Unlock()

	now := r.clock()
	interval, err := values.NewOpenInstantInterval(values.NewInstant(now))
	if err != nil {
		return RecordResult{}, err
	}
	record := CustomRecordRevision{
		ObjectID: p.ObjectID, ObjectKind: r.Definition.Kind, Namespace: r.Definition.Namespace,
		DefinitionVersion: r.Definition.Version, Effective: interval,
		FieldValues: map[string]TypedValue{},
	}
	var preWrite map[string]TypedValue
	var head int64
	var current *RecordResult
	if op == OperationChange {
		read, err := r.readRevision(ctx, p)
		if err != nil {
			return RecordResult{}, err
		}
		if read.Revision != p.ExpectedRevision {
			return RecordResult{}, fmt.Errorf("%w: object=%s expected=%d actual=%d", ErrStaleRevision, p.ObjectID, p.ExpectedRevision, read.Revision)
		}
		preWrite = read.Fields
		head = int64(read.Revision)
		current = &read
	}
	resolved, err := r.resolveFieldValues(ctx, op, p, current, now)
	if err != nil {
		return RecordResult{}, err
	}
	for name, field := range r.Definition.Fields {
		text, ok := resolved[name]
		if !ok {
			return RecordResult{}, fmt.Errorf("%w: missing field value for %q", ErrRegistrationInvalid, name)
		}
		record.FieldValues[name] = TypedValue{FieldName: name, Type: field.Type, Value: text}
	}
	receipt, err := r.store.Commit(ctx, MutationRequest{
		Tenant: values.TenantId(r.Tenant), ObjectID: p.ObjectID, Definition: r.Definition,
		Record: record, Operation: op, ExpectedHead: head, ExpectedRevision: p.ExpectedRevision,
		Actor: p.Actor, Purpose: p.Purpose, EvidenceDigest: p.EvidenceDigest,
		GrantedDomains: r.granted, OccurredAt: now, EffectiveAt: now,
	})
	if err != nil {
		return RecordResult{}, err
	}
	result := RecordResult{
		Receipt: receipt, ObjectID: p.ObjectID, Revision: receipt.Event.Revision,
		Fields: cloneFields(receipt.Event.Record.FieldValues),
	}
	if op == OperationCreate {
		result.Reversal = &Reversal{
			Operation: OperationRetire, ObjectID: p.ObjectID, ExpectedRevision: receipt.Event.Revision,
			IdempotencyKey: "reverse:" + p.IdempotencyKey,
			Reason:         "a create reverses by retiring its revision",
		}
	} else {
		result.Reversal = &Reversal{
			Operation: OperationCorrect, ObjectID: p.ObjectID, ExpectedRevision: receipt.Event.Revision,
			IdempotencyKey: "reverse:" + p.IdempotencyKey,
			Reason:         "a change reverses by correcting back to the pre-write values",
		}
	}
	r.mu.Lock()
	r.journal[key] = journalEntry{result: result, preWrite: preWrite}
	r.mu.Unlock()
	return result, nil
}

// resolveFieldValues resolves governed values through the shared helper,
// carrying the stored texts a change revises.
func (r *Registration) resolveFieldValues(ctx context.Context, op Operation, p RecordPayload, current *RecordResult, now time.Time) (map[string]string, error) {
	var stored map[string]string
	if current != nil {
		stored = make(map[string]string, len(current.Fields))
		for name, value := range current.Fields {
			// Governed values are canonical text; plain values keep
			// their types and never reach the sequence comparison.
			if text, ok := value.Value.(string); ok {
				stored[name] = text
			}
		}
	}
	return ResolveGovernedFields(ctx, r.Definition, r.Tenant, op, p.Fields, stored, now, FieldValueDeps{
		Sequences: r.sequences, RefReleases: r.refReleases, Artifacts: r.artifacts,
	})
}

// ExecuteRead reads one record by object id.
func (r *Registration) ExecuteRead(ctx context.Context, p RecordPayload) (RecordResult, error) {
	if err := r.checkTenant(p.Tenant); err != nil {
		return RecordResult{}, err
	}
	if strings.TrimSpace(p.ObjectID) == "" {
		return RecordResult{}, fmt.Errorf("%w: object id is required", ErrRegistrationInvalid)
	}
	return r.readRevision(ctx, p)
}

func (r *Registration) readRevision(ctx context.Context, p RecordPayload) (RecordResult, error) {
	report, err := r.store.Search(ctx, SearchRequest{
		Tenant: values.TenantId(r.Tenant), Definition: r.Definition, Purpose: p.Purpose,
		GrantedDomains: r.granted,
		Principal:      Principal{ID: p.Principal, Scopes: p.Scopes}, Access: r.access,
	})
	if err != nil {
		return RecordResult{}, err
	}
	for _, rec := range report.Records {
		if rec.ObjectID == p.ObjectID {
			return RecordResult{
				ObjectID: rec.ObjectID, Revision: rec.Revision,
				Fields: cloneFields(rec.Fields),
			}, nil
		}
	}
	return RecordResult{}, fmt.Errorf("%w: object %s", ErrNotFound, p.ObjectID)
}

// ExecuteQuery runs one collection read.
func (r *Registration) ExecuteQuery(ctx context.Context, q QueryPayload) (QueryResult, error) {
	if err := r.checkTenant(q.Tenant); err != nil {
		return QueryResult{}, err
	}
	report, err := r.store.Search(ctx, SearchRequest{
		Tenant: values.TenantId(r.Tenant), Definition: r.Definition, Query: q.Filters,
		RequestedFields: q.Fields, GrantedDomains: r.granted, Purpose: q.Purpose,
		Principal: Principal{ID: q.Principal, Scopes: q.Scopes}, Access: r.access,
		Limit: q.Limit, Offset: q.Offset,
	})
	if err != nil {
		return QueryResult{}, err
	}
	return QueryResult{Report: report}, nil
}

// ExecuteReversal executes one reversal descriptor: a create reverses by
// retiring its revision, a change by correcting back to the journaled
// pre-write values. Reversals are terminal and carry no further reversal.
func (r *Registration) ExecuteReversal(ctx context.Context, rev Reversal, actor, purpose, evidenceDigest string) (RecordResult, error) {
	if strings.TrimSpace(rev.ObjectID) == "" {
		return RecordResult{}, fmt.Errorf("%w: reversal names no object", ErrRegistrationInvalid)
	}
	now := r.clock()
	interval, err := values.NewOpenInstantInterval(values.NewInstant(now))
	if err != nil {
		return RecordResult{}, err
	}
	switch rev.Operation {
	case OperationRetire:
		current, err := r.readRevision(ctx, RecordPayload{Tenant: r.Tenant, Principal: actor, ObjectID: rev.ObjectID, Purpose: purpose})
		if err != nil {
			return RecordResult{}, err
		}
		if current.Revision != rev.ExpectedRevision {
			return RecordResult{}, fmt.Errorf("%w: object=%s expected=%d actual=%d", ErrStaleRevision, rev.ObjectID, rev.ExpectedRevision, current.Revision)
		}
		record := CustomRecordRevision{
			ObjectID: rev.ObjectID, ObjectKind: r.Definition.Kind, Namespace: r.Definition.Namespace,
			DefinitionVersion: r.Definition.Version, Effective: interval,
			FieldValues: cloneFields(current.Fields),
		}
		receipt, err := r.store.Commit(ctx, MutationRequest{
			Tenant: values.TenantId(r.Tenant), ObjectID: rev.ObjectID, Definition: r.Definition,
			Record: record, Operation: OperationRetire,
			ExpectedHead: int64(rev.ExpectedRevision), ExpectedRevision: rev.ExpectedRevision,
			Actor: actor, Purpose: purpose, EvidenceDigest: evidenceDigest,
			GrantedDomains: r.granted, OccurredAt: now, EffectiveAt: now,
		})
		if err != nil {
			return RecordResult{}, err
		}
		return RecordResult{
			Receipt: receipt, ObjectID: rev.ObjectID, Revision: receipt.Event.Revision,
			Fields: cloneFields(receipt.Event.Record.FieldValues), Retired: true,
		}, nil
	case OperationCorrect:
		orig, found := strings.CutPrefix(rev.IdempotencyKey, "reverse:")
		if !found {
			return RecordResult{}, fmt.Errorf("%w: reversal key %q names no write", ErrReversalUnavailable, rev.IdempotencyKey)
		}
		r.mu.Lock()
		prior, ok := r.journal[r.journalKey(rev.ObjectID, orig)]
		r.mu.Unlock()
		if !ok || prior.preWrite == nil {
			return RecordResult{}, fmt.Errorf("%w: no journaled pre-write for %s", ErrReversalUnavailable, rev.ObjectID)
		}
		record := CustomRecordRevision{
			ObjectID: rev.ObjectID, ObjectKind: r.Definition.Kind, Namespace: r.Definition.Namespace,
			DefinitionVersion: r.Definition.Version, Effective: interval,
			FieldValues: cloneFields(prior.preWrite),
		}
		receipt, err := r.store.Commit(ctx, MutationRequest{
			Tenant: values.TenantId(r.Tenant), ObjectID: rev.ObjectID, Definition: r.Definition,
			Record: record, Operation: OperationCorrect,
			ExpectedHead: int64(rev.ExpectedRevision), ExpectedRevision: rev.ExpectedRevision,
			Actor: actor, Purpose: purpose, EvidenceDigest: evidenceDigest,
			GrantedDomains: r.granted, OccurredAt: now, EffectiveAt: now,
		})
		if err != nil {
			return RecordResult{}, err
		}
		return RecordResult{
			Receipt: receipt, ObjectID: rev.ObjectID, Revision: receipt.Event.Revision,
			Fields: cloneFields(receipt.Event.Record.FieldValues),
		}, nil
	default:
		return RecordResult{}, fmt.Errorf("%w: %s is not a produced reversal", ErrRegistrationInvalid, rev.Operation)
	}
}

func (r *Registration) serveRecord(ctx context.Context, op Operation, p RecordPayload, translate func(RecordResult) (any, error)) (any, error) {
	var result RecordResult
	var err error
	switch op {
	case OperationCreate, OperationChange:
		result, err = r.ExecuteRecord(ctx, op, p)
	case OperationRead:
		result, err = r.ExecuteRead(ctx, p)
	default:
		return nil, fmt.Errorf("%w: %s takes no record payload", ErrRegistrationInvalid, op)
	}
	if err != nil {
		return nil, err
	}
	if translate != nil {
		return translate(result)
	}
	return result, nil
}

func (r *Registration) serveQuery(ctx context.Context, q QueryPayload, translate func(QueryResult) (any, error)) (any, error) {
	result, err := r.ExecuteQuery(ctx, q)
	if err != nil {
		return nil, err
	}
	if translate != nil {
		return translate(result)
	}
	return result, nil
}

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseNonNegative(raw, what string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%w: %s %q is not a non-negative integer", ErrRegistrationInvalid, what, raw)
	}
	return n, nil
}

// serveRunnerCall decodes one workflow runner call through its input texts
// and translates the local result with the PublishDeps answer builder.
func (r *Registration) serveRunnerCall(ctx context.Context, op Operation, texts map[string]string) (any, error) {
	if r.answer == nil {
		return nil, fmt.Errorf("%w: registration has no runner answer builder", ErrRegistrationInvalid)
	}
	if op == OperationQuery {
		limit, err := parseNonNegative(texts[callLimit], "limit")
		if err != nil {
			return nil, err
		}
		offset, err := parseNonNegative(texts[callOffset], "offset")
		if err != nil {
			return nil, err
		}
		filters := map[string]string{}
		for key, value := range texts {
			if rest, ok := strings.CutPrefix(key, callFilterPrefix); ok {
				filters[rest] = value
			}
		}
		return r.serveQuery(ctx, QueryPayload{
			Tenant: texts[callTenant], Principal: texts[callPrincipal], Scopes: splitList(texts[callScopes]),
			Purpose: texts[callPurpose], Filters: filters,
			Fields: splitList(texts[callFields]), Limit: limit, Offset: offset,
		}, func(result QueryResult) (any, error) { return r.answer(op, RecordResult{}, result, true) })
	}
	var expected uint64
	if raw := strings.TrimSpace(texts[callExpectedRevision]); raw != "" {
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: expected revision %q is not a number", ErrRegistrationInvalid, raw)
		}
		expected = n
	}
	fields := map[string]string{}
	for key, value := range texts {
		if rest, ok := strings.CutPrefix(key, callFieldPrefix); ok {
			fields[rest] = value
		}
	}
	return r.serveRecord(ctx, op, RecordPayload{
		Tenant: texts[callTenant], Principal: texts[callPrincipal], Scopes: splitList(texts[callScopes]),
		ObjectID: texts[callObjectID], Fields: fields, IdempotencyKey: texts[callIdempotencyKey],
		Actor: texts[callActor], Purpose: texts[callPurpose], EvidenceDigest: texts[callEvidenceDigest],
		ExpectedRevision: expected,
	}, func(result RecordResult) (any, error) { return r.answer(op, result, QueryResult{}, false) })
}
