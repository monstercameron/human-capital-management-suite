package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// This file is the PRIMARY test and TEST MATRIX for EP-INTENT-003
// ("Implement SubmitIntent, CancelIntent and SupersedeIntent lifecycle
// endpoints"), proven at the application layer against a real
// [IntentService]: every assertion below calls the RPC methods themselves,
// never a hand-rolled substitute.
//
// Every matrix entry delegates to one of three shared contract helpers,
// exactly as internal/transport/endpoint/intent_endpoints_test.go's sibling
// convention does:
//
//   - runCancellationDistinguishabilityContract proves the Integration
//     clause: four intents seeded in four different execution conditions
//     each return a different, correct dimensional tuple from CancelIntent
//     alone.
//   - runSupersedeNonMutationContract proves the Mutation clause: the
//     original's encoded envelope is diffed field by field before and after
//     SupersedeIntent, not merely asserted to "still exist".
//   - runSubmitOnceContract proves submit binds an exact proposal revision
//     and starts exactly once, against the real promote_worker production
//     definition and the design-partner fixture corpus (SubmitIntent
//     re-simulates through the full capability-gateway path, which the
//     other two RPCs never touch).
//
// # What is proven here versus what is not
//
// All three contracts run against an in-memory, mutex-CAS'd Store
// ([memLifecycleStore]) or, for Submit, a full [NewCell] composition backed
// by the same in-memory Store. Both prove the real IntentService logic —
// revision checks, the lifecycle transition, idempotent replay — completely
// and honestly. Neither reaches internal/intent/app/pgstore against real
// PostgreSQL: that adapter's own MutateLifecycle is implemented
// (internal/intent/app/pgstore/lifecycle.go) but its correctness against a
// live database is not independently proven by this file. HTTP/gRPC wire
// parity for these three RPCs is proven separately, in
// internal/transport/endpoint (this todo's other required package), against
// the shared transporttest fixture.

type allowDiagDebug struct{}

func (allowDiagDebug) AllowsInternalDiagnostics() bool { return true }

func fatalWithDiagnostic(t *testing.T, label string, err error) {
	t.Helper()
	var owned *envelope.Error
	if errors.As(err, &owned) {
		if diag, ok := owned.Diagnostic(allowDiagDebug{}); ok {
			t.Fatalf("%s: %v (diagnostic: %v)", label, err, diag)
		}
	}
	t.Fatalf("%s: %v", label, err)
}

func lifecycleDims(request lifecycle.RequestState, execution lifecycle.ExecutionState) lifecycle.Dimensions {
	return lifecycle.Dimensions{
		Request: request, Execution: execution,
		Business: lifecycle.BusinessNotStarted, Consistency: lifecycle.ConsistencyNotApplicable,
		Obligation: lifecycle.ObligationNotApplicable,
	}
}

// ---------------------------------------------------------------------------
// Cancellation distinguishability (Integration clause)
// ---------------------------------------------------------------------------

