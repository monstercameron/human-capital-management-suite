// Package roleaccess owns the tenant-configurable product authorization model.
// It records role definitions, employee role assignments, and the organization
// directory boundary granted by each role. Roles are additive: an employee can
// discover the union of the organization units granted by all assigned roles.
package roleaccess

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrUnavailable     = errors.New("roleaccess: store unavailable")
	ErrVersionConflict = errors.New("roleaccess: stale version")
	ErrInvalid         = errors.New("roleaccess: invalid value")
)

const (
	VisibilityAll       = "ALL"
	VisibilityOwnUnit   = "OWN_UNIT"
	VisibilityAllowlist = "ALLOWLIST"
	VisibilityDenylist  = "DENYLIST"
	ActionView          = "view"
	ActionCreate        = "create"
	ActionUpdate        = "update"
	ActionDelete        = "delete"
)

var (
	roleIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,62}$`)
	pageIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
)

type Role struct {
	Version     int64
	ID          string
	Name        string
	Description string
	System      bool
	Active      bool
}

type Assignment struct {
	Version   int64
	WorkerRef string
	RoleIDs   []string
}

type VisibilityPolicy struct {
	Version           int64
	RoleID            string
	Mode              string
	OrganizationUnits []string
}

// PagePermission is one role's explicit authority on one product page.
// The four operations are deliberately independent: a role may, for example,
// view reports without being able to create or change them.
type PagePermission struct {
	Version int64
	RoleID  string
	PageID  string
	View    bool
	Create  bool
	Update  bool
	Delete  bool
}

type Snapshot struct {
	Roles           []Role
	Assignments     []Assignment
	Policies        []VisibilityPolicy
	PagePermissions []PagePermission
}

type Store interface {
	Bootstrap(context.Context, values.TenantId, string) error
	Load(context.Context, values.TenantId, string) (Snapshot, error)
	SaveRole(context.Context, values.TenantId, string, Role) (Role, error)
	SaveAssignment(context.Context, values.TenantId, string, Assignment) (Assignment, error)
	SaveVisibility(context.Context, values.TenantId, string, string, VisibilityPolicy) (VisibilityPolicy, error)
	SavePagePermission(context.Context, values.TenantId, string, PagePermission) (PagePermission, error)
}

func NormalizeRole(value Role) Role {
	value.ID = strings.ToLower(strings.TrimSpace(value.ID))
	value.Name = strings.TrimSpace(value.Name)
	value.Description = strings.TrimSpace(value.Description)
	return value
}

func ValidateRole(value Role) error {
	value = NormalizeRole(value)
	if !roleIDPattern.MatchString(value.ID) || value.Name == "" || len(value.Name) > 96 || len(value.Description) > 400 {
		return ErrInvalid
	}
	return nil
}

func NormalizeRoleIDs(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func NormalizeAssignment(value Assignment) Assignment {
	value.WorkerRef = strings.TrimSpace(value.WorkerRef)
	value.RoleIDs = NormalizeRoleIDs(value.RoleIDs)
	return value
}

func ValidateAssignment(value Assignment) error {
	value = NormalizeAssignment(value)
	if value.WorkerRef == "" || len(value.WorkerRef) > 256 || len(value.RoleIDs) == 0 {
		return ErrInvalid
	}
	for _, roleID := range value.RoleIDs {
		if !roleIDPattern.MatchString(roleID) {
			return ErrInvalid
		}
	}
	return nil
}

func NormalizeVisibility(value VisibilityPolicy) VisibilityPolicy {
	value.RoleID = strings.ToLower(strings.TrimSpace(value.RoleID))
	value.Mode = strings.ToUpper(strings.TrimSpace(value.Mode))
	seen := make(map[string]bool, len(value.OrganizationUnits))
	units := make([]string, 0, len(value.OrganizationUnits))
	for _, unit := range value.OrganizationUnits {
		unit = strings.TrimSpace(unit)
		key := strings.ToLower(unit)
		if unit == "" || seen[key] {
			continue
		}
		seen[key] = true
		units = append(units, unit)
	}
	sort.Slice(units, func(i, j int) bool { return strings.ToLower(units[i]) < strings.ToLower(units[j]) })
	value.OrganizationUnits = units
	return value
}

func ValidateVisibility(value VisibilityPolicy) error {
	value = NormalizeVisibility(value)
	if !roleIDPattern.MatchString(value.RoleID) {
		return ErrInvalid
	}
	switch value.Mode {
	case VisibilityAll, VisibilityOwnUnit, VisibilityAllowlist, VisibilityDenylist:
		return nil
	default:
		return ErrInvalid
	}
}

func NormalizePagePermission(value PagePermission) PagePermission {
	value.RoleID = strings.ToLower(strings.TrimSpace(value.RoleID))
	value.PageID = strings.ToLower(strings.TrimSpace(value.PageID))
	return value
}

func ValidatePagePermission(value PagePermission) error {
	value = NormalizePagePermission(value)
	if !roleIDPattern.MatchString(value.RoleID) || !pageIDPattern.MatchString(value.PageID) {
		return ErrInvalid
	}
	// An action on an undiscoverable page is incoherent and easy to
	// misconfigure. Revocation remains expressible by saving all four false.
	if !value.View && (value.Create || value.Update || value.Delete) {
		return ErrInvalid
	}
	return nil
}

func (value PagePermission) Allows(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case ActionView:
		return value.View
	case ActionCreate:
		return value.Create
	case ActionUpdate:
		return value.Update
	case ActionDelete:
		return value.Delete
	default:
		return false
	}
}

// EffectivePagePermissions merges grants from all assigned roles. Grants are
// additive; one role can never revoke an operation another role grants.
func EffectivePagePermissions(snapshot Snapshot, roleIDs []string) []PagePermission {
	wanted := make(map[string]bool, len(roleIDs))
	for _, roleID := range NormalizeRoleIDs(roleIDs) {
		wanted[roleID] = true
	}
	merged := make(map[string]PagePermission)
	for _, permission := range snapshot.PagePermissions {
		permission = NormalizePagePermission(permission)
		if !wanted[permission.RoleID] || ValidatePagePermission(permission) != nil {
			continue
		}
		current := merged[permission.PageID]
		current.PageID = permission.PageID
		current.View = current.View || permission.View
		current.Create = current.Create || permission.Create
		current.Update = current.Update || permission.Update
		current.Delete = current.Delete || permission.Delete
		merged[permission.PageID] = current
	}
	result := make([]PagePermission, 0, len(merged))
	for _, permission := range merged {
		result = append(result, permission)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PageID < result[j].PageID })
	return result
}

func CanPageAction(permissions []PagePermission, pageID, action string) bool {
	pageID = strings.ToLower(strings.TrimSpace(pageID))
	for _, permission := range permissions {
		if strings.EqualFold(permission.PageID, pageID) && permission.Allows(action) {
			return true
		}
	}
	return false
}

// AssignedRoles returns the durable assignment for subject when one exists;
// otherwise it returns the admitted credential roles. The fallback makes
// rollout non-disruptive while an administrator works through the directory.
func AssignedRoles(snapshot Snapshot, subject string, admitted []string) []string {
	for _, assignment := range snapshot.Assignments {
		if strings.EqualFold(strings.TrimSpace(assignment.WorkerRef), strings.TrimSpace(subject)) {
			return NormalizeRoleIDs(assignment.RoleIDs)
		}
	}
	return NormalizeRoleIDs(admitted)
}

// PoliciesForRoles selects active policies for an employee's additive roles.
func PoliciesForRoles(snapshot Snapshot, roleIDs []string) []VisibilityPolicy {
	wanted := make(map[string]bool, len(roleIDs))
	for _, roleID := range NormalizeRoleIDs(roleIDs) {
		wanted[roleID] = true
	}
	active := make(map[string]bool, len(snapshot.Roles))
	for _, role := range snapshot.Roles {
		role = NormalizeRole(role)
		if role.Active {
			active[role.ID] = true
		}
	}
	resolved := make(map[string]bool, len(snapshot.Policies))
	result := make([]VisibilityPolicy, 0, len(wanted))
	for _, policy := range snapshot.Policies {
		policy = NormalizeVisibility(policy)
		if wanted[policy.RoleID] && ValidateVisibility(policy) == nil {
			result = append(result, policy)
			resolved[policy.RoleID] = true
		}
	}
	// Role visibility is a data-release boundary. An active role that has not
	// yet been configured defaults to the signed-in worker's unit, matching
	// the admin UI's initial selection and avoiding an implicit all-workforce
	// grant while an administrator works through new roles.
	for roleID := range wanted {
		if active[roleID] && !resolved[roleID] {
			result = append(result, VisibilityPolicy{RoleID: roleID, Mode: VisibilityOwnUnit})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RoleID < result[j].RoleID })
	return result
}

func DefaultRoles() []Role {
	return []Role{
		{ID: "hcm_admin", Name: "HCM administrator", Description: "Administers HCM configuration and workforce access.", System: true, Active: true},
		{ID: "comp_admin", Name: "Compensation administrator", Description: "Administers compensation and sensitive workforce configuration.", System: true, Active: true},
		{ID: "hiring_manager", Name: "Hiring manager", Description: "Manages recruiting and hiring workflows.", System: true, Active: true},
		{ID: "manager", Name: "People manager", Description: "Manages employees and their governed workflows.", System: true, Active: true},
		{ID: "payroll_manager", Name: "Payroll manager", Description: "Manages payroll operations and review.", System: true, Active: true},
		{ID: "hr_partner", Name: "HR partner", Description: "Supports assigned organization units and their people.", System: true, Active: true},
		{ID: "finance_partner", Name: "Finance partner", Description: "Decides the finance approvals routed to them.", System: true, Active: true},
		{ID: "intent_author", Name: "Workflow author", Description: "Creates governed workflow proposals.", System: true, Active: true},
		{ID: "promotion_operator", Name: "Promotion operator", Description: "Executes governed promotion workflows.", System: true, Active: true},
		{ID: "worker_self", Name: "Employee self-service", Description: "Accesses personal employment information and self-service workflows.", System: true, Active: true},
	}
}

// PageJourneyDiagnostics is PROMOUX-008's authorized diagnostics disclosure:
// the raw entity refs, digests, workflow instance/node internals and
// evidence references a promotion journey's ordinary business review must
// never carry. It is a page id like any other in [DefaultPagePermissions],
// gated the same way (View), but it names a disclosure within the Journeys
// and My Work pages rather than a navigable route of its own, so it is
// deliberately absent from [PageVisible]-style navigation registries.
// hcm_admin and comp_admin already hold it through their blanket grant
// below; promotion_operator holds it explicitly because operating the
// governed promotion machinery is exactly the job diagnosing it serves.
// Every other role -- including the manager or HR partner who can approve a
// promotion -- has no grant, and CanPageAction denies by default rather
// than falling back to any other page's permission.
const PageJourneyDiagnostics = "journey-diagnostics"

// DefaultPagePermissions preserves the existing role experience while making
// it explicit and editable. The employee self-service role demonstrates the
// important read-only case: it can view Insights but cannot create, update, or
// delete reports there.
func DefaultPagePermissions() []PagePermission {
	pages := []string{"home", "myself", "journeys", "work", "history", "people", "person", "organization", "insights", "admin", "worker-ids", "roles", "organization-visibility", "appearance", "studio", "help", "settings", PageJourneyDiagnostics}
	result := make([]PagePermission, 0, len(pages)*2+64)
	grant := func(role, page string, create, update, delete bool) {
		result = append(result, PagePermission{RoleID: role, PageID: page, View: true, Create: create, Update: update, Delete: delete})
	}
	for _, role := range []string{"hcm_admin", "comp_admin"} {
		for _, page := range pages {
			grant(role, page, true, true, true)
		}
	}
	for _, role := range []string{"manager", "hr_partner", "hiring_manager", "payroll_manager"} {
		for _, page := range []string{"home", "myself", "journeys", "work", "history", "people", "person", "organization", "insights", "help", "settings"} {
			create, update := page == "journeys", page == "journeys" || page == "work" || page == "settings"
			grant(role, page, create, update, false)
		}
	}
	for _, page := range []string{"home", "myself", "organization", "insights", "help", "settings"} {
		grant("worker_self", page, false, page == "settings", false)
	}
	// PROMOUX-015: a finance partner decides the approvals routed to them
	// (My Work, update) and reviews their outcome (Work History); it reaches
	// no workforce directory, person profile or journey launcher.
	for _, page := range []string{"home", "myself", "work", "history", "organization", "help", "settings"} {
		grant("finance_partner", page, false, page == "work" || page == "settings", false)
	}
	for _, page := range []string{"home", "journeys", "work", "history", "people", "person", "organization", "insights", "help", "settings"} {
		grant("intent_author", page, page == "journeys", page == "settings", false)
		grant("promotion_operator", page, false, page == "journeys" || page == "work" || page == "settings", false)
	}
	grant("promotion_operator", PageJourneyDiagnostics, false, false, false)
	return result
}
