package roleaccess

import (
	"errors"
	"reflect"
	"testing"
)

func rev09301Members() []PreviewMember {
	return []PreviewMember{
		{Refs: []string{"w-ana", "E-1"}, Unit: "Engineering"},
		{Refs: []string{"w-ben"}, Unit: "Finance"},
		{Refs: []string{"w-cy"}, Unit: "Sales"},
		{Refs: []string{"w-dee"}, Unit: "People"},
		{Refs: []string{"w-eve"}, Unit: "engineering"},
	}
}

func rev09301Snapshot() Snapshot {
	return Snapshot{
		Roles: append(DefaultRoles(), Role{ID: "auditor", Name: "Auditor", Active: false}),
		Assignments: []Assignment{
			{WorkerRef: "w-ana", RoleIDs: []string{"manager", "hr_partner"}},
			{WorkerRef: "W-BEN", RoleIDs: []string{"manager"}},
			{WorkerRef: "w-dee", RoleIDs: []string{"manager", "comp_admin", "auditor"}},
			{WorkerRef: "w-cy", RoleIDs: []string{"worker_self"}},
		},
		Policies: []VisibilityPolicy{
			{RoleID: "manager", Mode: VisibilityAllowlist, OrganizationUnits: []string{"Engineering", "Finance"}},
			{RoleID: "hcm_admin", Mode: VisibilityOwnUnit},
		},
	}
}

// TestTodo_REV_093_01 resolves current versus proposed access for a role
// from durable role and visibility data: the saved policy, the saved
// holders, the other roles those holders carry, and the governed units.
func TestTodo_REV_093_01(t *testing.T) {
	preview, err := ResolveAccessPreview(rev09301Snapshot(), VisibilityPolicy{RoleID: " Manager ", Mode: "denylist", OrganizationUnits: []string{"Finance"}}, rev09301Members())
	if err != nil {
		t.Fatalf("ResolveAccessPreview: %v", err)
	}
	if preview.RoleID != "manager" || preview.RoleName != "People manager" || !reflect.DeepEqual(preview.ExplicitRoles, []string{"manager"}) {
		t.Fatalf("role identity = %q %q %v", preview.RoleID, preview.RoleName, preview.ExplicitRoles)
	}
	// hr_partner and comp_admin are co-held by saved holders; the inactive
	// auditor role grants nothing and is not reported.
	if !reflect.DeepEqual(preview.InheritedRoles, []string{"comp_admin", "hr_partner"}) {
		t.Fatalf("inherited roles = %v", preview.InheritedRoles)
	}
	if preview.AdministratorOverride {
		t.Fatal("manager must not report the administrator override")
	}
	if preview.HolderCount != 3 || preview.OverriddenHolderCount != 1 {
		t.Fatalf("holders = %d overridden = %d, want 3 and 1", preview.HolderCount, preview.OverriddenHolderCount)
	}
	if preview.Current.Mode != VisibilityAllowlist || !reflect.DeepEqual(preview.Current.Units, []string{"Engineering", "Finance"}) || preview.Current.Relative {
		t.Fatalf("current scope = %+v", preview.Current)
	}
	if preview.Proposed.Mode != VisibilityDenylist || !reflect.DeepEqual(preview.Proposed.Units, []string{"Engineering", "People", "Sales"}) {
		t.Fatalf("proposed scope = %+v", preview.Proposed)
	}
	if !reflect.DeepEqual(preview.AddedUnits, []string{"People", "Sales"}) || !reflect.DeepEqual(preview.RemovedUnits, []string{"Finance"}) {
		t.Fatalf("added %v removed %v", preview.AddedUnits, preview.RemovedUnits)
	}

	// An unconfigured active role reports the directory's OWN_UNIT default,
	// resolved across its saved holders' own units.
	own, err := ResolveAccessPreview(rev09301Snapshot(), VisibilityPolicy{RoleID: "hr_partner", Mode: VisibilityAll}, rev09301Members())
	if err != nil {
		t.Fatal(err)
	}
	if own.Current.Mode != VisibilityOwnUnit || !own.Current.Relative || !reflect.DeepEqual(own.Current.Units, []string{"Engineering"}) {
		t.Fatalf("default OWN_UNIT scope = %+v", own.Current)
	}
	if !reflect.DeepEqual(own.Proposed.Units, []string{"Engineering", "Finance", "People", "Sales"}) || own.Proposed.Relative {
		t.Fatalf("ALL scope = %+v", own.Proposed)
	}
	if !reflect.DeepEqual(own.InheritedRoles, []string{"manager"}) {
		t.Fatalf("hr_partner inherited roles = %v", own.InheritedRoles)
	}
}

