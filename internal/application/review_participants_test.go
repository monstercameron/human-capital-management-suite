package application

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/performance"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type reviewParticipantsRoleStore struct{ snapshot roleaccess.Snapshot }

func (s reviewParticipantsRoleStore) Bootstrap(context.Context, values.TenantId, string) error {
	return nil
}
func (s reviewParticipantsRoleStore) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return s.snapshot, nil
}
func (reviewParticipantsRoleStore) SaveRole(context.Context, values.TenantId, string, roleaccess.Role) (roleaccess.Role, error) {
	return roleaccess.Role{}, nil
}
func (reviewParticipantsRoleStore) SaveAssignment(context.Context, values.TenantId, string, roleaccess.Assignment) (roleaccess.Assignment, error) {
	return roleaccess.Assignment{}, nil
}
func (reviewParticipantsRoleStore) SaveVisibility(context.Context, values.TenantId, string, string, roleaccess.VisibilityPolicy) (roleaccess.VisibilityPolicy, error) {
	return roleaccess.VisibilityPolicy{}, nil
}
func (reviewParticipantsRoleStore) SavePagePermission(context.Context, values.TenantId, string, roleaccess.PagePermission) (roleaccess.PagePermission, error) {
	return roleaccess.PagePermission{}, nil
}
func (reviewParticipantsRoleStore) SaveFeaturePermission(context.Context, values.TenantId, string, roleaccess.FeaturePermission) (roleaccess.FeaturePermission, error) {
	return roleaccess.FeaturePermission{}, nil
}

func TestTodo_REV_075_02(t *testing.T) {
	ctx := context.Background()
	tenant := values.TenantId("tenant-a")
	store := reviewParticipantsGraphFixture(t, tenant)
	service := ReviewParticipantsReadService{
		Graphs: store, Roles: reviewParticipantsRoleStore{snapshot: reviewParticipantsRoleSnapshot()},
		ResolveGraphTenant: identityReviewGraphTenant,
		ResolveWorker: func(_ context.Context, principal *trust.Principal) (string, error) {
			switch principal.Subject() {
			case "subject-manager":
				return "worker-manager", nil
			case "subject-participant":
				return "worker-participant", nil
			default:
				return "worker-outsider", nil
			}
		},
	}
	manager := reviewParticipantsPrincipal(t, tenant, "subject-manager")
	got, err := service.Read(ctx, manager)
	if err != nil || len(got.Cycles) != 1 || len(got.Cycles[0].Assignments) != 1 {
		t.Fatalf("authorized manager projection = %+v, %v", got, err)
	}
	assignment := got.Cycles[0].Assignments[0]
	if assignment.ParticipantID != "worker-participant" || assignment.ReviewerID != "worker-manager" {
		t.Fatalf("assignment = %+v", assignment)
	}
	outsider, err := service.Read(ctx, reviewParticipantsPrincipal(t, tenant, "subject-outsider"))
	if err != nil || len(outsider.Cycles) != 0 {
		t.Fatalf("outsider projection = %+v, %v", outsider, err)
	}
	noCycle := ReviewParticipantsReadService{Graphs: performance.NewMemoryStore(), Roles: service.Roles, ResolveWorker: service.ResolveWorker, ResolveGraphTenant: service.ResolveGraphTenant}
	empty, err := noCycle.Read(ctx, reviewParticipantsPrincipal(t, tenant, "subject-outsider"))
	if err != nil || len(empty.Cycles) != 0 || len(empty.Cycles) != len(outsider.Cycles) {
		t.Fatalf("no-cycle disclosure differs: outsider=%+v no-cycle=%+v err=%v", outsider, empty, err)
	}
}

func TestTodo_REV_075_02_Security(t *testing.T) {
	ctx := context.Background()
	tenant := values.TenantId("tenant-a")
	store := reviewParticipantsGraphFixture(t, tenant)
	service := ReviewParticipantsReadService{
		Graphs: store, Roles: reviewParticipantsRoleStore{snapshot: reviewParticipantsRoleSnapshot()},
		ResolveGraphTenant: identityReviewGraphTenant,
		ResolveWorker: func(_ context.Context, principal *trust.Principal) (string, error) {
			return map[string]string{"subject-manager": "worker-manager", "subject-participant": "worker-participant", "subject-outsider": "worker-outsider"}[principal.Subject()], nil
		},
	}
	inside, err := service.Read(ctx, reviewParticipantsPrincipal(t, tenant, "subject-participant"))
	if err != nil || len(inside.Cycles) != 1 || len(inside.Cycles[0].Assignments) != 1 {
		t.Fatalf("in-graph participant cannot see their edge: %+v, %v", inside, err)
	}
	outside, err := service.Read(ctx, reviewParticipantsPrincipal(t, tenant, "subject-outsider"))
	if err != nil || len(outside.Cycles) != 0 {
		t.Fatalf("outside graph disclosed data: %+v, %v", outside, err)
	}
	emptyService := ReviewParticipantsReadService{Graphs: performance.NewMemoryStore(), Roles: service.Roles, ResolveWorker: service.ResolveWorker, ResolveGraphTenant: service.ResolveGraphTenant}
	noCycle, err := emptyService.Read(ctx, reviewParticipantsPrincipal(t, tenant, "subject-outsider"))
	if err != nil || len(noCycle.Cycles) != 0 {
		t.Fatalf("no-cycle response = %+v, %v", noCycle, err)
	}
	if !reflect.DeepEqual(outside, noCycle) {
		t.Fatalf("outsider and no-cycle disclosures differ: outside=%+v no-cycle=%+v", outside, noCycle)
	}
	outsidePage := productui.NewView(productui.PageReviewParticipants, "tenant-a", "subject-outsider", "scope-a")
	outsidePage.ReviewParticipants = &outside
	noCyclePage := productui.NewView(productui.PageReviewParticipants, "tenant-a", "subject-outsider", "scope-a")
	noCyclePage.ReviewParticipants = &noCycle
	outsideMarkup, err := productui.Render(outsidePage)
	if err != nil {
		t.Fatal(err)
	}
	noCycleMarkup, err := productui.Render(noCyclePage)
	if err != nil {
		t.Fatal(err)
	}
	if outsideMarkup != noCycleMarkup {
		t.Fatal("outsider and no-cycle render disclosures differ")
	}
	denied := service
	denied.Roles = reviewParticipantsRoleStore{snapshot: roleaccess.Snapshot{}}
	if _, err := denied.Read(ctx, reviewParticipantsPrincipal(t, tenant, "subject-outsider")); !errors.Is(err, ErrReviewParticipantsDenied) {
		t.Fatalf("unauthorized page read = %v", err)
	}
}

