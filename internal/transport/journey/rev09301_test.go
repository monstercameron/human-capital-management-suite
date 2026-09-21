package journey_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

func rev09301Engine() *fakeEngine {
	engine := newFakeEngine()
	engine.workers = []workspace.WorkerSummary{
		{WorkerRef: "w-ana", WorkerID: "id-ana", OrgUnit: "Engineering"},
		{WorkerRef: "w-ben", OrgUnit: "Finance"},
		{WorkerRef: "w-cy", OrgUnit: "Sales"},
		{WorkerRef: "w-dee", OrgUnit: "People"},
	}
	return engine
}

func rev09301Access() *roleAccessSpy {
	return &roleAccessSpy{snapshot: roleaccess.Snapshot{
		Roles: roleaccess.DefaultRoles(),
		Assignments: []roleaccess.Assignment{
			{WorkerRef: "id-ana", RoleIDs: []string{"manager", "hr_partner"}},
			{WorkerRef: "w-ben", RoleIDs: []string{"manager"}},
			{WorkerRef: "w-dee", RoleIDs: []string{"comp_admin"}},
		},
		Policies: []roleaccess.VisibilityPolicy{
			{RoleID: "manager", Mode: roleaccess.VisibilityAllowlist, OrganizationUnits: []string{"Engineering", "Finance"}},
			{RoleID: "comp_admin", Mode: roleaccess.VisibilityOwnUnit},
		},
		PagePermissions: []roleaccess.PagePermission{
			{RoleID: "comp_admin", PageID: "roles", View: true, Update: true},
			{RoleID: "comp_admin", PageID: "organization-visibility", View: true},
		},
		FeaturePermissions: []roleaccess.FeaturePermission{
			{RoleID: "comp_admin", PageID: "roles", FeatureID: "feature_access", View: true, Update: true},
			{RoleID: "comp_admin", PageID: "organization-visibility", FeatureID: "content", View: true},
		},
	}}
}

func rev09301AdminContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return withToken(ctx, fixtureAppearanceAdminToken)
}

// TestTodo_REV_093_01_Transport drives PreviewRoleAccess through the real
// admission pipeline: the handler reads the durable role store and the
// governed workforce, and answers current versus proposed scope for a role.
func TestTodo_REV_093_01_Transport(t *testing.T) {
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: rev09301Engine(), RoleAccess: rev09301Access()}))
	response, err := client.PreviewRoleAccess(rev09301AdminContext(t), &journeyv1.PreviewRoleAccessRequest{Proposed: &journeyv1.RoleOrganizationVisibilityPolicy{RoleId: "manager", Mode: "ALLOWLIST", OrganizationUnits: []string{"Engineering", "Sales"}}})
	if err != nil {
		t.Fatalf("PreviewRoleAccess: %v", err)
	}
	if response.GetRoleId() != "manager" || response.GetAdministratorOverride() || response.GetHolderCount() != 2 {
		t.Fatalf("preview identity = %+v", response)
	}
	if !reflect.DeepEqual(response.GetInheritedRoles(), []string{"hr_partner"}) {
		t.Fatalf("inherited = %v", response.GetInheritedRoles())
	}
	if !reflect.DeepEqual(response.GetCurrent().GetOrganizationUnits(), []string{"Engineering", "Finance"}) || !reflect.DeepEqual(response.GetProposed().GetOrganizationUnits(), []string{"Engineering", "Sales"}) {
		t.Fatalf("scopes = %v -> %v", response.GetCurrent().GetOrganizationUnits(), response.GetProposed().GetOrganizationUnits())
	}
	if !reflect.DeepEqual(response.GetAddedUnits(), []string{"Sales"}) || !reflect.DeepEqual(response.GetRemovedUnits(), []string{"Finance"}) {
		t.Fatalf("added %v removed %v", response.GetAddedUnits(), response.GetRemovedUnits())
	}

	// Missing proposal and unknown role are refused as invalid input.
	if _, err := client.PreviewRoleAccess(rev09301AdminContext(t), &journeyv1.PreviewRoleAccessRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing proposal code = %v", status.Code(err))
	}
	if _, err := client.PreviewRoleAccess(rev09301AdminContext(t), &journeyv1.PreviewRoleAccessRequest{Proposed: &journeyv1.RoleOrganizationVisibilityPolicy{RoleId: "ghost", Mode: "ALL"}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unknown role code = %v", status.Code(err))
	}
	// Without an engine the preview cannot know the workforce and says so.
	bare := dialJourneyClient(startTestServer(t, journey.Dependencies{RoleAccess: rev09301Access()}))
	if _, err := bare.PreviewRoleAccess(rev09301AdminContext(t), &journeyv1.PreviewRoleAccessRequest{Proposed: &journeyv1.RoleOrganizationVisibilityPolicy{RoleId: "manager", Mode: "ALL"}}); status.Code(err) != codes.Unavailable {
		t.Fatalf("engineless code = %v", status.Code(err))
	}
}

