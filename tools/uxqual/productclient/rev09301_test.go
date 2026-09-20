package productclient

import (
	"context"
	"errors"
	"reflect"
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/protobuf/proto"
)

func rev09301ManagerResponse() *journeyv1.PreviewRoleAccessResponse {
	return &journeyv1.PreviewRoleAccessResponse{
		RoleId: "manager", RoleName: "People manager",
		ExplicitRoles: []string{"manager"}, InheritedRoles: []string{"hr_partner"},
		Current:      &journeyv1.RoleAccessScope{Mode: "ALLOWLIST", OrganizationUnits: []string{"Engineering", "Finance"}},
		Proposed:     &journeyv1.RoleAccessScope{Mode: "ALLOWLIST", OrganizationUnits: []string{"Engineering", "Sales"}},
		AddedUnits:   []string{"Sales"},
		RemovedUnits: []string{"Finance"},
		HolderCount:  2,
	}
}

func rev09301AdminResponse() *journeyv1.PreviewRoleAccessResponse {
	all := []string{"Engineering", "Finance", "People", "Sales"}
	return &journeyv1.PreviewRoleAccessResponse{
		RoleId: "comp_admin", RoleName: "Compensation administrator",
		ExplicitRoles: []string{"comp_admin"}, AdministratorOverride: true,
		Current:     &journeyv1.RoleAccessScope{Mode: "OWN_UNIT", OrganizationUnits: all},
		Proposed:    &journeyv1.RoleAccessScope{Mode: "ALLOWLIST", OrganizationUnits: all},
		HolderCount: 1, OverriddenHolderCount: 1,
	}
}

type rev09301PreviewService struct {
	request  *journeyv1.PreviewRoleAccessRequest
	response *journeyv1.PreviewRoleAccessResponse
	err      error
}

func (s *rev09301PreviewService) PreviewRoleAccess(_ context.Context, request *journeyv1.PreviewRoleAccessRequest) (*journeyv1.PreviewRoleAccessResponse, error) {
	s.request = request
	return s.response, s.err
}

// TestTodo_REV_093_01_Golden pins the explanation a real server answer
// produces for a narrowing change and for the administrator override.
func TestTodo_REV_093_01_Golden(t *testing.T) {
	manager, err := AccessPreviewFromResponse("manager", rev09301ManagerResponse())
	if err != nil {
		t.Fatalf("manager preview rejected: %v", err)
	}
	wantManager := "Role \"People manager\" would reveal 2 organization units.\n" +
		"Explicitly granted: manager.\n" +
		"Inherited: hr_partner.\n" +
		"Effective scope: Engineering, Sales.\n" +
		"Currently visible: Engineering, Finance.\n" +
		"Newly visible: Sales.\n" +
		"No longer visible: Finance.\n" +
		"Withheld: Everyone always sees their own record, and people managers also see their reporting line.\n" +
		"Next: Review the proposed scope, then save to apply it.\n"
	if got := manager.Explain(); got != wantManager {
		t.Fatalf("manager explanation drifted:\ngot:\n%s\nwant:\n%s", got, wantManager)
	}

	admin, err := AccessPreviewFromResponse("comp_admin", rev09301AdminResponse())
	if err != nil {
		t.Fatalf("administrator preview rejected: %v", err)
	}
	wantAdmin := "Role \"Compensation administrator\" would reveal 4 organization units.\n" +
		"Explicitly granted: comp_admin.\n" +
		"Inherited: none.\n" +
		"Effective scope: Every organization unit (administrator override).\n" +
		"Currently visible: Engineering, Finance, People, Sales.\n" +
		"Newly visible: none.\n" +
		"Administrator override: this role sees every organization unit whatever its visibility setting.\n" +
		"Withheld: Everyone always sees their own record, and people managers also see their reporting line.\n" +
		"Next: Saving does not narrow this role; administrators always see every unit.\n"
	if got := admin.Explain(); got != wantAdmin {
		t.Fatalf("administrator explanation drifted:\ngot:\n%s\nwant:\n%s", got, wantAdmin)
	}
}

