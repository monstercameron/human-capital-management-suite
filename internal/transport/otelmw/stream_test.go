package otelmw_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	transportjourney "github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/otelmw"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
)

// The streaming half of this package's claim. harness_test.go proves one
// span per unary call on both transports; what follows proves the same of a
// server-streaming gRPC call, which is a different claim: the span covers
// the stream's whole lifetime, it still sees the admitted context (so the
// correlation attribute is the admitted request id, not something this
// package re-derived), and an abandoned stream is not recorded as a success.

const watchProcedure = "/hcmnext.journey.v1.JourneyService/WatchJourney"

// streamEngine is the smallest workspace.JourneyEngine WatchJourney can be
// driven against: one fixed detail, or one fixed refusal.
type streamEngine struct {
	mu         sync.Mutex
	inspectErr error
}

var _ workspace.JourneyEngine = (*streamEngine)(nil)

func (e *streamEngine) Inspect(context.Context, string) (workspace.JourneyDetail, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.inspectErr != nil {
		return workspace.JourneyDetail{}, e.inspectErr
	}
	return workspace.JourneyDetail{
		Summary:  workspace.JourneySummary{IntentID: "intent-stream", Stage: workspace.JourneyStageProposed},
		Approver: "approver",
	}, nil
}

func (e *streamEngine) ListJourneys(context.Context) ([]workspace.JourneySummary, error) {
	return nil, nil
}

func (e *streamEngine) Propose(context.Context, workspace.ProposalInput) (workspace.JourneySummary, error) {
	return workspace.JourneySummary{}, nil
}

func (e *streamEngine) Execute(ctx context.Context, id string) (workspace.JourneyDetail, error) {
	return e.Inspect(ctx, id)
}

func (e *streamEngine) Decide(ctx context.Context, id string, _ workspace.Decision) (workspace.JourneyDetail, error) {
	return e.Inspect(ctx, id)
}

func (e *streamEngine) Acknowledge(ctx context.Context, id string, _ workspace.Acknowledgement) (workspace.JourneyDetail, error) {
	return e.Inspect(ctx, id)
}

func (e *streamEngine) EditProposal(context.Context, string, uint64, string, string, workspace.EditProposalInput) (workspace.JourneySummary, string, error) {
	return workspace.JourneySummary{}, "", nil
}

func (e *streamEngine) PreviewIntervention(context.Context, string, workspace.JourneyInterventionKind) (workspace.JourneyInterventionPreview, error) {
	return workspace.JourneyInterventionPreview{}, nil
}

func (e *streamEngine) RequestIntervention(context.Context, string, workspace.JourneyInterventionRequest) (workspace.JourneyInterventionResult, error) {
	return workspace.JourneyInterventionResult{}, nil
}

func (e *streamEngine) ListWorkers(context.Context) ([]workspace.WorkerSummary, workspace.WorkforceOptions, error) {
	return nil, workspace.WorkforceOptions{}, nil
}

func (e *streamEngine) CreateWorker(context.Context, workspace.WorkerInput) (workspace.WorkerSummary, error) {
	return workspace.WorkerSummary{}, nil
}

// streamHarness is a gRPC server carrying the shared admission chain for
// both cardinalities plus this package's two interceptors after it - the
// exact composition internal/transport/cell builds for a cell with
// Telemetry - with JourneyService registered over streamEngine.
type streamHarness struct {
	provider     *hcmotel.Provider
	spanExporter *tracetest.InMemoryExporter
	token        string
	engine       *streamEngine
	client       journeyv1.JourneyServiceClient
}

func newStreamHarness(t testing.TB) *streamHarness {
	t.Helper()
	provider, spanExporter := newTestProvider(t)
	clock := &fakeClock{now: baseTime}
	verifier, err := transporttest.NewVerifier(clock.Now)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(baseTime))
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	cfg := transporttest.Config(verifier, clock.Now, fixedRequestID, nil)

	engine := &streamEngine{}
	srv, err := grpcserver.NewServer(grpcserver.Options{
		Config:   cfg,
		Intent:   &transporttest.IntentHandler{},
		Registry: &transporttest.RegistryHandler{},
		ServerOptions: []grpc.ServerOption{
			grpc.ChainUnaryInterceptor(otelmw.UnaryServerInterceptor(provider)),
			grpc.ChainStreamInterceptor(otelmw.StreamServerInterceptor(provider)),
		},
	})
	if err != nil {
		t.Fatalf("grpcserver.NewServer: %v", err)
	}
	transportjourney.Register(srv, transportjourney.Dependencies{Engine: engine, PollInterval: 20 * time.Millisecond})

	listener := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = listener.Close()
	})
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &streamHarness{
		provider: provider, spanExporter: spanExporter, token: token,
		engine: engine, client: journeyv1.NewJourneyServiceClient(conn),
	}
}

