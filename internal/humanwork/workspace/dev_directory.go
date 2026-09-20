package workspace

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
)

// This file owns the dev-only employee directory the sign-in page offers, and
// nothing else. It is deliberately a narrow port rather than a read through
// [Cell]: the sign-in page runs before any credential exists, so it must not
// be able to reach the live workforce read chain at all. A composition that
// does not supply a directory renders no directory, and PathLogin itself is
// registered only when Options.DevBrowserLogin is set, so the whole surface is
// absent from a production-shaped cell.

// devEmployeePersonaPrefix namespaces every per-employee sign-in identifier.
// The four canonical ids in [DevPersonaRoleSets] ("admin", "hiring-manager",
// "finance-partner", "individual-contributor") carry no prefix, so the two id
// spaces cannot collide and a per-employee entry can never be mistaken for a
// quick-pick persona by either the renderer or the submit handler.
const devEmployeePersonaPrefix = "employee:"

// DevEmployeePersonaID derives the opaque sign-in id for one seeded worker.
// Like every persona id it is a selector, not authority: the credential it
// selects lives only in the server-owned persona collection.
func DevEmployeePersonaID(workerKey string) string {
	key := strings.TrimSpace(workerKey)
	if key == "" {
		return ""
	}
	return devEmployeePersonaPrefix + key
}

// IsDevEmployeePersonaID reports whether id belongs to the per-employee id
// space rather than the four canonical quick-pick personas.
func IsDevEmployeePersonaID(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), devEmployeePersonaPrefix)
}

// DevDirectoryUnit is one organization unit of the seeded demo company.
// ParentCode is empty for the single root unit.
type DevDirectoryUnit struct {
	Code       string
	Name       string
	ParentCode string
}

// DevDirectoryEmployee is one seeded employee, carrying only the already
// public plan facts a tester needs in order to choose the right person. It
// deliberately carries no credential and no compensation: the sign-in page
// renders this struct verbatim, so a field added here is a field disclosed to
// an unauthenticated browser.
type DevDirectoryEmployee struct {
	// PersonaID is the sign-in selector, normally [DevEmployeePersonaID] of
	// WorkerKey. An employee whose id matches no server-held persona is
	// rendered without a sign-in control rather than with a dead one.
	PersonaID    string
	WorkerKey    string
	Name         string
	WorkerNumber string
	JobTitle     string
	UnitCode     string
	// ManagerKey is the worker key of this employee's manager, or a
	// non-worker sentinel (the seed uses "board:harborcare" for the CEO).
	ManagerKey string
}

// DevDirectorySnapshot is the whole immutable directory one render reads.
type DevDirectorySnapshot struct {
	Units     []DevDirectoryUnit
	Employees []DevDirectoryEmployee
}

// DevDirectory supplies the seeded demo org chart to the dev sign-in page.
// It is an interface so this package stays testable with a stub and never
// imports the demo seed: the composition root owns that dependency.
type DevDirectory interface {
	DevDirectorySnapshot() DevDirectorySnapshot
}

// DevEmployeeBundle names one role bundle a seeded employee may sign in with.
// The classification itself (which employee is an executive, who runs people
// operations) is a fact about the demo plan and is decided by the composition
// root that owns that plan; this package owns only what each bundle grants,
// so the sign-in page, the token issuer and the tests read one fixture.
type DevEmployeeBundle string

const (
	DevEmployeeBundleExecutive DevEmployeeBundle = "executive"
	DevEmployeeBundlePeopleOps DevEmployeeBundle = "people-operations"
	DevEmployeeBundleFinance   DevEmployeeBundle = "finance"
	DevEmployeeBundleManager   DevEmployeeBundle = "manager"
	DevEmployeeBundleSelf      DevEmployeeBundle = "self"
)

