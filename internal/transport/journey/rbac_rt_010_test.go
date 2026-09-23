package journey_test

import (
	"context"
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
)

// RBAC-RT-010 authentication suite. The cell verifies with identity-only
// semantics: a credential that carries caller-selected authority is refused
// at the transport boundary before any handler runs, so a token naming
// another tenant can no longer be admitted for ListWorkers (case A-03) or
// GetRoleAccess (case A-05). Authority reaches a handler only through
// server-side resolution (durable assignments, JIT grants), never through
// the token.

func startRT10Server(t *testing.T, deps journey.Dependencies) (journeyv1.JourneyServiceClient, *trust.HMACVerifier) {
	t.Helper()
	verifier, err := trust.NewHMACVerifier(trust.HMACVerifierConfig{
		Key:      []byte("01234567890123456789012345678901"),
		Issuer:   "rt10-issuer",
		Audience: "rt10-audience",
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := transport.Config{Verifier: verifier.IdentityVerifier()}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)))
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
	return journeyv1.NewJourneyServiceClient(conn), verifier
}

func rt10Claims(tenant, subject string) trust.Claims {
	now := time.Now()
	return trust.Claims{
		Issuer: "rt10-issuer", Audience: "rt10-audience",
		Subject: subject, SubjectKind: "human", Tenant: tenant,
		AuthenticationMethod: "bearer_token", Assurance: "substantial",
		SessionRef:   "session-rt10-" + subject,
		IssuedAtUnix: now.Add(-time.Minute).Unix(), ExpiresAtUnix: now.Add(time.Hour).Unix(),
	}
}

// rt10AuthorityToken mints the pre-RBAC-RT-010 shape: a token that names a
// tenant and also carries caller-selected roles and purposes.
func rt10AuthorityToken(t *testing.T, v *trust.HMACVerifier, tenant, subject string) string {
	t.Helper()
	claims := rt10Claims(tenant, subject)
	claims.Roles = []string{"hcm_admin"}
	claims.Purposes = []string{"compensation_review"}
	token, err := v.Issue(claims)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func rt10Snapshot() roleaccess.Snapshot {
	return roleaccess.Snapshot{
		Roles: roleaccess.DefaultRoles(),
		Assignments: []roleaccess.Assignment{
			{Version: 1, WorkerRef: "rt10-admin", RoleIDs: []string{"hcm_admin"}},
		},
		PagePermissions: []roleaccess.PagePermission{
			{Version: 1, RoleID: "hcm_admin", PageID: "roles", View: true, Create: true, Update: true, Delete: true},
		},
		FeaturePermissions: []roleaccess.FeaturePermission{
			{Version: 1, RoleID: "hcm_admin", PageID: "roles", FeatureID: "actions", View: true, Create: true, Update: true},
		},
	}
}

// TestTodo_RBAC_RT_010_Integration is the RBAC-RT-010 cell suite: the
// authentication cases A-03 (ListWorkers) and A-05 (GetRoleAccess) run
// against the real transport boundary (HMAC verifier, interceptor,
// handlers). A token naming another tenant is refused with Unauthenticated
// rather than admitted, the refusal ground is the caller-selected authority
// it carries rather than the tenant name, and an identity-only token joined
// to a server-side durable assignment still authorizes.
func TestTodo_RBAC_RT_010_Integration(t *testing.T) {
	newClient := func(t *testing.T) (journeyv1.JourneyServiceClient, *trust.HMACVerifier) {
		t.Helper()
		return startRT10Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt10Snapshot()}})
	}

	t.Run("A-03 other-tenant authority token is refused ListWorkers", func(t *testing.T) {
		client, verifier := newClient(t)
		token := rt10AuthorityToken(t, verifier, "tenant-b", "rt10-admin")
		_, err := client.ListWorkers(rt2CallContext(t, token), &journeyv1.ListWorkersRequest{})
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("other-tenant ListWorkers code = %v, want Unauthenticated (err=%v)", status.Code(err), err)
		}
	})

	t.Run("A-05 other-tenant authority token is refused GetRoleAccess", func(t *testing.T) {
		client, verifier := newClient(t)
		token := rt10AuthorityToken(t, verifier, "tenant-b", "rt10-admin")
		_, err := client.GetRoleAccess(rt2CallContext(t, token), &journeyv1.GetRoleAccessRequest{})
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("other-tenant GetRoleAccess code = %v, want Unauthenticated (err=%v)", status.Code(err), err)
		}
	})

	t.Run("same-tenant authority token is refused the same way", func(t *testing.T) {
		client, verifier := newClient(t)
		token := rt10AuthorityToken(t, verifier, fixtureTenant, "rt10-admin")
		if _, err := client.ListWorkers(rt2CallContext(t, token), &journeyv1.ListWorkersRequest{}); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("same-tenant ListWorkers code = %v, want Unauthenticated (err=%v)", status.Code(err), err)
		}
		if _, err := client.GetRoleAccess(rt2CallContext(t, token), &journeyv1.GetRoleAccessRequest{}); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("same-tenant GetRoleAccess code = %v, want Unauthenticated (err=%v)", status.Code(err), err)
		}
	})

	t.Run("identity-only token authorizes through the durable assignment", func(t *testing.T) {
		client, verifier := newClient(t)
		token, err := verifier.IssueIdentity(rt10Claims(fixtureTenant, "rt10-admin"))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		resp, err := client.GetRoleAccess(withToken(ctx, token), &journeyv1.GetRoleAccessRequest{})
		if err != nil {
			t.Fatalf("identity GetRoleAccess: %v", err)
		}
		found := false
		for _, a := range resp.GetAssignments() {
			if a.GetWorkerRef() == "rt10-admin" {
				found = true
			}
		}
		if !found {
			t.Fatalf("identity GetRoleAccess assignments = %+v, want the durable rt10-admin assignment", resp.GetAssignments())
		}
	})
}