// TestTodo_REV_093_01_Security proves the preview is an administrator-only
// read that reports the admin override honestly and never carries a worker
// record over the wire.
func TestTodo_REV_093_01_Security(t *testing.T) {
	engine := rev09301Engine()
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine, RoleAccess: rev09301Access()}))
	request := &journeyv1.PreviewRoleAccessRequest{Proposed: &journeyv1.RoleOrganizationVisibilityPolicy{RoleId: "comp_admin", Mode: "ALLOWLIST", OrganizationUnits: []string{"Sales"}}}

	// A non-administrator is refused before the workforce is read.
	_, err := client.PreviewRoleAccess(testContext(t), request)
	assertOwnedCode(t, err, envelope.CodePermissionDenied)
	if engine.listWorkersCalls != 0 {
		t.Fatalf("workforce read %d times before authorization", engine.listWorkersCalls)
	}
	// An unauthenticated call never reaches the handler.
	if _, err := client.PreviewRoleAccess(context.Background(), request); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("anonymous code = %v", status.Code(err))
	}

	// hcm_admin and comp_admin see every unit before any policy runs; the
	// preview must say so rather than show the stored or drafted narrowing.
	all := []string{"Engineering", "Finance", "People", "Sales"}
	for _, role := range []string{"comp_admin", "hcm_admin"} {
		request.Proposed.RoleId = role
		response, err := client.PreviewRoleAccess(rev09301AdminContext(t), request)
		if err != nil {
			t.Fatalf("%s preview: %v", role, err)
		}
		if !response.GetAdministratorOverride() {
			t.Fatalf("%s preview hides the administrator override", role)
		}
		if !reflect.DeepEqual(response.GetCurrent().GetOrganizationUnits(), all) || !reflect.DeepEqual(response.GetProposed().GetOrganizationUnits(), all) {
			t.Fatalf("%s preview silently narrowed: %v -> %v", role, response.GetCurrent().GetOrganizationUnits(), response.GetProposed().GetOrganizationUnits())
		}
		if len(response.GetRemovedUnits()) != 0 || len(response.GetAddedUnits()) != 0 {
			t.Fatalf("%s override reported a change: %+v", role, response)
		}
		encoded, err := protojson.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		for _, worker := range engine.workers {
			for _, identity := range []string{worker.WorkerRef, worker.WorkerID} {
				if identity != "" && strings.Contains(string(encoded), `"`+identity+`"`) {
					t.Fatalf("%s preview carries worker identity %q: %s", role, identity, encoded)
				}
			}
		}
	}

	// No field of the response message can carry a worker record.
	fields := (&journeyv1.PreviewRoleAccessResponse{}).ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		name := strings.ToLower(string(field.Name()))
		if strings.Contains(name, "worker") || strings.Contains(name, "subject") || strings.Contains(name, "person") || field.Kind() == protoreflect.MessageKind && field.Message().FullName() != "hcmnext.journey.v1.RoleAccessScope" {
			t.Fatalf("response field %s can carry a worker record", field.Name())
		}
	}
}