// TestTodo_REV_093_01_Regression keeps malformed proposals and inactive or
// unknown roles out of the preview rather than resolving a guess.
func TestTodo_REV_093_01_Regression(t *testing.T) {
	for name, proposed := range map[string]VisibilityPolicy{
		"unknown role":  {RoleID: "ghost", Mode: VisibilityAll},
		"inactive role": {RoleID: "auditor", Mode: VisibilityAll},
		"bad mode":      {RoleID: "manager", Mode: "EVERYONE"},
		"bad role id":   {RoleID: "", Mode: VisibilityAll},
	} {
		if _, err := ResolveAccessPreview(rev09301Snapshot(), proposed, rev09301Members()); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	// A role nobody holds yet with OWN_UNIT reveals no enumerable unit but
	// still says the scope is relative to each viewer.
	lonely, err := ResolveAccessPreview(rev09301Snapshot(), VisibilityPolicy{RoleID: "payroll_manager", Mode: VisibilityOwnUnit}, rev09301Members())
	if err != nil {
		t.Fatal(err)
	}
	if len(lonely.Current.Units) != 0 || !lonely.Current.Relative || lonely.HolderCount != 0 || len(lonely.AddedUnits)+len(lonely.RemovedUnits) != 0 {
		t.Fatalf("unheld OWN_UNIT preview = %+v", lonely)
	}
}

// TestTodo_REV_093_01_Security proves the administrator override is
// reported as the directory enforces it: hcm_admin and comp_admin see every
// unit before any visibility policy runs, so a narrowing draft must not be
// previewed as narrowing them.
func TestTodo_REV_093_01_Security(t *testing.T) {
	all := []string{"Engineering", "Finance", "People", "Sales"}
	for _, role := range []string{"hcm_admin", "comp_admin"} {
		if !IsAdministratorRole(role) {
			t.Fatalf("%s is not an administrator role", role)
		}
		preview, err := ResolveAccessPreview(rev09301Snapshot(), VisibilityPolicy{RoleID: role, Mode: VisibilityAllowlist, OrganizationUnits: []string{"Sales"}}, rev09301Members())
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if !preview.AdministratorOverride {
			t.Fatalf("%s preview hides the administrator override", role)
		}
		if !reflect.DeepEqual(preview.Current.Units, all) || !reflect.DeepEqual(preview.Proposed.Units, all) {
			t.Fatalf("%s scope narrowed: current %v proposed %v, want %v", role, preview.Current.Units, preview.Proposed.Units, all)
		}
		if len(preview.RemovedUnits) != 0 || len(preview.AddedUnits) != 0 || preview.Proposed.Relative || preview.Current.Relative {
			t.Fatalf("%s override reported a change: %+v", role, preview)
		}
		if preview.OverriddenHolderCount != preview.HolderCount {
			t.Fatalf("%s: every holder is overridden, got %d of %d", role, preview.OverriddenHolderCount, preview.HolderCount)
		}
	}
	if IsAdministratorRole("manager") {
		t.Fatal("manager must not carry the administrator override")
	}
	// The preview carries names and counts, never a member reference.
	preview, err := ResolveAccessPreview(rev09301Snapshot(), VisibilityPolicy{RoleID: "manager", Mode: VisibilityAll}, rev09301Members())
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"w-ana", "E-1", "w-ben", "w-cy", "w-dee", "w-eve"} {
		for _, list := range [][]string{preview.ExplicitRoles, preview.InheritedRoles, preview.Current.Units, preview.Proposed.Units, preview.AddedUnits, preview.RemovedUnits} {
			for _, value := range list {
				if value == ref {
					t.Fatalf("preview leaked member reference %q", ref)
				}
			}
		}
	}
}
