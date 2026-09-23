package grpcserver_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The streaming boundary is exercised through the exported interceptor
// directly, without a listener, for the same reason the unary one is in
// server_test.go: what is under test is the chain's own behaviour, not
// grpc-go's plumbing. The end-to-end proof that a real streaming method is
// admitted this way lives in internal/transport/journey and test/tunnel.

// streamTestMethod is the method descriptor these tests present.
var streamTestMethod = grpc.StreamServerInfo{
	FullMethod:     "/hcmnext.journey.v1.JourneyService/WatchJourney",
	IsServerStream: true,
}

// fakeServerStream is the transport stream grpc-go would hand an
// interceptor. Only Context is meaningful here; the send and receive paths
// record that they were reached so a test can prove the wrapper forwards
// them rather than swallowing them.
type fakeServerStream struct {
	ctx context.Context

	mu    sync.Mutex
	sent  []any
	recvd int
}

func (s *fakeServerStream) SetHeader(metadata.MD) error  { return nil }
func (s *fakeServerStream) SendHeader(metadata.MD) error { return nil }
func (s *fakeServerStream) SetTrailer(metadata.MD)       {}
func (s *fakeServerStream) Context() context.Context     { return s.ctx }

func (s *fakeServerStream) SendMsg(m any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, m)
	return nil
}

func (s *fakeServerStream) RecvMsg(any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recvd++
	return nil
}

func (s *fakeServerStream) sentCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

// streamFixture is one configured stream boundary plus the credential it
// accepts and the records it emitted.
type streamFixture struct {
	interceptor grpc.StreamServerInterceptor
	token       string

	mu      sync.Mutex
	records []transport.LogRecord
}

// newStreamFixture builds the boundary over the shared qualification
// fixture's verifier, so the credential these tests present is a real one
// verified by the real verifier rather than a stub that always says yes.
func newStreamFixture(t *testing.T, mutate func(*transport.Config)) *streamFixture {
	t.Helper()

	now := time.Unix(1_800_000_000, 0)
	verifier, err := transporttest.NewVerifier(func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(now))
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}

	f := &streamFixture{token: token}
	cfg := transport.Config{
		Verifier: verifier,
		Logger: transport.LoggerFunc(func(record transport.LogRecord) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.records = append(f.records, record)
		}),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	f.interceptor = grpcserver.StreamInterceptor(cfg)
	return f
}

// authorized returns a transport stream carrying the fixture credential.
// transporttest.BearerToken already returns the whole header value, scheme
// included, so it is used verbatim.
func (f *streamFixture) authorized(ctx context.Context) *fakeServerStream {
	return &fakeServerStream{ctx: metadata.NewIncomingContext(ctx, metadata.Pairs(
		transport.AuthorizationMetadataKey, f.token))}
}

// emitted returns the records the boundary logged.
func (f *streamFixture) emitted() []transport.LogRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]transport.LogRecord(nil), f.records...)
}

// TestStreamInterceptorRejectsAnUnauthenticatedStream is the streaming
// mirror of TestUnaryInterceptorRejectsAnUnauthenticatedCall, and it is the
// assertion the whole change rests on: opening a stream is a request, and a
// request with no credential never reaches a handler.
func TestStreamInterceptorRejectsAnUnauthenticatedStream(t *testing.T) {
	f := newStreamFixture(t, nil)

	called := false
	err := f.interceptor(nil, &fakeServerStream{ctx: context.Background()}, &streamTestMethod,
		func(any, grpc.ServerStream) error {
			called = true
			return nil
		})
	if err == nil {
		t.Fatal("the interceptor admitted a stream with no credential")
	}
	if called {
		t.Fatal("the handler ran for an unauthenticated stream")
	}

	owned, ok := envelope.As(err)
	if !ok {
		t.Fatalf("error %v is not an owned envelope error", err)
	}
	if owned.Code() != envelope.CodeUnauthenticated {
		t.Fatalf("Code() = %s, want UNAUTHENTICATED (reason=%s)", owned.Code(), owned.ReasonRef())
	}
	if owned.CorrelationID() == "" {
		t.Fatal("a refused stream must still be correlated")
	}
}

