package roleaccess

import (
	"sort"
	"strings"
)

// administratorRoles are the roles the workforce directory admits to every
// organization unit before any role visibility policy runs
// (internal/transport/journey visibleWorkforce). A visibility policy saved
// for one of them is stored but never narrows what its holders see, so an
// access preview must report that override instead of the policy's scope.
var administratorRoles = map[string]bool{"hcm_admin": true, "comp_admin": true}

// IsAdministratorRole reports whether roleID carries the directory-wide
// administrator override.
func IsAdministratorRole(roleID string) bool {
	return administratorRoles[strings.ToLower(strings.TrimSpace(roleID))]
}

// PreviewMember is the minimum the preview needs about one person in the
// governed workforce: the identities a saved assignment may name and the
// organization unit they belong to. Members are inputs only; a resolved
// preview carries unit names and counts, never a member.
type PreviewMember struct {
	Refs []string
	Unit string
}

// PreviewScope is what one visibility policy reveals for a role.
type PreviewScope struct {
	// Mode is the normalized policy mode (ALL, OWN_UNIT, ALLOWLIST,
	// DENYLIST).
	Mode string
	// Units are the organization units the policy reveals, sorted
	// case-insensitively. For OWN_UNIT they are the union of the saved
	// holders' own units.
	Units []string
	// Relative is true when the scope depends on each viewer's own unit
	// (OWN_UNIT), so Units is a union rather than a single boundary.
	Relative bool
}

// AccessPreview is the server-resolved effective-access summary for one
// role visibility change (UXSCAN-008, REV-093-01). It is derived only from
// the durable role store and the governed workforce's unit names.
type AccessPreview struct {
	RoleID   string
	RoleName string
	// ExplicitRoles is the role being changed.
	ExplicitRoles []string
	// InheritedRoles are the other active roles the role's saved holders
	// also carry. Roles are additive, so those holders keep whatever these
	// roles reveal regardless of this change.
	InheritedRoles []string
	// AdministratorOverride is true when the role itself is an
	// administrator role: its holders see every unit before any visibility
	// policy runs, so current and proposed scope are both unrestricted.
	AdministratorOverride bool
	Current               PreviewScope
	Proposed              PreviewScope
	AddedUnits            []string
	RemovedUnits          []string
	// HolderCount counts people with a saved assignment to the role.
	// People who hold it only through their sign-in credential are not
	// counted: the store has no record of them until an assignment is saved.
	HolderCount int
	// OverriddenHolderCount counts holders who also hold an administrator
	// role and therefore keep access to every unit whatever this role's
	// policy says.
	OverriddenHolderCount int
}