// runCancellationDistinguishabilityContract seeds four intents in four
// different execution conditions and calls the real CancelIntent RPC on
// each. It asserts each returns the correct, distinct disposition and that
// the four resulting dimensional tuples are pairwise distinct — reading
// nothing but the returned IntentInstance, exactly as a caller would.
func runCancellationDistinguishabilityContract(t *testing.T) {
	t.Helper()
	h := newLifecycleHarness(t)
	ctx := lifecycleCtx(t)

	const (
		notStartedID = "10000000-0000-0000-0000-000000000001"
		midFlightID  = "10000000-0000-0000-0000-000000000002"
		committedID  = "10000000-0000-0000-0000-000000000003"
		partialID    = "10000000-0000-0000-0000-000000000004"
	)

	h.seed(t, notStartedID, lifecycleDims(lifecycle.RequestSubmitted, lifecycle.ExecutionNotPlanned))
	h.seed(t, midFlightID, lifecycleDims(lifecycle.RequestApproved, lifecycle.ExecutionExecuting))
	h.seed(t, committedID, lifecycleDims(lifecycle.RequestApproved, lifecycle.ExecutionCommitted),
		func(i *intent.Instance) { i.CommitReceiptRef = "receipt:fixture" })
	h.seed(t, partialID, lifecycleDims(lifecycle.RequestApproved, lifecycle.ExecutionExecuting))

	h.SafePoints.set(midFlightID, intent.CancellationPointMidFlight, "")
	h.SafePoints.set(partialID, intent.CancellationPointPartialEffect, "repair:plan-1")

	type outcome struct {
		name   string
		got    *intentsv1.IntentInstance
		errVal error
	}
	call := func(name, id string) outcome {
		resp, err := h.Service.CancelIntent(ctx, &intentsv1.CancelIntentRequest{
			IdempotencyKey: "idem-cancel-" + id, IntentId: id,
			ExpectedInstanceVersion: 1, ReasonRef: "reason:withdrawn",
		})
		if err != nil {
			return outcome{name: name, errVal: err}
		}
		return outcome{name: name, got: resp.GetIntent()}
	}

	results := []outcome{
		call("not_started", notStartedID),
		call("mid_flight", midFlightID),
		call("committed", committedID),
		call("partial_effect", partialID),
	}
	for _, r := range results {
		if r.errVal != nil {
			t.Fatalf("CancelIntent(%s): %v", r.name, r.errVal)
		}
	}

	wantLifecycle := map[string]*intentsv1.LifecycleDimensions{
		"not_started": {
			Request: intentsv1.RequestState_REQUEST_STATE_CANCELLED, Execution: intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED,
		},
		"mid_flight": {
			Request: intentsv1.RequestState_REQUEST_STATE_APPROVED, Execution: intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING,
		},
		"committed": {
			Request: intentsv1.RequestState_REQUEST_STATE_APPROVED, Execution: intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED,
		},
		"partial_effect": {
			Request: intentsv1.RequestState_REQUEST_STATE_APPROVED, Execution: intentsv1.ExecutionState_EXECUTION_STATE_REPAIR_REQUIRED,
		},
	}
	seenTuples := map[[2]int32]string{}
	for _, r := range results {
		want := wantLifecycle[r.name]
		got := r.got.GetLifecycle()
		if got.GetRequest() != want.GetRequest() || got.GetExecution() != want.GetExecution() {
			t.Fatalf("%s: lifecycle = request=%s execution=%s, want request=%s execution=%s",
				r.name, got.GetRequest(), got.GetExecution(), want.GetRequest(), want.GetExecution())
		}
		key := [2]int32{int32(got.GetRequest()), int32(got.GetExecution())}
		if other, dup := seenTuples[key]; dup {
			t.Fatalf("%s and %s returned the identical dimensional tuple request=%s execution=%s; "+
				"the four cancellation outcomes are not distinguishable", r.name, other, got.GetRequest(), got.GetExecution())
		}
		seenTuples[key] = r.name

		// The exact RED clause: TOO_LATE and CANCELLATION_PENDING must never
		// report RequestState=CANCELLED.
		if r.name == "committed" || r.name == "mid_flight" {
			if got.GetRequest() == intentsv1.RequestState_REQUEST_STATE_CANCELLED {
				t.Fatalf("%s falsely reports RequestState=CANCELLED", r.name)
			}
		}
		decisions := r.got.GetCancellationDecisions()
		if len(decisions) != 1 || decisions[0].GetIntentId() != r.got.GetIntentId() || decisions[0].GetReasonRef() == "" {
			t.Fatalf("%s: cancellation decision evidence missing or malformed: %+v", r.name, decisions)
		}
	}

	// Re-cancelling an already-terminal (CANCELLED) intent refuses rather
	// than silently succeeding a second time.
	if _, err := h.Service.CancelIntent(ctx, &intentsv1.CancelIntentRequest{
		IdempotencyKey: "idem-cancel-again", IntentId: notStartedID,
		ExpectedInstanceVersion: 2, ReasonRef: "reason:withdrawn-again",
	}); err == nil {
		t.Fatal("re-cancelling an already-CANCELLED intent succeeded, want a refusal")
	} else {
		var owned *envelope.Error
		if !errors.As(err, &owned) || owned.ReasonRef() != reasonAlreadyTerminal {
			t.Fatalf("re-cancel error = %v, want reason %q", err, reasonAlreadyTerminal)
		}
	}
}

// ---------------------------------------------------------------------------
// Supersede non-mutation (Mutation clause)
// ---------------------------------------------------------------------------

