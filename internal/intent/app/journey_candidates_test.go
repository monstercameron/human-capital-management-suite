package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	intentdefinitions "github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_PROMOUX_002(t *testing.T) {
	active := intent.Instance{Tenant: "tenant-a", Definition: intent.Ref{TypeID: promotion.IntentType}, Lifecycle: lifecycle.Dimensions{Request: lifecycle.RequestSimulated}, Subjects: []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "worker-1"}}}
	terminal := active
	terminal.IntentID = "terminal"
	terminal.Lifecycle.Request = lifecycle.RequestClosed
	foreign := active
	foreign.IntentID = "foreign"
	foreign.Tenant = "tenant-b"
	wrongSubject := active
	wrongSubject.IntentID = "position-only"
	wrongSubject.Subjects = []intent.SubjectReference{{Kind: "POSITION", SubjectID: "worker-1"}}
	wrongType := active
	wrongType.IntentID = "other-intent"
	wrongType.Definition.TypeID = "hcmnext.people.transfer_worker"

	if got, ok := FindActivePromotion([]intent.Instance{terminal, foreign, wrongSubject, wrongType, active}, "tenant-a", "worker-1"); !ok || got.IntentID != active.IntentID {
		t.Fatalf("FindActivePromotion = %s/%t, want the active same-tenant worker", got.IntentID, ok)
	}
	if _, ok := FindActivePromotion([]intent.Instance{terminal, foreign, wrongSubject, wrongType}, "tenant-a", "worker-1"); ok {
		t.Fatal("terminal, cross-tenant, wrong-subject and wrong-type intents must not trigger the duplicate guard")
	}
	if got, ok := FindActivePromotion([]intent.Instance{active}, " tenant-a ", " worker-1 "); !ok || got.IntentID != active.IntentID {
		t.Fatalf("FindActivePromotion should normalize lookup references, got %s/%t", got.IntentID, ok)
	}
}

// promotionScanStore exposes the persisted envelopes to the admission scan
// without introducing a second fake service. It embeds the lifecycle harness
// store for the unused Store methods and overrides only the tenant list read.
type promotionScanStore struct{ *memLifecycleStore }

func (s *promotionScanStore) ListIntents(_ context.Context, tenant string, _ int32, _ string) (IntentPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	page := IntentPage{}
	for key, record := range s.byKey {
		if strings.HasPrefix(key, tenant+"|") {
			page.Records = append(page.Records, record)
		}
	}
	return page, nil
}

func promotionScanRecord(t *testing.T, inst intent.Instance) IntentRecord {
	t.Helper()
	bytes, err := encodeEnvelope(inst)
	if err != nil {
		t.Fatalf("encode promotion envelope: %v", err)
	}
	return IntentRecord{
		Tenant: string(inst.Tenant), IntentID: inst.IntentID, Definition: inst.Definition,
		IdempotencyKey: inst.IdempotencyKey, Lifecycle: inst.Lifecycle,
		InstanceVersion: inst.InstanceVersion, RequestDigest: inst.CanonicalRequestDigest,
		CreatedAt: inst.CreatedAt.Time(), RecordedAt: inst.RecordedAt.Time(),
		LastTransitionAt: inst.LastTransitionAt.Time(), Envelope: bytes,
		EnvelopeSchemaRef: EnvelopeSchemaRef,
	}
}

func TestTodo_PROMOUX_002_AdmissionScanReadsTenantScopedPersistedPromotion(t *testing.T) {
	now := values.NewInstant(time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC))
	active := intent.Instance{
		IntentID: "active-promotion", Definition: intent.Ref{TypeID: promotion.IntentType, Version: 1},
		Tenant: "tenant-a", Initiator: intent.PrincipalReference{PrincipalID: "p", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "assurance"},
		Purpose: "hcm_operations", Subjects: []intent.SubjectReference{{Kind: "EMPLOYMENT", SubjectID: "worker-1", AuthorityDomain: "PEOPLE"}},
		Request:        intent.TypedPayload{Schema: intent.SchemaRef{SchemaID: "request", Version: 1, ProtobufFullName: "request"}, WireBytes: []byte("payload")},
		IdempotencyKey: "active-key", CorrelationID: "correlation", TraceID: "trace",
		CanonicalRequestDigest: digest.Reference{AlgorithmID: "sha256", Digest: "active-digest"},
		Lifecycle:              lifecycle.Dimensions{Request: lifecycle.RequestSubmitted, Execution: lifecycle.ExecutionNotPlanned},
		CreatedAt:              now, RecordedAt: now, LastTransitionAt: now, ExecutionMode: intent.ModeSimulate, InstanceVersion: 1,
	}
	store := &promotionScanStore{memLifecycleStore: newMemLifecycleStore()}
	store.byKey[memStoreKey("tenant-a", active.IntentID)] = promotionScanRecord(t, active)
	svc := &IntentService{store: store}

	got, found, err := svc.findActivePromotion(context.Background(), "tenant-a", "worker-1")
	if err != nil {
		t.Fatalf("findActivePromotion: %v", err)
	}
	if !found || got.IntentID != active.IntentID {
		t.Fatalf("findActivePromotion = %s/%t, want persisted active promotion", got.IntentID, found)
	}
	if _, found, err := svc.findActivePromotion(context.Background(), "tenant-b", "worker-1"); err != nil || found {
		t.Fatalf("cross-tenant scan = found %t/error %v, want no visible active promotion", found, err)
	}
}

