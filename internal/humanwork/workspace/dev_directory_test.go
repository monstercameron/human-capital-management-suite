package workspace

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
)

func TestDevEmployeePersonaIDKeepsTheTwoIDSpacesDisjoint(t *testing.T) {
	id := DevEmployeePersonaID("hc-001-amina-rahman")
	if id != "employee:hc-001-amina-rahman" {
		t.Fatalf("persona id = %q", id)
	}
	if !IsDevEmployeePersonaID(id) {
		t.Fatal("a derived employee id is not recognized as one")
	}
	if got := DevEmployeePersonaID("   "); got != "" {
		t.Fatalf("blank worker key produced the id %q, which would select nothing", got)
	}
	// The four canonical quick-pick ids must never be mistaken for employee
	// ids: writeLoginPage renders them from DevPersonaRoleSets and the
	// existing persona-card tests split the document on their form class.
	for _, set := range DevPersonaRoleSets() {
		if IsDevEmployeePersonaID(set.ID) {
			t.Errorf("canonical persona %q collides with the employee id space", set.ID)
		}
		if DevEmployeePersonaID(set.ID) == set.ID {
			t.Errorf("employee id derivation is a no-op for canonical persona %q", set.ID)
		}
	}
}

func TestDevEmployeeBundleAccessReusesTheCanonicalRoleBundles(t *testing.T) {
	canonical := map[DevEmployeeBundle]string{
		DevEmployeeBundleExecutive: "admin",
		DevEmployeeBundleFinance:   "finance-partner",
		DevEmployeeBundleManager:   "hiring-manager",
		DevEmployeeBundleSelf:      "individual-contributor",
	}
	for bundle, personaID := range canonical {
		access, ok := DevEmployeeBundleAccess(bundle)
		if !ok {
			t.Fatalf("bundle %q did not resolve", bundle)
		}
		want, found := DevPersonaRoles(personaID)
		if !found {
			t.Fatalf("no canonical fixture for persona %q", personaID)
		}
		if !reflect.DeepEqual(access.Roles, want) {
			t.Errorf("bundle %q roles = %v, want the canonical %q bundle %v", bundle, access.Roles, personaID, want)
		}
	}
	peopleOps, ok := DevEmployeeBundleAccess(DevEmployeeBundlePeopleOps)
	if !ok || !reflect.DeepEqual(peopleOps.Roles, []string{"hr_partner"}) {
		t.Fatalf("people-operations bundle = %v, ok=%v", peopleOps.Roles, ok)
	}
}

func TestDevEmployeeBundleAccessIsLabelledPurposefullyAndRefusesTheUnknown(t *testing.T) {
	if _, ok := DevEmployeeBundleAccess(DevEmployeeBundle("chief-of-everything")); ok {
		t.Fatal("an unnamed bundle resolved; an unscoped credential would be issued for it")
	}
	if _, ok := DevEmployeeBundleAccess(""); ok {
		t.Fatal("the empty bundle resolved")
	}
	labels := map[string]bool{}
	for _, bundle := range []DevEmployeeBundle{
		DevEmployeeBundleExecutive, DevEmployeeBundlePeopleOps, DevEmployeeBundleFinance,
		DevEmployeeBundleManager, DevEmployeeBundleSelf,
	} {
		access, ok := DevEmployeeBundleAccess(bundle)
		if !ok {
			t.Fatalf("bundle %q did not resolve", bundle)
		}
		if access.Bundle != bundle {
			t.Errorf("bundle %q reports itself as %q", bundle, access.Bundle)
		}
		if strings.TrimSpace(access.Label) == "" {
			t.Errorf("bundle %q has no access label", bundle)
		}
		if labels[access.Label] {
			t.Errorf("bundle %q reuses the access label %q, so two different authorizations read alike", bundle, access.Label)
		}
		labels[access.Label] = true
		switch access.Purpose {
		case devPurposeCompensationReview, devPurposeSelfService:
		default:
			t.Errorf("bundle %q declares the unrecognized purpose %q", bundle, access.Purpose)
		}
		// A card that named a page the bundle cannot open is UXAUDIT-014's
		// defect. Assert the narrow bundles stay narrow against the same
		// projection the product shell enforces, so widening a bundle here
		// without meaning to is a failure rather than a silent overpromise.
		permissions := roleaccess.EffectivePagePermissions(roleaccess.Snapshot{PagePermissions: roleaccess.DefaultPagePermissions()}, access.Roles)
		reachesPeople := roleaccess.CanPageAction(permissions, "people", roleaccess.ActionView)
		reachesAdmin := roleaccess.CanPageAction(permissions, "admin", roleaccess.ActionView)
		switch bundle {
		case DevEmployeeBundleExecutive:
			if !reachesAdmin {
				t.Error("the executive bundle cannot reach Admin, which its label claims")
			}
		case DevEmployeeBundleSelf, DevEmployeeBundleFinance:
			if reachesPeople || reachesAdmin {
				t.Errorf("the %q bundle reaches the workforce directory or Admin", bundle)
			}
		case DevEmployeeBundleManager, DevEmployeeBundlePeopleOps:
			if !reachesPeople {
				t.Errorf("the %q bundle cannot reach People, which its label claims", bundle)
			}
			if reachesAdmin {
				t.Errorf("the %q bundle reaches Admin", bundle)
			}
		}
	}
}

func TestDevRoleLabelsNameEveryRoleTheCredentialCarries(t *testing.T) {
	labels := devRoleLabels([]string{"manager", "intent_author", "not_a_registered_role"})
	want := []string{"People manager", "Workflow author", "not_a_registered_role"}
	if !reflect.DeepEqual(labels, want) {
		t.Fatalf("role labels = %v, want %v", labels, want)
	}
	if devRoleLabels(nil) != nil {
		t.Fatal("an empty role list produced labels")
	}
	// Every label must come from the live role registry rather than a copy
	// of it typed here, so a renamed role renames the sign-in copy too.
	registered := roleaccess.DefaultRoles()
	index := slices.IndexFunc(registered, func(role roleaccess.Role) bool { return role.ID == "manager" })
	if index < 0 || registered[index].Name != labels[0] {
		t.Fatalf("label %q is not the registry's name for manager", labels[0])
	}
}