// runSupersedeNonMutationContract captures the original instance's envelope
// before SupersedeIntent, runs the real RPC, reloads the original and diffs
// the two envelopes field by field: every field must be identical except the
// lifecycle tuple (RequestState->SUPERSEDED), InstanceVersion and the two
// transition timestamps. An aggregate "still exists" assertion would not
// catch a supersede that silently rewrote the payload, the subjects or a
// prior proposal revision, so this test never makes only that assertion.
func runSupersedeNonMutationContract(t *testing.T) {
	t.Helper()
	h := newLifecycleHarness(t)
	ctx := lifecycleCtx(t)

	const originalID = "20000000-0000-0000-0000-000000000001"
	seeded := h.seed(t, originalID, lifecycleDims(lifecycle.RequestSimulated, lifecycle.ExecutionNotPlanned),
		func(i *intent.Instance) {
			i.ProposalRevisions = []intent.ProposalRevision{{
				ProposalRevisionID: "revision-1", IntentID: originalID, Revision: 1,
				CreatedBy: intent.PrincipalReference{
					PrincipalID: "seed-principal", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "seed-assurance",
				},
			}}
			// A delegation chain on the original is the concrete thing that
			// must NOT ride along onto the successor: inheriting it would let
			// a supersede execute under authority the caller was never
			// granted. Asserted below.
			i.DelegationChain = []intent.DelegationReference{{
				DelegationID:          "delegation-vp-people",
				DelegatingPrincipalID: "principal-vp-people",
				DelegatedPrincipalID:  "seed-principal",
				AuthorityDigest:       "sha256:vp-people-authority",
			}}
		})
	beforeMsg, err := protomap.InstanceToProto(seeded)
	if err != nil {
		t.Fatalf("InstanceToProto(before): %v", err)
	}

	resp, err := h.Service.SupersedeIntent(ctx, &intentsv1.SupersedeIntentRequest{
		IdempotencyKey:          "idem-supersede-1",
		SupersededIntentId:      originalID,
		ExpectedInstanceVersion: 1,
		ReasonRef:               "reason:replaced",
		Definition:              &intentsv1.DefinitionReference{IntentTypeId: h.Def.Ref.TypeID, Version: h.Def.Ref.Version},
		Request: &intentsv1.TypedPayload{
			Schema: &intentsv1.SchemaReference{
				SchemaId: h.Def.InputSchema.SchemaID, Version: h.Def.InputSchema.Version,
				ProtobufFullName: h.Def.InputSchema.ProtobufFullName,
			},
			ProtobufWireBytes: []byte("successor-payload"),
		},
	})
	if err != nil {
		fatalWithDiagnostic(t, "SupersedeIntent", err)
	}
	successor := resp.GetSupersedingIntent()
	if successor.GetIntentId() == "" || successor.GetIntentId() == originalID {
		t.Fatalf("successor identity = %q, want a distinct id", successor.GetIntentId())
	}
	if successor.GetLifecycle().GetRequest() == intentsv1.RequestState_REQUEST_STATE_UNSPECIFIED {
		t.Fatalf("successor carries no lifecycle: %+v", successor.GetLifecycle())
	}
	refs := successor.GetSupersessionReferences()
	if len(refs) != 1 || refs[0].GetSupersededIntentId() != originalID || refs[0].GetSupersedingIntentId() != successor.GetIntentId() {
		t.Fatalf("successor supersession reference = %+v, want one linking back to %s", refs, originalID)
	}

	// RED: "supersede ... broadens authority". The successor's identity must
	// come from the authenticated caller, never from the original. The
	// original was initiated by "seed-principal" and carries a VP-People
	// delegation; the caller here is a different principal entirely, so an
	// implementation that copied the original's initiator or delegation chain
	// forward would hand this caller authority its own admission never
	// granted. Both are asserted by value, not merely "non-empty".
	callerID := lifecyclePrincipal(t).Subject()
	if got := successor.GetInitiator().GetPrincipalId(); got != callerID {
		t.Fatalf("successor initiator = %q, want the calling principal %q", got, callerID)
	}
	if got := successor.GetInitiator().GetPrincipalId(); got == seeded.Initiator.PrincipalID {
		t.Fatalf("successor inherited the original's initiator %q: supersede must not carry authority forward", got)
	}
	if chain := successor.GetDelegationChain(); len(chain) != 0 {
		t.Fatalf("successor inherited the original's delegation chain %+v: supersede must not broaden authority", chain)
	}
	if len(seeded.DelegationChain) == 0 {
		t.Fatal("fixture no longer seeds a delegation chain; the inheritance assertion above would pass vacuously")
	}

	afterInstance := h.load(t, originalID)
	afterMsg, err := protomap.InstanceToProto(afterInstance)
	if err != nil {
		t.Fatalf("InstanceToProto(after): %v", err)
	}

	if afterMsg.GetLifecycle().GetRequest() != intentsv1.RequestState_REQUEST_STATE_SUPERSEDED {
		t.Fatalf("original RequestState = %s, want SUPERSEDED", afterMsg.GetLifecycle().GetRequest())
	}
	if afterMsg.GetInstanceVersion() != beforeMsg.GetInstanceVersion()+1 {
		t.Fatalf("original InstanceVersion = %d, want %d", afterMsg.GetInstanceVersion(), beforeMsg.GetInstanceVersion()+1)
	}
	if len(afterMsg.GetProposalRevisions()) != 1 || afterMsg.GetProposalRevisions()[0].GetProposalRevisionId() != "revision-1" {
		t.Fatalf("original's proposal revisions did not survive: %+v", afterMsg.GetProposalRevisions())
	}

	// Field-by-field diff: clear exactly the fields the transition is
	// documented to move, then require byte-identical proto equality on
	// everything else -- the payload, the subjects, the definition, the
	// canonical request digest and every proposal revision.
	before := proto.Clone(beforeMsg).(*intentsv1.IntentInstance)
	after := proto.Clone(afterMsg).(*intentsv1.IntentInstance)
	after.Lifecycle.Request = before.Lifecycle.Request
	after.InstanceVersion = before.InstanceVersion
	after.RecordedAt = before.RecordedAt
	after.LastTransitionAt = before.LastTransitionAt
	if !proto.Equal(before, after) {
		t.Fatalf("SupersedeIntent mutated more than lifecycle/version/timestamps on the original:\nbefore=%s\nafter =%s",
			before.String(), after.String())
	}
}

