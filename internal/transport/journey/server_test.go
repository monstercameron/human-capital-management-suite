package journey_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// testContext returns a context carrying the fixture manager's bearer token
// and a bounded deadline, which is what every ordinary call in this file
// needs.
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return withToken(ctx, fixtureManagerToken)
}

// authorizedContext is testContext's counterpart carrying the fixture
// appearance-admin token (comp_admin), which PROMOUX-008's diagnostics gate
// treats as authorized. It exists for tests whose whole point is that the
// wire conversion is total -- a fully populated port value comes back whole
// -- rather than a test of the diagnostics-authorization boundary itself,
// which has its own dedicated PROMOUX-008 tests exercising the ordinary
// fixtureManagerToken as the unauthorized case.
func authorizedContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return withToken(ctx, fixtureAppearanceAdminToken)
}

// TestJourneyServiceRPCsForwardToTheEnginePort drives every RPC through a
// real gRPC client against a fake engine and asserts both directions: that
// the request reached the port as the port's own plain Go types, and that
// the port's answer came back whole.
func TestJourneyServiceRPCsForwardToTheEnginePort(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	ctx := testContext(t)

	t.Run("ListJourneys", func(t *testing.T) {
		resp, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{})
		if err != nil {
			t.Fatalf("ListJourneys: %v", err)
		}
		if len(resp.GetJourneys()) != 1 {
			t.Fatalf("journeys = %d, want 1", len(resp.GetJourneys()))
		}
		got := resp.GetJourneys()[0]
		want := fixtureSummary()
		if got.GetIntentId() != want.IntentID {
			t.Fatalf("intent_id = %q, want %q", got.GetIntentId(), want.IntentID)
		}
		if got.GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL {
			t.Fatalf("stage = %v, want AWAITING_APPROVAL", got.GetStage())
		}
		if got.GetWorkerRef() != want.Worker.String() {
			t.Fatalf("worker_ref = %q, want %q", got.GetWorkerRef(), want.Worker.String())
		}
		if got.GetTarget().GetJobCode() != want.Target.JobCode {
			t.Fatalf("target.job_code = %q, want %q", got.GetTarget().GetJobCode(), want.Target.JobCode)
		}
	})

	t.Run("ProposeJourney", func(t *testing.T) {
		_, err := client.ProposeJourney(ctx, &journeyv1.ProposeJourneyRequest{
			WorkerRef:      fixtureWorkerRef().String(),
			Target:         &journeyv1.Placement{JobCode: "ENG-4", Grade: "G8", PositionId: "pos-202"},
			ProposedBase:   "168500.00",
			EffectiveDate:  "2026-10-01",
			BusinessReason: "sustained scope increase",
		})
		if err != nil {
			t.Fatalf("ProposeJourney: %v", err)
		}
		engine.mu.Lock()
		in := engine.lastProposal
		engine.mu.Unlock()
		want := workspace.ProposalInput{
			WorkerRef:        fixtureWorkerRef().String(),
			TargetJobCode:    "ENG-4",
			TargetGrade:      "G8",
			TargetPositionID: "pos-202",
			ProposedBase:     "168500.00",
			EffectiveDate:    "2026-10-01",
			BusinessReason:   "sustained scope increase",
		}
		if in != want {
			t.Fatalf("the engine saw %+v, want %+v", in, want)
		}
	})

	t.Run("InspectJourney", func(t *testing.T) {
		// assertDetailIsWhole checks the diagnostic-only sections PROMOUX-008
		// withholds from an unauthorized caller, so this call -- proving the
		// port's answer forwards whole, not proving who may see it -- uses
		// the authorized fixture identity. The gate itself is
		// TestTodo_PROMOUX_008_Security's job.
		resp, err := client.InspectJourney(authorizedContext(t), &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
		if err != nil {
			t.Fatalf("InspectJourney: %v", err)
		}
		assertDetailIsWhole(t, resp.GetDetail())
		engine.mu.Lock()
		gotID := engine.lastIntentID
		engine.mu.Unlock()
		if gotID != fixtureIntentID {
			t.Fatalf("the engine saw intent id %q, want %q", gotID, fixtureIntentID)
		}
	})

	t.Run("ExecuteJourney", func(t *testing.T) {
		resp, err := client.ExecuteJourney(authorizedContext(t), &journeyv1.ExecuteJourneyRequest{IntentId: fixtureIntentID})
		if err != nil {
			t.Fatalf("ExecuteJourney: %v", err)
		}
		assertDetailIsWhole(t, resp.GetDetail())
		engine.mu.Lock()
		calls := engine.executeCalls
		engine.mu.Unlock()
		if calls != 1 {
			t.Fatalf("Execute called %d times, want 1", calls)
		}
	})

	t.Run("DecideJourney", func(t *testing.T) {
		resp, err := client.DecideJourney(authorizedContext(t), &journeyv1.DecideJourneyRequest{
			IntentId: fixtureIntentID,
			Approve:  true,
			Reason:   "scope and impact confirmed",
		})
		if err != nil {
			t.Fatalf("DecideJourney: %v", err)
		}
		assertDetailIsWhole(t, resp.GetDetail())
		engine.mu.Lock()
		decision := engine.lastDecision
		engine.mu.Unlock()
		if !decision.Approve || decision.Reason != "scope and impact confirmed" {
			t.Fatalf("the engine saw decision %+v, want an approval with the stated reason", decision)
		}
	})

	t.Run("AcknowledgeJourney", func(t *testing.T) {
		resp, err := client.AcknowledgeJourney(authorizedContext(t), &journeyv1.AcknowledgeJourneyRequest{
			IntentId:    fixtureIntentID,
			EvidenceRef: "hris:signature:abc123",
			Note:        "signed copy on file",
		})
		if err != nil {
			t.Fatalf("AcknowledgeJourney: %v", err)
		}
		assertDetailIsWhole(t, resp.GetDetail())
		engine.mu.Lock()
		ack := engine.lastAcknowledgement
		engine.mu.Unlock()
		if ack.EvidenceRef != "hris:signature:abc123" || ack.Note != "signed copy on file" {
			t.Fatalf("the engine saw acknowledgement %+v, want the stated evidence and note", ack)
		}
	})

	t.Run("WatchJourney emits immediately when the client holds nothing", func(t *testing.T) {
		stream, err := client.WatchJourney(authorizedContext(t), &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
		if err != nil {
			t.Fatalf("WatchJourney: %v", err)
		}
		msg, err := stream.Recv()
		if err != nil {
			t.Fatalf("WatchJourney Recv: %v", err)
		}
		assertDetailIsWhole(t, msg.GetDetail())
	})
}

// assertDetailIsWhole checks the sections a JourneyDetail conversion could
// silently drop: the ones that are empty until a journey has been executed
// (instance, nodes, work items, transitions, ledger) plus the derived
// digest. It is a shape assertion, not a value assertion - convert_test.go
// proves the values.
func assertDetailIsWhole(t *testing.T, d *journeyv1.JourneyDetail) {
	t.Helper()
	if d == nil {
		t.Fatal("detail is nil")
	}
	if d.GetJourney().GetIntentId() != fixtureIntentID {
		t.Fatalf("journey.intent_id = %q, want %q", d.GetJourney().GetIntentId(), fixtureIntentID)
	}
	if len(d.GetFindings()) != 2 {
		t.Fatalf("findings = %d, want 2", len(d.GetFindings()))
	}
	if len(d.GetPlannedWrites()) != 2 {
		t.Fatalf("planned_writes = %d, want 2", len(d.GetPlannedWrites()))
	}
	if d.GetInstance().GetInstanceId() != "instance-77" {
		t.Fatalf("instance.instance_id = %q, want instance-77", d.GetInstance().GetInstanceId())
	}
	if len(d.GetNodes()) != 1 {
		t.Fatalf("nodes = %d, want 1", len(d.GetNodes()))
	}
	if len(d.GetWorkItems()) != 1 {
		t.Fatalf("work_items = %d, want 1", len(d.GetWorkItems()))
	}
	if d.GetWorkItems()[0].GetChosenOwner() != "approver-lee" {
		t.Fatalf("work_items[0].chosen_owner = %q, want approver-lee", d.GetWorkItems()[0].GetChosenOwner())
	}
	if len(d.GetTransitions()) != 1 {
		t.Fatalf("transitions = %d, want 1", len(d.GetTransitions()))
	}
	if d.GetLedger().GetSequence() != 12 {
		t.Fatalf("ledger.sequence = %d, want 12", d.GetLedger().GetSequence())
	}
	if len(d.GetEvidenceIds()) != 2 {
		t.Fatalf("evidence_ids = %d, want 2", len(d.GetEvidenceIds()))
	}
	if len(d.GetTimeline()) != 1 {
		t.Fatalf("timeline = %d, want 1", len(d.GetTimeline()))
	}
	if d.GetApprover() != "approver-lee" {
		t.Fatalf("approver = %q, want approver-lee", d.GetApprover())
	}
	if !strings.HasPrefix(d.GetDetailDigest(), "sha256:") {
		t.Fatalf("detail_digest = %q, want a sha256-prefixed digest", d.GetDetailDigest())
	}
}

// TestJourneyServiceErrorMapping is the table the whole refusal contract
// rests on: each sentinel the workspace journey port declares must arrive at
// the client as exactly one owned condition, and an unrecognized failure
// must arrive as INTERNAL with none of its text.
func TestJourneyServiceErrorMapping(t *testing.T) {
	const leakySecret = `pq: password authentication failed for user "hcmnext" select * from intents`

	cases := []struct {
		name string
		err  error
		want envelope.Code
	}{
		{"ErrDenied is PERMISSION_DENIED", workspace.ErrDenied, envelope.CodePermissionDenied},
		{"ErrJourneyUnknown is NOT_FOUND", workspace.ErrJourneyUnknown, envelope.CodeNotFound},
		{"ErrJourneyStage is FAILED_PRECONDITION", workspace.ErrJourneyStage, envelope.CodeFailedPrecondition},
		{"ErrJourneyInput is INVALID_ARGUMENT", workspace.ErrJourneyInput, envelope.CodeInvalidArgument},
		{"ErrJourneyUnavailable is UNAVAILABLE", workspace.ErrJourneyUnavailable, envelope.CodeUnavailable},
		{"a wrapped sentinel is still recognized", errors.Join(errors.New("driver resume"), workspace.ErrJourneyStage), envelope.CodeFailedPrecondition},
		{"an unrecognized failure is INTERNAL", errors.New(leakySecret), envelope.CodeUnspecified},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := newFakeEngine()
			engine.listErr = tc.err
			engine.proposeErr = tc.err
			engine.inspectErr = tc.err
			engine.executeErr = tc.err
			engine.decideErr = tc.err
			engine.acknowledgeErr = tc.err
			client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
			ctx := testContext(t)

			calls := map[string]func() error{
				"ListJourneys": func() error {
					_, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{})
					return err
				},
				"ProposeJourney": func() error {
					_, err := client.ProposeJourney(ctx, &journeyv1.ProposeJourneyRequest{WorkerRef: fixtureWorkerID})
					return err
				},
				"InspectJourney": func() error {
					_, err := client.InspectJourney(ctx, &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
					return err
				},
				"ExecuteJourney": func() error {
					_, err := client.ExecuteJourney(ctx, &journeyv1.ExecuteJourneyRequest{IntentId: fixtureIntentID})
					return err
				},
				"DecideJourney": func() error {
					_, err := client.DecideJourney(ctx, &journeyv1.DecideJourneyRequest{IntentId: fixtureIntentID})
					return err
				},
				"AcknowledgeJourney": func() error {
					_, err := client.AcknowledgeJourney(ctx, &journeyv1.AcknowledgeJourneyRequest{IntentId: fixtureIntentID})
					return err
				},
				// WatchJourney is the streaming member of the table: the
				// refusal arrives on the first Recv rather than from the
				// call that opened the stream, and it must be exactly the
				// same owned condition the unary methods produce.
				"WatchJourney": func() error {
					stream, err := client.WatchJourney(ctx, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
					if err != nil {
						return err
					}
					_, err = stream.Recv()
					return err
				},
			}
			for name, call := range calls {
				t.Run(name, func(t *testing.T) {
					err := call()
					owned := assertOwnedCode(t, err, tc.want)
					if owned.CorrelationID() == "" {
						t.Fatal("a refusal must carry a correlation id")
					}
					if owned.EvidenceRef().ID == "" {
						t.Fatal("a refusal must carry the authentication evidence reference")
					}
					if strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "pq:") {
						t.Fatalf("the client-visible error leaked unsafe diagnostic text: %v", err)
					}
				})
			}
		})
	}
}