func TestTodo_PROMOUX_002_CreateIntentRejectsSecondActivePromotion(t *testing.T) {
	reg, err := intentdefinitions.NewRegistry()
	if err != nil {
		t.Fatalf("definitions.NewRegistry: %v", err)
	}
	caps := capability.NewRegistry()
	sink := NewMemoryEvidenceSink()
	digester, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("NewDefaultDigester: %v", err)
	}
	store := &promotionScanStore{memLifecycleStore: newMemLifecycleStore()}
	svc, err := NewIntentService(Options{
		Definitions: reg, Capabilities: caps, Gateway: capability.NewGateway(caps, sink), Store: store,
		Inputs: noLifecycleInputs{}, Digester: digester, Controls: NewControls(reg, caps),
		Clock: func() values.Instant { return values.NewInstant(lifecycleFixedNow) }, Evidence: sink,
		Idempotency: endpoint.NewCoordinator(),
	})
	if err != nil {
		t.Fatalf("NewIntentService: %v", err)
	}
	principal := lifecyclePrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), principal)

	firstRequest := promoteWorkerCreateRequest(t, principal, "promo-active-first")
	first, err := svc.CreateIntent(ctx, firstRequest)
	if err != nil {
		t.Fatalf("first CreateIntent: %v", err)
	}
	if first.GetIntent().GetIntentId() == "" {
		t.Fatal("first CreateIntent returned no intent id")
	}
	replay, err := svc.CreateIntent(ctx, firstRequest)
	if err != nil {
		t.Fatalf("exact replay CreateIntent: %v", err)
	}
	if replay.GetIntent().GetIntentId() != first.GetIntent().GetIntentId() {
		t.Fatalf("exact replay returned intent %q, want %q", replay.GetIntent().GetIntentId(), first.GetIntent().GetIntentId())
	}

	secondRequest := promoteWorkerCreateRequest(t, principal, "promo-active-second")
	if _, err := svc.CreateIntent(ctx, secondRequest); err == nil {
		t.Fatal("second active promotion succeeded, want a duplicate refusal")
	} else {
		var owned *envelope.Error
		if !errors.As(err, &owned) || owned.Code() != envelope.CodeAlreadyExists || owned.ReasonRef() != reasonPromotionActive {
			t.Fatalf("second active promotion error = %v, want ALREADY_EXISTS/%s", err, reasonPromotionActive)
		}
	}

	// A terminal promotion does not block a later start for the same worker.
	store.mu.Lock()
	activeRecord := store.byKey[memStoreKey(string(principal.Tenant()), first.GetIntent().GetIntentId())]
	terminal, err := decodeEnvelope(activeRecord.Envelope)
	if err != nil {
		store.mu.Unlock()
		t.Fatalf("decode first promotion: %v", err)
	}
	terminal.Lifecycle.Request = lifecycle.RequestClosed
	store.byKey[memStoreKey(string(principal.Tenant()), terminal.IntentID)] = promotionScanRecord(t, terminal)
	store.mu.Unlock()
	terminalRequest := promoteWorkerCreateRequest(t, principal, "promo-terminal-replacement")
	if _, err := svc.CreateIntent(ctx, terminalRequest); err != nil {
		t.Fatalf("terminal promotion should allow a replacement: %v", err)
	}

	// A different worker is independent even while Omar's replacement is active.
	otherWorkerRequest := promoteWorkerCreateRequest(t, principal, "promo-other-worker")
	otherWorkerRequest.Subjects[0].SubjectId = "worker-other"
	if _, err := svc.CreateIntent(ctx, otherWorkerRequest); err != nil {
		t.Fatalf("other worker promotion should be admitted: %v", err)
	}
}

