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
	adminpolicy "github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// RBAC-RT-009 fixture tokens. The operator token mirrors the runtime
// suite's platform operator: the credential signs the reserved operator
// role and holds no durable assignment. The both token signs an HCM
// administrator role AND the operator role: separation of duties must void
// the administrator claim while the operator duty is present. The revoked
// token mirrors rbac-rex: the credential still signs comp_admin while the
// durable assignment says worker_self.
const (
	rt9OperatorToken = "test-rt9-operator-token"
	rt9BothToken     = "test-rt9-both-token"
	rt9AdminToken    = "test-rt9-admin-token"
	rt9CompToken     = "test-rt9-comp-token"
	rt9RevokedToken  = "test-rt9-revoked-token"
)

type rt9Identity struct {
	subject  string
	roles    []string
	purposes []string
}

var rt9Identities = map[string]rt9Identity{
	rt9OperatorToken: {subject: "principal:rt9-operator", roles: []string{adminpolicy.OperatorRole}, purposes: []string{"operator_diagnostics"}},
	rt9BothToken:     {subject: "principal:rt9-both", roles: []string{"hcm_admin", adminpolicy.OperatorRole}, purposes: []string{"compensation_review"}},
	rt9AdminToken:    {subject: "principal:rt9-admin", roles: []string{"hcm_admin"}, purposes: []string{"compensation_review"}},
	rt9CompToken:     {subject: "principal:rt9-comp", roles: []string{"comp_admin"}, purposes: []string{"compensation_review"}},
	rt9RevokedToken:  {subject: "rbac-rt9-rex", roles: []string{"comp_admin"}, purposes: []string{"compensation_review"}},
}

type rt9Verifier struct{}

func (rt9Verifier) Verify(_ context.Context, cred trust.Credential) (*trust.Principal, error) {
	id, ok := rt9Identities[cred.Token]
	if !ok {
		return nil, trust.ErrInvalidCredential
	}
	now := time.Now()
	return trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               fixtureTenant,
		OrganizationScopeID:  fixtureOrganization,
		Subject:              id.subject,
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                id.roles,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "session-rt9-" + id.subject,
		Purposes:             id.purposes,
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "digest:" + cred.Token,
	})
}

func rt9CallContext(t *testing.T, token string) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return withToken(ctx, token)
}

func startRT9Server(t *testing.T, deps journey.Dependencies) journeyv1.JourneyServiceClient {
	t.Helper()
	cfg := transport.Config{Verifier: rt9Verifier{}}
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
	return journeyv1.NewJourneyServiceClient(conn)
}

// rt9Assignment is one durable assignment row for the RT-009 world:
// workerRef holds exactly roles at version 1. Principals without an
// assignment (operator, both, admin, comp) resolve through the admitted
// credential roles; rbac-rt9-rex is assigned worker_self while its
// credential still signs comp_admin.
func rt9Assignment(workerRef string, roles ...string) roleaccess.Assignment {
	return roleaccess.Assignment{Version: 1, WorkerRef: workerRef, RoleIDs: roles}
}

func rt9Deps(store *rt2Store) journey.Dependencies {
	return journey.Dependencies{Engine: newFakeEngine(), RoleAccess: store}
}

