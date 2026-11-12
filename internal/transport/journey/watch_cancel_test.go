package journey_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// recordingLogger captures the one structured record the shared boundary
// emits per call, so a test can assert how a stream's ending was classified
// - which the client, who sees CANCELED either way, cannot tell.
type recordingLogger struct {
	mu      sync.Mutex
	records []transport.LogRecord
}

func (l *recordingLogger) LogRequest(rec transport.LogRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, rec)
}

// last returns the record for method, waiting for it to be emitted.
func (l *recordingLogger) last(t *testing.T, method string) transport.LogRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		for i := len(l.records) - 1; i >= 0; i-- {
			if l.records[i].Method == method {
				rec := l.records[i]
				l.mu.Unlock()
				return rec
			}
		}
		l.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no record was emitted for %s", method)
	return transport.LogRecord{}
}

// startLoggingTestServer is [startTestServer] with a logger on the admission
// configuration.
func startLoggingTestServer(t *testing.T, deps journey.Dependencies, logger transport.Logger) *grpc.ClientConn {
	t.Helper()
	cfg := transport.Config{Verifier: fakeVerifier{}, Logger: logger}
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)),
		grpc.ChainStreamInterceptor(grpcserver.StreamInterceptor(cfg)),
	)
	journey.Register(srv, deps)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		srv.Stop()
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	})
	return conn
}

// TestWatchJourneyCancelRacingAPollIsRecordedAsACancellation proves the
// handler does not turn a client's cancel into an engine failure. The real
// engine opens a transaction on the stream context and fails the instant the
// caller is gone; when that failure lands on a poll that was already running
// as the cancel arrived, the stream must still end as a cancellation
// (transport.request_canceled, UNAVAILABLE) and never as the INTERNAL
// "journey.watch.failed" a genuine engine fault produces.
func TestWatchJourneyCancelRacingAPollIsRecordedAsACancellation(t *testing.T) {
	const method = "/hcmnext.journey.v1.JourneyService/WatchJourney"
	engine := newFakeEngine()
	engine.inspectHonorsContext = true
	logger := &recordingLogger{}
	deps := watchDeps(engine)
	deps.RoleAccess = defaultFixtureRoleAccess()
	client := dialJourneyClient(startLoggingTestServer(t, deps, logger))

	// Every poll runs against the stream's own context, so cancelling right
	// after the opening emission, over several rounds, reaches the poll path
	// with a done context often enough that a misclassification would show.
	for round := range 6 {
		ctx, cancel := context.WithCancel(withToken(context.Background(), fixtureManagerToken))
		watch := openWatch(t, ctx, client, &journeyv1.WatchJourneyRequest{IntentId: fixtureIntentID})
		watch.recv(5 * time.Second)
		// Let at least one poll start before the cancel lands.
		time.Sleep(testPollInterval / 2)
		cancel()
		if _, err := watch.next(5 * time.Second); err == nil {
			t.Fatalf("round %d: a cancelled stream delivered another message", round)
		}
		rec := logger.last(t, method)
		if rec.Code == envelope.CodeUnspecified {
			t.Fatalf("round %d: an abandoned stream was recorded as a success", round)
		}
		if rec.Code != envelope.CodeUnavailable || rec.ReasonRef != "transport.request_canceled" {
			t.Fatalf("round %d: cancelled stream recorded as %s/%s, want UNAVAILABLE/transport.request_canceled",
				round, rec.Code, rec.ReasonRef)
		}
	}
}
