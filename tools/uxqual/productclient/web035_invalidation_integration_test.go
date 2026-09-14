package productclient_test

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/invalidation"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type web035JourneyService struct {
	journeyv1.UnimplementedJourneyServiceServer
	workerReads atomic.Int32
}

func (s *web035JourneyService) ListWorkers(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
	s.workerReads.Add(1)
	return &journeyv1.ListWorkersResponse{Workers: []*journeyv1.Worker{{
		WorkerRef:           "worker-from-authoritative-rpc",
		WorkerId:            "W-035",
		PreferredName:       "Fresh Authorized Worker",
		JobTitle:            "Care Manager",
		Source:              "JourneyService.ListWorkers",
		ManagerRelationship: &journeyv1.ManagerRelationshipProjection{Disposition: journeyv1.ManagerRelationshipProjection_DISPOSITION_ROOT},
	}}}, nil
}

// ListJourneys is answered trivially: PROMOUX-001 made PagePeople also
// require journeys (the directory's per-worker promotion availability needs
// to know about an in-flight journey), so this test's People-page load now
// exercises both RPCs; this fixture is not testing journey data at all.
func (s *web035JourneyService) ListJourneys(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
	return &journeyv1.ListJourneysResponse{}, nil
}

// TestTodo_WEB_035_Integration proves that accepting a hint reaches
// productclient.LoadWithBaseline through the generated Journey gRPC client.
// The only applied view is the fresh RPC projection; no display field exists
// in, or is derived from, the invalidation message.
func TestTodo_WEB_035_Integration(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	var credentialAccepted atomic.Bool
	rpcServer := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		incoming, ok := metadata.FromIncomingContext(ctx)
		values := incoming.Get(journeyclient.AuthorizationHeader)
		if !ok || len(values) != 1 || values[0] != "Bearer web035-authorized" {
			return nil, status.Error(codes.Unauthenticated, "credential required")
		}
		credentialAccepted.Store(true)
		return handler(ctx, request)
	}))
	serviceServer := &web035JourneyService{}
	journeyv1.RegisterJourneyServiceServer(rpcServer, serviceServer)
	go func() { _ = rpcServer.Serve(listener) }()
	t.Cleanup(func() {
		rpcServer.Stop()
		_ = listener.Close()
	})

	conn, err := grpc.NewClient(
		"passthrough:///web035-bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := journeyclient.NewGRPCService(conn, "wrong-web035-credential").ListWorkers(context.Background(), &journeyv1.ListWorkersRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("wrong bearer code = %v, want UNAUTHENTICATED", status.Code(err))
	}
	if serviceServer.workerReads.Load() != 0 {
		t.Fatal("unauthenticated generated RPC reached Journey service")
	}
	qualified := journeyclient.NewGRPCService(conn, "web035-authorized")
	liveService := productclient.Service{
		ListWorkers: func(ctx context.Context, request *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return qualified.ListWorkers(ctx, request)
		},
		ListJourneys: func(ctx context.Context, request *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return qualified.ListJourneys(ctx, request)
		},
	}
	session := productclient.Session{Tenant: "acme", Principal: "authorized-reader", Scope: "people-directory"}
	state := productclient.State{Page: productui.PagePeople, Request: productui.PageRequest{Page: productui.PagePeople}}
	baseline := productclient.LoadingView(session, state)
	baseline.People = []productui.Person{{ID: "worker-from-old-view", Name: "Stale Worker"}}

	subject := values.EntityRef{Tenant: "acme", Kind: "worker", Id: "00000000-0000-4000-8000-000000000035"}
	hint := productquery.InvalidationMessage{
		ContractVersion: 1,
		Tenant:          "acme",
		Projection:      "worker_summary",
		SourceSequence:  11,
		Watermark:       11,
		Items:           []productquery.InvalidationItem{{Subject: subject, Revision: 11}},
	}
	raw, err := hint.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Fresh Authorized Worker", "Care Manager", "JourneyService.ListWorkers", "Stale Worker"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("invalidation hint carried display field %q", forbidden)
		}
	}

	var applied productui.View
	client, err := invalidation.New(invalidation.Scope{
		Tenant:         "acme",
		Projection:     "worker_summary",
		SourceSequence: 10,
		Watermark:      10,
		Subjects:       []values.EntityRef{subject},
	}, func(ctx context.Context, refresh invalidation.Refresh) error {
		if refresh.SourceSequence != 11 || len(refresh.Subjects) != 1 || refresh.Subjects[0] != subject {
			return errors.New("unexpected invalidation metadata")
		}
		fresh, loadErr := productclient.LoadWithBaseline(ctx, liveService, session, state, baseline)
		if loadErr != nil {
			return loadErr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		applied = fresh
		return nil
	}, invalidation.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Run(context.Background(), &web035FrameStream{frame: raw}); err != nil {
		t.Fatal(err)
	}
	if serviceServer.workerReads.Load() != 1 {
		t.Fatalf("generated Journey ListWorkers calls = %d, want 1", serviceServer.workerReads.Load())
	}
	if !credentialAccepted.Load() {
		t.Fatal("qualified Journey adapter did not deliver its bearer metadata")
	}
	if snapshot := client.Snapshot(); snapshot.Refetched != 1 || snapshot.RefetchErrors != 0 || snapshot.LastSourceSequence != 11 {
		t.Fatalf("invalidation snapshot = %+v, want one committed authoritative refetch", snapshot)
	}
	if len(applied.People) != 1 || applied.People[0].ID != "worker-from-authoritative-rpc" || applied.People[0].Name != "Fresh Authorized Worker" || !strings.HasPrefix(applied.People[0].Role, "Care Manager") || applied.People[0].Source != "JourneyService.ListWorkers" {
		t.Fatalf("applied view people = %+v, want only fresh Journey RPC projection", applied.People)
	}
	if applied.People[0].ID == subject.String() || strings.Contains(applied.People[0].Name, subject.Id) {
		t.Fatalf("hint content supplied display state: %+v", applied.People[0])
	}
}

type web035FrameStream struct {
	frame []byte
	used  bool
}

func (s *web035FrameStream) Recv() ([]byte, error) {
	if s.used {
		return nil, io.EOF
	}
	s.used = true
	return append([]byte(nil), s.frame...), nil
}

func (*web035FrameStream) Close() error { return nil }