// TestTodo_REV_093_01_Security refuses any answer that would mislead the
// administrator: a narrowed administrator override, an answer about another
// role, or a hidden unit the answer does not name.
func TestTodo_REV_093_01_Security(t *testing.T) {
	narrowed := proto.Clone(rev09301AdminResponse()).(*journeyv1.PreviewRoleAccessResponse)
	narrowed.Proposed.OrganizationUnits = []string{"Sales"}
	narrowed.RemovedUnits = []string{"Engineering", "Finance", "People"}
	if _, err := AccessPreviewFromResponse("comp_admin", narrowed); !errors.Is(err, ErrAccessPreviewInvalid) {
		t.Fatalf("narrowed administrator override accepted: %v", err)
	}
	silent := proto.Clone(rev09301ManagerResponse()).(*journeyv1.PreviewRoleAccessResponse)
	silent.RemovedUnits = nil
	if _, err := AccessPreviewFromResponse("manager", silent); !errors.Is(err, ErrAccessPreviewInvalid) {
		t.Fatalf("silent narrowing accepted: %v", err)
	}
	if _, err := AccessPreviewFromResponse("hr_partner", rev09301ManagerResponse()); !errors.Is(err, ErrAccessPreviewInvalid) {
		t.Fatalf("answer for another role accepted: %v", err)
	}
	if _, err := AccessPreviewFromResponse("manager", nil); !errors.Is(err, ErrAccessPreviewInvalid) {
		t.Fatalf("empty answer accepted: %v", err)
	}
	unsourced := proto.Clone(rev09301ManagerResponse()).(*journeyv1.PreviewRoleAccessResponse)
	unsourced.ExplicitRoles, unsourced.InheritedRoles = nil, nil
	if _, err := AccessPreviewFromResponse("manager", unsourced); !errors.Is(err, ErrAccessPreviewInvalid) {
		t.Fatalf("unsourced preview accepted: %v", err)
	}

	// The browser sends the draft; a refused server answer never renders.
	service := &rev09301PreviewService{response: narrowed}
	if _, err := PreviewRoleAccess(context.Background(), service, productui.OrganizationVisibilityPolicy{RoleID: "comp_admin", Mode: "ALLOWLIST", OrganizationUnits: []string{"Sales"}}); !errors.Is(err, ErrAccessPreviewInvalid) {
		t.Fatalf("PreviewRoleAccess rendered a narrowed override: %v", err)
	}
	if _, err := PreviewRoleAccess(context.Background(), nil, productui.OrganizationVisibilityPolicy{RoleID: "manager"}); !errors.Is(err, ErrAccessPreviewUnavailable) {
		t.Fatalf("nil service err = %v", err)
	}
	refusal := errors.New("permission denied")
	if _, err := PreviewRoleAccess(context.Background(), &rev09301PreviewService{err: refusal}, productui.OrganizationVisibilityPolicy{RoleID: "manager", Mode: "ALL"}); !errors.Is(err, refusal) {
		t.Fatalf("server refusal lost: %v", err)
	}
}

// TestTodo_REV_093_01_ClientProjection proves the client forwards the draft
// verbatim and projects the qualified answer onto the editor's type.
func TestTodo_REV_093_01_ClientProjection(t *testing.T) {
	service := &rev09301PreviewService{response: rev09301ManagerResponse()}
	draft := productui.OrganizationVisibilityPolicy{Version: 4, RoleID: "manager", Mode: "ALLOWLIST", OrganizationUnits: []string{"Engineering", "Sales"}}
	view, err := PreviewRoleAccess(context.Background(), service, draft)
	if err != nil {
		t.Fatalf("PreviewRoleAccess: %v", err)
	}
	sent := service.request.GetProposed()
	if sent.GetVersion() != 4 || sent.GetRoleId() != "manager" || sent.GetMode() != "ALLOWLIST" || !reflect.DeepEqual(sent.GetOrganizationUnits(), []string{"Engineering", "Sales"}) {
		t.Fatalf("draft not forwarded verbatim: %+v", sent)
	}
	want := productui.RoleAccessPreview{
		RoleID: "manager", RoleName: "People manager", ExplicitRoles: []string{"manager"}, InheritedRoles: []string{"hr_partner"},
		CurrentMode: "ALLOWLIST", CurrentUnits: []string{"Engineering", "Finance"},
		ProposedMode: "ALLOWLIST", ProposedUnits: []string{"Engineering", "Sales"},
		AddedUnits: []string{"Sales"}, RemovedUnits: []string{"Finance"}, HolderCount: 2,
	}
	if !reflect.DeepEqual(view, want) {
		t.Fatalf("projection = %+v\nwant %+v", view, want)
	}
	// An unchanged, relative scope needs no next action and names no unit.
	steady := &journeyv1.PreviewRoleAccessResponse{RoleId: "hr_partner", ExplicitRoles: []string{"hr_partner"},
		Current: &journeyv1.RoleAccessScope{Mode: "OWN_UNIT", Relative: true}, Proposed: &journeyv1.RoleAccessScope{Mode: "OWN_UNIT", Relative: true}}
	preview, err := AccessPreviewFromResponse("hr_partner", steady)
	if err != nil {
		t.Fatalf("steady relative preview rejected: %v", err)
	}
	if preview.RoleName != "hr_partner" || preview.EffectiveScope != "Each person's own organization unit" || preview.NextAction != "" {
		t.Fatalf("steady preview = %+v", preview)
	}
	for mode, want := range map[string]string{"ALL": "Every organization unit", "ALLOWLIST": "No organization units"} {
		response := &journeyv1.PreviewRoleAccessResponse{RoleId: "x", ExplicitRoles: []string{"x"}, Current: &journeyv1.RoleAccessScope{}, Proposed: &journeyv1.RoleAccessScope{Mode: mode}}
		if got := effectiveScopeText(response); got != want {
			t.Errorf("%s scope text = %q, want %q", mode, got, want)
		}
	}
	overridden := rev09301ManagerResponse()
	overridden.OverriddenHolderCount = 1
	if got := withheldText(overridden); got != "Everyone always sees their own record, and people managers also see their reporting line. Administrators among the people with this role (1) see every unit." {
		t.Errorf("withheld note = %q", got)
	}
}
