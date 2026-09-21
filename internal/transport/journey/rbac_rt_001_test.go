package journey_test

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RBAC-RT-001 directory fixture tokens. Each maps to a principal carrying a
// policy-decision-point role and purpose, mirroring the runtime conformance
// suite's users (dana/gus managers, eli worker_self, comp/hcm admins,
// auditor) without standing up the composed cell.
const (
	dirDanaToken      = "test-dir-dana-token"
	dirGusToken       = "test-dir-gus-token"
	dirEliToken       = "test-dir-eli-token"
	dirCompAdminToken = "test-dir-comp-admin-token"
	dirHCMAdminToken  = "test-dir-hcm-admin-token"
	dirAuditorToken   = "test-dir-auditor-token"
	dirNoRolesToken   = "test-dir-no-roles-token"
	dirHRToken        = "test-dir-hr-token"
)

type dirIdentity struct {
	subject  string
	roles    []string
	purposes []string
}

func dirIdentities() map[string]dirIdentity {
	return map[string]dirIdentity{
		dirDanaToken:      {subject: "rbac-dana", roles: []string{"manager"}, purposes: []string{"compensation_review"}},
		dirGusToken:       {subject: "rbac-gus", roles: []string{"manager"}, purposes: []string{"compensation_review"}},
		dirEliToken:       {subject: "rbac-eli", roles: []string{"worker_self"}, purposes: []string{"self_service_view"}},
		dirCompAdminToken: {subject: "principal:rbac-comp-admin", roles: []string{"comp_admin"}, purposes: []string{"compensation_review"}},
		dirHCMAdminToken:  {subject: "principal:rbac-admin", roles: []string{"hcm_admin"}, purposes: []string{"compensation_review"}},
		dirAuditorToken:   {subject: "principal:rbac-auditor", roles: []string{"auditor"}, purposes: []string{"audit_review"}},
		dirNoRolesToken:   {subject: "principal:rbac-no-roles", roles: nil, purposes: []string{"self_service_view"}},
		dirHRToken:        {subject: "principal:rbac-hr", roles: []string{"hr_partner"}, purposes: []string{"compensation_review"}},
	}
}

// dirVerifier admits the directory tokens through the real admission
// pipeline, minting principals whose roles and purposes the policy decision
// point recognizes.
type dirVerifier struct{}

func (dirVerifier) Verify(_ context.Context, cred trust.Credential) (*trust.Principal, error) {
	id, ok := dirIdentities()[cred.Token]
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
		SessionRef:           "session-dir-" + id.subject,
		Purposes:             id.purposes,
		IssuedAt:             now.Add(-time.Minute),
		ExpiresAt:            now.Add(time.Hour),
		CredentialDigest:     "digest:" + cred.Token,
	})
}