// TestTodo_RBAC_RT_009 is the PRIMARY matrix entry: administrator and
// operator authority are separated at the HCM administration gate. The
// platform operator duty never confers HCM administration, even beside an
// administrator claim on the same credential; the durable assignment still
// governs over the credential; and an unassigned administrator keeps the
// rollout fallback.
func TestTodo_RBAC_RT_009(t *testing.T) {
	newClient := func(t *testing.T) journeyv1.JourneyServiceClient {
		t.Helper()
		snapshot := rt2Snapshot()
		snapshot.Assignments = append(snapshot.Assignments,
			rt9Assignment("rbac-rt9-rex", "worker_self"))
		return startRT9Server(t, rt9Deps(&rt2Store{snapshot: snapshot}))
	}

	t.Run("operator-only is refused role administration", func(t *testing.T) {
		client := newClient(t)
		ctx := rt9CallContext(t, rt9OperatorToken)
		if _, err := client.GetRoleAccess(ctx, &journeyv1.GetRoleAccessRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("operator GetRoleAccess code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
		if _, err := client.SaveWorkerRoleAssignment(ctx, &journeyv1.SaveWorkerRoleAssignmentRequest{Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "rbac-rt9-rex", RoleIds: []string{"worker_self"}}}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("operator SaveWorkerRoleAssignment code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("operator duty voids an administrator claim on the same credential", func(t *testing.T) {
		client := newClient(t)
		ctx := rt9CallContext(t, rt9BothToken)
		if _, err := client.GetRoleAccess(ctx, &journeyv1.GetRoleAccessRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("both-roles GetRoleAccess code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
		if _, err := client.SaveWorkerRoleAssignment(ctx, &journeyv1.SaveWorkerRoleAssignmentRequest{Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "rbac-rt9-rex", RoleIds: []string{"worker_self"}}}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("both-roles SaveWorkerRoleAssignment code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("unassigned administrator keeps role administration", func(t *testing.T) {
		client := newClient(t)
		ctx := rt9CallContext(t, rt9AdminToken)
		if _, err := client.GetRoleAccess(ctx, &journeyv1.GetRoleAccessRequest{}); err != nil {
			t.Fatalf("admin GetRoleAccess: %v", err)
		}
		saved, err := client.SaveWorkerRoleAssignment(ctx, &journeyv1.SaveWorkerRoleAssignmentRequest{Assignment: &journeyv1.WorkerRoleAssignment{Version: 1, WorkerRef: "rbac-rt9-rex", RoleIds: []string{"worker_self"}}})
		if err != nil {
			t.Fatalf("admin SaveWorkerRoleAssignment: %v", err)
		}
		if saved.GetAssignment().GetVersion() == 0 {
			t.Fatal("admin SaveWorkerRoleAssignment returned no version")
		}
	})

	t.Run("revoked administrator is refused", func(t *testing.T) {
		client := newClient(t)
		if _, err := client.GetRoleAccess(rt9CallContext(t, rt9RevokedToken), &journeyv1.GetRoleAccessRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("revoked GetRoleAccess code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})
}

// TestTodo_RBAC_RT_009_Security proves token-only operator and
// administrator claims are refused on every role-administration write,
// and that separation of duties is not bypassed by holding both duties at
// once.
func TestTodo_RBAC_RT_009_Security(t *testing.T) {
	newClient := func(t *testing.T) journeyv1.JourneyServiceClient {
		t.Helper()
		snapshot := rt2Snapshot()
		snapshot.Assignments = append(snapshot.Assignments,
			rt9Assignment("rbac-rt9-rex", "worker_self"))
		return startRT9Server(t, rt9Deps(&rt2Store{snapshot: snapshot}))
	}

	t.Run("token-only operator is refused every administration write", func(t *testing.T) {
		client := newClient(t)
		ctx := rt9CallContext(t, rt9OperatorToken)
		if _, err := client.SaveAccessRole(ctx, &journeyv1.SaveAccessRoleRequest{Role: &journeyv1.AccessRole{RoleId: "rt9-probe", Name: "RT9 probe", Active: true}}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("operator SaveAccessRole code = %v, want PermissionDenied", status.Code(err))
		}
		if _, err := client.SaveRoleOrganizationVisibility(ctx, &journeyv1.SaveRoleOrganizationVisibilityRequest{Policy: &journeyv1.RoleOrganizationVisibilityPolicy{RoleId: "rt9-probe", Mode: "OWN_UNIT"}}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("operator SaveRoleOrganizationVisibility code = %v, want PermissionDenied", status.Code(err))
		}
		if _, err := client.SaveRolePagePermission(ctx, &journeyv1.SaveRolePagePermissionRequest{Permission: &journeyv1.RolePagePermission{RoleId: "rt9-probe", PageId: "help"}}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("operator SaveRolePagePermission code = %v, want PermissionDenied", status.Code(err))
		}
		if _, err := client.SaveRoleFeaturePermission(ctx, &journeyv1.SaveRoleFeaturePermissionRequest{Permission: &journeyv1.RoleFeaturePermission{RoleId: "rt9-probe", PageId: "help", FeatureId: "content"}}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("operator SaveRoleFeaturePermission code = %v, want PermissionDenied", status.Code(err))
		}
	})

	t.Run("both duties at once is refused every administration write", func(t *testing.T) {
		client := newClient(t)
		ctx := rt9CallContext(t, rt9BothToken)
		if _, err := client.SaveAccessRole(ctx, &journeyv1.SaveAccessRoleRequest{Role: &journeyv1.AccessRole{RoleId: "rt9-probe", Name: "RT9 probe", Active: true}}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("both-roles SaveAccessRole code = %v, want PermissionDenied", status.Code(err))
		}
		if _, err := client.SaveRolePagePermission(ctx, &journeyv1.SaveRolePagePermissionRequest{Permission: &journeyv1.RolePagePermission{RoleId: "rt9-probe", PageId: "help"}}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("both-roles SaveRolePagePermission code = %v, want PermissionDenied", status.Code(err))
		}
	})

	t.Run("revoked administrator claim is refused writes", func(t *testing.T) {
		client := newClient(t)
		ctx := rt9CallContext(t, rt9RevokedToken)
		if _, err := client.SaveWorkerRoleAssignment(ctx, &journeyv1.SaveWorkerRoleAssignmentRequest{Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "rbac-rt9-rex", RoleIds: []string{"worker_self"}}}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("revoked SaveWorkerRoleAssignment code = %v, want PermissionDenied", status.Code(err))
		}
	})
}

// TestTodo_RBAC_RT_009_Integration proves the durable assignment governs
// the separated gate end to end: the same mixed credential is refused
// while no durable assignment exists, and allowed once a durable
// administrator-only assignment replaces the mixed admitted claims.
func TestTodo_RBAC_RT_009_Integration(t *testing.T) {
	snapshot := rt2Snapshot()
	store := &rt2Store{snapshot: snapshot}
	client := startRT9Server(t, rt9Deps(store))

	if _, err := client.GetRoleAccess(rt9CallContext(t, rt9BothToken), &journeyv1.GetRoleAccessRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("both-roles GetRoleAccess before assignment code = %v, want PermissionDenied", status.Code(err))
	}
	adminCtx := func(t *testing.T) context.Context { return rt9CallContext(t, rt9AdminToken) }
	if _, err := client.SaveWorkerRoleAssignment(adminCtx(t), &journeyv1.SaveWorkerRoleAssignmentRequest{Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "principal:rt9-both", RoleIds: []string{"hcm_admin"}}}); err != nil {
		t.Fatalf("admin SaveWorkerRoleAssignment: %v", err)
	}
	// The durable hcm_admin-only assignment wholly replaces the admitted
	// hcm_admin-plus-operator claims: the operator duty is gone, so the
	// administrator duty is no longer voided.
	if _, err := client.GetRoleAccess(rt9CallContext(t, rt9BothToken), &journeyv1.GetRoleAccessRequest{}); err != nil {
		t.Fatalf("both-roles GetRoleAccess after durable assignment: %v", err)
	}
}
