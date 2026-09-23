package journey

import (
	"context"
	"errors"
	"strings"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	adminpolicy "github.com/monstercameron/human-capital-management-suite/internal/operations/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func (s *server) roleAccessStore(principal *trust.Principal, requestID string) (roleaccess.Store, error) {
	if s.deps.RoleAccess == nil {
		return nil, envelope.New(envelope.CodeUnavailable, "journey.role_access.store_unconfigured", "role administration is not configured").WithCorrelation(requestID).WithEvidence(evidence(principal))
	}
	return s.deps.RoleAccess, nil
}

// roleSnapshot loads the durable role-access snapshot every role
// administration decision reads, failing closed when the store is missing
// or unreadable.
func (s *server) roleSnapshot(ctx context.Context, principal *trust.Principal, requestID string) (roleaccess.Snapshot, error) {
	store, err := s.roleAccessStore(principal, requestID)
	if err != nil {
		return roleaccess.Snapshot{}, err
	}
	snapshot, err := store.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
	if err != nil {
		return roleaccess.Snapshot{}, roleAccessError(err, principal, requestID, "load")
	}
	return snapshot, nil
}

// snapshotAdministers reports whether roles may administer roles: the
// stored roles page must grant update through an active role. Authority
// comes from durable rows, never from credential claims, so a custom role
// granted the roles page administers while a revoked administrator whose
// durable assignment lost it does not. A principal with no durable
// assignment resolves through the admitted credential roles, the rollout
// fallback RBAC-RT-002 keeps.
//
// Separation of duties (RBAC-RT-009): the platform operator duty and the
// role administration duty never mix in one call. A principal acting under
// operator authority ([adminpolicy.OperatorRole] in the resolved set) is
// refused role administration even when the same set also names an
// administrator grant: operator authority is granted only through durable,
// reviewable operator bindings ([adminpolicy.AuthorizeOperator]), never
// through this gate, so holding both duties at once authorizes neither
// here. The refusal carries the same code and reason as any other
// non-administrator: the wire does not distinguish why administration was
// refused.
func snapshotAdministers(snapshot roleaccess.Snapshot, roles []string) bool {
	if roleaccess.ContainsRole(roles, adminpolicy.OperatorRole) {
		return false
	}
	return roleaccess.CanPageAction(roleaccess.EffectivePagePermissions(snapshot, roles), "roles", roleaccess.ActionUpdate)
}

func roleRequiredError(principal *trust.Principal, requestID string) error {
	return envelope.New(envelope.CodePermissionDenied, "journey.role_access.role_required", "role administration requires the stored roles administration grant").WithCorrelation(requestID).WithEvidence(evidence(principal))
}

func (s *server) requireRoleAdministrator(ctx context.Context, principal *trust.Principal, requestID string) error {
	snapshot, err := s.roleSnapshot(ctx, principal, requestID)
	if err != nil {
		return err
	}
	if !snapshotAdministers(snapshot, s.effectiveRoles(ctx, principal)) {
		return roleRequiredError(principal, requestID)
	}
	return nil
}

// isSelfRoleChange reports whether workerRef names the caller. A principal
// cannot change their own assignment, not even to grant themselves
// administration: self-escalation is refused before the store is touched.
func isSelfRoleChange(principal *trust.Principal, workerRef string) bool {
	return strings.EqualFold(strings.TrimSpace(principal.Subject()), strings.TrimSpace(workerRef))
}

func selfAssignmentError(principal *trust.Principal, requestID string) error {
	return envelope.New(envelope.CodePermissionDenied, "journey.role_access.self_assignment", "a principal cannot change their own role assignment").WithCorrelation(requestID).WithEvidence(evidence(principal))
}

// grantedPageActions lists the page actions a saved page permission turns on.
func grantedPageActions(permission roleaccess.PagePermission) []string {
	var actions []string
	if permission.View {
		actions = append(actions, roleaccess.ActionView)
	}
	if permission.Create {
		actions = append(actions, roleaccess.ActionCreate)
	}
	if permission.Update {
		actions = append(actions, roleaccess.ActionUpdate)
	}
	if permission.Delete {
		actions = append(actions, roleaccess.ActionDelete)
	}
	return actions
}

// isAdministrationGrant reports whether pageID is the roles page whose
// grants are the administration model itself.
func isAdministrationGrant(pageID string) bool {
	return strings.EqualFold(strings.TrimSpace(pageID), "roles")
}