// awaitStreamSpans is awaitSpans for this harness.
func awaitStreamSpans(t testing.TB, h *streamHarness, want int) tracetest.SpanStubs {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if report := h.provider.ForceFlush(context.Background()); report.Err() != nil {
			t.Fatalf("ForceFlush: %v", report.Err())
		}
		var matched tracetest.SpanStubs
		for _, s := range h.spanExporter.GetSpans() {
			if s.Name == watchProcedure {
				matched = append(matched, s)
			}
		}
		if len(matched) >= want || time.Now().After(deadline) {
			return matched
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestStreamServerInterceptorSpansAnAbandonedStreamAsCancelled proves the
// stream span exists, is named by the procedure, carries the admitted
// correlation id, and records a client-cancelled stream as the cancellation
// it was rather than as a success - the handler returns nil on cancel, and
// the span must not take that at face value.
func TestStreamServerInterceptorSpansAnAbandonedStreamAsCancelled(t *testing.T) {
	h := newStreamHarness(t)
	harness := &harness{token: h.token}

	ctx, cancel := context.WithCancel(harness.grpcContext(context.Background()))
	stream, err := h.client.WatchJourney(ctx, &journeyv1.WatchJourneyRequest{IntentId: "intent-stream"})
	if err != nil {
		t.Fatalf("WatchJourney: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("the opening emission never arrived: %v", err)
	}
	// Hold the stream open for a measurable lifetime before abandoning it:
	// the assertion below is that the span covers the whole stream, and on a
	// fast host open-to-cancel is otherwise well under a millisecond.
	time.Sleep(30 * time.Millisecond)
	cancel()
	if _, err := stream.Recv(); err == nil {
		t.Fatal("a cancelled stream delivered another message")
	}

	spans := awaitStreamSpans(t, h, 1)
	if len(spans) != 1 {
		t.Fatalf("found %d WatchJourney spans; want 1", len(spans))
	}
	span := spans[0]
	if v, ok := attrValue(span, "correlation_id"); !ok || v != fixedRequestID {
		t.Errorf("correlation_id = %q, %v; want the admitted request id %q", v, ok, fixedRequestID)
	}
	if v, ok := attrValue(span, "outcome"); !ok || v != "error" {
		t.Errorf("outcome = %q, %v; want \"error\" for an abandoned stream", v, ok)
	}
	if v, ok := attrValue(span, "error_type"); !ok || v != "UNAVAILABLE" {
		t.Errorf("error_type = %q, %v; want UNAVAILABLE (transport.request_canceled's own code)", v, ok)
	}
	if span.Status.Code != otelcodes.Error {
		t.Errorf("status code = %v; want Error", span.Status.Code)
	}
	if span.EndTime.Before(span.StartTime.Add(20 * time.Millisecond)) {
		t.Errorf("span lasted %v; want the stream's lifetime, not one round trip", span.EndTime.Sub(span.StartTime))
	}
}

// TestStreamServerInterceptorSetsErrorStatusFromTheOwnedRefusal proves a
// stream the handler ended with an owned refusal is projected exactly as a
// unary refusal is (TestTodo_OBS_002_TransportInterceptorsSetErrorSpanStatus),
// through the same transport.OwnedError classification.
func TestStreamServerInterceptorSetsErrorStatusFromTheOwnedRefusal(t *testing.T) {
	h := newStreamHarness(t)
	h.engine.mu.Lock()
	h.engine.inspectErr = workspace.ErrJourneyUnknown
	h.engine.mu.Unlock()
	harness := &harness{token: h.token}

	stream, err := h.client.WatchJourney(harness.grpcContext(context.Background()),
		&journeyv1.WatchJourneyRequest{IntentId: "intent-missing"})
	if err != nil {
		t.Fatalf("WatchJourney: %v", err)
	}
	if _, err := stream.Recv(); err == nil {
		t.Fatal("a refused stream delivered a message")
	}

	spans := awaitStreamSpans(t, h, 1)
	if len(spans) != 1 {
		t.Fatalf("found %d WatchJourney spans; want 1", len(spans))
	}
	span := spans[0]
	if v, ok := attrValue(span, "error_type"); !ok || v != "NOT_FOUND" {
		t.Errorf("error_type = %q, %v; want NOT_FOUND", v, ok)
	}
	if v, ok := attrValue(span, "outcome"); !ok || v != "error" {
		t.Errorf("outcome = %q, %v; want \"error\"", v, ok)
	}
	if span.Status.Code != otelcodes.Error {
		t.Errorf("status code = %v; want Error", span.Status.Code)
	}
}

// TestStreamServerInterceptorRefusedStreamsAreNotSpanned proves the ordering
// requirement holds on the stream path: a stream admission refuses never
// reaches this package's interceptor, so no span is exported for it.
func TestStreamServerInterceptorRefusedStreamsAreNotSpanned(t *testing.T) {
	h := newStreamHarness(t)
	stream, err := h.client.WatchJourney(context.Background(), &journeyv1.WatchJourneyRequest{IntentId: "intent-stream"})
	if err != nil {
		t.Fatalf("WatchJourney: %v", err)
	}
	_, err = stream.Recv()
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("an unauthenticated stream must be refused, got %v", err)
	}
	if spans := awaitStreamSpans(t, h, 1); len(spans) != 0 {
		t.Fatalf("found %d WatchJourney spans for a refused stream; want none", len(spans))
	}
}
