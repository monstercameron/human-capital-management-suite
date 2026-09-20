package journey_test

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// RBAC-RT-002 fixture tokens. The revoked token mirrors the runtime suite's
// rbac-rex: the credential still signs comp_admin while the durable
// assignment says worker_self. The admin token mirrors principal:rbac-admin:
// an administrator with no durable assignment at all.
const (
	rt2RevokedToken = "test-rt2-revoked-token"
	rt2AdminToken   = "test-rt2-admin-token"
	rt2WorkerToken  = "test-rt2-worker-token"
	rt2FayToken     = "test-rt2-fay-token"
)

type rt2Identity struct {
	subject  string
	roles    []string
	purposes []string
}

var rt2Identities = map[string]rt2Identity{
	rt2RevokedToken: {subject: "rbac-rex", roles: []string{"comp_admin"}, purposes: []string{"compensation_review"}},
	rt2AdminToken:   {subject: "principal:rt2-admin", roles: []string{"hcm_admin"}, purposes: []string{"compensation_review"}},
	rt2WorkerToken:  {subject: "rbac-eli", roles: []string{"worker_self"}, purposes: []string{"self_service_view"}},
	rt2FayToken:     {subject: "rbac-fay", roles: []string{"worker_self"}, purposes: []string{"compensation_review"}},
}

type rt2Verifier struct{}

func (rt2Verifier) Verify(_ context.Context, cred trust.Credential) (*trust.Principal, error) {
	id, ok := rt2Identities[cred.Token]
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
		SessionRef:           "session-rt2-" + id.subject,
		Purposes:             id.purposes,
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "digest:" + cred.Token,
	})
}

func rt2CallContext(t *testing.T, token string) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return withToken(ctx, token)
}

func startRT2Server(t *testing.T, deps journey.Dependencies) journeyv1.JourneyServiceClient {
	t.Helper()
	cfg := transport.Config{Verifier: rt2Verifier{}}
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

// rt2Store is a counting, mutable roleaccess.Store: every Load is counted so
// the integration test can prove the short cache serves repeated checks,
// and SaveAssignment mutates the snapshot so it can prove invalidation.
type rt2Store struct {
	mu       sync.Mutex
	snapshot roleaccess.Snapshot
	loads    int
}

func (s *rt2Store) Bootstrap(context.Context, values.TenantId, string) error { return nil }

func (s *rt2Store) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	return s.snapshot, nil
}

func (s *rt2Store) loadCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loads
}

func (s *rt2Store) SaveRole(_ context.Context, _ values.TenantId, _ string, value roleaccess.Role) (roleaccess.Role, error) {
	value.Version++
	return value, nil
}

func (s *rt2Store) SaveAssignment(_ context.Context, _ values.TenantId, _ string, value roleaccess.Assignment) (roleaccess.Assignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value = roleaccess.NormalizeAssignment(value)
	replaced := false
	for i, current := range s.snapshot.Assignments {
		if equalFoldTrim(current.WorkerRef, value.WorkerRef) {
			value.Version = current.Version + 1
			s.snapshot.Assignments[i] = value
			replaced = true
		}
	}
	if !replaced {
		value.Version++
		s.snapshot.Assignments = append(s.snapshot.Assignments, value)
	}
	return value, nil
}

func (s *rt2Store) SaveVisibility(_ context.Context, _ values.TenantId, _, _ string, value roleaccess.VisibilityPolicy) (roleaccess.VisibilityPolicy, error) {
	value.Version++
	return value, nil
}

func (s *rt2Store) SavePagePermission(_ context.Context, _ values.TenantId, _ string, value roleaccess.PagePermission) (roleaccess.PagePermission, error) {
	value.Version++
	return value, nil
}

func (s *rt2Store) SaveFeaturePermission(_ context.Context, _ values.TenantId, _ string, value roleaccess.FeaturePermission) (roleaccess.FeaturePermission, error) {
	value.Version++
	return value, nil
}