func TestTodo_PROMOUX_002_RaceSerializesActivePromotionAdmission(t *testing.T) {
	reg, err := intentdefinitions.NewRegistry()
	if err != nil {
		t.Fatalf("definitions.NewRegistry: %v", err)
	}
	caps := capability.NewRegistry()
	sink := NewMemoryEvidenceSink()
	digester, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("NewDefaultDigester: %v", err)
	}
	store := &promotionScanStore{memLifecycleStore: newMemLifecycleStore()}
	svc, err := NewIntentService(Options{
		Definitions: reg, Capabilities: caps, Gateway: capability.NewGateway(caps, sink), Store: store,
		Inputs: noLifecycleInputs{}, Digester: digester, Controls: NewControls(reg, caps),
		Clock: func() values.Instant { return values.NewInstant(lifecycleFixedNow) }, Evidence: sink,
		Idempotency: endpoint.NewCoordinator(),
	})
	if err != nil {
		t.Fatalf("NewIntentService: %v", err)
	}
	principal := lifecyclePrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), principal)
	const callers = 8
	requests := make([]*intentsv1.CreateIntentRequest, callers)
	for i := range requests {
		requests[i] = promoteWorkerCreateRequest(t, principal, fmt.Sprintf("promo-race-%d", i))
	}
	results := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, callErr := svc.CreateIntent(ctx, requests[i])
			results <- callErr
		}(i)
	}
	wg.Wait()
	close(results)

	var admitted, refused int
	for callErr := range results {
		switch {
		case callErr == nil:
			admitted++
		default:
			var owned *envelope.Error
			if !errors.As(callErr, &owned) || owned.Code() != envelope.CodeAlreadyExists || owned.ReasonRef() != reasonPromotionActive {
				t.Fatalf("race refusal = %v, want active-promotion conflict", callErr)
			}
			refused++
		}
	}
	if admitted != 1 || refused != callers-1 {
		t.Fatalf("race admission counts = admitted %d/refused %d, want 1/%d", admitted, refused, callers-1)
	}
}

