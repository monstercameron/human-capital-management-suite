package tunnel_test

// This file is the streaming half of the tunnel's claim. tunnel_test.go
// proves a unary RPC survives the websocket and is admitted per call; what
// follows proves the same of a server-streaming one, which is a different
// claim: a stream is admitted once when it opens, its handler runs for the
// life of the socket rather than for one round trip, and the messages it
// sends have to cross the bridge with no request to prompt each of them.

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

// tunnelJourneyIntentID is the journey these tests watch.
const tunnelJourneyIntentID = "intent-tunnel-watch"

// fakeJourneyEngine is a minimal workspace.JourneyEngine whose detail can be
// changed while a WatchJourney stream is open. It is deliberately thin: what
// this suite proves is that a server-streaming RPC survives the tunnel and
// the shared stream interceptor, not what the real engine reads.
type fakeJourneyEngine struct {
	mu     sync.Mutex
	detail workspace.JourneyDetail
}

var _ workspace.JourneyEngine = (*fakeJourneyEngine)(nil)

func newFakeJourneyEngine() *fakeJourneyEngine {
	return &fakeJourneyEngine{detail: workspace.JourneyDetail{
		Summary: workspace.JourneySummary{
			IntentID:   tunnelJourneyIntentID,
			WorkerName: "Jordan Vega",
			Stage:      workspace.JourneyStageAwaitingApproval,
		},
		Approver: "approver-lee",
	}}
}

// advance moves the journey on, which is what the watch stream must notice
// without being asked again.
func (f *fakeJourneyEngine) advance() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detail.Summary.Stage = workspace.JourneyStageCompleted
	f.detail.Approver = "approver-kim"
}

func (f *fakeJourneyEngine) Inspect(context.Context, string) (workspace.JourneyDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.detail, nil
}

func (f *fakeJourneyEngine) ListJourneys(context.Context) ([]workspace.JourneySummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []workspace.JourneySummary{f.detail.Summary}, nil
}

func (f *fakeJourneyEngine) Propose(context.Context, workspace.ProposalInput) (workspace.JourneySummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.detail.Summary, nil
}

func (f *fakeJourneyEngine) Execute(ctx context.Context, intentID string) (workspace.JourneyDetail, error) {
	return f.Inspect(ctx, intentID)
}

func (f *fakeJourneyEngine) Decide(ctx context.Context, intentID string, _ workspace.Decision) (workspace.JourneyDetail, error) {
	return f.Inspect(ctx, intentID)
}

func (f *fakeJourneyEngine) EditProposal(context.Context, string, uint64, string, string, workspace.EditProposalInput) (workspace.JourneySummary, string, error) {
	return workspace.JourneySummary{}, "", nil
}

func (f *fakeJourneyEngine) PreviewIntervention(context.Context, string, workspace.JourneyInterventionKind) (workspace.JourneyInterventionPreview, error) {
	return workspace.JourneyInterventionPreview{}, nil
}

func (f *fakeJourneyEngine) RequestIntervention(context.Context, string, workspace.JourneyInterventionRequest) (workspace.JourneyInterventionResult, error) {
	return workspace.JourneyInterventionResult{}, nil
}

func (f *fakeJourneyEngine) ListWorkers(context.Context) ([]workspace.WorkerSummary, workspace.WorkforceOptions, error) {
	return nil, workspace.WorkforceOptions{}, nil
}

func (f *fakeJourneyEngine) CreateWorker(context.Context, workspace.WorkerInput) (workspace.WorkerSummary, error) {
	return workspace.WorkerSummary{}, workspace.ErrJourneyUnavailable
}

// watchEvent is one thing that happened on a tunnelled watch stream.
type watchEvent struct {
	msg *journeyv1.WatchJourneyResponse
	err error
}

// drain reads a watch stream on one goroutine and reports everything it saw.
// One reader, because a gRPC client stream admits no concurrent Recv and a
// second reader would silently swallow the message an assertion waits for.
// The goroutine ends when the stream does, which every caller here bounds.
func drain(stream journeyv1.JourneyService_WatchJourneyClient) <-chan watchEvent {
	events := make(chan watchEvent, 8)
	go func() {
		defer close(events)
		for {
			msg, err := stream.Recv()
			events <- watchEvent{msg, err}
			if err != nil {
				return
			}
		}
	}()
	return events
}