func equalFoldTrim(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// rt2Snapshot is the durable world: rbac-rex is assigned worker_self while
// its credential still signs comp_admin; principals without an assignment
// (the admin) resolve through the admitted credential roles.
func rt2Snapshot() roleaccess.Snapshot {
	return roleaccess.Snapshot{
		Roles: roleaccess.DefaultRoles(),
		Assignments: []roleaccess.Assignment{
			{Version: 1, WorkerRef: "rbac-rex", RoleIDs: []string{"worker_self"}},
		},
		PagePermissions: []roleaccess.PagePermission{
			{Version: 1, RoleID: "comp_admin", PageID: "roles", View: true, Create: true, Update: true, Delete: true},
			{Version: 1, RoleID: "hcm_admin", PageID: "roles", View: true, Create: true, Update: true, Delete: true},
			{Version: 1, RoleID: "comp_admin", PageID: "people", View: true},
			{Version: 1, RoleID: "hcm_admin", PageID: "people", View: true},
			{Version: 1, RoleID: "worker_self", PageID: "people", View: true},
			{Version: 1, RoleID: "hcm_admin", PageID: "appearance", View: true, Create: true, Update: true},
		},
		FeaturePermissions: []roleaccess.FeaturePermission{
			{Version: 1, RoleID: "comp_admin", PageID: "roles", FeatureID: "actions", View: true, Create: true, Update: true},
			{Version: 1, RoleID: "hcm_admin", PageID: "roles", FeatureID: "actions", View: true, Create: true, Update: true},
			{Version: 1, RoleID: "hcm_admin", PageID: "appearance", FeatureID: "actions", View: true, Update: true},
			{Version: 1, RoleID: "worker_self", PageID: "people", FeatureID: "directory", View: true},
		},
	}
}

func rt2Engine() *fakeEngine {
	engine := newFakeEngine()
	engine.workers = []workspace.WorkerSummary{
		dirWorker("rbac-rex", "rex-id", "Rex Revoked", "Rex", "W-REX", "sales", "121000.00", "0.1700", "rbac-hana"),
		dirWorker("rbac-eli", "eli-id", "Eli Individual", "Eli", "W-ELI", "sales", "105000.00", "0.0900", "rbac-dana"),
	}
	return engine
}

// TestTodo_RBAC_RT_002 is the PRIMARY matrix entry: the principal's
// effective roles come from durable assignments for every check. A revoked
// administrator (credential comp_admin, durable worker_self) is refused role
// administration reads and writes and worker-id administration, while an
// administrator with no durable assignment keeps the already-authorized
// behavior.
func TestTodo_RBAC_RT_002(t *testing.T) {
	newClient := func(t *testing.T) journeyv1.JourneyServiceClient {
		t.Helper()
		return startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt2Snapshot()}})
	}

	t.Run("revoked administrator is refused role administration reads", func(t *testing.T) {
		client := newClient(t)
		_, err := client.GetRoleAccess(rt2CallContext(t, rt2RevokedToken), &journeyv1.GetRoleAccessRequest{})
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("revoked GetRoleAccess code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("revoked administrator is refused role administration writes", func(t *testing.T) {
		client := newClient(t)
		ctx := rt2CallContext(t, rt2RevokedToken)
		if _, err := client.SaveWorkerRoleAssignment(ctx, &journeyv1.SaveWorkerRoleAssignmentRequest{Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "rbac-fay", RoleIds: []string{"manager"}}}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("revoked SaveWorkerRoleAssignment code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
		if _, err := client.SaveAccessRole(ctx, &journeyv1.SaveAccessRoleRequest{Role: &journeyv1.AccessRole{RoleId: "recruiter", Name: "Recruiter", Active: true}}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("revoked SaveAccessRole code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("revoked administrator is refused worker identifier administration", func(t *testing.T) {
		client := newClient(t)
		_, err := client.GetWorkerIDPolicy(rt2CallContext(t, rt2RevokedToken), &journeyv1.GetWorkerIDPolicyRequest{})
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("revoked GetWorkerIDPolicy code = %v, want PermissionDenied (err=%v)", status.Code(err), err)
		}
	})

	t.Run("unassigned administrator keeps role administration", func(t *testing.T) {
		client := newClient(t)
		ctx := rt2CallContext(t, rt2AdminToken)
		loaded, err := client.GetRoleAccess(ctx, &journeyv1.GetRoleAccessRequest{})
		if err != nil {
			t.Fatalf("admin GetRoleAccess: %v", err)
		}
		if len(loaded.GetAssignments()) != 1 {
			t.Fatalf("admin GetRoleAccess assignments = %d, want 1", len(loaded.GetAssignments()))
		}
		saved, err := client.SaveWorkerRoleAssignment(ctx, &journeyv1.SaveWorkerRoleAssignmentRequest{Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "rbac-rex", RoleIds: []string{"worker_self"}}})
		if err != nil {
			t.Fatalf("admin SaveWorkerRoleAssignment: %v", err)
		}
		if saved.GetAssignment().GetVersion() == 0 {
			t.Fatal("admin SaveWorkerRoleAssignment returned no version")
		}
	})
}

// TestTodo_RBAC_RT_002_Security proves revoked-role refusal with no leakage:
// the revoked administrator's listing carries no pay or legal names for
// other rows, and a durable grant (not the credential) is what authorizes.
func TestTodo_RBAC_RT_002_Security(t *testing.T) {
	t.Run("revoked listing discloses no pay or names for other rows", func(t *testing.T) {
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: rt2Snapshot()}})
		resp, err := client.ListWorkers(rt2CallContext(t, rt2RevokedToken), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("revoked ListWorkers: %v", err)
		}
		other := dirFind(t, resp.GetWorkers(), "rbac-eli")
		if other.GetBasePay() != "" || other.GetBonusTarget() != "" {
			t.Errorf("revoked listing discloses pay (base=%q bonus=%q)", other.GetBasePay(), other.GetBonusTarget())
		}
		if other.GetLegalName() != "" {
			t.Errorf("revoked listing discloses legal name: %q", other.GetLegalName())
		}
		self := dirFind(t, resp.GetWorkers(), "rbac-rex")
		if self.GetWorkerRef() != "rbac-rex" {
			t.Errorf("revoked listing lost the caller's own row: %+v", self)
		}
	})

	t.Run("durable grant authorizes where the credential alone does not", func(t *testing.T) {
		snapshot := rt2Snapshot()
		snapshot.Assignments = append(snapshot.Assignments, roleaccess.Assignment{Version: 2, WorkerRef: "rbac-fay", RoleIDs: []string{"comp_admin"}})
		client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: &rt2Store{snapshot: snapshot}})
		// rbac-fay's credential signs only worker_self; the durable
		// comp_admin assignment is what must authorize this read.
		if _, err := client.GetRoleAccess(rt2CallContext(t, rt2FayToken), &journeyv1.GetRoleAccessRequest{}); err != nil {
			t.Fatalf("durably granted GetRoleAccess: %v", err)
		}
	})
}

