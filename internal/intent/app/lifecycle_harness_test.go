package app

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// This file is the EP-INTENT-003 application-layer harness: a real
// [IntentService] composed with an in-memory, mutex-protected [Store] (so
// CancelIntent and SupersedeIntent reach a real compare-and-swap the same
// shape internal/intent/app/pgstore uses, without embedded PostgreSQL) and a
// configurable fake [SafePoints] port. It never fakes the RPC methods
// themselves: every assertion in lifecycle_contract_test.go calls
// SubmitIntent, CancelIntent and SupersedeIntent on this real service.
//
// SubmitIntent's own contract test composes a *separate* harness through
// [NewCell] and the real promote_worker production definition, because
// SubmitIntent re-simulates through the full capability-gateway path
// ([IntentService.simulateDetailed]) and this file's harness definition
// deliberately has no domain behind it: Cancel and Supersede never invoke
// the gateway at all, so a minimal definition is what proves their own
// contract without dragging in unrelated domain fixtures.

const (
	lifecycleTestTenant   = "acme-corp"
	lifecycleTestOrgScope = "org-north-america"
	lifecycleTestPurpose  = "hcm_operations"
)

var lifecycleFixedNow = time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)

// lifecycleHarnessDefinition is a self-contained ChangeRequest definition
// with no AllowedTransitions narrowing, so [intent.CancelInstance] and
// [intent.SupersedeOriginal] are exercised against the full kernel lifecycle
// profile ([lifecycle.KernelProfiles]) -- including EXECUTING, COMMITTED and
// REPAIR_REQUIRED, which the checked-in production promote_worker definition
// does not reach in this release (it is simulate-only).
func lifecycleHarnessDefinition() intent.Definition {
	return intent.Definition{
		Ref:         intent.Ref{TypeID: "hcmnext.test.lifecycle_harness", Version: 1},
		DisplayName: "LifecycleHarnessFixture",
		Description: "EP-INTENT-003 application-layer harness fixture.",
		OwnerDomain: "TEST",
		Family:      intent.FamilyChangeRequest,
		Maturity:    intent.MaturityDraftContract,
		SideEffect:  intent.SideEffectInternalMutation,
		EffectClass: intent.EffectClassInternalMutation,
		Release:     intent.ReleaseP1B,
		InputSchema: intent.SchemaRef{
			SchemaID: "hcmnext.test.v1.HarnessRequest", Version: 1,
			ProtobufFullName: "hcmnext.test.v1.HarnessRequest",
		},
		ResultSchema: intent.SchemaRef{
			SchemaID: "hcmnext.test.v1.HarnessResult", Version: 1,
			ProtobufFullName: "hcmnext.test.v1.HarnessResult",
		},
		PhaseDepth:              "GATE_B_IMPLEMENT",
		AllowedInitiators:       []intent.Initiator{intent.InitiatorHuman},
		AllowedModes:            []intent.Mode{intent.ModeSimulate, intent.ModeExecute},
		SubjectKinds:            []string{"WORKER"},
		ApprovalRequired:        false,
		DataClassificationFloor: "CONFIDENTIAL_HR",
		RetentionClass:          "TEST_RETENTION",
	}
}