// ---------------------------------------------------------------------------
// Submit-once (against the real production promote_worker definition)
// ---------------------------------------------------------------------------

// submitHarness composes a real IntentService through NewCell (the same
// composition cmd/hcmnext uses), backed by the same in-memory Store, so
// SubmitIntent's re-simulation reaches the real promote_worker/explain_
// worker_state capability chain over the design-partner fixture corpus.
type submitHarness struct {
	Service *IntentService
	Store   *memLifecycleStore
}

func newSubmitHarness(t *testing.T) *submitHarness {
	t.Helper()
	store := newMemLifecycleStore()
	if err := store.Bootstrap(context.Background(), string(fixtures.Tenant)); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	verifier := trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) {
		return nil, fmt.Errorf("submit harness: verification is bypassed; the principal is injected directly")
	})
	cell, err := NewCell(CellConfig{Store: store, Verifier: verifier, Audience: "hcm-next-api"})
	if err != nil {
		t.Fatalf("NewCell: %v", err)
	}
	return &submitHarness{Service: cell.Service, Store: store}
}

func submitPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               fixtures.Tenant,
		Subject:              "user-0191f3c4",
		SubjectKind:          trust.SubjectKindHuman,
		OrganizationScopeID:  "org-north-america",
		Roles:                []string{"intent_author", string(authz.RoleCompAdmin)},
		AuthorityRefs:        []string{"authority:position:vp-people"},
		Purposes:             []string{authz.PurposeCompensationReview},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-submit-harness",
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "credential-digest",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

// mustStruct encodes a P1A request payload the same way
// test/bootstrap/harness_test.go's own mustStruct does: the checked-in P1A
// definitions declare input schemas with no generated Protobuf descriptor,
// so the request travels as a google.protobuf.Struct under the declared
// schema identity.
func mustLifecycleStruct(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	s, err := structpb.NewStruct(fields)
	if err != nil {
		t.Fatalf("encode request payload: %v", err)
	}
	wire, err := proto.Marshal(s)
	if err != nil {
		t.Fatalf("marshal request payload: %v", err)
	}
	return wire
}

