package journey

import (
	"context"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func (s *server) visibleWorkforce(ctx context.Context, principal *trust.Principal, workers []workspace.WorkerSummary, options workspace.WorkforceOptions) ([]workspace.WorkerSummary, workspace.WorkforceOptions, error) {
	// The administrator shortcut reads the server-side role set: a revoked
	// administrator whose durable assignment no longer holds the role lists
	// through policy like anyone else.
	roles := s.effectiveRoles(ctx, principal)
	if isAdministratorRoleSet(roles) {
		return projectAuthorizedManagers(workers, workers), options, nil
	}
	visible, visibleOptions, err := s.visibleByPolicy(ctx, principal, roles, workers, options)
	if err != nil {
		return nil, workspace.WorkforceOptions{}, err
	}
	return withManagedReports(principal, roles, workers, visible, visibleOptions)
}

// maxReportingDepth bounds one reporting-line walk, matching the engine's own
// manager-chain bound, so a malformed cycle can never make a listing unbounded.
const maxReportingDepth = 16

// withManagedReports adds, for a principal holding the manager role, every
// worker whose reporting line reaches that principal, directly or indirectly.
//
// PROMOUX-015: unit visibility alone hid a manager's own reports whenever they
// sat in a different organization unit, so a skip-level manager the governed
// read authorizes to propose a promotion (a MANAGER_CHAIN fact) could not
// discover that worker on the listing at all. The walk reads only the
// ManagerRef each listed worker already carries, never a relationship the
// listing does not hold, and a reference naming no listed worker ends it.
// withManagedReports adds, for a principal whose server-side role set holds
// the manager role, every worker whose reporting line reaches that
// principal. roles is the already-resolved set from [server.effectiveRoles],
// never the credential.
func withManagedReports(principal *trust.Principal, roles []string, all, visible []workspace.WorkerSummary, options workspace.WorkforceOptions) ([]workspace.WorkerSummary, workspace.WorkforceOptions, error) {
	subject := strings.ToLower(strings.TrimSpace(principal.Subject()))
	if !roleaccess.ContainsRole(roles, "manager") || subject == "" {
		return visible, options, nil
	}
	byRef := make(map[string]workspace.WorkerSummary, len(all)*2)
	for _, worker := range all {
		for _, key := range []string{worker.WorkerRef, worker.WorkerID} {
			if k := strings.ToLower(strings.TrimSpace(key)); k != "" {
				byRef[k] = worker
			}
		}
	}
	present := make(map[string]bool, len(visible))
	for _, worker := range visible {
		present[strings.ToLower(strings.TrimSpace(worker.WorkerRef))] = true
	}
	added := false
	for _, worker := range all {
		if present[strings.ToLower(strings.TrimSpace(worker.WorkerRef))] || !reportsTo(worker, subject, byRef) {
			continue
		}
		visible = append(visible, worker)
		present[strings.ToLower(strings.TrimSpace(worker.WorkerRef))] = true
		added = true
	}
	if !added {
		return visible, options, nil
	}
	return redactHiddenManagers(visible), visibleWorkforceOptions(options, visible), nil
}

// reportsTo reports whether subject appears on worker's reporting line.
func reportsTo(worker workspace.WorkerSummary, subject string, byRef map[string]workspace.WorkerSummary) bool {
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(worker.WorkerRef)): true}
	ref := strings.ToLower(strings.TrimSpace(worker.ManagerRef))
	for depth := 0; depth < maxReportingDepth && ref != ""; depth++ {
		if ref == subject {
			return true
		}
		manager, ok := byRef[ref]
		if !ok {
			return false
		}
		if workerMatchesPrincipal(manager, subject) {
			return true
		}
		key := strings.ToLower(strings.TrimSpace(manager.WorkerRef))
		if seen[key] {
			return false
		}
		seen[key] = true
		ref = strings.ToLower(strings.TrimSpace(manager.ManagerRef))
	}
	return false
}

