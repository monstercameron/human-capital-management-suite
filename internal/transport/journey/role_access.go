package journey

import (
	"context"
	"errors"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func (s *server) roleAccessStore(principal *trust.Principal, requestID string) (roleaccess.Store, error) {
	if s.deps.RoleAccess == nil {
		return nil, envelope.New(envelope.CodeUnavailable, "journey.role_access.store_unconfigured", "role administration is not configured").WithCorrelation(requestID).WithEvidence(evidence(principal))
	}
	return s.deps.RoleAccess, nil
}

func requireRoleAdministrator(principal *trust.Principal, requestID string) error {
	if principal.HasRole("hcm_admin") || principal.HasRole("comp_admin") {
		return nil
	}
	return envelope.New(envelope.CodePermissionDenied, "journey.role_access.role_required", "role administration requires the HCM administrator role").WithCorrelation(requestID).WithEvidence(evidence(principal))
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
	if err := requireRoleAdministrator(principal, inv.RequestID()); err != nil {
		return nil, err
	}
	store, err := s.roleAccessStore(principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	snapshot, err := store.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
	if err != nil {
		return nil, roleAccessError(err, principal, inv.RequestID(), "load")
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
	if err := requireRoleAdministrator(principal, inv.RequestID()); err != nil {
		return nil, err
	}
	if req.GetRole() == nil {
		return nil, roleAccessError(roleaccess.ErrInvalid, principal, inv.RequestID(), "save_role")
	}
	store, err := s.roleAccessStore(principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	role, err := store.SaveRole(ctx, principal.Tenant(), principal.Subject(), fromAccessRole(req.GetRole()))
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
	if err := requireRoleAdministrator(principal, inv.RequestID()); err != nil {
		return nil, err
	}
	if req.GetAssignment() == nil {
		return nil, roleAccessError(roleaccess.ErrInvalid, principal, inv.RequestID(), "save_assignment")
	}
	store, err := s.roleAccessStore(principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	assignment, err := store.SaveAssignment(ctx, principal.Tenant(), principal.Subject(), fromWorkerRoleAssignment(req.GetAssignment()))
	if err != nil {
		return nil, roleAccessError(err, principal, inv.RequestID(), "save_assignment")
	}
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
	if err := requireRoleAdministrator(principal, inv.RequestID()); err != nil {
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
	if err := requireRoleAdministrator(principal, inv.RequestID()); err != nil {
		return nil, err
	}
	if req.GetPermission() == nil {
		return nil, roleAccessError(roleaccess.ErrInvalid, principal, inv.RequestID(), "save_page_permission")
	}
	store, err := s.roleAccessStore(principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	permission, err := store.SavePagePermission(ctx, principal.Tenant(), principal.Subject(), fromRolePagePermission(req.GetPermission()))
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
	if err := requireRoleAdministrator(principal, inv.RequestID()); err != nil {
		return nil, err
	}
	if req.GetPermission() == nil {
		return nil, roleAccessError(roleaccess.ErrInvalid, principal, inv.RequestID(), "save_feature_permission")
	}
	store, err := s.roleAccessStore(principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	permission, err := store.SaveFeaturePermission(ctx, principal.Tenant(), principal.Subject(), fromRoleFeaturePermission(req.GetPermission()))
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