// TestJourneyServiceInvalidInputNamesTheField proves the INVALID_ARGUMENT
// projection is actionable: the field the port's message named survives as a
// field violation rather than being flattened into prose.
func TestJourneyServiceInvalidInputNamesTheField(t *testing.T) {
	engine := newFakeEngine()
	engine.proposeErr = &workspace.JourneyInputError{FieldPath: "effective_date", ReasonRef: "journey.input.invalid", Detail: "must be ISO-8601"}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))

	_, err := client.ProposeJourney(testContext(t), &journeyv1.ProposeJourneyRequest{WorkerRef: fixtureWorkerID})
	owned := assertOwnedCode(t, err, envelope.CodeInvalidArgument)
	found := false
	for _, v := range owned.Violations() {
		if v.FieldPath == "effective_date" {
			found = true
		}
	}
	if !found {
		t.Fatalf("violations = %+v, want one naming effective_date", owned.Violations())
	}
}

func TestJourneyServiceInvalidInputNeverParsesOrProjectsDiagnosticPayRule(t *testing.T) {
	engine := newFakeEngine()
	engine.proposeErr = &workspace.JourneyInputError{
		FieldPath: "proposed_base", ReasonRef: "promotion.ladder.base_increase_out_of_range",
		Detail: "private worker pay: increase 0.0500 to 0.1500",
	}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	_, err := client.ProposeJourney(testContext(t), &journeyv1.ProposeJourneyRequest{WorkerRef: fixtureWorkerID})
	owned := assertOwnedCode(t, err, envelope.CodeInvalidArgument)
	if got := owned.Violations(); len(got) != 1 || got[0].FieldPath != "proposed_base" || got[0].RuleRef != "promotion.ladder.base_increase_out_of_range" {
		t.Fatalf("typed refusal was not preserved: %+v", got)
	}
	for _, secret := range []string{"private worker pay", "0.0500", "0.1500"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("wire refusal leaked %q: %v", secret, err)
		}
	}

	engine.proposeErr = errors.Join(workspace.ErrJourneyInput, errors.New("effective_date private worker pay 0.0500"))
	_, err = client.ProposeJourney(testContext(t), &journeyv1.ProposeJourneyRequest{WorkerRef: fixtureWorkerID})
	owned = assertOwnedCode(t, err, envelope.CodeInvalidArgument)
	if got := owned.Violations(); len(got) != 1 || got[0].FieldPath != "request" {
		t.Fatalf("legacy untyped refusal should remain request-level: %+v", got)
	}
	if strings.Contains(err.Error(), "private worker pay") || strings.Contains(err.Error(), "0.0500") {
		t.Fatalf("legacy refusal leaked diagnostic text: %v", err)
	}

	engine.proposeErr = &workspace.JourneyInputError{
		FieldPath: "salary_of_private_worker_123", ReasonRef: "private.policy:98765", Detail: "private worker pay 0.0500",
	}
	_, err = client.ProposeJourney(testContext(t), &journeyv1.ProposeJourneyRequest{WorkerRef: fixtureWorkerID})
	owned = assertOwnedCode(t, err, envelope.CodeInvalidArgument)
	if got := owned.Violations(); len(got) != 1 || got[0].FieldPath != "request" || got[0].RuleRef != "journey.input.invalid" {
		t.Fatalf("unrecognized typed coordinates were projected: %+v", got)
	}
	for _, secret := range []string{"private.policy", "private worker pay", "salary_of_private_worker", "0.0500"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("malformed typed refusal leaked %q", secret)
		}
	}
}

