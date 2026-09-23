package journey_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/machine"
)

// TestTodo_INTAPI_002_Integration is the served loop: a machine token
// admits one served call, its replay is refused at the transport boundary
// with Unauthenticated, and a token minted after the client is revoked is
// refused without reaching any handler.
func TestTodo_INTAPI_002_Integration(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	key, err := machine.GenerateServerKey("srv-i2", now.Add(time.Hour), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := machine.NewIssuer([]machine.ServerKey{key}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := machine.NewVerifier([]machine.ServerKey{key}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	source := trust.NewMemoryRevocationSource(func() time.Time { return now })
	served := &trust.ServedVerifier{
		Machine:     verifier,
		Request:     machine.VerifyRequest{Issuer: "intapi002-issuer", Audience: []string{"hcm-next-api"}},
		Revocations: source,
	}

	snapshot := roleaccess.Snapshot{
		Roles: roleaccess.DefaultRoles(),
		Assignments: []roleaccess.Assignment{
			{Version: 1, WorkerRef: "intapi002-admin", RoleIDs: []string{"hcm_admin"}},
		},
		PagePermissions: []roleaccess.PagePermission{
			{Version: 1, RoleID: "hcm_admin", PageID: "roles", View: true, Create: true, Update: true, Delete: true},
		},
		FeaturePermissions: []roleaccess.FeaturePermission{
			{Version: 1, RoleID: "hcm_admin", PageID: "roles", FeatureID: "actions", View: true, Create: true, Update: true},
		},
	}
	cfg := transport.Config{Verifier: served}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)))
	journey.Register(srv, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: snapshot}})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(lis) }()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		srv.Stop()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	})
	client := journeyv1.NewJourneyServiceClient(conn)

	var seq int
	mint := func() string {
		seq++
		token, err := issuer.Issue(machine.IssueRequest{
			Issuer: "intapi002-issuer", Audience: []string{"hcm-next-api"},
			Subject: "intapi002-admin", Client: "intapi002-admin", Tenant: fixtureTenant,
			Session: "mcs-i2", Assurance: "substantial",
			TokenID: fmt.Sprintf("i2-token-%d", seq), Lifetime: 10 * time.Minute,
		})
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	call := func(token string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := client.GetRoleAccess(withToken(ctx, token), &journeyv1.GetRoleAccessRequest{})
		return err
	}

	first := mint()
	if err := call(first); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if err := call(first); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("replay code = %v, want Unauthenticated (err=%v)", status.Code(err), err)
	}
	source.RevokeClient("intapi002-admin")
	if err := call(mint()); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("post-revocation code = %v, want Unauthenticated (err=%v)", status.Code(err), err)
	}
}