// TestTodo_RBAC_RT_002_Integration proves the short cache end to end: a
// repeated check reuses the resolved set without reloading, and an
// assignment change is visible to the very next check.
func TestTodo_RBAC_RT_002_Integration(t *testing.T) {
	store := &rt2Store{snapshot: rt2Snapshot()}
	client := startRT2Server(t, journey.Dependencies{Engine: rt2Engine(), RoleAccess: store})
	adminCtx := func(t *testing.T) context.Context { return rt2CallContext(t, rt2AdminToken) }

	if _, err := client.GetRoleAccess(adminCtx(t), &journeyv1.GetRoleAccessRequest{}); err != nil {
		t.Fatalf("admin GetRoleAccess: %v", err)
	}
	afterFirst := store.loadCount()
	if _, err := client.GetRoleAccess(adminCtx(t), &journeyv1.GetRoleAccessRequest{}); err != nil {
		t.Fatalf("admin GetRoleAccess: %v", err)
	}
	if got, want := store.loadCount()-afterFirst, 1; got != want {
		t.Fatalf("second check loaded the store %d times, want %d (the cached role set must not reload)", got, want)
	}

	if _, err := client.GetRoleAccess(rt2CallContext(t, rt2RevokedToken), &journeyv1.GetRoleAccessRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("revoked GetRoleAccess before reassignment code = %v, want PermissionDenied", status.Code(err))
	}
	if _, err := client.SaveWorkerRoleAssignment(adminCtx(t), &journeyv1.SaveWorkerRoleAssignmentRequest{Assignment: &journeyv1.WorkerRoleAssignment{WorkerRef: "rbac-rex", RoleIds: []string{"comp_admin"}}}); err != nil {
		t.Fatalf("admin SaveWorkerRoleAssignment: %v", err)
	}
	if _, err := client.GetRoleAccess(rt2CallContext(t, rt2RevokedToken), &journeyv1.GetRoleAccessRequest{}); err != nil {
		t.Fatalf("regranted GetRoleAccess after assignment change: %v (the cache was not invalidated)", err)
	}
}