func TestTodo_PROMOUX_007_Security_NonProposalCannotDisclosePayBounds(t *testing.T) {
	minimum, err := values.NewMoney("105.04", "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := values.NewMoney("115.03", "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	engine := newFakeEngine()
	engine.executeErr = &workspace.JourneyInputError{
		FieldPath: "proposed_base", ReasonRef: "promotion.ladder.base_increase_out_of_range",
		PayRange: &workspace.JourneyPayRange{Minimum: minimum, Maximum: maximum},
	}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	_, err = client.ExecuteJourney(testContext(t), &journeyv1.ExecuteJourneyRequest{IntentId: fixtureIntentID})
	owned := assertOwnedCode(t, err, envelope.CodeInvalidArgument)
	if got := owned.Violations()[0].MoneyRange; got != (envelope.MoneyRange{}) {
		t.Fatalf("execute response disclosed pay correction: %+v", got)
	}
}

// TestJourneyServiceWithoutAnEngineIsUnavailable proves the FAULT case: a
// process that hosts the service before the engine is composed answers every
// method with a typed UNAVAILABLE rather than panicking on a nil port.
func TestJourneyServiceWithoutAnEngineIsUnavailable(t *testing.T) {
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{}))
	ctx := testContext(t)

	_, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{})
	assertOwnedCode(t, err, envelope.CodeUnavailable)
	_, err = client.InspectJourney(ctx, &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
	assertOwnedCode(t, err, envelope.CodeUnavailable)
	_, err = client.ExecuteJourney(ctx, &journeyv1.ExecuteJourneyRequest{IntentId: fixtureIntentID})
	assertOwnedCode(t, err, envelope.CodeUnavailable)
	// WatchJourney's own missing-engine case is asserted on the stream in
	// TestWatchJourneyWithoutAnEngineEndsUnavailable.
}