func startDirectoryServer(t *testing.T, deps journey.Dependencies) journeyv1.JourneyServiceClient {
	t.Helper()
	if deps.RoleAccess == nil {
		deps.RoleAccess = directoryFixtureRoleAccess()
	} else if access, ok := deps.RoleAccess.(*roleAccessSpy); ok && len(access.snapshot.FeaturePermissions) == 0 {
		access.snapshot.FeaturePermissions = directoryFeaturePermissions(access.snapshot.PagePermissions)
	}
	cfg := transport.Config{Verifier: dirVerifier{}}
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

func directoryFixtureRoleAccess() *roleAccessSpy {
	roleIDs := []string{"manager", "worker_self", "comp_admin", "hcm_admin", "auditor", "hr_partner", "fixture_access"}
	pages := make([]roleaccess.PagePermission, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		pages = append(pages, roleaccess.PagePermission{
			RoleID: roleID, PageID: "people", View: true, Create: true,
		})
	}
	return &roleAccessSpy{snapshot: roleaccess.Snapshot{
		Assignments:        []roleaccess.Assignment{{WorkerRef: "principal:rbac-no-roles", RoleIDs: []string{"fixture_access"}}},
		PagePermissions:    pages,
		FeaturePermissions: directoryFeaturePermissions(pages),
	}}
}

func directoryFeaturePermissions(pages []roleaccess.PagePermission) []roleaccess.FeaturePermission {
	result := make([]roleaccess.FeaturePermission, 0, len(pages))
	for _, page := range pages {
		if page.PageID != "people" {
			continue
		}
		result = append(result, roleaccess.FeaturePermission{
			RoleID: page.RoleID, PageID: page.PageID, FeatureID: "directory",
			View: page.View, Create: page.Create,
		})
	}
	return result
}

// dirWorker builds one directory row. ManagerRef names the manager's
// WorkerRef, which is what the reporting-line walk reads.
func dirWorker(ref, id, legal, preferred, number, unit, base, bonus, manager string) workspace.WorkerSummary {
	return workspace.WorkerSummary{
		WorkerRef: ref, WorkerID: id,
		LegalName: legal, PreferredName: preferred, WorkerNumber: number,
		JobCode: "MKT-DIR", JobTitle: "Marketing Director", Grade: "M4", OrgUnit: unit,
		PositionID: "POS-" + ref, Location: "Boston, MA", PayZone: "US-EAST",
		BasePay: base, Currency: "USD", BonusTarget: bonus,
		HireDate: "2021-04-05", Source: workspace.WorkerSourceCreated,
		CreatedAt: fixtureTime(), ManagerRef: manager,
	}
}

// dirEngine returns a fake engine holding a three-level reporting line plus
// a same-unit peer outside the viewing manager's chain:
//
//	rbac-sponsor (absent) <- rbac-gus (director, unit-a)
//	rbac-gus <- rbac-dana (manager, unit-a) <- rbac-eli, rbac-fay (ICs, unit-a)
//	rbac-gus <- rbac-otto (peer of dana's team, unit-a)
func dirEngine() *fakeEngine {
	engine := newFakeEngine()
	engine.workers = []workspace.WorkerSummary{
		dirWorker("rbac-gus", "gus-id", "Gus Direct", "Gus", "W-GUS", "unit-a", "176000.00", "0.1900", "rbac-sponsor"),
		dirWorker("rbac-dana", "dana-id", "Dana Manage", "Dana", "W-DANA", "unit-a", "152000.00", "0.1700", "rbac-gus"),
		dirWorker("rbac-eli", "eli-id", "Eli Individual", "Eli", "W-ELI", "unit-a", "105000.00", "0.0900", "rbac-dana"),
		dirWorker("rbac-fay", "fay-id", "Fay Individual", "Fay", "W-FAY", "unit-a", "110000.00", "0.1000", "rbac-dana"),
		dirWorker("rbac-otto", "otto-id", "Otto Peer", "Otto", "W-OTTO", "unit-a", "118500.00", "0.1150", "rbac-gus"),
	}
	return engine
}

func dirCallContext(t *testing.T, token string) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return withToken(ctx, token)
}

func dirFind(t *testing.T, workers []*journeyv1.Worker, ref string) *journeyv1.Worker {
	t.Helper()
	for _, w := range workers {
		if w.GetWorkerRef() == ref {
			return w
		}
	}
	t.Fatalf("worker %q absent from a listing of %d rows", ref, len(workers))
	return nil
}

