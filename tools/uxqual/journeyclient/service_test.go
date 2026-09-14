package journeyclient

import (
	"context"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// recordingConn is a grpc.ClientConnInterface that answers every call with
// success and remembers the context it was called with, which is where the
// credential this client must attach ends up.
type recordingConn struct {
	invoked []string
	streams []string
	ctx     context.Context
}

func (c *recordingConn) Invoke(ctx context.Context, method string, _, _ any, _ ...grpc.CallOption) error {
	c.invoked = append(c.invoked, method)
	c.ctx = ctx
	return nil
}

func (c *recordingConn) NewStream(ctx context.Context, _ *grpc.StreamDesc, method string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
	c.streams = append(c.streams, method)
	c.ctx = ctx
	return &nullClientStream{ctx: ctx}, nil
}

// nullClientStream is the minimum grpc.ClientStream the generated
// server-streaming client needs to be constructed.
type nullClientStream struct{ ctx context.Context }

func (s *nullClientStream) Header() (metadata.MD, error) { return nil, nil }
func (s *nullClientStream) Trailer() metadata.MD         { return nil }
func (s *nullClientStream) CloseSend() error             { return nil }
func (s *nullClientStream) Context() context.Context     { return s.ctx }
func (s *nullClientStream) SendMsg(any) error            { return nil }
func (s *nullClientStream) RecvMsg(any) error            { return nil }

// TestEveryCallCarriesTheBearer is the security-relevant test in this
// package. The tunnel forwards no Authorization header from the upgrade
// request, so an RPC that reaches the cell without this metadata is admitted
// anonymously and refused: every method must attach it, not most of them.
func TestEveryCallCarriesTheBearer(t *testing.T) {
	conn := &recordingConn{}
	svc := NewGRPCService(conn, "tok_abc123")
	ctx := context.Background()

	calls := map[string]func() error{
		"ListJourneys": func() error {
			_, err := svc.ListJourneys(ctx, &journeyv1.ListJourneysRequest{})
			return err
		},
		"ProposePromotion": func() error {
			_, err := svc.ProposePromotion(ctx, &journeyv1.ProposePromotionRequest{})
			return err
		},
		"InspectJourney": func() error {
			_, err := svc.InspectJourney(ctx, &journeyv1.InspectJourneyRequest{})
			return err
		},
		"ExecuteJourney": func() error {
			_, err := svc.ExecuteJourney(ctx, &journeyv1.ExecuteJourneyRequest{})
			return err
		},
		"DecideJourney": func() error {
			_, err := svc.DecideJourney(ctx, &journeyv1.DecideJourneyRequest{})
			return err
		},
		"WatchJourney": func() error {
			_, err := svc.WatchJourney(ctx, &journeyv1.WatchJourneyRequest{})
			return err
		},
		"ListWorkers": func() error {
			_, err := svc.ListWorkers(ctx, &journeyv1.ListWorkersRequest{})
			return err
		},
		"CreateWorker": func() error {
			_, err := svc.CreateWorker(ctx, &journeyv1.CreateWorkerRequest{})
			return err
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			conn.ctx = nil
			if err := call(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			md, ok := metadata.FromOutgoingContext(conn.ctx)
			if !ok {
				t.Fatalf("%s carried no outgoing metadata at all", name)
			}
			got := md.Get(AuthorizationHeader)
			if len(got) != 1 || got[0] != "Bearer tok_abc123" {
				t.Fatalf("%s authorization = %v, want [Bearer tok_abc123]", name, got)
			}
		})
	}

	if len(conn.invoked) != 7 {
		t.Errorf("unary calls = %v, want the seven unary methods", conn.invoked)
	}
	if len(conn.streams) != 1 {
		t.Errorf("streams opened = %v, want one (WatchJourney)", conn.streams)
	}
}

// TestTheRPCsAreTheCanonicalOnes pins the full method names, which is what a
// server-side route or interceptor is keyed on.
func TestTheRPCsAreTheCanonicalOnes(t *testing.T) {
	conn := &recordingConn{}
	svc := NewGRPCService(conn, "tok")
	ctx := context.Background()

	_, _ = svc.ListJourneys(ctx, &journeyv1.ListJourneysRequest{})
	_, _ = svc.ProposePromotion(ctx, &journeyv1.ProposePromotionRequest{})
	_, _ = svc.InspectJourney(ctx, &journeyv1.InspectJourneyRequest{})
	_, _ = svc.ExecuteJourney(ctx, &journeyv1.ExecuteJourneyRequest{})
	_, _ = svc.DecideJourney(ctx, &journeyv1.DecideJourneyRequest{})
	_, _ = svc.ListWorkers(ctx, &journeyv1.ListWorkersRequest{})
	_, _ = svc.CreateWorker(ctx, &journeyv1.CreateWorkerRequest{})
	_, _ = svc.WatchJourney(ctx, &journeyv1.WatchJourneyRequest{})

	want := []string{
		journeyv1.JourneyService_ListJourneys_FullMethodName,
		journeyv1.JourneyService_ProposePromotion_FullMethodName,
		journeyv1.JourneyService_InspectJourney_FullMethodName,
		journeyv1.JourneyService_ExecuteJourney_FullMethodName,
		journeyv1.JourneyService_DecideJourney_FullMethodName,
		journeyv1.JourneyService_ListWorkers_FullMethodName,
		journeyv1.JourneyService_CreateWorker_FullMethodName,
	}
	for i, w := range want {
		if conn.invoked[i] != w {
			t.Errorf("call %d = %q, want %q", i, conn.invoked[i], w)
		}
	}
	if conn.streams[0] != journeyv1.JourneyService_WatchJourney_FullMethodName {
		t.Errorf("stream = %q, want %q", conn.streams[0], journeyv1.JourneyService_WatchJourney_FullMethodName)
	}
}

// TestNoBearerFailsClosed keeps an unconfigured client from sending an
// anonymous call: the browser boundary refuses before the connection sees it.
func TestNoBearerFailsClosed(t *testing.T) {
	conn := &recordingConn{}
	svc := NewGRPCService(conn, "")
	if _, err := svc.ListJourneys(context.Background(), &journeyv1.ListJourneysRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("ListJourneys code = %v, want UNAUTHENTICATED", status.Code(err))
	}
}