func lifecycleHarnessRegistry(t *testing.T) *intent.Registry {
	t.Helper()
	def := lifecycleHarnessDefinition()
	reg, err := intent.NewRegistry(intent.ProfileBootstrap, []intent.Definition{def}, nil, intent.Catalog{
		Schemas: []intent.SchemaRef{def.InputSchema, def.ResultSchema},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return reg
}

// memLifecycleStore is a minimal, mutex-protected, in-memory app.Store plus
// app.LifecycleMutator. MutateLifecycle performs the exact same
// compare-and-swap shape pgstore.Store.MutateLifecycle performs against
// PostgreSQL: it refuses a stale ExpectedInstanceVersion under the store's
// own lock, which is what makes the RACE test in
// lifecycle_contract_test.go meaningful rather than merely "ran goroutines".
type memLifecycleStore struct {
	mu         sync.Mutex
	byKey      map[string]IntentRecord
	idempotent map[string]string
}

func newMemLifecycleStore() *memLifecycleStore {
	return &memLifecycleStore{byKey: map[string]IntentRecord{}, idempotent: map[string]string{}}
}

func memStoreKey(tenant, id string) string { return tenant + "|" + id }

func (m *memLifecycleStore) Bootstrap(context.Context, string) error { return nil }

func (m *memLifecycleStore) AppendIntent(_ context.Context, rec IntentRecord) (AppendResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idemKey := memStoreKey(rec.Tenant, rec.IdempotencyKey)
	if existingID, ok := m.idempotent[idemKey]; ok {
		existing := m.byKey[memStoreKey(rec.Tenant, existingID)]
		return AppendResult{Record: existing, Replayed: true}, nil
	}
	m.byKey[memStoreKey(rec.Tenant, rec.IntentID)] = rec
	m.idempotent[idemKey] = rec.IntentID
	return AppendResult{Record: rec}, nil
}

func (m *memLifecycleStore) LoadIntent(_ context.Context, tenant, intentID string) (IntentRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.byKey[memStoreKey(tenant, intentID)]
	if !ok {
		return IntentRecord{}, ErrIntentNotFound
	}
	return rec, nil
}

// ListIntents returns every record of tenant in identifier order, as the
// PostgreSQL store does, in one page.
func (m *memLifecycleStore) ListIntents(_ context.Context, tenant string, _ int32, _ string) (IntentPage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	page := IntentPage{}
	for _, rec := range m.byKey {
		if rec.Tenant == tenant {
			page.Records = append(page.Records, rec)
		}
	}
	sort.Slice(page.Records, func(i, j int) bool { return page.Records[i].IntentID < page.Records[j].IntentID })
	return page, nil
}

func (m *memLifecycleStore) Timeline(context.Context, string, string) ([]TimelineEntry, error) {
	return nil, nil
}

// MutateLifecycle implements LifecycleMutator with a real compare-and-swap
// under the store's own mutex: a caller presenting a stale
// ExpectedInstanceVersion is refused with [ErrOutcomeProjectionConflict],
// never silently applied.
func (m *memLifecycleStore) MutateLifecycle(_ context.Context, mut LifecycleMutation) (IntentRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memStoreKey(mut.Tenant, mut.IntentID)
	rec, ok := m.byKey[key]
	if !ok {
		return IntentRecord{}, ErrIntentNotFound
	}
	if rec.InstanceVersion != mut.ExpectedInstanceVersion {
		return IntentRecord{}, fmt.Errorf("%w: stored instance version %d, expected %d",
			ErrOutcomeProjectionConflict, rec.InstanceVersion, mut.ExpectedInstanceVersion)
	}
	rec.Lifecycle = mut.Lifecycle
	rec.InstanceVersion++
	if mut.CommitReceiptRef != "" {
		rec.CommitReceiptRef = mut.CommitReceiptRef
	}
	if mut.RepairRef != "" {
		rec.RepairRef = mut.RepairRef
	}
	rec.RecordedAt = mut.RecordedAt
	rec.LastTransitionAt = mut.RecordedAt
	m.byKey[key] = rec
	return rec, nil
}

var (
	_ Store            = (*memLifecycleStore)(nil)
	_ LifecycleMutator = (*memLifecycleStore)(nil)
	_ OutcomeBinder    = (*memLifecycleStore)(nil)
)

// BindOutcome is not exercised by any EP-INTENT-003 test; it exists only so
// memLifecycleStore satisfies OutcomeBinder for the compile-time assertion
// above, which documents that a real adapter implements both ports.
func (m *memLifecycleStore) BindOutcome(context.Context, OutcomeBinding) error {
	return fmt.Errorf("memLifecycleStore: BindOutcome is not used by this harness")
}

// noLifecycleInputs is the DomainInputs stub for the lifecycle harness:
// Submit, Cancel and Supersede never resolve a domain read, and a test that
// somehow reached this port would rather fail loudly than silently answer
// with a zero-value DomainCall.
type noLifecycleInputs struct{}

func (noLifecycleInputs) Resolve(context.Context, ResolveRequest) (DomainCall, error) {
	return DomainCall{}, fmt.Errorf("lifecycle harness: this fixture resolves no domain read")
}

// safePointFact is one configured answer [fakeSafePoints] returns for one
// intent id.
type safePointFact struct {
	point     intent.CancellationPoint
	repairRef string
}

// fakeSafePoints is the configurable SafePoints fake: each test tells it
// what to answer for the specific intents it seeds, so the four cancellation
// scenarios in lifecycle_contract_test.go are driven by explicit, named
// configuration rather than an implicit default.
type fakeSafePoints struct {
	mu   sync.Mutex
	byID map[string]safePointFact
}

func newFakeSafePoints() *fakeSafePoints {
	return &fakeSafePoints{byID: map[string]safePointFact{}}
}

func (f *fakeSafePoints) set(intentID string, point intent.CancellationPoint, repairRef string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[intentID] = safePointFact{point: point, repairRef: repairRef}
}

func (f *fakeSafePoints) At(_ context.Context, _, intentID string) (intent.CancellationPoint, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fact, ok := f.byID[intentID]
	if !ok {
		return intent.CancellationPointUnknown, "", nil
	}
	return fact.point, fact.repairRef, nil
}

var _ SafePoints = (*fakeSafePoints)(nil)

// lifecyclePrincipal returns the harness's one authenticated principal.
func lifecyclePrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               lifecycleTestTenant,
		Subject:              "lifecycle-test-subject",
		SubjectKind:          trust.SubjectKindHuman,
		OrganizationScopeID:  lifecycleTestOrgScope,
		Roles:                []string{"intent_author"},
		Purposes:             []string{lifecycleTestPurpose},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "lifecycle-test-session",
		IssuedAt:             lifecycleFixedNow.Add(-time.Minute),
		ExpiresAt:            lifecycleFixedNow.Add(time.Hour),
		CredentialDigest:     "credential-digest",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

// lifecycleCtx returns a context carrying the harness's authenticated
// principal, exactly as an admission interceptor would before a handler runs.
func lifecycleCtx(t *testing.T) context.Context {
	return trust.WithPrincipal(context.Background(), lifecyclePrincipal(t))
}

// lifecycleHarness is everything one EP-INTENT-003 application-layer test
// needs: a real IntentService, its in-memory store and its safe-point fake.
type lifecycleHarness struct {
	Service    *IntentService
	Store      *memLifecycleStore
	SafePoints *fakeSafePoints
	Def        intent.Definition
}

func newLifecycleHarness(t *testing.T) *lifecycleHarness {
	t.Helper()
	def := lifecycleHarnessDefinition()
	reg := lifecycleHarnessRegistry(t)
	caps := capability.NewRegistry()
	sink := NewMemoryEvidenceSink()
	gateway := capability.NewGateway(caps, sink)
	digester, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("NewDefaultDigester: %v", err)
	}
	store := newMemLifecycleStore()
	safePoints := newFakeSafePoints()

	svc, err := NewIntentService(Options{
		Definitions:  reg,
		Capabilities: caps,
		Gateway:      gateway,
		Store:        store,
		Inputs:       noLifecycleInputs{},
		Digester:     digester,
		Controls:     NewControls(reg, caps),
		Clock:        func() values.Instant { return values.NewInstant(lifecycleFixedNow) },
		Evidence:     sink,
		Idempotency:  endpoint.NewCoordinator(),
		SafePoints:   safePoints,
	})
	if err != nil {
		t.Fatalf("NewIntentService: %v", err)
	}
	return &lifecycleHarness{Service: svc, Store: store, SafePoints: safePoints, Def: def}
}

// seed inserts one intent directly into the store's projection, under the
// harness definition, at the given dimensional tuple -- bypassing
// CreateIntent/SimulateIntent entirely, because CancelIntent and
// SupersedeIntent never re-simulate: they act on whatever the store already
// holds. opts may adjust the instance before it is encoded (CommitReceiptRef
// for a COMMITTED fixture, RepairRef for a pre-existing REPAIR_REQUIRED
// fixture, extra proposal revisions for the Mutation test's survival proof).
func (h *lifecycleHarness) seed(t *testing.T, intentID string, dims lifecycle.Dimensions, opts ...func(*intent.Instance)) intent.Instance {
	t.Helper()
	inst := intent.Instance{
		IntentID:            intentID,
		Definition:          h.Def.Ref,
		Tenant:              lifecycleTestTenant,
		OrganizationScopeID: lifecycleTestOrgScope,
		Initiator: intent.PrincipalReference{
			PrincipalID: "seed-principal", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "seed-assurance",
		},
		Purpose: lifecycleTestPurpose,
		Subjects: []intent.SubjectReference{
			{Kind: "WORKER", SubjectID: "worker-" + intentID, AuthorityDomain: "PEOPLE"},
		},
		Request:                intent.TypedPayload{Schema: h.Def.InputSchema, WireBytes: []byte("seed-payload-" + intentID)},
		IdempotencyKey:         "seed-" + intentID,
		CorrelationID:          "seed-correlation-" + intentID,
		TraceID:                "seed-trace-" + intentID,
		Classification:         "CONFIDENTIAL_HR",
		RetentionClass:         "TEST_RETENTION",
		CanonicalRequestDigest: digest.Reference{AlgorithmID: "sha256", Digest: "seed-digest-" + intentID},
		Lifecycle:              dims,
		CreatedAt:              values.NewInstant(lifecycleFixedNow),
		RecordedAt:             values.NewInstant(lifecycleFixedNow),
		LastTransitionAt:       values.NewInstant(lifecycleFixedNow),
		ExecutionMode:          intent.ModeExecute,
		InstanceVersion:        1,
	}
	for _, opt := range opts {
		opt(&inst)
	}
	bytes, err := encodeEnvelope(inst)
	if err != nil {
		t.Fatalf("encodeEnvelope(%s): %v", intentID, err)
	}
	h.Store.mu.Lock()
	h.Store.byKey[memStoreKey(string(inst.Tenant), inst.IntentID)] = IntentRecord{
		Tenant: string(inst.Tenant), IntentID: inst.IntentID, Definition: inst.Definition,
		IdempotencyKey: inst.IdempotencyKey, CorrelationID: inst.CorrelationID,
		Lifecycle: inst.Lifecycle, InstanceVersion: inst.InstanceVersion,
		RequestDigest: inst.CanonicalRequestDigest, CreatedAt: inst.CreatedAt.Time(),
		RecordedAt: inst.RecordedAt.Time(), LastTransitionAt: inst.LastTransitionAt.Time(),
		CommitReceiptRef: inst.CommitReceiptRef, RepairRef: inst.RepairRef,
		Envelope: bytes, EnvelopeSchemaRef: EnvelopeSchemaRef,
	}
	h.Store.mu.Unlock()
	return inst
}

// load reads one intent back exactly as the RPC methods do (frozen envelope
// plus the projected lifecycle/version/commit/repair fields).
func (h *lifecycleHarness) load(t *testing.T, intentID string) intent.Instance {
	t.Helper()
	inst, _, ownedErr := h.Service.loadInstance(lifecycleCtx(t), lifecycleTestTenant, intentID)
	if ownedErr != nil {
		t.Fatalf("loadInstance(%s): %v", intentID, ownedErr)
	}
	return inst
}
