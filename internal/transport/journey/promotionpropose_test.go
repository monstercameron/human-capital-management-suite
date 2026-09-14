package journey_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// The wire half of the intent-only promotion.propose contract.
//
// What this file proves is the transport's own share of it and nothing more:
// the handler admits through the shared trusted-request boundary, refuses a
// request carrying bytes the contract does not define before any application
// code runs, forwards an accepted request unchanged to the one Promotion
// application service, and answers UNAVAILABLE rather than Unimplemented on a
// cell whose engine does not serve this contract. What the application service
// then does with the request is internal/intent/app's test's claim.

// fakePromotionEngine is [fakeEngine] that also serves the promotion.propose
// contract. It is a wrapper rather than more fields on fakeEngine so that the
// "engine composed without this contract" case stays reachable: a cell whose
// engine is a plain fakeEngine is exactly a cell that does not implement
// journey.PromotionProposer.
type fakePromotionEngine struct {
	*fakeEngine

	mu    sync.Mutex
	res   *journeyv1.ProposePromotionResponse
	err   error
	last  *journeyv1.ProposePromotionRequest
	calls int
}

var _ journey.PromotionProposer = (*fakePromotionEngine)(nil)

func newFakePromotionEngine() *fakePromotionEngine {
	return &fakePromotionEngine{
		fakeEngine: newFakeEngine(),
		res: &journeyv1.ProposePromotionResponse{
			IntentId:               "intent-promo-007",
			CorrelationId:          "corr-promo-007",
			ProposalRevisionId:     "revision-1",
			MaterialDigest:         "sha256:fixture-material-digest",
			CanonicalRequestDigest: "sha256:fixture-canonical-request-digest",
			RequestDigest:          "sha256:fixture-request-digest",
			Stage:                  journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED,
		},
	}
}

func (f *fakePromotionEngine) ProposePromotion(
	_ context.Context, req *journeyv1.ProposePromotionRequest,
) (*journeyv1.ProposePromotionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.last = req
	if f.err != nil {
		return nil, f.err
	}
	return f.res, nil
}

func (f *fakePromotionEngine) observed() (int, *journeyv1.ProposePromotionRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.last
}

// promotionProposeWireFixture is a complete request as a client sends it.
func promotionProposeWireFixture() *journeyv1.ProposePromotionRequest {
	return &journeyv1.ProposePromotionRequest{
		SubjectWorkerRef:        "omar-reyes",
		DesiredJobCode:          "OPS-HRBP3",
		DesiredGrade:            "P3",
		DesiredPositionId:       "POS-HRBP-301",
		DesiredOrgUnit:          "people-ops",
		DesiredManagerRef:       "rel_mgr_9f2a",
		DesiredBasePay:          "98000.00",
		DesiredPayCurrency:      "USD",
		EffectiveDate:           "2026-06-01",
		Reason:                  "promotion_into_senior_hrbp",
		ExpectedSubjectRevision: "rewards.package.omar-reyes@1",
		ClientRequestId:         "req-0191f3c4-1",
	}
}

// smuggledCurrentPayBytes is one syntactically valid but undefined field -
// number 900, wire type 2, carrying "93000.00" - which is the shape of the
// smuggling this contract refuses: a caller asserting the subject's current
// base pay in a message with nowhere to put it.
var smuggledCurrentPayBytes = append([]byte{0xA2, 0x38, 0x08}, []byte("93000.00")...)