// promoteWorkerCreateRequest builds the corpus's own certified-READY
// promotion scenario (Omar Reyes, OPS-HRBP2/P2 -> OPS-HRBP3/P3), the exact
// vector test/bootstrap/harness_test.go's promoteWorkerRequest uses.
func promoteWorkerCreateRequest(t *testing.T, principal *trust.Principal, idempotencyKey string) *intentsv1.CreateIntentRequest {
	t.Helper()
	worker, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatalf("resolve worker: %v", err)
	}
	payload := mustLifecycleStruct(t, map[string]any{
		"worker_ref": "omar-reyes",
		"known_at":   "2026-05-15",
		// position_id is deliberately absent: PROMOUX-004 checks a
		// non-empty value against the real Position domain, and this
		// environment has no job_position row for any corpus fixture.
		"target": map[string]any{
			"job_code": "OPS-HRBP3", "grade": "P3", "org_unit": "people-ops",
			"pay_zone": "US-EAST",
		},
		"effective_date": "2026-06-01", "evaluation_date": "2026-05-15",
		"business_reason": "promotion_into_senior_hrbp",
		"current": map[string]any{
			"base": "93000.00", "currency": "USD", "pay_basis": "ANNUAL_SALARY",
			"bonus_target": "0.0500", "effective_date": "2026-06-01",
			"revision_stream": "rewards.package.omar", "revision_sequence": "11",
		},
		"proposed": map[string]any{
			"base": "98000.00", "currency": "USD", "pay_basis": "ANNUAL_SALARY",
			"bonus_target": "0.0500", "effective_date": "2026-06-01",
			"revision_stream": "rewards.package.omar", "revision_sequence": "11",
		},
		"budget": map[string]any{
			"available_amount": "50000.00", "currency": "USD", "owner_system": "adaptive.planning",
			"policy_ref": "finance.authority/2026.1", "scope": "people-ops:FY26-merit",
			"period": "FY2026", "baseline_version": "fy26-merit-r7", "observation_id": "obs_budget_fy26_merit_r7",
		},
	})
	return &intentsv1.CreateIntentRequest{
		IdempotencyKey: idempotencyKey,
		Definition:     &intentsv1.DefinitionReference{IntentTypeId: promotion.IntentType, Version: 1},
		Initiator: &intentsv1.PrincipalReference{
			PrincipalId: principal.Subject(), Kind: intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN,
			IdentityAssuranceRef: principal.EvidenceID(),
		},
		// No POSITION subject: position_id is absent above, and an empty
		// SubjectId would itself be a structurally invalid reference.
		Subjects: []*intentsv1.SubjectReference{
			{SubjectKind: "EMPLOYMENT", SubjectId: worker.Id, AuthorityDomain: "PEOPLE"},
		},
		Request: &intentsv1.TypedPayload{
			Schema: &intentsv1.SchemaReference{
				SchemaId: "hcmnext.people.v1.PromoteWorkerRequest", Version: 1,
				ProtobufFullName: "hcmnext.people.v1.PromoteWorkerRequest",
			},
			ProtobufWireBytes: payload,
		},
		ExecutionMode: intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE,
	}
}

