package productclient_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type rev06301RefusalServer struct {
	journeyv1.UnimplementedJourneyServiceServer
}

func (rev06301RefusalServer) ListJourneys(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
	return nil, status.Error(codes.Unavailable, "journey projection temporarily unavailable")
}

func (rev06301RefusalServer) ListWorkers(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
	return nil, status.Error(codes.Unauthenticated, "session expired")
}

// TestTodo_REV_063_01_Integration crosses the generated gRPC client and a
// real in-process gRPC transport. The route is read-only: authentication is
// refused at the RPC boundary before any store access, so a real data store
// would not strengthen this transport-classification proof.
func TestTodo_REV_063_01_Integration(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	rpcServer := grpc.NewServer()
	journeyv1.RegisterJourneyServiceServer(rpcServer, rev06301RefusalServer{})
	serveDone := make(chan error, 1)
	go func() { serveDone <- rpcServer.Serve(listener) }()
	t.Cleanup(func() {
		rpcServer.Stop()
		_ = listener.Close()
		<-serveDone
	})
	conn, err := grpc.NewClient(
		"passthrough:///rev06301-in-process",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	qualified := journeyclient.NewGRPCService(conn, "expired-session")
	service := productclient.Service{
		ListJourneys: qualified.ListJourneys,
		ListWorkers:  qualified.ListWorkers,
	}
	session := productclient.Session{Tenant: "tenant-a", Principal: "worker-a"}
	state, err := productclient.ParseState(productui.Path(productui.PagePeople), "locale=de-DE")
	if err != nil {
		t.Fatal(err)
	}
	baseline := productclient.LoadingView(session, state)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	view, loadErr := productclient.LoadWithBaseline(ctx, service, session, state, baseline)
	if status.Code(loadErr) != codes.Unavailable || !journeyclient.ErrorHasCode(loadErr, codes.Unauthenticated) {
		t.Fatalf("gRPC aggregate = %s, want earlier UNAVAILABLE plus a later UNAUTHENTICATED refusal: %v", status.Code(loadErr), loadErr)
	}
	if view.SignedOut == nil || view.SignedOut.SignInHref != "/workspace/login" || view.SignedOut.Detail != "" || len(view.SignedOut.Revoked) != 0 {
		t.Fatalf("gRPC refusal did not project the safe sign-in recovery: %+v", view.SignedOut)
	}
	markup, err := productui.Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !containsREV06301(markup, `id="signed-out"`, `href="/workspace/login"`) {
		t.Fatal("gRPC refusal did not render the sign-in recovery link")
	}
}

func containsREV06301(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}