// pageGrantExceedsHolding reports whether saving requested would grant role
// actions the caller does not hold themselves. The check scopes to the
// roles page: the administration duty already requires the caller to hold
// the roles page update, so a page-level roles grant is always held, while
// ordinary page grants stay the super-administrator latitude the committed
// round-trip suite pins (an administrator holding the duty may grant a
// workforce view they never read themselves).
func pageGrantExceedsHolding(permissions []roleaccess.PagePermission, requested roleaccess.PagePermission) bool {
	if !isAdministrationGrant(requested.PageID) {
		return false
	}
	for _, action := range grantedPageActions(requested) {
		if !roleaccess.CanPageAction(permissions, requested.PageID, action) {
			return true
		}
	}
	return false
}

// featureGrantExceedsHolding reports whether saving requested would grant
// roles-page feature actions the caller does not hold themselves: the
// fine-grained administration powers (role assignments, page and feature
// access, the catalog) cannot be conferred by an administrator who lacks
// them. Grants on other pages skip the check for the same super-admin
// latitude the page check documents.
func featureGrantExceedsHolding(permissions []roleaccess.PagePermission, features []roleaccess.FeaturePermission, requested roleaccess.FeaturePermission) bool {
	if !isAdministrationGrant(requested.PageID) {
		return false
	}
	for _, action := range grantedPageActions(roleaccess.PagePermission{View: requested.View, Create: requested.Create, Update: requested.Update, Delete: requested.Delete}) {
		if !roleaccess.CanFeatureAction(permissions, features, requested.PageID, requested.FeatureID, action) {
			return true
		}
	}
	return false
}

func grantExceedsHoldingError(principal *trust.Principal, requestID string) error {
	return envelope.New(envelope.CodePermissionDenied, "journey.role_access.grant_exceeds_holding", "a grant cannot confer actions its author does not hold").WithCorrelation(requestID).WithEvidence(evidence(principal))
}

// roleAdministrators lists the roles holding the roles page update grant
// through active roles. Grants decide, never headcount: with no such role
// left, no principal could administer roles afterwards.
func roleAdministrators(snapshot roleaccess.Snapshot) []string {
	seen := map[string]bool{}
	var result []string
	for _, permission := range snapshot.PagePermissions {
		roleID := roleaccess.NormalizeRoleIDs([]string{permission.RoleID})
		if len(roleID) == 0 || seen[roleID[0]] {
			continue
		}
		seen[roleID[0]] = true
		if roleaccess.CanPageAction(roleaccess.EffectivePagePermissions(snapshot, []string{roleID[0]}), "roles", roleaccess.ActionUpdate) {
			result = append(result, roleID[0])
		}
	}
	return result
}

// withPagePermission returns the snapshot with requested saved over the
// matching page row, the way the store would persist it.
func withPagePermission(snapshot roleaccess.Snapshot, requested roleaccess.PagePermission) roleaccess.Snapshot {
	requested = roleaccess.NormalizePagePermission(requested)
	result := snapshot
	result.PagePermissions = append([]roleaccess.PagePermission(nil), snapshot.PagePermissions...)
	replaced := false
	for i, current := range result.PagePermissions {
		current = roleaccess.NormalizePagePermission(current)
		if current.RoleID == requested.RoleID && current.PageID == requested.PageID {
			result.PagePermissions[i] = requested
			replaced = true
		}
	}
	if !replaced {
		result.PagePermissions = append(result.PagePermissions, requested)
	}
	return result
}

// withRoleActive returns the snapshot with roleID's active flag set the way
// the store would persist a role save. A role the snapshot does not know is
// appended: a brand-new role carries no grants, so it never affects the
// administrator count either way.
func withRoleActive(snapshot roleaccess.Snapshot, role roleaccess.Role) roleaccess.Snapshot {
	result := snapshot
	result.Roles = append([]roleaccess.Role(nil), snapshot.Roles...)
	for i, current := range result.Roles {
		if strings.EqualFold(strings.TrimSpace(current.ID), strings.TrimSpace(role.ID)) {
			result.Roles[i].Active = role.Active
			return result
		}
	}
	result.Roles = append(result.Roles, roleaccess.Role{ID: role.ID, Name: role.Name, Description: role.Description, System: role.System, Active: role.Active})
	return result
}