// TestProposePromotionForwardsToThePromotionService is the ordinary path: an
// admitted caller's request reaches the application port unchanged, and the
// server-derived answer comes back unchanged.
func TestProposePromotionForwardsToThePromotionService(t *testing.T) {
	engine := newFakePromotionEngine()
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))

	sent := promotionProposeWireFixture()
	res, err := client.ProposePromotion(withToken(context.Background(), fixtureManagerToken), sent)
	if err != nil {
		t.Fatalf("ProposePromotion: %v", err)
	}
	if res.GetIntentId() != "intent-promo-007" || res.GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED {
		t.Fatalf("response = %+v, want the port's own answer", res)
	}
	if res.GetRequestDigest() != "sha256:fixture-request-digest" ||
		res.GetCanonicalRequestDigest() != "sha256:fixture-canonical-request-digest" {
		t.Fatalf("digests = %q / %q, want the port's own", res.GetRequestDigest(), res.GetCanonicalRequestDigest())
	}

	calls, got := engine.observed()
	if calls != 1 {
		t.Fatalf("the port was called %d times, want once", calls)
	}
	for _, field := range []struct{ name, got, want string }{
		{"subject_worker_ref", got.GetSubjectWorkerRef(), sent.GetSubjectWorkerRef()},
		{"desired_job_code", got.GetDesiredJobCode(), sent.GetDesiredJobCode()},
		{"desired_grade", got.GetDesiredGrade(), sent.GetDesiredGrade()},
		{"desired_position_id", got.GetDesiredPositionId(), sent.GetDesiredPositionId()},
		{"desired_org_unit", got.GetDesiredOrgUnit(), sent.GetDesiredOrgUnit()},
		{"desired_manager_ref", got.GetDesiredManagerRef(), sent.GetDesiredManagerRef()},
		{"desired_base_pay", got.GetDesiredBasePay(), sent.GetDesiredBasePay()},
		{"desired_pay_currency", got.GetDesiredPayCurrency(), sent.GetDesiredPayCurrency()},
		{"effective_date", got.GetEffectiveDate(), sent.GetEffectiveDate()},
		{"reason", got.GetReason(), sent.GetReason()},
		{"expected_subject_revision", got.GetExpectedSubjectRevision(), sent.GetExpectedSubjectRevision()},
		{"client_request_id", got.GetClientRequestId(), sent.GetClientRequestId()},
	} {
		if field.got != field.want {
			t.Errorf("%s reached the port as %q, want %q", field.name, field.got, field.want)
		}
	}
}

// TestTodo_PROMO_007_Security is the wire-level refusal: appending fields the
// contract does not define is an error, and the application service is never
// reached, so nothing downstream has to be trusted to ignore them.
//
// The refusal is raised by the shared trusted-request boundary's strict
// decoding rather than by this package, which is why it is asserted by that
// rule's own reference: the property belongs to every method on this server,
// on both transports, because it is one interceptor chain.
func TestTodo_PROMO_007_Security(t *testing.T) {
	t.Run("smuggled wire fields are refused", func(t *testing.T) {
		engine := newFakePromotionEngine()
		client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))

		req := promotionProposeWireFixture()
		req.ProtoReflect().SetUnknown(smuggledCurrentPayBytes)

		_, err := client.ProposePromotion(withToken(context.Background(), fixtureManagerToken), req)
		owned := assertOwnedCode(t, err, envelope.CodeInvalidArgument)
		if owned.ReasonRef() != "structural.request_rejected" {
			t.Fatalf("ReasonRef() = %q, want structural.request_rejected", owned.ReasonRef())
		}
		if calls, _ := engine.observed(); calls != 0 {
			t.Fatalf("the application service was called %d times for a smuggled request", calls)
		}
	})

	t.Run("an unauthenticated caller is refused before anything else", func(t *testing.T) {
		engine := newFakePromotionEngine()
		client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))

		_, err := client.ProposePromotion(context.Background(), promotionProposeWireFixture())
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
		if calls, _ := engine.observed(); calls != 0 {
			t.Fatalf("the application service was called %d times for an unauthenticated request", calls)
		}
	})

	t.Run("an unrecognized credential is refused", func(t *testing.T) {
		engine := newFakePromotionEngine()
		client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))

		_, err := client.ProposePromotion(withToken(context.Background(), fixtureUnknownToken), promotionProposeWireFixture())
		assertOwnedCode(t, err, envelope.CodeUnauthenticated)
		if calls, _ := engine.observed(); calls != 0 {
			t.Fatalf("the application service was called %d times for an unrecognized credential", calls)
		}
	})
}

// TestProposePromotionIsUnavailableWithoutTheContract covers both shapes of
// "this cell does not serve it": no engine at all, and an engine that is not a
// journey.PromotionProposer. Both must be UNAVAILABLE rather than the
// generated service's Unimplemented, so a client learns the surface exists and
// why it cannot act - the same rule every other method here follows.
func TestProposePromotionIsUnavailableWithoutTheContract(t *testing.T) {
	t.Run("no engine", func(t *testing.T) {
		client := dialJourneyClient(startTestServer(t, journey.Dependencies{}))
		_, err := client.ProposePromotion(withToken(context.Background(), fixtureManagerToken), promotionProposeWireFixture())
		assertOwnedCode(t, err, envelope.CodeUnavailable)
	})

	t.Run("an engine that does not serve the contract", func(t *testing.T) {
		client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: newFakeEngine()}))
		_, err := client.ProposePromotion(withToken(context.Background(), fixtureManagerToken), promotionProposeWireFixture())
		owned := assertOwnedCode(t, err, envelope.CodeUnavailable)
		if owned.ReasonRef() != "journey.propose_promotion.engine_unconfigured" {
			t.Fatalf("ReasonRef() = %q, want journey.propose_promotion.engine_unconfigured", owned.ReasonRef())
		}
	})
}