// TestTodo_RBAC_RT_001 is the PRIMARY matrix entry: every ListWorkers row
// resolves its compensation, legal-name and manager-linkage disclosure
// through the policy decision point for the viewing subject. A manager sees
// pay, legal name and manager linkage for workers in her reporting chain
// (direct and skip-level) and for herself, while the same-unit peer outside
// her chain and her own manager stay listed with those fields omitted. The
// RED failure this was written against shows the peer's and the manager's
// raw pay on the wire.
func TestTodo_RBAC_RT_001(t *testing.T) {
	client := startDirectoryServer(t, journey.Dependencies{Engine: dirEngine()})

	t.Run("manager sees chain pay name and linkage", func(t *testing.T) {
		resp, err := client.ListWorkers(dirCallContext(t, dirDanaToken), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("ListWorkers: %v", err)
		}
		for _, ref := range []string{"rbac-eli", "rbac-fay"} {
			w := dirFind(t, resp.GetWorkers(), ref)
			if w.GetBasePay() == "" || w.GetBonusTarget() == "" {
				t.Errorf("%s: chain pay omitted (base=%q bonus=%q)", ref, w.GetBasePay(), w.GetBonusTarget())
			}
			if w.GetLegalName() == "" {
				t.Errorf("%s: chain legal name omitted", ref)
			}
			if rel := w.GetManagerRelationship(); rel == nil || rel.GetManagerWorkerRef() != "rbac-dana" {
				t.Errorf("%s: chain manager linkage = %+v, want endpoint rbac-dana", ref, rel)
			}
		}
	})

	t.Run("manager sees own pay and name", func(t *testing.T) {
		resp, err := client.ListWorkers(dirCallContext(t, dirDanaToken), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("ListWorkers: %v", err)
		}
		w := dirFind(t, resp.GetWorkers(), "rbac-dana")
		if w.GetBasePay() != "152000.00" || w.GetBonusTarget() != "0.1700" {
			t.Errorf("own pay = %q/%q, want 152000.00/0.1700", w.GetBasePay(), w.GetBonusTarget())
		}
		if w.GetLegalName() != "Dana Manage" {
			t.Errorf("own legal name = %q, want Dana Manage", w.GetLegalName())
		}
	})

	t.Run("same-unit peer outside the chain stays listed with pay name and linkage omitted", func(t *testing.T) {
		resp, err := client.ListWorkers(dirCallContext(t, dirDanaToken), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("ListWorkers: %v", err)
		}
		w := dirFind(t, resp.GetWorkers(), "rbac-otto")
		if w.GetBasePay() != "" || w.GetBonusTarget() != "" {
			t.Errorf("peer pay disclosed (base=%q bonus=%q)", w.GetBasePay(), w.GetBonusTarget())
		}
		if w.GetLegalName() != "" {
			t.Errorf("peer legal name disclosed: %q", w.GetLegalName())
		}
		if rel := w.GetManagerRelationship(); rel != nil && (rel.GetManagerWorkerRef() != "" || rel.GetDisposition() == journeyv1.ManagerRelationshipProjection_DISPOSITION_VISIBLE) {
			t.Errorf("peer manager linkage disclosed: %+v", rel)
		}
		if w.GetManagerRef() != "" {
			t.Errorf("peer raw manager ref disclosed: %q", w.GetManagerRef())
		}
	})

	t.Run("upward manager stays listed with pay and name omitted", func(t *testing.T) {
		resp, err := client.ListWorkers(dirCallContext(t, dirDanaToken), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("ListWorkers: %v", err)
		}
		w := dirFind(t, resp.GetWorkers(), "rbac-gus")
		if w.GetBasePay() != "" || w.GetBonusTarget() != "" {
			t.Errorf("upward pay disclosed (base=%q bonus=%q)", w.GetBasePay(), w.GetBonusTarget())
		}
		if w.GetLegalName() != "" {
			t.Errorf("upward legal name disclosed: %q", w.GetLegalName())
		}
	})

	t.Run("skip-level chain sees pay", func(t *testing.T) {
		resp, err := client.ListWorkers(dirCallContext(t, dirGusToken), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("ListWorkers: %v", err)
		}
		if w := dirFind(t, resp.GetWorkers(), "rbac-eli"); w.GetBasePay() != "105000.00" {
			t.Errorf("skip-level pay = %q, want 105000.00", w.GetBasePay())
		}
		if w := dirFind(t, resp.GetWorkers(), "rbac-otto"); w.GetBasePay() != "118500.00" {
			t.Errorf("direct-report pay = %q, want 118500.00", w.GetBasePay())
		}
	})

	t.Run("administrative roles see pay", func(t *testing.T) {
		for _, tc := range []struct{ name, token string }{
			{"comp_admin", dirCompAdminToken},
			{"hcm_admin", dirHCMAdminToken},
		} {
			resp, err := client.ListWorkers(dirCallContext(t, tc.token), &journeyv1.ListWorkersRequest{})
			if err != nil {
				t.Fatalf("%s ListWorkers: %v", tc.name, err)
			}
			if w := dirFind(t, resp.GetWorkers(), "rbac-otto"); w.GetBasePay() != "118500.00" || w.GetLegalName() != "Otto Peer" {
				t.Errorf("%s: pay/name = %q/%q, want 118500.00/Otto Peer", tc.name, w.GetBasePay(), w.GetLegalName())
			}
		}
	})

	t.Run("auditor never receives raw pay", func(t *testing.T) {
		resp, err := client.ListWorkers(dirCallContext(t, dirAuditorToken), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("ListWorkers: %v", err)
		}
		w := dirFind(t, resp.GetWorkers(), "rbac-otto")
		if w.GetBasePay() == "118500.00" || w.GetBonusTarget() == "0.1150" {
			t.Errorf("auditor received raw pay (base=%q bonus=%q)", w.GetBasePay(), w.GetBonusTarget())
		}
	})
}