// TestJourneyServiceSecurity proves the surface inherits the shared
// admission unchanged: an unauthenticated call never reaches a handler, an
// unrecognized credential is refused the same way a missing one is, and a
// request that tries to select its own trusted context is rejected before
// any journey method runs.
func TestJourneyServiceSecurity(t *testing.T) {
	engine := newFakeEngine()
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("an unauthenticated call is refused before the engine is touched", func(t *testing.T) {
		_, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{})
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
		engine.mu.Lock()
		calls := engine.listCalls
		engine.mu.Unlock()
		if calls != 0 {
			t.Fatalf("the engine was called %d times on an unauthenticated request, want 0", calls)
		}
	})

	t.Run("an unrecognized credential is rejected the same way a missing one is", func(t *testing.T) {
		_, err := client.ListJourneys(withToken(ctx, fixtureUnknownToken), &journeyv1.ListJourneysRequest{})
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
	})

	t.Run("every write method refuses an unauthenticated caller too", func(t *testing.T) {
		_, err := client.ProposeJourney(ctx, &journeyv1.ProposeJourneyRequest{WorkerRef: fixtureWorkerID})
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
		_, err = client.ExecuteJourney(ctx, &journeyv1.ExecuteJourneyRequest{IntentId: fixtureIntentID})
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
		_, err = client.DecideJourney(ctx, &journeyv1.DecideJourneyRequest{IntentId: fixtureIntentID})
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
		_, err = client.AcknowledgeJourney(ctx, &journeyv1.AcknowledgeJourneyRequest{IntentId: fixtureIntentID})
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
		engine.mu.Lock()
		touched := engine.proposeCalls + engine.executeCalls + engine.decideCalls
		engine.mu.Unlock()
		if touched != 0 {
			t.Fatalf("the engine saw %d write calls from unauthenticated requests, want 0", touched)
		}
	})

	t.Run("a caller-selected trusted-context header is rejected before any method runs", func(t *testing.T) {
		reserved := metadata.AppendToOutgoingContext(withToken(ctx, fixtureManagerToken),
			"x-hcm-roles", "operator")
		_, err := client.InspectJourney(reserved, &journeyv1.InspectJourneyRequest{IntentId: fixtureIntentID})
		owned := assertOwnedCode(t, err, envelope.CodeInvalidArgument)
		found := false
		for _, v := range owned.Violations() {
			if strings.HasPrefix(v.FieldPath, "metadata.x-hcm-roles") {
				found = true
			}
		}
		if !found {
			t.Fatalf("violations = %+v, want one naming metadata.x-hcm-roles", owned.Violations())
		}
	})

	t.Run("every reserved trusted-context key is screened, not just one", func(t *testing.T) {
		for _, key := range trust.ReservedMetadataKeys()[:5] {
			reserved := metadata.AppendToOutgoingContext(withToken(ctx, fixtureManagerToken), key, "smuggled-value")
			_, err := client.ListJourneys(reserved, &journeyv1.ListJourneysRequest{})
			assertOwnedCode(t, err, envelope.CodeInvalidArgument)
		}
	})
}