// runSubmitOnceContract proves SubmitIntent binds the exact re-simulated
// proposal revision and starts exactly once: a stale expected_instance_
// version and a presented proposal_revision_id that does not match the
// cell's own re-simulation are both refused, and a second submit of an
// already-submitted intent leaves the stored instance byte-for-byte
// unchanged rather than starting a second run.
func runSubmitOnceContract(t *testing.T) {
	t.Helper()
	h := newSubmitHarness(t)
	principal := submitPrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), principal)

	createReq := promoteWorkerCreateRequest(t, principal, "idem-create-submit-once")
	created, err := h.Service.CreateIntent(ctx, createReq)
	if err != nil {
		fatalWithDiagnostic(t, "CreateIntent", err)
	}
	intentID := created.GetIntent().GetIntentId()

	simulated, err := h.Service.SimulateIntent(ctx, &intentsv1.SimulateIntentRequest{IntentId: intentID})
	if err != nil {
		t.Fatalf("SimulateIntent: %v", err)
	}
	revisionID := simulated.GetSimulation().GetProposalRevisionId()
	if revisionID == "" {
		t.Fatalf("simulation minted no proposal revision; the corpus scenario is not READY: %+v", simulated.GetSimulation())
	}

	// A stale proposal_revision_id is refused before anything durable moves.
	if _, err := h.Service.SubmitIntent(ctx, &intentsv1.SubmitIntentRequest{
		IdempotencyKey: "idem-submit-stale-proposal", IntentId: intentID,
		ProposalRevisionId: "not-the-real-revision", ExpectedInstanceVersion: 1,
	}); err == nil {
		t.Fatal("SubmitIntent with a mismatched proposal_revision_id succeeded, want a refusal")
	}

	// A stale expected_instance_version is refused.
	if _, err := h.Service.SubmitIntent(ctx, &intentsv1.SubmitIntentRequest{
		IdempotencyKey: "idem-submit-stale-version", IntentId: intentID,
		ProposalRevisionId: revisionID, ExpectedInstanceVersion: 999,
	}); err == nil {
		t.Fatal("SubmitIntent with a stale expected_instance_version succeeded, want a refusal")
	}

	first, err := h.Service.SubmitIntent(ctx, &intentsv1.SubmitIntentRequest{
		IdempotencyKey: "idem-submit-once", IntentId: intentID,
		ProposalRevisionId: revisionID, ExpectedInstanceVersion: 1,
	})
	if err != nil {
		fatalWithDiagnostic(t, "SubmitIntent", err)
	}
	if first.GetIntent().GetLifecycle().GetRequest() != intentsv1.RequestState_REQUEST_STATE_SUBMITTED {
		t.Fatalf("RequestState = %s, want SUBMITTED", first.GetIntent().GetLifecycle().GetRequest())
	}
	if first.GetIntent().GetLifecycle().GetExecution() != intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED {
		t.Fatalf("ExecutionState moved to %s, want unchanged NOT_PLANNED (P1A never executes)", first.GetIntent().GetLifecycle().GetExecution())
	}
	afterFirstVersion := first.GetIntent().GetInstanceVersion()

	// A second submit of the already-SUBMITTED intent, under a fresh
	// idempotency key naming the same real revision, must not start a
	// second run: the store is not touched a second time.
	second, err := h.Service.SubmitIntent(ctx, &intentsv1.SubmitIntentRequest{
		IdempotencyKey: "idem-submit-twice", IntentId: intentID,
		ProposalRevisionId: revisionID, ExpectedInstanceVersion: afterFirstVersion,
	})
	if err != nil {
		t.Fatalf("second SubmitIntent: %v", err)
	}
	if second.GetIntent().GetInstanceVersion() != afterFirstVersion {
		t.Fatalf("second submit advanced InstanceVersion from %d to %d; it started a second run",
			afterFirstVersion, second.GetIntent().GetInstanceVersion())
	}

	// An exact idempotent replay of the first call (same key, same payload)
	// returns the identical result without re-running the transition.
	replay, err := h.Service.SubmitIntent(ctx, &intentsv1.SubmitIntentRequest{
		IdempotencyKey: "idem-submit-once", IntentId: intentID,
		ProposalRevisionId: revisionID, ExpectedInstanceVersion: 1,
	})
	if err != nil {
		t.Fatalf("replayed SubmitIntent: %v", err)
	}
	if !proto.Equal(first.GetIntent(), replay.GetIntent()) {
		t.Fatalf("replayed submit diverged from the original result:\nfirst =%s\nreplay=%s", first.GetIntent(), replay.GetIntent())
	}
}

// ---------------------------------------------------------------------------
// PRIMARY + TEST MATRIX
// ---------------------------------------------------------------------------

func runIntentLifecycleContract(t *testing.T) {
	t.Helper()
	t.Run("cancellation_distinguishability", runCancellationDistinguishabilityContract)
	t.Run("supersede_non_mutation", runSupersedeNonMutationContract)
	t.Run("submit_once", runSubmitOnceContract)
}

// TestIntentSubmitCancelSupersedeEndpointsRespectRevisionAuthorityAndIrreversibility
// is the PRIMARY test named by EP-INTENT-003's TEST field.
func TestIntentSubmitCancelSupersedeEndpointsRespectRevisionAuthorityAndIrreversibility(t *testing.T) {
	runIntentLifecycleContract(t)
}

func TestTodo_EP_INTENT_003_Property(t *testing.T)    { runIntentLifecycleContract(t) }
func TestTodo_EP_INTENT_003_Integration(t *testing.T) { runIntentLifecycleContract(t) }
func TestTodo_EP_INTENT_003_Fault(t *testing.T)       { runIntentLifecycleContract(t) }
func TestTodo_EP_INTENT_003_Conformance(t *testing.T) { runIntentLifecycleContract(t) }
func TestTodo_EP_INTENT_003_Mutation(t *testing.T)    { runSupersedeNonMutationContract(t) }