// TestStreamInterceptorRefusesACallerSelectedTrustedContext proves the
// reserved-metadata screen runs on the streaming path too. A stream that
// could select its own authority would be a hole in the boundary that no
// unary method has.
func TestStreamInterceptorRefusesACallerSelectedTrustedContext(t *testing.T) {
	f := newStreamFixture(t, nil)

	ss := &fakeServerStream{ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		transport.AuthorizationMetadataKey, f.token,
		trust.ReservedMetadataKeys()[0], "smuggled-value"))}

	called := false
	err := f.interceptor(nil, ss, &streamTestMethod, func(any, grpc.ServerStream) error {
		called = true
		return nil
	})
	if called {
		t.Fatal("the handler ran for a stream that selected its own trusted context")
	}
	owned, ok := envelope.As(err)
	if !ok {
		t.Fatalf("error %v is not an owned envelope error", err)
	}
	if owned.Code() != envelope.CodeInvalidArgument {
		t.Fatalf("Code() = %s, want INVALID_ARGUMENT (reason=%s)", owned.Code(), owned.ReasonRef())
	}
}

// TestStreamInterceptorHandsTheHandlerAnAdmittedStream is the wrapper's
// contract: the stream the handler receives carries the enriched context -
// principal and invocation - while every other stream method still reaches
// the transport's own stream.
func TestStreamInterceptorHandsTheHandlerAnAdmittedStream(t *testing.T) {
	f := newStreamFixture(t, nil)
	underlying := f.authorized(context.Background())

	var (
		gotPrincipal *trust.Principal
		gotInv       *transport.Invocation
	)
	err := f.interceptor(nil, underlying, &streamTestMethod, func(_ any, ss grpc.ServerStream) error {
		if ss == underlying {
			t.Error("the handler received the raw transport stream, not the admitted one")
		}
		principal, ok := trust.FromContext(ss.Context())
		if !ok {
			t.Error("the admitted stream's context carries no principal")
		}
		gotPrincipal = principal
		inv, ok := transport.InvocationFromContext(ss.Context())
		if !ok {
			t.Error("the admitted stream's context carries no invocation")
		}
		gotInv = inv
		// The send path must reach the transport stream unchanged.
		return ss.SendMsg("one")
	})
	if err != nil {
		t.Fatalf("an authenticated stream was refused: %v", err)
	}
	if gotPrincipal == nil || gotPrincipal.Subject() != transporttest.Subject {
		t.Fatalf("principal = %+v, want subject %q", gotPrincipal, transporttest.Subject)
	}
	if gotInv == nil {
		t.Fatal("the handler saw no invocation")
	}
	if gotInv.Method() != streamTestMethod.FullMethod {
		t.Fatalf("invocation method = %q, want %q", gotInv.Method(), streamTestMethod.FullMethod)
	}
	if gotInv.Kind() != transport.KindGRPC {
		t.Fatalf("invocation kind = %q, want %q", gotInv.Kind(), transport.KindGRPC)
	}
	if gotInv.TenantID() != transporttest.Tenant {
		t.Fatalf("invocation tenant = %q, want %q", gotInv.TenantID(), transporttest.Tenant)
	}
	if underlying.sentCount() != 1 {
		t.Fatalf("the transport stream saw %d sends, want 1: the wrapper is not forwarding", underlying.sentCount())
	}
}