func identityReviewGraphTenant(tenant values.TenantId) (performance.TenantID, error) {
	return tenant, nil
}

func TestTodo_REV_075_02_TenantMapping(t *testing.T) {
	ctx := context.Background()
	principalTenant := values.TenantId("harborcare-demo")
	storageTenant := values.TenantId(pgstore.TenantID(string(principalTenant)).String())
	graphs := &reviewParticipantTenantSpy{Store: reviewParticipantsGraphFixture(t, storageTenant)}
	service := ReviewParticipantsReadService{
		Graphs: graphs, Roles: reviewParticipantsRoleStore{snapshot: reviewParticipantsRoleSnapshot()},
		ResolveGraphTenant: func(tenant values.TenantId) (performance.TenantID, error) {
			if tenant != principalTenant {
				t.Fatalf("tenant resolver received %q, want authenticated key %q", tenant, principalTenant)
			}
			return storageTenant, nil
		},
		ResolveWorker: func(context.Context, *trust.Principal) (string, error) { return "worker-participant", nil },
	}
	projection, err := service.Read(ctx, reviewParticipantsPrincipal(t, principalTenant, "subject-participant"))
	if err != nil || graphs.seenTenant != storageTenant.String() || len(projection.Cycles) != 1 {
		t.Fatalf("mapped tenant=%q projection=%+v err=%v", graphs.seenTenant, projection, err)
	}
}

type reviewParticipantTenantSpy struct {
	performance.Store
	seenTenant string
}

func (s *reviewParticipantTenantSpy) ListOpenParticipantReviewerGraphsForMember(ctx context.Context, tenant performance.TenantID, memberID string) ([]performance.FrozenParticipantReviewerGraph, error) {
	s.seenTenant = tenant.String()
	return s.Store.ListOpenParticipantReviewerGraphsForMember(ctx, tenant, memberID)
}

func reviewParticipantsGraphFixture(t *testing.T, tenant values.TenantId) *performance.MemoryStore {
	t.Helper()
	cycle, err := performance.NewPerformanceCycle("cycle-a",
		performance.PopulationBindingRef{DefinitionID: "population", RevisionVersion: "1", Digest: "sha256:population"},
		performance.CalendarBindingRef{Ref: "calendar", Version: "1", Digest: "sha256:calendar"},
		performance.RatingScaleVersionRef{ID: "scale", Version: "1", Digest: "sha256:scale"})
	if err != nil {
		t.Fatal(err)
	}
	store := performance.NewMemoryStore()
	if err := store.SaveCycle(context.Background(), tenant, performance.CycleRevision{CycleID: cycle.CycleID, Revision: cycle.Revision, State: cycle.State, CanonicalDigest: cycle.CanonicalDigest}); err != nil {
		t.Fatal(err)
	}
	opened, err := cycle.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCycle(context.Background(), tenant, performance.CycleRevision{CycleID: opened.CycleID, Revision: opened.Revision, State: opened.State, SupersedesRevision: cycle.Revision, CanonicalDigest: opened.CanonicalDigest}); err != nil {
		t.Fatal(err)
	}
	graph, err := performance.FreezeParticipantReviewerGraph(opened,
		[]performance.ParticipantRef{{ID: "worker-participant"}},
		[]performance.ReviewerAssignment{{ParticipantID: "worker-participant", ReviewerID: "worker-manager", Relationship: performance.ReviewerRelationshipManager}},
		performance.DefaultReviewerGraphRules(), values.NewInstant(time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveParticipantReviewerGraph(context.Background(), tenant, graph); err != nil {
		t.Fatal(err)
	}
	return store
}

func reviewParticipantsRoleSnapshot() roleaccess.Snapshot {
	return roleaccess.Snapshot{
		Roles:           []roleaccess.Role{{ID: "manager", Active: true}},
		Assignments:     []roleaccess.Assignment{{WorkerRef: "subject-manager", RoleIDs: []string{"manager"}}, {WorkerRef: "subject-participant", RoleIDs: []string{"manager"}}, {WorkerRef: "subject-outsider", RoleIDs: []string{"manager"}}},
		PagePermissions: []roleaccess.PagePermission{{RoleID: "manager", PageID: string(productui.PageReviewParticipants), View: true}},
	}
}

func reviewParticipantsPrincipal(t *testing.T, tenant values.TenantId, subject string) *trust.Principal {
	t.Helper()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: tenant, Subject: subject, SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "scope-a", Roles: []string{"manager"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-" + subject, IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "digest-" + subject})
	if err != nil {
		t.Fatal(err)
	}
	return principal
}
