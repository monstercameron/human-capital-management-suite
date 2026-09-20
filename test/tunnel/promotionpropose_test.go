package tunnel_test

// The intent-only `promotion.propose` contract (PROMO-007) over the
// gRPC-over-WebSocket tunnel.
//
// test/bootstrap proves the contract's semantics against the real composed
// Promotion service on both transports. What this file adds is the tunnel's
// own share of the claim, which that suite cannot make on its own: that a
// browser-shaped client - one that reached this cell through a websocket
// upgrade and speaks canonical gRPC frames over it, with no HTTP/JSON
// business surface anywhere - invokes the same registered handler, is
// admitted per call exactly as a native client is, and is refused the same
// way when it appends bytes the contract does not define.

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	transportjourney "github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// fakePromotionEngine is [fakeJourneyEngine] that also serves the
// promotion.propose contract.
//
// It is a fake for the same reason the watch tests' engine is: what is under
// test is whether the request and the response survive the websocket, the
// bridge's HTTP/2 framing and the shared interceptor chain, not what the real
// engine reads. Its ProposePromotion records the request it was handed, which
// is how "the handler was reached, and reached with the caller's own fields"
// is asserted rather than assumed.
type fakePromotionEngine struct {
	*fakeJourneyEngine

	mu    sync.Mutex
	last  *journeyv1.ProposePromotionRequest
	calls int
}

var _ transportjourney.PromotionProposer = (*fakePromotionEngine)(nil)

func newFakePromotionEngine() *fakePromotionEngine {
	return &fakePromotionEngine{fakeJourneyEngine: newFakeJourneyEngine()}
}

func (f *fakePromotionEngine) ProposePromotion(
	_ context.Context, req *journeyv1.ProposePromotionRequest,
) (*journeyv1.ProposePromotionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.last = req
	return &journeyv1.ProposePromotionResponse{
		IntentId:               "intent-tunnel-promo",
		CorrelationId:          "corr-tunnel-promo",
		ProposalRevisionId:     "revision-1",
		MaterialDigest:         "sha256:tunnel-material-digest",
		CanonicalRequestDigest: "sha256:tunnel-canonical-request-digest",
		RequestDigest:          "sha256:tunnel-request-digest",
		Stage:                  journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED,
	}, nil
}

func (f *fakePromotionEngine) observed() (int, *journeyv1.ProposePromotionRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.last
}

// tunnelPromotionRequest is a complete intent-only promotion.propose request.
func tunnelPromotionRequest() *journeyv1.ProposePromotionRequest {
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
		Reason:                  "Promotion into the senior HRBP role",
		ExpectedSubjectRevision: "rewards.package.omar-reyes@1",
		ClientRequestId:         "req-tunnel-1",
	}
}

// tunnelSmuggledCurrentPay is one syntactically valid but undefined Protobuf
// field - number 900, wire type 2, carrying "93000.00" - the exact shape of
// the smuggling this contract refuses.
var tunnelSmuggledCurrentPay = append([]byte{0xA2, 0x38, 0x08}, []byte("93000.00")...)

// TestTodo_PROMO_007_Integration is the browser-shaped path end to end: the
// closed request crosses the websocket, the handler is reached with every
// field the caller stated and nothing added, and the server-derived answer
// crosses back.
func TestTodo_PROMO_007_Integration(t *testing.T) {
	engine := newFakePromotionEngine()
	c := newCellWith(t, true, engine)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := c.dial(ctx, c.authorizedUpgrade())
	client := journeyv1.NewJourneyServiceClient(conn)
	callCtx := metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, c.token)

	sent := tunnelPromotionRequest()
	res, err := client.ProposePromotion(callCtx, sent)
	if err != nil {
		t.Fatalf("ProposePromotion over the tunnel: %v", err)
	}
	if res.GetIntentId() != "intent-tunnel-promo" ||
		res.GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED {
		t.Fatalf("response = %+v, want the application service's own answer", res)
	}
	if res.GetRequestDigest() != "sha256:tunnel-request-digest" {
		t.Fatalf("request_digest = %q, want the application service's own", res.GetRequestDigest())
	}

	calls, got := engine.observed()
	if calls != 1 {
		t.Fatalf("the application service was called %d times, want once", calls)
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
			t.Errorf("%s arrived as %q, want %q", field.name, field.got, field.want)
		}
	}
	if n := len(got.ProtoReflect().GetUnknown()); n != 0 {
		t.Errorf("the request arrived carrying %d bytes of undefined fields", n)
	}
}