// TestTodo_EP_PROMO_001_Unit is the in-package half of EP-PROMO-001: the
// durable-candidate helpers derive stable identities, build the canonical
// bodies the snapshot and simulation rows store, and refuse to record a
// member the schema cannot express - while an engine composed with no
// durable database records nothing and fails nothing.
func TestTodo_EP_PROMO_001_Unit(t *testing.T) {
	tenantID := uuid.MustParse("aaaaaaaa-1111-4222-8333-444444444444")
	intentID := uuid.MustParse("bbbbbbbb-2222-4333-8444-555555555555")
	otherIntentID := uuid.MustParse("cccccccc-3333-4333-8444-666666666666")

	t.Run("candidate identities are derived, not allocated", func(t *testing.T) {
		first := proposalCandidateUUID(tenantID, intentID, "snapshot:"+intentcontrol.PurposeSimulation)
		again := proposalCandidateUUID(tenantID, intentID, "snapshot:"+intentcontrol.PurposeSimulation)
		if first != again {
			t.Fatalf("the same derivation minted two ids: %s vs %s", first, again)
		}
		if other := proposalCandidateUUID(tenantID, intentID, "simulation"); other == first {
			t.Fatal("the snapshot and simulation candidates share one id")
		}
		if other := proposalCandidateUUID(tenantID, otherIntentID, "snapshot:"+intentcontrol.PurposeSimulation); other == first {
			t.Fatal("two intents share one snapshot id")
		}
	})

	t.Run("the candidate digest is the body hash", func(t *testing.T) {
		body := []byte(`{"k":"v"}`)
		want := sha256.Sum256(body)
		if got := candidateDigest(body); got != hex.EncodeToString(want[:]) {
			t.Fatalf("candidateDigest = %q, want %q", got, hex.EncodeToString(want[:]))
		}
	})

	t.Run("the snapshot body carries the baselines the read produced", func(t *testing.T) {
		token, err := values.NewSequenceRevision("stream:worker:omar-reyes", 7)
		if err != nil {
			t.Fatalf("build the baseline token: %v", err)
		}
		rev := intent.ProposalRevision{
			ProposalRevisionID: "rev-1",
			Revision:           1,
			SourceBaselines: []intent.SourceBaseline{
				{StreamID: "stream:worker:omar-reyes", ExpectedRevision: token},
			},
		}
		inst := intent.Instance{IntentID: "i-1", Purpose: "P1A", CanonicalRequestDigest: digest.Reference{
			AlgorithmID: "sha256",
			Digest:      strings.Repeat("ab", 32),
		}}
		doc := proposalSnapshotDocument{
			IntentID:               inst.IntentID,
			Revision:               rev.Revision,
			RequestDigestAlgorithm: inst.CanonicalRequestDigest.AlgorithmID,
			RequestDigest:          inst.CanonicalRequestDigest.Digest,
			Purpose:                inst.Purpose,
			Subjects:               proposalSnapshotSubjects(inst),
			SourceBaselines:        proposalSnapshotBaselines(rev),
			ControlDigest:          controlSnapshotDigest(rev.ControlSnapshots),
		}
		body, err := doc.marshal()
		if err != nil {
			t.Fatalf("marshal the snapshot body: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatalf("the snapshot body is not JSON: %v", err)
		}
		baselines, ok := decoded["source_baselines"].([]any)
		if !ok || len(baselines) != 1 {
			t.Fatalf("the snapshot body carries %v baselines, want the one the read produced", decoded["source_baselines"])
		}
		baseline := baselines[0].(map[string]any)
		if baseline["stream_id"] != "stream:worker:omar-reyes" || baseline["expected_revision"] != token.String() {
			t.Fatalf("the recorded baseline = %v, want (%s, %s)", baseline, "stream:worker:omar-reyes", token.String())
		}
	})

	t.Run("the simulation body names what the result claims", func(t *testing.T) {
		artifact := &intentsv1.SimulationArtifact{
			PlannedWrites: []*intentsv1.PlannedWrite{
				{Operation: "WRITE_SET", TargetRef: "worker:omar-reyes.job"},
			},
			Findings: []*intentsv1.Finding{{Code: "F-1"}, {Code: "F-2"}},
		}
		if got := proposalPlannedWrites(artifact); len(got) != 1 || got[0] != "WRITE_SET worker:omar-reyes.job" {
			t.Fatalf("planned writes = %v", got)
		}
		if got := proposalFindingCodes(artifact); len(got) != 2 || got[0] != "F-1" || got[1] != "F-2" {
			t.Fatalf("finding codes = %v", got)
		}
		if proposalPlannedWrites(nil) != nil || proposalFindingCodes(nil) != nil {
			t.Fatal("a nil artifact produced non-nil members")
		}
	})

	t.Run("set mapping keeps every member the schema can express", func(t *testing.T) {
		token, err := values.NewSequenceRevision("stream:x", 1)
		if err != nil {
			t.Fatalf("build the token: %v", err)
		}
		resourceKey, err := values.NewResourceKey(values.TenantId("tenant-1"), values.Kind("worker"), "job")
		if err != nil {
			t.Fatalf("build the resource key: %v", err)
		}
		interval, err := values.NewOpenInstantInterval(values.NewInstant(time.Unix(1_700_000_000, 0)))
		if err != nil {
			t.Fatalf("build the interval: %v", err)
		}
		rev := intent.ProposalRevision{
			ProposalRevisionID: "rev-1",
			Writes: []intent.PlannedWrite{{
				Subject:                 intent.SubjectReference{Kind: "WORKER", SubjectID: "omar-reyes", AuthorityDomain: "workforce"},
				ResourceKey:             resourceKey,
				FieldPath:               "job",
				CurrentCanonicalText:    "eng",
				ProposedCanonicalText:   "senior eng",
				SourceAuthorityDecision: "authority.local_master/v1",
				ExpectedRevision:        token,
				Operation:               intent.WriteOperationUpdate,
				EffectiveInterval:       interval,
			}},
			RequiredApprovals: []intent.RequiredApproval{
				{RequirementID: "req-1", SeparationConstraint: "not_requester"},
			},
		}
		sets, err := proposalCandidateSets(rev)
		if err != nil {
			t.Fatalf("map the sets: %v", err)
		}
		if len(sets.Writes) != 1 || len(sets.Approvals) != 1 {
			t.Fatalf("sets = %+v", sets)
		}
		if sets.Approvals[0].MaterialityClass != intentcontrol.Material {
			t.Fatalf("the approval is %q materiality", sets.Approvals[0].MaterialityClass)
		}
	})

	t.Run("an obligation the schema cannot hold is an error, not a dropped row", func(t *testing.T) {
		rev := intent.ProposalRevision{
			ProposalRevisionID: "rev-ob",
			Obligations:        []intent.Obligation{{ObligationID: "ob-1", Kind: "notice"}},
		}
		if _, err := proposalCandidateSets(rev); err == nil || !strings.Contains(err.Error(), "obligation") {
			t.Fatalf("proposalCandidateSets = %v, want an obligation refusal", err)
		}
	})

	t.Run("nothing records without a minted revision or a database", func(t *testing.T) {
		engine := newJourneyEngine(nil, nil, "", nil, nil)
		if err := engine.recordProposalCandidates(context.Background(), nil, intent.Instance{}, simulationResult{}); err != nil {
			t.Fatalf("a blocked simulation recorded: %v", err)
		}
		rev := &intent.ProposalRevision{ProposalRevisionID: "rev-1", Revision: 1}
		if err := engine.recordProposalCandidates(context.Background(), nil, intent.Instance{}, simulationResult{Revision: rev}); err != nil {
			t.Fatalf("a database-free engine failed rather than recording nothing: %v", err)
		}
	})
}
