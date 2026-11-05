package journey_test

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// startBenchServer is startTestServer's testing.TB counterpart: the same
// admitted gRPC harness, usable from benchmarks.
func startBenchServer(tb testing.TB, deps journey.Dependencies) *grpc.ClientConn {
	tb.Helper()
	cfg := transport.Config{Verifier: fakeVerifier{}}
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)),
		grpc.ChainStreamInterceptor(grpcserver.StreamInterceptor(cfg)),
	)
	journey.Register(srv, deps)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		srv.Stop()
		tb.Fatalf("dial: %v", err)
	}
	tb.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	})
	return conn
}

// countingRoleAccess wraps a roleaccess.Store, counting snapshot loads and
// optionally sleeping per load to stand in for a database round trip.
type countingRoleAccess struct {
	roleaccess.Store
	loads   atomic.Int64
	latency time.Duration
}

func (s *countingRoleAccess) Load(ctx context.Context, tenant values.TenantId, scope string) (roleaccess.Snapshot, error) {
	s.loads.Add(1)
	if s.latency > 0 {
		select {
		case <-ctx.Done():
			return roleaccess.Snapshot{}, ctx.Err()
		case <-time.After(s.latency):
		}
	}
	return s.Store.Load(ctx, tenant, scope)
}

// TestTodo_JOURNEY_ListJourneysLoadsRolesOnce pins the per-RPC snapshot
// budget: one ListJourneys answers its page gate and its diagnostics
// disclosure from a single role-snapshot load, cold or warm. Every extra
// load is another database round trip on the journey list's critical path.
func TestTodo_JOURNEY_ListJourneysLoadsRolesOnce(t *testing.T) {
	roles := &countingRoleAccess{Store: defaultFixtureRoleAccess()}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: newFakeEngine(), RoleAccess: roles}))
	ctx := testContext(t)

	if _, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{}); err != nil {
		t.Fatalf("cold ListJourneys: %v", err)
	}
	if got := roles.loads.Load(); got != 1 {
		t.Fatalf("cold ListJourneys loaded the role snapshot %d times, want 1", got)
	}
	if _, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{}); err != nil {
		t.Fatalf("warm ListJourneys: %v", err)
	}
	if got := roles.loads.Load(); got != 2 {
		t.Fatalf("two ListJourneys loaded the role snapshot %d times, want 2", got)
	}
}

// BenchmarkListJourneysRPC measures the journey list over gRPC with a
// configurable snapshot-store latency, so a change in per-RPC store reads
// shows up as a change in RPC latency.
func BenchmarkListJourneysRPC(b *testing.B) {
	for _, latency := range []time.Duration{0, 2 * time.Millisecond} {
		roles := &countingRoleAccess{Store: defaultFixtureRoleAccess(), latency: latency}
		client := dialJourneyClient(startBenchServer(b, journey.Dependencies{Engine: newFakeEngine(), RoleAccess: roles}))
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		ctx = withToken(ctx, fixtureManagerToken)
		if _, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{}); err != nil {
			b.Fatalf("warmup: %v", err)
		}
		roles.loads.Store(0)
		b.Run(latency.String(), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := client.ListJourneys(ctx, &journeyv1.ListJourneysRequest{}); err != nil {
					b.Fatalf("ListJourneys: %v", err)
				}
			}
			b.ReportMetric(float64(roles.loads.Load())/float64(b.N), "loads/op")
		})
	}
}