// TestTodo_PROMO_007_Security is the tunnel's half of the smuggling refusal.
//
// A browser is the caller with the most room to try: it composes the frames
// itself, and the socket it is on was already admitted. Neither helps. The
// undefined bytes are refused by the same strict decoding every method on
// this server is admitted through, and the application service is never
// reached; and a call that carries no credential of its own is refused even
// though the upgrade carried one, because upgrading is not authenticating.
func TestTodo_PROMO_007_Security(t *testing.T) {
	t.Run("a smuggled current pay is refused and never reaches the service", func(t *testing.T) {
		engine := newFakePromotionEngine()
		c := newCellWith(t, true, engine)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		conn := c.dial(ctx, c.authorizedUpgrade())
		client := journeyv1.NewJourneyServiceClient(conn)
		callCtx := metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, c.token)

		req := tunnelPromotionRequest()
		req.ProtoReflect().SetUnknown(tunnelSmuggledCurrentPay)

		_, err := client.ProposePromotion(callCtx, req)
		if err == nil {
			t.Fatal("a request carrying undefined fields was answered")
		}
		owned, ok := envelope.FromGRPC(err)
		if !ok {
			t.Fatalf("error %v did not decode as an owned envelope.Error", err)
		}
		if owned.Code() != envelope.CodeInvalidArgument {
			t.Fatalf("Code() = %s, want INVALID_ARGUMENT (reason=%s)", owned.Code(), owned.ReasonRef())
		}
		if owned.ReasonRef() != "structural.request_rejected" {
			t.Fatalf("ReasonRef() = %q, want structural.request_rejected", owned.ReasonRef())
		}
		if calls, _ := engine.observed(); calls != 0 {
			t.Fatalf("the application service was called %d times for a refused request", calls)
		}
	})

	t.Run("an admitted socket does not authenticate the call", func(t *testing.T) {
		engine := newFakePromotionEngine()
		c := newCellWith(t, true, engine)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn := c.dial(ctx, c.authorizedUpgrade())
		client := journeyv1.NewJourneyServiceClient(conn)

		_, err := client.ProposePromotion(ctx, tunnelPromotionRequest())
		if err == nil {
			t.Fatal("a call with no credential was answered")
		}
		if got := status.Code(err); got != codes.Unauthenticated {
			t.Fatalf("status = %s, want UNAUTHENTICATED: %v", got, err)
		}
		if calls, _ := engine.observed(); calls != 0 {
			t.Fatalf("the application service was called %d times for an unauthenticated call", calls)
		}
	})

	t.Run("a cell whose engine does not serve the contract answers UNAVAILABLE", func(t *testing.T) {
		// newFakeJourneyEngine is a workspace.JourneyEngine and nothing more,
		// which is exactly a cell composed before this contract existed. The
		// client must learn that this deployment cannot act, not that the API
		// does not exist.
		c := newCellWith(t, true, newFakeJourneyEngine())

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn := c.dial(ctx, c.authorizedUpgrade())
		client := journeyv1.NewJourneyServiceClient(conn)
		callCtx := metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, c.token)

		_, err := client.ProposePromotion(callCtx, tunnelPromotionRequest())
		if err == nil {
			t.Fatal("a cell without the contract answered a proposal")
		}
		owned, ok := envelope.FromGRPC(err)
		if !ok {
			t.Fatalf("error %v did not decode as an owned envelope.Error", err)
		}
		if owned.Code() != envelope.CodeUnavailable {
			t.Fatalf("Code() = %s, want UNAVAILABLE", owned.Code())
		}
	})
}