// TestStreamInterceptorCapsTheStreamDeadline proves a stream is bounded by
// the server, not by the client's ambition: a stream opened with no deadline
// at all gets the configured cap, and one opened with a nearer deadline
// keeps its own.
//
// The bound is MaxStreamDeadline, not MaxDeadline. A live feed is not a slow
// request: capping a server stream at the unary request budget ended every chat
// and journey watch at exactly thirty seconds, so the two caps are configured
// and asserted separately.
func TestStreamInterceptorCapsTheStreamDeadline(t *testing.T) {
	const limit = 250 * time.Millisecond
	f := newStreamFixture(t, func(cfg *transport.Config) {
		// The unary cap is set far shorter on purpose: a stream that took it
		// would fail every assertion below.
		cfg.MaxDeadline = limit / 25
		cfg.MaxStreamDeadline = limit
	})

	t.Run("a stream with no deadline gets the cap", func(t *testing.T) {
		var deadline time.Time
		var ok bool
		err := f.interceptor(nil, f.authorized(context.Background()), &streamTestMethod,
			func(_ any, ss grpc.ServerStream) error {
				deadline, ok = ss.Context().Deadline()
				return nil
			})
		if err != nil {
			t.Fatalf("interceptor: %v", err)
		}
		if !ok {
			t.Fatal("the admitted stream has no deadline; an unbounded stream is exactly what the cap prevents")
		}
		if remaining := time.Until(deadline); remaining > limit {
			t.Fatalf("the admitted stream has %v remaining, past the %v cap", remaining, limit)
		}
	})

	t.Run("a nearer client deadline is not extended", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), limit/5)
		defer cancel()

		var remaining time.Duration
		err := f.interceptor(nil, f.authorized(ctx), &streamTestMethod,
			func(_ any, ss grpc.ServerStream) error {
				deadline, ok := ss.Context().Deadline()
				if !ok {
					t.Error("the admitted stream lost the client's deadline")
					return nil
				}
				remaining = time.Until(deadline)
				return nil
			})
		if err != nil {
			t.Fatalf("interceptor: %v", err)
		}
		if remaining > limit/5 {
			t.Fatalf("remaining = %v, want no more than the client's own %v", remaining, limit/5)
		}
	})

	t.Run("the cap ends a handler that outstays it", func(t *testing.T) {
		err := f.interceptor(nil, f.authorized(context.Background()), &streamTestMethod,
			func(_ any, ss grpc.ServerStream) error {
				<-ss.Context().Done()
				// A handler that observes the end and returns cleanly has
				// still not produced a successful stream.
				return nil
			})
		owned, ok := envelope.As(err)
		if !ok {
			t.Fatalf("error %v is not an owned envelope error", err)
		}
		if owned.Code() != envelope.CodeDeadlineExceeded {
			t.Fatalf("Code() = %s, want DEADLINE_EXCEEDED", owned.Code())
		}
	})
}

// TestStreamInterceptorPropagatesCancellationToTheHandler proves the
// admitted context is a child of the transport stream's own: a client that
// disconnects ends the handler through the context it is already selecting
// on, with no forwarding of any kind in the wrapper.
func TestStreamInterceptorPropagatesCancellationToTheHandler(t *testing.T) {
	f := newStreamFixture(t, nil)
	ctx, cancel := context.WithCancel(context.Background())

	observed := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		done <- f.interceptor(nil, f.authorized(ctx), &streamTestMethod,
			func(_ any, ss grpc.ServerStream) error {
				<-ss.Context().Done()
				observed <- ss.Context().Err()
				return nil
			})
	}()

	select {
	case <-observed:
		t.Fatal("the handler's context ended before the client cancelled")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()

	select {
	case err := <-observed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("the handler observed %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the client did not end the handler's context")
	}

	select {
	case err := <-done:
		owned, ok := envelope.As(err)
		if !ok {
			t.Fatalf("error %v is not an owned envelope error", err)
		}
		if owned.Code() != envelope.CodeUnavailable {
			t.Fatalf("Code() = %s, want UNAVAILABLE for a cancelled stream", owned.Code())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the interceptor did not return after the handler did")
	}
}

// TestStreamInterceptorProjectsAHandlerFailure is the error-model half: what
// a streaming handler raises is projected exactly as a unary handler's
// failure is, owned errors passed through and raw ones never leaking their
// text.
func TestStreamInterceptorProjectsAHandlerFailure(t *testing.T) {
	const rawDiagnostic = `pq: password authentication failed for user "hcmnext"`

	cases := []struct {
		name string
		err  error
		want envelope.Code
	}{
		{
			name: "an owned refusal is passed through",
			err: envelope.New(envelope.CodeNotFound, "journey.watch.unknown",
				"the resource does not exist or is not visible"),
			want: envelope.CodeNotFound,
		},
		{
			name: "a deadline is recognized rather than coerced",
			err:  context.DeadlineExceeded,
			want: envelope.CodeDeadlineExceeded,
		},
		{
			name: "an unclassified failure is projected without its text",
			err:  errors.New(rawDiagnostic),
			want: envelope.CodeUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newStreamFixture(t, nil)
			err := f.interceptor(nil, f.authorized(context.Background()), &streamTestMethod,
				func(any, grpc.ServerStream) error { return tc.err })

			owned, ok := envelope.As(err)
			if !ok {
				t.Fatalf("error %v is not an owned envelope error", err)
			}
			if owned.Code() != tc.want {
				t.Fatalf("Code() = %s, want %s (reason=%s)", owned.Code(), tc.want, owned.ReasonRef())
			}
			if owned.CorrelationID() == "" {
				t.Fatal("a stream failure must carry a correlation id")
			}
			if owned.EvidenceRef().ID == "" {
				t.Fatal("a failure after authentication must carry the evidence reference")
			}
			if strings.Contains(err.Error(), "password") || strings.Contains(owned.Message(), "password") {
				t.Fatalf("the projection leaked the raw diagnostic: %v", err)
			}
		})
	}
}