// visibleByPolicy applies the role-access or personal organization-visibility
// policy that governs the listing. roles is the already-resolved
// server-side set; the fresh snapshot only re-applies it so a concurrent
// assignment change is honored, and it never reintroduces credential claims.
func (s *server) visibleByPolicy(ctx context.Context, principal *trust.Principal, roles []string, workers []workspace.WorkerSummary, options workspace.WorkforceOptions) ([]workspace.WorkerSummary, workspace.WorkforceOptions, error) {
	if s.deps.RoleAccess != nil {
		snapshot, err := s.deps.RoleAccess.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
		if err != nil {
			return nil, workspace.WorkforceOptions{}, err
		}
		if policies := roleaccess.PoliciesForRoles(snapshot, roleaccess.AssignedRoles(snapshot, principal.Subject(), roles)); len(policies) > 0 {
			visible, visibleOptions := visibleWorkforceForRolePolicies(principal, workers, options, policies)
			return projectAuthorizedManagers(workers, visible), visibleOptions, nil
		}
	}
	if s.deps.Preferences == nil {
		return projectAuthorizedManagers(workers, workers), options, nil
	}
	snapshot, err := s.deps.Preferences.Load(ctx, principal.Tenant(), principal.OrganizationScopeID(), principal.Subject())
	if err != nil {
		return nil, workspace.WorkforceOptions{}, err
	}
	if err := preferences.ValidateOrganizationVisibility(snapshot.OrganizationVisibility); err != nil {
		return nil, workspace.WorkforceOptions{}, err
	}
	policy := preferences.NormalizeOrganizationVisibility(snapshot.OrganizationVisibility)
	if policy.Mode == preferences.OrganizationVisibilityAll {
		return projectAuthorizedManagers(workers, workers), options, nil
	}

	ownUnit := ""
	for _, worker := range workers {
		if workerMatchesPrincipal(worker, principal.Subject()) {
			ownUnit = normalizedOrganizationUnit(worker.OrgUnit)
			break
		}
	}
	qualified := make(map[string]bool, len(policy.OrganizationUnits))
	for _, unit := range policy.OrganizationUnits {
		qualified[normalizedOrganizationUnit(unit)] = true
	}
	visible := make([]workspace.WorkerSummary, 0, len(workers))
	for _, worker := range workers {
		unit := normalizedOrganizationUnit(worker.OrgUnit)
		self := workerMatchesPrincipal(worker, principal.Subject())
		include := self
		switch policy.Mode {
		case preferences.OrganizationVisibilityOwnUnit:
			include = include || ownUnit != "" && unit == ownUnit
		case preferences.OrganizationVisibilityAllowlist:
			include = include || qualified[unit]
		case preferences.OrganizationVisibilityDenylist:
			include = include || !qualified[unit]
		}
		if include {
			visible = append(visible, worker)
		}
	}
	return projectAuthorizedManagers(workers, visible), visibleWorkforceOptions(options, visible), nil
}

func visibleWorkforceForRolePolicies(principal *trust.Principal, workers []workspace.WorkerSummary, options workspace.WorkforceOptions, policies []roleaccess.VisibilityPolicy) ([]workspace.WorkerSummary, workspace.WorkforceOptions) {
	ownUnit := ""
	for _, worker := range workers {
		if workerMatchesPrincipal(worker, principal.Subject()) {
			ownUnit = normalizedOrganizationUnit(worker.OrgUnit)
			break
		}
	}
	evaluator := roleaccess.NewVisibilityEvaluator(policies, ownUnit)
	visible := make([]workspace.WorkerSummary, 0, len(workers))
	for _, worker := range workers {
		if evaluator.Allows(worker.OrgUnit, workerMatchesPrincipal(worker, principal.Subject())) {
			visible = append(visible, worker)
		}
	}
	return visible, visibleWorkforceOptions(options, visible)
}

func workerMatchesPrincipal(worker workspace.WorkerSummary, subject string) bool {
	subject = strings.ToLower(strings.TrimSpace(subject))
	if subject == "" {
		return false
	}
	for _, candidate := range []string{worker.WorkerRef, worker.WorkerID, worker.WorkerNumber} {
		if strings.ToLower(strings.TrimSpace(candidate)) == subject {
			return true
		}
	}
	return false
}

