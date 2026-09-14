package workspace

// DevPersonaRoleSet pairs one local-development persona's stable ID with the
// exact role bundle its signed credential carries.
type DevPersonaRoleSet struct {
	ID    string
	Roles []string
}

// DevPersonaRoleSets enumerates the four local-development personas, in the
// order the sign-in page displays them, and is the single fixture both the
// token issuer (internal/application's composeDevPersonas) and this
// package's own login copy (loginPersonaDescription) and tests read. A
// persona's granted roles, its derived sign-in description, and the tests
// that pin its promised destinations therefore cannot drift out of sync with
// one another: change a role bundle here and every consumer sees the same
// change atomically (UXAUDIT-014 REFACTOR).
//
// UXAUDIT-014: payroll-manager previously carried the standalone
// "payroll_manager" role. Both productui.PageVisible and
// roleaccess.DefaultPagePermissions grant that role the exact same
// destinations as "manager"/"hiring_manager"/"hr_partner" -- no
// payroll-specific page is admitted anywhere in the registry yet, so that
// role bundle promised a hiring-manager-equivalent workspace under a
// payroll label, and rendered byte-identical navigation to hiring-manager
// despite the two personas holding disjoint role sets. Until a real payroll
// surface is admitted (option (a) in the todo), "worker_self" is the only
// role that honestly describes what a payroll-context persona can reach
// today: their own employment record and organization context, nothing
// workforce-wide. This is option (b): the smaller, safer fix that makes the
// fixture's promise match its admitted capability instead of inventing a
// new surface.
//
// PROMOUX-015: that slot is now the finance approver. A promotion needs four
// separated people -- a proposer, a finance approver, a manager approver and
// the employee -- and the worker_self payroll slot could do none of those
// steps. It is renamed "finance-partner" and carries the narrow
// "finance_partner" role (roleaccess: Home, Myself, My Work with update, Work
// History, Organization, Help and Settings; authz: the worker core and
// compensation under compensation_review), so it can decide the finance
// approval routed to it and nothing else. The admin persona is the manager
// approver: its worker manages the employee persona's worker, and it holds the
// execution role. The hiring-manager persona is the proposer, bound to the
// employee's skip-level manager, because the reference workflow's manager
// approval is CurrentManagerOf(worker) and the requester may not approve.
func DevPersonaRoleSets() []DevPersonaRoleSet {
	return []DevPersonaRoleSet{
		{ID: "admin", Roles: []string{"hcm_admin", "comp_admin", "intent_author", "promotion_operator"}},
		{ID: "hiring-manager", Roles: []string{"hiring_manager", "manager", "intent_author"}},
		{ID: "finance-partner", Roles: []string{"finance_partner"}},
		{ID: "individual-contributor", Roles: []string{"worker_self"}},
	}
}

// DevPersonaRoles looks up one persona's role bundle by ID. The returned
// slice is a copy: callers may not mutate the canonical fixture.
func DevPersonaRoles(id string) ([]string, bool) {
	for _, set := range DevPersonaRoleSets() {
		if set.ID == id {
			return append([]string(nil), set.Roles...), true
		}
	}
	return nil, false
}