// nextWatchEvent takes the stream's next event, failing the test if none
// arrives inside d rather than blocking until the suite's own timeout.
func nextWatchEvent(t *testing.T, events <-chan watchEvent, d time.Duration) watchEvent {
	t.Helper()
	select {
	case e, ok := <-events:
		if !ok {
			t.Fatal("the watch stream's reader ended without reporting why")
		}
		return e
	case <-time.After(d):
		t.Fatalf("nothing arrived on the tunnelled watch stream within %v", d)
		return watchEvent{}
	}
}

// TestTunnelServesAServerStreamingRPC is what the whole change exists for.
// hcmnext.journey.v1.JourneyService.WatchJourney is a real server stream,
// and everything between the browser-shaped client and the handler has to
// carry it: the websocket upgrade, the bridge's HTTP/2 framing, the shared
// server's stream interceptor, and the per-RPC credential the bridge
// deliberately does not forward from the upgrade request.
//
// Two messages is the assertion that matters. One would prove only that a
// stream opened; the second, arriving after the engine moved and with no
// second request from the client, is precisely what distinguishes a stream
// from the unary long poll this replaced.
func TestTunnelServesAServerStreamingRPC(t *testing.T) {
	engine := newFakeJourneyEngine()
	c := newCellWith(t, true, engine)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn := c.dial(ctx, c.authorizedUpgrade())
	client := journeyv1.NewJourneyServiceClient(conn)

	callCtx, endStream := context.WithCancel(
		metadata.AppendToOutgoingContext(ctx, transport.AuthorizationMetadataKey, c.token))
	defer endStream()

	stream, err := client.WatchJourney(callCtx, &journeyv1.WatchJourneyRequest{IntentId: tunnelJourneyIntentID})
	if err != nil {
		t.Fatalf("open WatchJourney over the tunnel: %v", err)
	}
	events := drain(stream)

	opening := nextWatchEvent(t, events, 20*time.Second)
	if opening.err != nil {
		t.Fatalf("the opening emission never arrived: %v", opening.err)
	}
	held := opening.msg.GetDetail().GetDetailDigest()
	if held == "" {
		t.Fatal("the opening emission carries no detail digest")
	}
	if got := opening.msg.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL {
		t.Fatalf("opening stage = %v, want AWAITING_APPROVAL", got)
	}

	engine.advance()

	changed := nextWatchEvent(t, events, 20*time.Second)
	if changed.err != nil {
		t.Fatalf("the change never arrived: %v", changed.err)
	}
	if got := changed.msg.GetDetail().GetDetailDigest(); got == held {
		t.Fatal("the second message carries the digest the first already delivered")
	}
	if got := changed.msg.GetDetail().GetJourney().GetStage(); got != journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED {
		t.Fatalf("changed stage = %v, want COMPLETED", got)
	}

	// Cancelling is how a browser closes a watch, and it must end the stream
	// rather than leave the socket holding an abandoned handler.
	endStream()
	ending := nextWatchEvent(t, events, 20*time.Second)
	if ending.err == nil {
		t.Fatal("a cancelled stream delivered another message")
	}
	if got := status.Code(ending.err); got != codes.Canceled {
		t.Fatalf("status = %s, want CANCELED: %v", got, ending.err)
	}
}

// TestTunnelStreamWithoutMetadataIsUnauthenticated is
// TestTunnelRPCWithoutMetadataIsUnauthenticated for the streaming path, and
// it is why the shared boundary grew a stream interceptor at all: a stream
// opened on an admitted socket but carrying no credential of its own is
// refused, exactly as a unary call on that same socket is.
func TestTunnelStreamWithoutMetadataIsUnauthenticated(t *testing.T) {
	c := newCellWith(t, true, newFakeJourneyEngine())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := c.dial(ctx, c.authorizedUpgrade())
	client := journeyv1.NewJourneyServiceClient(conn)

	stream, err := client.WatchJourney(ctx, &journeyv1.WatchJourneyRequest{IntentId: tunnelJourneyIntentID})
	if err != nil {
		t.Fatalf("open WatchJourney over the tunnel: %v", err)
	}
	ending := nextWatchEvent(t, drain(stream), 20*time.Second)
	if ending.err == nil {
		t.Fatal("a stream with no credential must not be answered")
	}
	if got := status.Code(ending.err); got != codes.Unauthenticated {
		t.Fatalf("status = %s, want UNAUTHENTICATED: %v", got, ending.err)
	}
}