// DevEmployeeAccess is one bundle resolved: the exact roles signed into the
// credential, the purpose it declares, and the access label its card shows.
//
// Every Purpose here is granted to at least one role in Roles by the live P1A
// policy (internal/trust/authz.PolicyTable), and every Label is checked
// against that same table by the composition's own test rather than being
// trusted as prose. A label must not imply a capability the bundle does not
// hold - that was UXAUDIT-014's defect and it is just as available here, where
// sixty credentials are minted instead of four.
type DevEmployeeAccess struct {
	Bundle  DevEmployeeBundle
	Label   string
	Roles   []string
	Purpose string
}

// devEmployeeBundlePurposes and devEmployeeBundleLabels are split out so the
// mapping is one table rather than a switch repeated per consumer.
//
// The executive, manager, finance and self bundles reuse the canonical
// quick-pick fixtures in [DevPersonaRoleSets] outright: a tester signing in as
// a real executive and a tester signing in as the "admin" quick pick must get
// the same authority, or the directory quietly becomes a second, divergent
// policy. Only the people-operations bundle has no quick-pick equivalent, so
// it names its role directly.
func devEmployeeBundleRoles(bundle DevEmployeeBundle) ([]string, bool) {
	switch bundle {
	case DevEmployeeBundleExecutive:
		return DevPersonaRoles("admin")
	case DevEmployeeBundlePeopleOps:
		return []string{"hr_partner"}, true
	case DevEmployeeBundleFinance:
		return DevPersonaRoles("finance-partner")
	case DevEmployeeBundleManager:
		return DevPersonaRoles("hiring-manager")
	case DevEmployeeBundleSelf:
		return DevPersonaRoles("individual-contributor")
	}
	return nil, false
}

// DevEmployeeBundleAccess resolves one bundle. It reports false for an
// unknown bundle rather than returning an empty role set: issuing an unscoped
// credential would be worse than not offering the employee at all.
func DevEmployeeBundleAccess(bundle DevEmployeeBundle) (DevEmployeeAccess, bool) {
	roles, ok := devEmployeeBundleRoles(bundle)
	if !ok || len(roles) == 0 {
		return DevEmployeeAccess{}, false
	}
	access := DevEmployeeAccess{Bundle: bundle, Roles: roles}
	switch bundle {
	case DevEmployeeBundleExecutive:
		access.Label, access.Purpose = "HCM administrator", devPurposeCompensationReview
	case DevEmployeeBundlePeopleOps:
		access.Label, access.Purpose = "HR partner", devPurposeCompensationReview
	case DevEmployeeBundleFinance:
		access.Label, access.Purpose = "Finance partner", devPurposeCompensationReview
	case DevEmployeeBundleManager:
		access.Label, access.Purpose = "People manager", devPurposeCompensationReview
	case DevEmployeeBundleSelf:
		access.Label, access.Purpose = "Individual contributor", devPurposeSelfService
	}
	return access, true
}

// devPurposeCompensationReview and devPurposeSelfService are the two purpose
// tokens these credentials declare. They are the literal authz purposes
// (authz.PurposeCompensationReview, authz.PurposeSelfService); this package
// does not import the policy table, and the composition's test asserts each
// one against it.
const (
	devPurposeCompensationReview = "compensation_review"
	devPurposeSelfService        = "self_service_view"
)

// devRoleLabels renders role ids as the names the access administration UI
// shows, so a card says "People manager" rather than "manager". An id with no
// registered role falls back to itself instead of being dropped: a tester must
// be able to see every role their credential will carry.
func devRoleLabels(roles []string) []string {
	if len(roles) == 0 {
		return nil
	}
	names := make(map[string]string, 12)
	for _, role := range roleaccess.DefaultRoles() {
		names[role.ID] = role.Name
	}
	labels := make([]string, 0, len(roles))
	for _, role := range roles {
		if name, ok := names[role]; ok {
			labels = append(labels, name)
			continue
		}
		labels = append(labels, role)
	}
	return labels
}