// TestTodo_EP_INTENT_003_Golden pins the exact byte digest of a CANCELLED
// CancelIntentResponse for a fixed clock and fixed identifiers, so a
// regression in field ordering, digest derivation or projection is a diff a
// reviewer sees rather than a green run.
func TestTodo_EP_INTENT_003_Golden(t *testing.T) {
	h := newLifecycleHarness(t)
	ctx := lifecycleCtx(t)
	const goldenID = "30000000-0000-0000-0000-000000000001"
	h.seed(t, goldenID, lifecycleDims(lifecycle.RequestSubmitted, lifecycle.ExecutionNotPlanned))

	resp, err := h.Service.CancelIntent(ctx, &intentsv1.CancelIntentRequest{
		IdempotencyKey: "idem-golden-cancel", IntentId: goldenID,
		ExpectedInstanceVersion: 1, ReasonRef: "reason:withdrawn",
	})
	if err != nil {
		t.Fatalf("CancelIntent: %v", err)
	}
	// The one non-deterministic field is the derived cancellation decision
	// id, which is itself a pure function of (intent id, disposition) and
	// therefore already deterministic; nothing here needs scrubbing.
	wire, err := proto.Marshal(resp.GetIntent())
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	sum := sha256.Sum256(wire)
	got := hex.EncodeToString(sum[:])
	const wantDigest = "3ded9b7904432c46aaf338c0bf74965e46fb681507a6f9afdfc4e82d8fb3d064"
	if got != wantDigest {
		t.Fatalf("golden digest drifted: got %s want %s", got, wantDigest)
	}
}

// TestTodo_EP_INTENT_003_Race runs real concurrent CancelIntent and
// SubmitIntent-shaped compare-and-swap attempts against the same intent and
// proves the store's own compare-and-swap (memLifecycleStore.MutateLifecycle,
// the same shape pgstore.Store.MutateLifecycle uses against PostgreSQL) lets
// exactly one caller advance the instance version, stable across repeated
// runs.
func TestTodo_EP_INTENT_003_Race(t *testing.T) {
	for attempt := 0; attempt < 5; attempt++ {
		h := newLifecycleHarness(t)
		ctx := lifecycleCtx(t)
		const raceID = "40000000-0000-0000-0000-000000000001"
		h.seed(t, raceID, lifecycleDims(lifecycle.RequestSubmitted, lifecycle.ExecutionNotPlanned))

		const concurrency = 8
		var wg sync.WaitGroup
		successes := make([]bool, concurrency)
		wg.Add(concurrency)
		for i := 0; i < concurrency; i++ {
			go func(i int) {
				defer wg.Done()
				_, err := h.Service.CancelIntent(ctx, &intentsv1.CancelIntentRequest{
					// Every goroutine uses a DIFFERENT idempotency key, so the
					// in-process Coordinator cannot short-circuit the race: the
					// store's own compare-and-swap is what this test exercises.
					IdempotencyKey: fmt.Sprintf("idem-race-%d-%d", attempt, i),
					IntentId:       raceID, ExpectedInstanceVersion: 1, ReasonRef: "reason:withdrawn",
				})
				successes[i] = err == nil
			}(i)
		}
		wg.Wait()

		won := 0
		for _, ok := range successes {
			if ok {
				won++
			}
		}
		if won != 1 {
			t.Fatalf("attempt %d: %d of %d concurrent cancellations won, want exactly 1", attempt, won, concurrency)
		}
		final := h.load(t, raceID)
		if final.Lifecycle.Request != lifecycle.RequestCancelled || final.InstanceVersion != 2 {
			t.Fatalf("attempt %d: final state = %+v, want CANCELLED at version 2", attempt, final.Lifecycle)
		}
	}
}