func lastAdministratorError(principal *trust.Principal, requestID string) error {
	return envelope.New(envelope.CodePermissionDenied, "journey.role_access.last_administrator", "the change would leave no role administrator").WithCorrelation(requestID).WithEvidence(evidence(principal))
}

func roleAccessError(err error, principal *trust.Principal, requestID, operation string) error {
	code, reason := envelope.CodeUnspecified, "failed"
	switch {
	case errors.Is(err, roleaccess.ErrVersionConflict):
		code, reason = envelope.CodeAborted, "version_conflict"
	case errors.Is(err, roleaccess.ErrInvalid):
		code, reason = envelope.CodeInvalidArgument, "invalid"
	case errors.Is(err, roleaccess.ErrUnavailable):
		code, reason = envelope.CodeUnavailable, "unavailable"
	}
	return envelope.New(code, "journey.role_access."+operation+"."+reason, "the role administration operation could not be completed").WithCorrelation(requestID).WithEvidence(evidence(principal))
}

func (s *server) GetRoleAccess(ctx context.Context, _ *journeyv1.GetRoleAccessRequest) (*journeyv1.GetRoleAccessResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	snapshot, err := s.roleSnapshot(ctx, principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	// The gates read the same snapshot the answer is built from, so one
	// load serves all three and the table pair stays authoritative.
	if err := s.requireSnapshotServedCall(snapshot, principal, inv, "GetRoleAccess"); err != nil {
		return nil, err
	}
	if !snapshotAdministers(snapshot, s.effectiveRoles(ctx, principal)) {
		return nil, roleRequiredError(principal, inv.RequestID())
	}
	response := &journeyv1.GetRoleAccessResponse{}
	for _, role := range snapshot.Roles {
		response.Roles = append(response.Roles, toAccessRole(role))
	}
	for _, assignment := range snapshot.Assignments {
		response.Assignments = append(response.Assignments, toWorkerRoleAssignment(assignment))
	}
	for _, policy := range snapshot.Policies {
		response.VisibilityPolicies = append(response.VisibilityPolicies, toRoleVisibility(policy))
	}
	for _, permission := range snapshot.PagePermissions {
		response.PagePermissions = append(response.PagePermissions, toRolePagePermission(permission))
	}
	for _, permission := range snapshot.FeaturePermissions {
		response.FeaturePermissions = append(response.FeaturePermissions, toRoleFeaturePermission(permission))
	}
	return response, nil
}

func (s *server) SaveAccessRole(ctx context.Context, req *journeyv1.SaveAccessRoleRequest) (*journeyv1.SaveAccessRoleResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	action := roleaccess.ActionCreate
	if req.GetRole().GetVersion() > 0 {
		action = roleaccess.ActionUpdate
	}
	if err := s.requirePageAction(ctx, principal, inv, "roles", action); err != nil {
		return nil, err
	}
	if req.GetRole() == nil {
		return nil, roleAccessError(roleaccess.ErrInvalid, principal, inv.RequestID(), "save_role")
	}
	requested := fromAccessRole(req.GetRole())
	snapshot, err := s.roleSnapshot(ctx, principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	if !snapshotAdministers(snapshot, s.effectiveRoles(ctx, principal)) {
		return nil, roleRequiredError(principal, inv.RequestID())
	}
	// Deactivating the last grant-bearing role locks every principal out
	// of role administration, including the caller. Renames and system
	// role protection stay with RBAC-RT-017.
	if len(roleAdministrators(withRoleActive(snapshot, requested))) == 0 {
		return nil, lastAdministratorError(principal, inv.RequestID())
	}
	store, err := s.roleAccessStore(principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	role, err := store.SaveRole(ctx, principal.Tenant(), principal.Subject(), requested)
	if err != nil {
		return nil, roleAccessError(err, principal, inv.RequestID(), "save_role")
	}
	return &journeyv1.SaveAccessRoleResponse{Role: toAccessRole(role)}, nil
}

func (s *server) SaveWorkerRoleAssignment(ctx context.Context, req *journeyv1.SaveWorkerRoleAssignmentRequest) (*journeyv1.SaveWorkerRoleAssignmentResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requirePageAction(ctx, principal, inv, "roles", roleaccess.ActionUpdate); err != nil {
		return nil, err
	}
	if err := s.requireRoleAdministrator(ctx, principal, inv.RequestID()); err != nil {
		return nil, err
	}
	if req.GetAssignment() == nil {
		return nil, roleAccessError(roleaccess.ErrInvalid, principal, inv.RequestID(), "save_assignment")
	}
	if isSelfRoleChange(principal, req.GetAssignment().GetWorkerRef()) {
		return nil, selfAssignmentError(principal, inv.RequestID())
	}
	store, err := s.roleAccessStore(principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	assignment, err := store.SaveAssignment(ctx, principal.Tenant(), principal.Subject(), fromWorkerRoleAssignment(req.GetAssignment()))
	if err != nil {
		return nil, roleAccessError(err, principal, inv.RequestID(), "save_assignment")
	}
	// The subject's durable assignment just changed: drop the cached role
	// set so the next check resolves the new one.
	s.roleResolver().Invalidate(principal.Tenant(), assignment.WorkerRef)
	return &journeyv1.SaveWorkerRoleAssignmentResponse{Assignment: toWorkerRoleAssignment(assignment)}, nil
}

func (s *server) SaveRoleOrganizationVisibility(ctx context.Context, req *journeyv1.SaveRoleOrganizationVisibilityRequest) (*journeyv1.SaveRoleOrganizationVisibilityResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requirePageAction(ctx, principal, inv, "organization-visibility", roleaccess.ActionUpdate); err != nil {
		return nil, err
	}
	if err := s.requireRoleAdministrator(ctx, principal, inv.RequestID()); err != nil {
		return nil, err
	}
	if req.GetPolicy() == nil {
		return nil, roleAccessError(roleaccess.ErrInvalid, principal, inv.RequestID(), "save_visibility")
	}
	store, err := s.roleAccessStore(principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	policy, err := store.SaveVisibility(ctx, principal.Tenant(), principal.OrganizationScopeID(), principal.Subject(), fromRoleVisibility(req.GetPolicy()))
	if err != nil {
		return nil, roleAccessError(err, principal, inv.RequestID(), "save_visibility")
	}
	return &journeyv1.SaveRoleOrganizationVisibilityResponse{Policy: toRoleVisibility(policy)}, nil
}

func (s *server) SaveRolePagePermission(ctx context.Context, req *journeyv1.SaveRolePagePermissionRequest) (*journeyv1.SaveRolePagePermissionResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requirePageAction(ctx, principal, inv, "roles", roleaccess.ActionUpdate); err != nil {
		return nil, err
	}
	if req.GetPermission() == nil {
		return nil, roleAccessError(roleaccess.ErrInvalid, principal, inv.RequestID(), "save_page_permission")
	}
	requested := fromRolePagePermission(req.GetPermission())
	snapshot, err := s.roleSnapshot(ctx, principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	callerRoles := s.effectiveRoles(ctx, principal)
	if !snapshotAdministers(snapshot, callerRoles) {
		return nil, roleRequiredError(principal, inv.RequestID())
	}
	if pageGrantExceedsHolding(roleaccess.EffectivePagePermissions(snapshot, callerRoles), requested) {
		return nil, grantExceedsHoldingError(principal, inv.RequestID())
	}
	if len(roleAdministrators(withPagePermission(snapshot, requested))) == 0 {
		return nil, lastAdministratorError(principal, inv.RequestID())
	}
	store, err := s.roleAccessStore(principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	permission, err := store.SavePagePermission(ctx, principal.Tenant(), principal.Subject(), requested)
	if err != nil {
		return nil, roleAccessError(err, principal, inv.RequestID(), "save_page_permission")
	}
	return &journeyv1.SaveRolePagePermissionResponse{Permission: toRolePagePermission(permission)}, nil
}

func (s *server) SaveRoleFeaturePermission(ctx context.Context, req *journeyv1.SaveRoleFeaturePermissionRequest) (*journeyv1.SaveRoleFeaturePermissionResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requireFeatureAction(ctx, principal, inv, "roles", "feature_access", roleaccess.ActionUpdate); err != nil {
		return nil, err
	}
	if req.GetPermission() == nil {
		return nil, roleAccessError(roleaccess.ErrInvalid, principal, inv.RequestID(), "save_feature_permission")
	}
	requested := fromRoleFeaturePermission(req.GetPermission())
	snapshot, err := s.roleSnapshot(ctx, principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	callerRoles := s.effectiveRoles(ctx, principal)
	if !snapshotAdministers(snapshot, callerRoles) {
		return nil, roleRequiredError(principal, inv.RequestID())
	}
	// Feature rows never carry the roles page grant the duty reads, so no
	// lockout simulation applies here; the holding check is the whole guard.
	if featureGrantExceedsHolding(
		roleaccess.EffectivePagePermissions(snapshot, callerRoles),
		roleaccess.EffectiveFeaturePermissions(snapshot, callerRoles),
		requested,
	) {
		return nil, grantExceedsHoldingError(principal, inv.RequestID())
	}
	store, err := s.roleAccessStore(principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	permission, err := store.SaveFeaturePermission(ctx, principal.Tenant(), principal.Subject(), requested)
	if err != nil {
		return nil, roleAccessError(err, principal, inv.RequestID(), "save_feature_permission")
	}
	return &journeyv1.SaveRoleFeaturePermissionResponse{Permission: toRoleFeaturePermission(permission)}, nil
}

func toAccessRole(value roleaccess.Role) *journeyv1.AccessRole {
	return &journeyv1.AccessRole{Version: value.Version, RoleId: value.ID, Name: value.Name, Description: value.Description, System: value.System, Active: value.Active}
}

func fromAccessRole(value *journeyv1.AccessRole) roleaccess.Role {
	return roleaccess.Role{Version: value.GetVersion(), ID: value.GetRoleId(), Name: value.GetName(), Description: value.GetDescription(), System: value.GetSystem(), Active: value.GetActive()}
}

func toWorkerRoleAssignment(value roleaccess.Assignment) *journeyv1.WorkerRoleAssignment {
	return &journeyv1.WorkerRoleAssignment{Version: value.Version, WorkerRef: value.WorkerRef, RoleIds: append([]string(nil), value.RoleIDs...)}
}

func fromWorkerRoleAssignment(value *journeyv1.WorkerRoleAssignment) roleaccess.Assignment {
	return roleaccess.Assignment{Version: value.GetVersion(), WorkerRef: value.GetWorkerRef(), RoleIDs: append([]string(nil), value.GetRoleIds()...)}
}

func toRoleVisibility(value roleaccess.VisibilityPolicy) *journeyv1.RoleOrganizationVisibilityPolicy {
	return &journeyv1.RoleOrganizationVisibilityPolicy{Version: value.Version, RoleId: value.RoleID, Mode: value.Mode, OrganizationUnits: append([]string(nil), value.OrganizationUnits...)}
}

func fromRoleVisibility(value *journeyv1.RoleOrganizationVisibilityPolicy) roleaccess.VisibilityPolicy {
	return roleaccess.VisibilityPolicy{Version: value.GetVersion(), RoleID: value.GetRoleId(), Mode: value.GetMode(), OrganizationUnits: append([]string(nil), value.GetOrganizationUnits()...)}
}

func toRolePagePermission(value roleaccess.PagePermission) *journeyv1.RolePagePermission {
	return &journeyv1.RolePagePermission{Version: value.Version, RoleId: value.RoleID, PageId: value.PageID, CanView: value.View, CanCreate: value.Create, CanUpdate: value.Update, CanDelete: value.Delete}
}

func fromRolePagePermission(value *journeyv1.RolePagePermission) roleaccess.PagePermission {
	return roleaccess.PagePermission{Version: value.GetVersion(), RoleID: value.GetRoleId(), PageID: value.GetPageId(), View: value.GetCanView(), Create: value.GetCanCreate(), Update: value.GetCanUpdate(), Delete: value.GetCanDelete()}
}

func toRoleFeaturePermission(value roleaccess.FeaturePermission) *journeyv1.RoleFeaturePermission {
	return &journeyv1.RoleFeaturePermission{Version: value.Version, RoleId: value.RoleID, PageId: value.PageID, FeatureId: value.FeatureID, CanView: value.View, CanCreate: value.Create, CanUpdate: value.Update, CanDelete: value.Delete}
}

func fromRoleFeaturePermission(value *journeyv1.RoleFeaturePermission) roleaccess.FeaturePermission {
	return roleaccess.FeaturePermission{Version: value.GetVersion(), RoleID: value.GetRoleId(), PageID: value.GetPageId(), FeatureID: value.GetFeatureId(), View: value.GetCanView(), Create: value.GetCanCreate(), Update: value.GetCanUpdate(), Delete: value.GetCanDelete()}
}