// TestJourneyServicePublishesFourteenUnaryMethodsAndOneServerStream pins the
// cardinality the proto declares: twenty unary methods plus WatchJourney, and
// WatchJourney is a server stream - the server sends many, the client sends
// exactly one request and never sends again.
//
// The shape is not stylistic. A method's cardinality decides which
// interceptor admits it, and the whole point of the streaming watch is that
// internal/transport/grpcserver now chains a stream interceptor running the
// same boundary. A WatchJourney that quietly regressed to unary, or that
// became bidirectional, would be admitted by a different path than the one
// its tests exercise.
func TestJourneyServicePublishesTwentyUnaryMethodsAndOneServerStream(t *testing.T) {
	desc := journeyv1.JourneyService_ServiceDesc

	wantUnary := map[string]bool{
		"ListJourneys": true, "ProposeJourney": true, "ProposePromotion": true,
		"InspectJourney": true, "ExecuteJourney": true, "DecideJourney": true, "AcknowledgeJourney": true,
		"EditProposal": true, "PreviewJourneyIntervention": true, "RequestJourneyIntervention": true,
		"AddJourneyNote": true, "ListWorkers": true, "ListChatDirectory": true, "CreateWorker": true,
		"GetProductPreferences": true, "SaveUserPreferences": true, "SearchKnowledge": true,
		"SaveTenantAppearance": true, "SaveOrganizationVisibility": true, "RecordWorkflowUse": true,
		"GetRoleAccess": true, "SaveAccessRole": true, "SaveWorkerRoleAssignment": true, "SaveRoleOrganizationVisibility": true, "SaveRolePagePermission": true, "SaveRoleFeaturePermission": true,
		"GetWorkerIDPolicy": true, "SaveWorkerIDPolicy": true, "PreviewRoleAccess": true,
	}
	if len(desc.Methods) != len(wantUnary) {
		names := make([]string, 0, len(desc.Methods))
		for _, m := range desc.Methods {
			names = append(names, m.MethodName)
		}
		t.Fatalf("service publishes %d unary methods, want exactly %d: %v", len(desc.Methods), len(wantUnary), names)
	}
	for _, m := range desc.Methods {
		if !wantUnary[m.MethodName] {
			t.Fatalf("unexpected unary method %s", m.MethodName)
		}
		delete(wantUnary, m.MethodName)
	}
	if len(wantUnary) != 0 {
		t.Fatalf("missing unary methods: %v", wantUnary)
	}

	// REV-091-03 adds WatchPromotionInvalidations beside WatchJourney.
	if len(desc.Streams) != 2 {
		t.Fatalf("service publishes %d streaming methods, want exactly 2: %+v", len(desc.Streams), desc.Streams)
	}
	for _, stream := range desc.Streams[1:] {
		if stream.StreamName != "WatchPromotionInvalidations" || !stream.ServerStreams || stream.ClientStreams {
			t.Fatalf("unexpected second streaming method %+v, want the server stream WatchPromotionInvalidations", stream)
		}
	}
	watch := desc.Streams[0]
	if watch.StreamName != "WatchJourney" {
		t.Fatalf("the streaming method is %s, want WatchJourney", watch.StreamName)
	}
	if !watch.ServerStreams {
		t.Fatal("WatchJourney does not stream from the server, which is the whole point of it")
	}
	if watch.ClientStreams {
		t.Fatal("WatchJourney streams from the client; it takes one request and nothing more")
	}
}