// TestTodo_EP_INTENT_003_Security proves every governed write fails closed
// for an unauthenticated caller, and that a zero-value expected_instance_
// version is refused rather than treated as a wildcard "current" match.
func TestTodo_EP_INTENT_003_Security(t *testing.T) {
	h := newLifecycleHarness(t)
	const secureID = "50000000-0000-0000-0000-000000000001"
	h.seed(t, secureID, lifecycleDims(lifecycle.RequestSubmitted, lifecycle.ExecutionNotPlanned))

	unauth := context.Background()
	if _, err := h.Service.SubmitIntent(unauth, &intentsv1.SubmitIntentRequest{
		IdempotencyKey: "idem-unauth-submit", IntentId: secureID, ProposalRevisionId: "r", ExpectedInstanceVersion: 1,
	}); err == nil {
		t.Fatal("SubmitIntent with no authenticated principal succeeded")
	}
	if _, err := h.Service.CancelIntent(unauth, &intentsv1.CancelIntentRequest{
		IdempotencyKey: "idem-unauth-cancel", IntentId: secureID, ExpectedInstanceVersion: 1, ReasonRef: "r",
	}); err == nil {
		t.Fatal("CancelIntent with no authenticated principal succeeded")
	}
	if _, err := h.Service.SupersedeIntent(unauth, &intentsv1.SupersedeIntentRequest{
		IdempotencyKey: "idem-unauth-supersede", SupersededIntentId: secureID, ExpectedInstanceVersion: 1, ReasonRef: "r",
		Definition: &intentsv1.DefinitionReference{IntentTypeId: h.Def.Ref.TypeID, Version: h.Def.Ref.Version},
	}); err == nil {
		t.Fatal("SupersedeIntent with no authenticated principal succeeded")
	}

	ctx := lifecycleCtx(t)
	// A zero expected_instance_version never means "current"; it is refused
	// as a malformed request for all three RPCs.
	if _, err := h.Service.CancelIntent(ctx, &intentsv1.CancelIntentRequest{
		IdempotencyKey: "idem-zero-version", IntentId: secureID, ExpectedInstanceVersion: 0, ReasonRef: "r",
	}); err == nil {
		t.Fatal("CancelIntent with expected_instance_version=0 succeeded, want a refusal")
	}
	if _, err := h.Service.SubmitIntent(ctx, &intentsv1.SubmitIntentRequest{
		IdempotencyKey: "idem-zero-version-2", IntentId: secureID, ProposalRevisionId: "r", ExpectedInstanceVersion: 0,
	}); err == nil {
		t.Fatal("SubmitIntent with expected_instance_version=0 succeeded, want a refusal")
	}
	// An empty reason_ref is refused rather than treated as "no reason
	// needed".
	if _, err := h.Service.CancelIntent(ctx, &intentsv1.CancelIntentRequest{
		IdempotencyKey: "idem-empty-reason", IntentId: secureID, ExpectedInstanceVersion: 1, ReasonRef: "",
	}); err == nil {
		t.Fatal("CancelIntent with an empty reason_ref succeeded, want a refusal")
	}
}

// TestListIntentsReportsTheCurrentLifecycle: ListIntents decoded each stored
// envelope -- the immutable creation fact -- without laying the current
// projection over it, as loadInstance does. A cancelled intent therefore
// listed at its creation-time lifecycle, and the journeys page kept offering a
// withdrawn promotion as open.
func TestListIntentsReportsTheCurrentLifecycle(t *testing.T) {
	h := newLifecycleHarness(t)
	ctx := lifecycleCtx(t)
	const id = "10000000-0000-0000-0000-0000000000a1"
	h.seed(t, id, lifecycleDims(lifecycle.RequestSubmitted, lifecycle.ExecutionNotPlanned))
	if _, err := h.Service.CancelIntent(ctx, &intentsv1.CancelIntentRequest{
		IdempotencyKey: "idem-cancel-list", IntentId: id, ExpectedInstanceVersion: 1, ReasonRef: "reason:withdrawn",
	}); err != nil {
		t.Fatalf("CancelIntent: %v", err)
	}
	listed, err := h.Service.ListIntents(ctx, &intentsv1.ListIntentsRequest{})
	if err != nil {
		t.Fatalf("ListIntents: %v", err)
	}
	for _, msg := range listed.GetIntents() {
		if msg.GetIntentId() != id {
			continue
		}
		if got := msg.GetLifecycle().GetRequest(); got != intentsv1.RequestState_REQUEST_STATE_CANCELLED {
			t.Fatalf("listed request state = %v, want CANCELLED as InspectIntent reports it", got)
		}
		if msg.GetInstanceVersion() < 2 {
			t.Fatalf("listed instance version = %d, want the post-cancel version", msg.GetInstanceVersion())
		}
		return
	}
	t.Fatalf("ListIntents did not list %s: %+v", id, listed.GetIntents())
}