// ResolveAccessPreview resolves current versus proposed visibility for the
// role named by proposed.RoleID. It fails with ErrInvalid when the proposal
// is malformed or names an unknown or inactive role. It reads nothing but
// its arguments.
func ResolveAccessPreview(snapshot Snapshot, proposed VisibilityPolicy, members []PreviewMember) (AccessPreview, error) {
	proposed = NormalizeVisibility(proposed)
	if ValidateVisibility(proposed) != nil {
		return AccessPreview{}, ErrInvalid
	}
	var role Role
	found := false
	active := make(map[string]bool, len(snapshot.Roles))
	for _, candidate := range snapshot.Roles {
		candidate = NormalizeRole(candidate)
		if candidate.Active {
			active[candidate.ID] = true
		}
		if candidate.ID == proposed.RoleID {
			role, found = candidate, true
		}
	}
	if !found || !role.Active {
		return AccessPreview{}, ErrInvalid
	}

	current := VisibilityPolicy{RoleID: role.ID, Mode: VisibilityOwnUnit}
	for _, policy := range snapshot.Policies {
		policy = NormalizeVisibility(policy)
		if policy.RoleID == role.ID && ValidateVisibility(policy) == nil {
			current = policy
		}
	}

	unitByRef := make(map[string]string, len(members)*2)
	allUnits := make(map[string]string)
	for _, member := range members {
		unit := strings.TrimSpace(member.Unit)
		if unit != "" {
			if _, seen := allUnits[normalizedUnit(unit)]; !seen {
				allUnits[normalizedUnit(unit)] = unit
			}
		}
		for _, ref := range member.Refs {
			if key := strings.ToLower(strings.TrimSpace(ref)); key != "" {
				unitByRef[key] = unit
			}
		}
	}

	preview := AccessPreview{
		RoleID: role.ID, RoleName: role.Name, ExplicitRoles: []string{role.ID},
		AdministratorOverride: IsAdministratorRole(role.ID),
	}
	holderUnits := make(map[string]string)
	inherited := make(map[string]bool)
	for _, assignment := range snapshot.Assignments {
		assignment = NormalizeAssignment(assignment)
		roles := make(map[string]bool, len(assignment.RoleIDs))
		for _, id := range assignment.RoleIDs {
			roles[id] = true
		}
		if !roles[role.ID] {
			continue
		}
		preview.HolderCount++
		overridden := preview.AdministratorOverride
		for id := range roles {
			if id == role.ID || !active[id] {
				continue
			}
			inherited[id] = true
			if IsAdministratorRole(id) {
				overridden = true
			}
		}
		if overridden {
			preview.OverriddenHolderCount++
		}
		if unit, ok := unitByRef[strings.ToLower(assignment.WorkerRef)]; ok && strings.TrimSpace(unit) != "" {
			holderUnits[normalizedUnit(unit)] = strings.TrimSpace(unit)
		}
	}
	for id := range inherited {
		preview.InheritedRoles = append(preview.InheritedRoles, id)
	}
	sort.Strings(preview.InheritedRoles)

	preview.Current = resolvePreviewScope(current, preview.AdministratorOverride, allUnits, holderUnits)
	preview.Proposed = resolvePreviewScope(proposed, preview.AdministratorOverride, allUnits, holderUnits)
	preview.AddedUnits = unitDifference(preview.Proposed.Units, preview.Current.Units)
	preview.RemovedUnits = unitDifference(preview.Current.Units, preview.Proposed.Units)
	return preview, nil
}

// resolvePreviewScope evaluates one policy with the same VisibilityEvaluator
// the live directory uses. An administrator role is resolved to every unit,
// matching the directory's override rather than the stored policy.
func resolvePreviewScope(policy VisibilityPolicy, override bool, allUnits, holderUnits map[string]string) PreviewScope {
	scope := PreviewScope{Mode: policy.Mode, Relative: !override && policy.Mode == VisibilityOwnUnit}
	visible := make(map[string]bool, len(allUnits))
	if override {
		for key := range allUnits {
			visible[key] = true
		}
	} else {
		viewers := make([]string, 0, len(holderUnits))
		for key := range holderUnits {
			viewers = append(viewers, key)
		}
		if len(viewers) == 0 {
			viewers = append(viewers, "")
		}
		for _, own := range viewers {
			evaluator := NewVisibilityEvaluator([]VisibilityPolicy{policy}, own)
			for key, unit := range allUnits {
				if evaluator.Allows(unit, false) {
					visible[key] = true
				}
			}
		}
	}
	for key := range visible {
		scope.Units = append(scope.Units, allUnits[key])
	}
	sortUnitsFold(scope.Units)
	return scope
}

func unitDifference(left, right []string) []string {
	have := make(map[string]bool, len(right))
	for _, unit := range right {
		have[normalizedUnit(unit)] = true
	}
	var out []string
	for _, unit := range left {
		if !have[normalizedUnit(unit)] {
			out = append(out, unit)
		}
	}
	sortUnitsFold(out)
	return out
}

func sortUnitsFold(units []string) {
	sort.Slice(units, func(i, j int) bool {
		left, right := strings.ToLower(units[i]), strings.ToLower(units[j])
		if left == right {
			return units[i] < units[j]
		}
		return left < right
	})
}