func normalizedOrganizationUnit(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func redactHiddenManagers(workers []workspace.WorkerSummary) []workspace.WorkerSummary {
	return projectAuthorizedManagers(workers, workers)
}

// projectAuthorizedManagers turns the overloaded persisted relationship
// reference into a total, authorization-safe endpoint projection. It receives
// the complete governed listing before population filtering and the exact
// visible subset after filtering, so WITHHELD cannot be confused with ROOT.
// A raw reference is never copied into ManagerWorkerRef: only the stable
// WorkerRef of a uniquely resolved, visible endpoint is disclosed.
func projectAuthorizedManagers(all, visible []workspace.WorkerSummary) []workspace.WorkerSummary {
	allByIdentity := workerIdentityIndex(all)
	visibleByIdentity := workerIdentityIndex(visible)
	result := append([]workspace.WorkerSummary(nil), visible...)
	for index := range result {
		worker := &result[index]
		sourceDisposition := worker.ManagerDisposition
		sourceManagerRef := worker.ManagerWorkerRef
		legacyManagerRef := worker.ManagerRef
		worker.ManagerDisposition = workspace.ManagerRelationshipUnspecified
		worker.ManagerWorkerRef = ""
		// ManagerRef is the persisted relationship fact, not an authorized
		// worker endpoint. Never serialize it through the workforce listing;
		// VISIBLE uses ManagerWorkerRef and every other state has no endpoint.
		worker.ManagerRef = ""
		if sourceDisposition == workspace.ManagerRelationshipRoot {
			worker.ManagerDisposition = workspace.ManagerRelationshipRoot
			worker.ManagerRef = ""
			continue
		}
		if sourceDisposition == workspace.ManagerRelationshipWithheld {
			worker.ManagerDisposition = workspace.ManagerRelationshipWithheld
			worker.ManagerRef = ""
			continue
		}
		if sourceDisposition == workspace.ManagerRelationshipOrphan {
			worker.ManagerDisposition = workspace.ManagerRelationshipOrphan
			worker.ManagerRef = ""
			continue
		}
		if sourceDisposition != workspace.ManagerRelationshipVisible {
			sourceManagerRef = legacyManagerRef
		}
		managerRef := normalizedWorkerIdentity(sourceManagerRef)
		if managerRef == "" {
			// Absence in the legacy listing is not authoritative proof that a
			// worker is a root. Until the application supplies an explicit ROOT,
			// fail closed as an unattached relationship.
			worker.ManagerDisposition = workspace.ManagerRelationshipOrphan
			continue
		}
		manager, exists := allByIdentity.resolve(managerRef)
		if !exists || sameWorker(*worker, manager) {
			worker.ManagerDisposition = workspace.ManagerRelationshipOrphan
			worker.ManagerRef = ""
			continue
		}
		visibleManager, allowed := visibleByIdentity.resolve(managerRef)
		if !allowed {
			worker.ManagerDisposition = workspace.ManagerRelationshipWithheld
			worker.ManagerRef = ""
			continue
		}
		worker.ManagerDisposition = workspace.ManagerRelationshipVisible
		worker.ManagerWorkerRef = visibleManager.WorkerRef
	}
	return breakManagerCycles(result)
}

type workerIdentityEntry struct {
	worker workspace.WorkerSummary
	index  int
}

type workerIdentityLookup struct {
	unique    map[string]workerIdentityEntry
	ambiguous map[string]bool
}

func workerIdentityIndex(workers []workspace.WorkerSummary) workerIdentityLookup {
	lookup := workerIdentityLookup{
		unique:    make(map[string]workerIdentityEntry, len(workers)*2),
		ambiguous: make(map[string]bool),
	}
	for index, worker := range workers {
		// A persisted manager relationship names the manager by the stored
		// worker key, which is the subject, so the subject resolves too.
		for _, value := range []string{worker.WorkerRef, worker.WorkerID, worker.SubjectID} {
			identity := normalizedWorkerIdentity(value)
			if identity == "" || lookup.ambiguous[identity] {
				continue
			}
			if existing, found := lookup.unique[identity]; found && existing.index != index {
				delete(lookup.unique, identity)
				lookup.ambiguous[identity] = true
				continue
			}
			lookup.unique[identity] = workerIdentityEntry{worker: worker, index: index}
		}
	}
	return lookup
}

func (lookup workerIdentityLookup) resolve(identity string) (workspace.WorkerSummary, bool) {
	identity = normalizedWorkerIdentity(identity)
	if identity == "" || lookup.ambiguous[identity] {
		return workspace.WorkerSummary{}, false
	}
	entry, found := lookup.unique[identity]
	return entry.worker, found
}

// breakManagerCycles refuses to present an arbitrary reporting hierarchy when
// the authoritative graph is corrupt. Every member of a directed cycle is
// projected as an unattached relationship; reports outside the cycle may still
// attach to those now-explicit roots without inventing a parent edge.
func breakManagerCycles(workers []workspace.WorkerSummary) []workspace.WorkerSummary {
	byRef := make(map[string]int, len(workers))
	ambiguous := make(map[string]bool)
	for index, worker := range workers {
		ref := normalizedWorkerIdentity(worker.WorkerRef)
		if ref == "" || ambiguous[ref] {
			continue
		}
		if _, found := byRef[ref]; found {
			delete(byRef, ref)
			ambiguous[ref] = true
			continue
		}
		byRef[ref] = index
	}

	edge := make(map[int]int, len(workers))
	for index, worker := range workers {
		if worker.ManagerDisposition != workspace.ManagerRelationshipVisible {
			continue
		}
		if manager, found := byRef[normalizedWorkerIdentity(worker.ManagerWorkerRef)]; found {
			edge[index] = manager
		}
	}

	state := make([]uint8, len(workers))
	stack := make([]int, 0, len(workers))
	stackIndex := make(map[int]int, len(workers))
	cyclic := make(map[int]bool)
	var visit func(int)
	visit = func(index int) {
		state[index] = 1
		stackIndex[index] = len(stack)
		stack = append(stack, index)
		if manager, found := edge[index]; found {
			switch state[manager] {
			case 0:
				visit(manager)
			case 1:
				for _, member := range stack[stackIndex[manager]:] {
					cyclic[member] = true
				}
			}
		}
		stack = stack[:len(stack)-1]
		delete(stackIndex, index)
		state[index] = 2
	}
	for index := range workers {
		if state[index] == 0 {
			visit(index)
		}
	}
	for index := range cyclic {
		workers[index].ManagerDisposition = workspace.ManagerRelationshipOrphan
		workers[index].ManagerWorkerRef = ""
		workers[index].ManagerRef = ""
	}
	return workers
}

func normalizedWorkerIdentity(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func sameWorker(left, right workspace.WorkerSummary) bool {
	return normalizedWorkerIdentity(left.WorkerRef) != "" && normalizedWorkerIdentity(left.WorkerRef) == normalizedWorkerIdentity(right.WorkerRef) ||
		normalizedWorkerIdentity(left.WorkerID) != "" && normalizedWorkerIdentity(left.WorkerID) == normalizedWorkerIdentity(right.WorkerID)
}

func visibleWorkforceOptions(options workspace.WorkforceOptions, workers []workspace.WorkerSummary) workspace.WorkforceOptions {
	seen := make(map[string]string)
	for _, worker := range workers {
		if unit := strings.TrimSpace(worker.OrgUnit); unit != "" {
			seen[normalizedOrganizationUnit(unit)] = unit
		}
	}
	options.OrgUnits = make([]string, 0, len(seen))
	for _, unit := range seen {
		options.OrgUnits = append(options.OrgUnits, unit)
	}
	sort.Slice(options.OrgUnits, func(i, j int) bool {
		return strings.ToLower(options.OrgUnits[i]) < strings.ToLower(options.OrgUnits[j])
	})
	return options
}