// TestTodo_RBAC_RT_001_Security is the SECURITY matrix entry: an unauthorized
// principal receives no pay, no legal name and no manager linkage, and no
// existence signal for anything it may not see. A caller refused at the page
// gate never reaches the engine, so the refusal carries no worker data; a
// caller that reaches serialization (open gate, no grant) still receives
// rows with the governed fields omitted; and a withheld row is byte-identical
// in its governed fields to a row that carries no baseline at all, so the
// two cannot be told apart.
func TestTodo_RBAC_RT_001_Security(t *testing.T) {
	t.Run("refused caller never reaches the engine", func(t *testing.T) {
		engine := dirEngine()
		access := &roleAccessSpy{snapshot: roleaccess.Snapshot{
			PagePermissions: []roleaccess.PagePermission{
				{Version: 1, RoleID: "comp_admin", PageID: "people", View: true},
			},
		}}
		client := startDirectoryServer(t, journey.Dependencies{Engine: engine, RoleAccess: access})

		_, err := client.ListWorkers(dirCallContext(t, dirNoRolesToken), &journeyv1.ListWorkersRequest{})
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("roleless ListWorkers code = %v, want PermissionDenied", status.Code(err))
		}
		if engine.listWorkersCalls != 0 {
			t.Fatalf("refused caller reached the engine %d times", engine.listWorkersCalls)
		}
		if _, err := client.CreateWorker(dirCallContext(t, dirNoRolesToken), &journeyv1.CreateWorkerRequest{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("roleless CreateWorker code = %v, want PermissionDenied", status.Code(err))
		}

		if _, err := client.ListWorkers(dirCallContext(t, dirCompAdminToken), &journeyv1.ListWorkersRequest{}); err != nil {
			t.Fatalf("granted ListWorkers: %v", err)
		}
	})

	t.Run("grantless caller receives rows with governed fields omitted", func(t *testing.T) {
		client := startDirectoryServer(t, journey.Dependencies{Engine: dirEngine()})
		resp, err := client.ListWorkers(dirCallContext(t, dirNoRolesToken), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("ListWorkers: %v", err)
		}
		if len(resp.GetWorkers()) == 0 {
			t.Fatal("open gate listed no rows to judge")
		}
		for _, w := range resp.GetWorkers() {
			if w.GetBasePay() != "" || w.GetBonusTarget() != "" {
				t.Errorf("%s: grantless caller received pay (base=%q bonus=%q)", w.GetWorkerRef(), w.GetBasePay(), w.GetBonusTarget())
			}
			if w.GetLegalName() != "" {
				t.Errorf("%s: grantless caller received legal name %q", w.GetWorkerRef(), w.GetLegalName())
			}
		}
	})

	t.Run("withheld fields are indistinguishable from an absent baseline", func(t *testing.T) {
		engine := dirEngine()
		engine.workers = append(engine.workers, dirWorker("rbac-ghost", "ghost-id", "Ghost Writer", "Ghost", "W-GHOST", "unit-a", "99000.00", "0.0500", "rbac-sponsor"))
		client := startDirectoryServer(t, journey.Dependencies{Engine: engine})
		resp, err := client.ListWorkers(dirCallContext(t, dirDanaToken), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("ListWorkers: %v", err)
		}
		// Otto carries a baseline the viewer may not see; the corpus-style
		// check below would read the same emptiness on a row that never had
		// one, so emptiness proves nothing about the stored record.
		for _, ref := range []string{"rbac-otto", "rbac-ghost"} {
			w := dirFind(t, resp.GetWorkers(), ref)
			if w.GetBasePay() != "" || w.GetBonusTarget() != "" || w.GetLegalName() != "" {
				t.Errorf("%s: governed fields present (base=%q bonus=%q name=%q)", ref, w.GetBasePay(), w.GetBonusTarget(), w.GetLegalName())
			}
			if w.GetManagerRelationship() != nil || w.GetManagerRef() != "" {
				t.Errorf("%s: manager linkage present (%+v/%q)", ref, w.GetManagerRelationship(), w.GetManagerRef())
			}
		}
	})
}