// TestProposePromotionProjectsThePortsRefusals pins that this handler adds no
// error model of its own: the port's sentinels project through the same
// mapping every other journey method uses.
func TestProposePromotionProjectsThePortsRefusals(t *testing.T) {
	cases := map[string]struct {
		err  error
		want envelope.Code
	}{
		"input":       {workspace.ErrJourneyInput, envelope.CodeInvalidArgument},
		"denied":      {workspace.ErrDenied, envelope.CodePermissionDenied},
		"unknown":     {workspace.ErrJourneyUnknown, envelope.CodeNotFound},
		"stage":       {workspace.ErrJourneyStage, envelope.CodeFailedPrecondition},
		"unavailable": {workspace.ErrJourneyUnavailable, envelope.CodeUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			engine := newFakePromotionEngine()
			engine.err = tc.err
			client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
			_, err := client.ProposePromotion(withToken(context.Background(), fixtureManagerToken), promotionProposeWireFixture())
			assertOwnedCode(t, err, tc.want)
		})
	}

	t.Run("an owned refusal travels unchanged", func(t *testing.T) {
		engine := newFakePromotionEngine()
		engine.err = envelope.New(envelope.CodeFailedPrecondition, "promotion.propose.stale_subject_revision",
			"a precondition for the operation is not met")
		client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
		_, err := client.ProposePromotion(withToken(context.Background(), fixtureManagerToken), promotionProposeWireFixture())
		owned := assertOwnedCode(t, err, envelope.CodeFailedPrecondition)
		if owned.ReasonRef() != "promotion.propose.stale_subject_revision" {
			t.Fatalf("ReasonRef() = %q, want the port's own", owned.ReasonRef())
		}
	})

	t.Run("an unclassified failure does not become retryable", func(t *testing.T) {
		engine := newFakePromotionEngine()
		engine.err = errors.New("the promotion service fell over")
		client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
		_, err := client.ProposePromotion(withToken(context.Background(), fixtureManagerToken), promotionProposeWireFixture())
		owned := assertOwnedCode(t, err, envelope.CodeUnspecified)
		if owned.ReasonRef() != "journey.propose_promotion.failed" {
			t.Fatalf("ReasonRef() = %q, want journey.propose_promotion.failed", owned.ReasonRef())
		}
	})
}

func TestTodo_PROMOUX_007_Conformance_ServerPayBoundsSurviveGRPC(t *testing.T) {
	minimum, err := values.NewMoney("105.04", "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := values.NewMoney("115.03", "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	engine := newFakePromotionEngine()
	engine.err = &workspace.JourneyInputError{
		FieldPath: "proposed_base", ReasonRef: "promotion.ladder.base_increase_out_of_range",
		Detail: "private pay baseline 100.03", PayRange: &workspace.JourneyPayRange{Minimum: minimum, Maximum: maximum},
	}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	_, err = client.ProposePromotion(withToken(context.Background(), fixtureManagerToken), promotionProposeWireFixture())
	owned := assertOwnedCode(t, err, envelope.CodeInvalidArgument)
	violations := owned.Violations()
	if len(violations) != 1 || violations[0].MoneyRange != (envelope.MoneyRange{Minimum: "105.04", Maximum: "115.03", Currency: "USD"}) {
		t.Fatalf("wire refusal lost exact bound data: %+v", violations)
	}
	if strings.Contains(err.Error(), "private pay baseline") || strings.Contains(err.Error(), "100.03") {
		t.Fatalf("wire refusal disclosed internal baseline: %v", err)
	}

	engine.err = &workspace.JourneyInputError{FieldPath: "reason", ReasonRef: "promotion.ladder.base_increase_out_of_range", PayRange: &workspace.JourneyPayRange{Minimum: minimum, Maximum: maximum}}
	_, err = client.ProposePromotion(withToken(context.Background(), fixtureManagerToken), promotionProposeWireFixture())
	owned = assertOwnedCode(t, err, envelope.CodeInvalidArgument)
	if got := owned.Violations()[0].MoneyRange; got != (envelope.MoneyRange{}) {
		t.Fatalf("pay bounds leaked through unrelated field: %+v", got)
	}
}