// TestStreamInterceptorEmitsOneRecordPerStream pins the telemetry contract:
// a stream produces exactly one record, at its end, whose Duration is the
// stream's own lifetime rather than the time it took to admit it, and whose
// trusted values are the ones admission derived.
func TestStreamInterceptorEmitsOneRecordPerStream(t *testing.T) {
	const held = 150 * time.Millisecond

	t.Run("a served stream", func(t *testing.T) {
		f := newStreamFixture(t, nil)
		err := f.interceptor(nil, f.authorized(context.Background()), &streamTestMethod,
			func(any, grpc.ServerStream) error {
				time.Sleep(held)
				return nil
			})
		if err != nil {
			t.Fatalf("interceptor: %v", err)
		}

		records := f.emitted()
		if len(records) != 1 {
			t.Fatalf("the boundary emitted %d records, want exactly 1", len(records))
		}
		record := records[0]
		if !record.Succeeded() {
			t.Fatalf("record code = %s, want a success", record.Code)
		}
		if record.Duration < held {
			t.Fatalf("record duration = %v, want at least the %v the stream was held open", record.Duration, held)
		}
		if record.Method != streamTestMethod.FullMethod {
			t.Fatalf("record method = %q, want %q", record.Method, streamTestMethod.FullMethod)
		}
		if record.Transport != transport.KindGRPC {
			t.Fatalf("record transport = %q, want %q", record.Transport, transport.KindGRPC)
		}
		if record.TenantID != transporttest.Tenant || record.SubjectID != transporttest.Subject {
			t.Fatalf("record trusted values = tenant %q subject %q, want %q / %q",
				record.TenantID, record.SubjectID, transporttest.Tenant, transporttest.Subject)
		}
		if record.EvidenceID == "" || record.RequestID == "" {
			t.Fatalf("record is not correlatable: %+v", record)
		}
	})

	t.Run("a refused stream", func(t *testing.T) {
		f := newStreamFixture(t, nil)
		_ = f.interceptor(nil, &fakeServerStream{ctx: context.Background()}, &streamTestMethod,
			func(any, grpc.ServerStream) error { return nil })

		records := f.emitted()
		if len(records) != 1 {
			t.Fatalf("a refused stream emitted %d records, want exactly 1", len(records))
		}
		if records[0].Code != envelope.CodeUnauthenticated {
			t.Fatalf("record code = %s, want UNAUTHENTICATED", records[0].Code)
		}
		if records[0].RequestID == "" {
			t.Fatal("a refused stream's record is not correlatable")
		}
		if records[0].SubjectID != "" {
			t.Fatalf("a refused stream's record names subject %q; nothing was authenticated", records[0].SubjectID)
		}
	})
}