// TestTodo_RBAC_RT_001_Integration is the INTEGRATION matrix entry: the
// transport and the policy decision point composed over gRPC, with no mock
// standing in for either. It pins the whole viewer-by-subject pay matrix,
// the hr_partner boundary owned by RBAC-RT-007, and CreateWorker masking for
// creators inside and outside the new row's chain.
func TestTodo_RBAC_RT_001_Integration(t *testing.T) {
	t.Run("viewer by subject pay matrix", func(t *testing.T) {
		client := startDirectoryServer(t, journey.Dependencies{Engine: dirEngine()})
		want := map[string]map[string]bool{
			dirDanaToken:      {"rbac-gus": false, "rbac-dana": true, "rbac-eli": true, "rbac-fay": true, "rbac-otto": false},
			dirGusToken:       {"rbac-gus": true, "rbac-dana": true, "rbac-eli": true, "rbac-fay": true, "rbac-otto": true},
			dirEliToken:       {"rbac-gus": false, "rbac-dana": false, "rbac-eli": true, "rbac-fay": false, "rbac-otto": false},
			dirCompAdminToken: {"rbac-gus": true, "rbac-dana": true, "rbac-eli": true, "rbac-fay": true, "rbac-otto": true},
			dirHCMAdminToken:  {"rbac-gus": true, "rbac-dana": true, "rbac-eli": true, "rbac-fay": true, "rbac-otto": true},
			dirNoRolesToken:   {"rbac-gus": false, "rbac-dana": false, "rbac-eli": false, "rbac-fay": false, "rbac-otto": false},
		}
		for token, subjects := range want {
			resp, err := client.ListWorkers(dirCallContext(t, token), &journeyv1.ListWorkersRequest{})
			if err != nil {
				t.Fatalf("%s ListWorkers: %v", token, err)
			}
			byRef := map[string]*journeyv1.Worker{}
			for _, w := range resp.GetWorkers() {
				byRef[w.GetWorkerRef()] = w
			}
			for ref, visible := range subjects {
				w, ok := byRef[ref]
				if !ok {
					t.Fatalf("%s: row %s absent, want listed", token, ref)
				}
				if got := w.GetBasePay() != ""; got != visible {
					t.Errorf("%s viewing %s: pay visible = %v, want %v (base=%q)", token, ref, got, visible, w.GetBasePay())
				}
			}
		}
	})

	t.Run("auditor receives stand-ins never raw values", func(t *testing.T) {
		client := startDirectoryServer(t, journey.Dependencies{Engine: dirEngine()})
		resp, err := client.ListWorkers(dirCallContext(t, dirAuditorToken), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("ListWorkers: %v", err)
		}
		for _, w := range resp.GetWorkers() {
			if w.GetBasePay() != "REDACTED" || w.GetBonusTarget() != "REDACTED" {
				t.Errorf("%s: auditor pay = %q/%q, want REDACTED/REDACTED", w.GetWorkerRef(), w.GetBasePay(), w.GetBonusTarget())
			}
		}
	})

	t.Run("hr_partner keeps its role-wide grant until RBAC-RT-007", func(t *testing.T) {
		client := startDirectoryServer(t, journey.Dependencies{Engine: dirEngine()})
		resp, err := client.ListWorkers(dirCallContext(t, dirHRToken), &journeyv1.ListWorkersRequest{})
		if err != nil {
			t.Fatalf("ListWorkers: %v", err)
		}
		if w := dirFind(t, resp.GetWorkers(), "rbac-eli"); w.GetBasePay() != "105000.00" {
			t.Errorf("hr_partner pay = %q, want 105000.00 (legacy role-wide grant)", w.GetBasePay())
		}
	})

	t.Run("create worker masks by the same rulings", func(t *testing.T) {
		engine := newFakeEngine()
		engine.workers = []workspace.WorkerSummary{
			dirWorker("rbac-new", "new-id", "New Hire", "New", "W-NEW", "unit-a", "95000.00", "0.0800", "rbac-dana"),
		}
		client := startDirectoryServer(t, journey.Dependencies{Engine: engine})
		form := &journeyv1.CreateWorkerRequest{
			LegalName: "New Hire", PreferredName: "New",
			JobCode: "MKT-CNT3", Grade: "P3", OrgUnit: "unit-a",
			BasePay: "95000.00", Currency: "USD", BonusTarget: "0.0800",
			HireDate: "2026-09-01", ManagerRef: "rbac-dana",
		}

		created, err := client.CreateWorker(dirCallContext(t, dirDanaToken), form)
		if err != nil {
			t.Fatalf("manager CreateWorker: %v", err)
		}
		if got := created.GetWorker().GetBasePay(); got != "95000.00" {
			t.Errorf("chain creator pay = %q, want 95000.00", got)
		}

		masked, err := client.CreateWorker(dirCallContext(t, dirNoRolesToken), form)
		if err != nil {
			t.Fatalf("grantless CreateWorker: %v", err)
		}
		if got := masked.GetWorker(); got.GetBasePay() != "" || got.GetLegalName() != "" {
			t.Errorf("grantless creator received pay/name (%q/%q), want omitted", got.GetBasePay(), got.GetLegalName())
		}

		full, err := client.CreateWorker(dirCallContext(t, dirCompAdminToken), form)
		if err != nil {
			t.Fatalf("admin CreateWorker: %v", err)
		}
		if got := full.GetWorker(); got.GetBasePay() != "95000.00" || got.GetLegalName() != "New Hire" {
			t.Errorf("admin creator received pay/name (%q/%q), want whole", got.GetBasePay(), got.GetLegalName())
		}
	})
}
