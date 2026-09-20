package journey

import (
	"context"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
)

// PreviewRoleAccess answers what a role would reveal under a proposed
// visibility policy compared with its saved one (REV-093-01). It is
// authorized exactly like the visibility write it previews, reads the
// durable role store and the governed workforce, and hands both to
// roleaccess.ResolveAccessPreview. The answer carries unit names and counts
// only; no worker record crosses the wire. READ_ONLY.
func (s *server) PreviewRoleAccess(ctx context.Context, req *journeyv1.PreviewRoleAccessRequest) (*journeyv1.PreviewRoleAccessResponse, error) {
	principal, inv, ctxErr := trustedContext(ctx)
	if ctxErr != nil {
		return nil, ctxErr
	}
	if err := s.requirePageAction(ctx, principal, inv, "organization-visibility", roleaccess.ActionView); err != nil {
		return nil, err
	}
	if err := s.requireRoleAdministrator(ctx, principal, inv.RequestID()); err != nil {
		return nil, err
	}
	if req.GetProposed() == nil {
		return nil, roleAccessError(roleaccess.ErrInvalid, principal, inv.RequestID(), "preview")
	}
	store, err := s.roleAccessStore(principal, inv.RequestID())
	if err != nil {
		return nil, err
	}
	eng, depErr := s.engine(principal, inv, "preview_role_access")
	if depErr != nil {
		return nil, depErr
	}
	snapshot, err := store.Load(ctx, principal.Tenant(), principal.OrganizationScopeID())
	if err != nil {
		return nil, roleAccessError(err, principal, inv.RequestID(), "preview")
	}
	workers, _, err := eng.ListWorkers(ctx)
	if err != nil {
		return nil, ownedError(err, principal, inv, "preview_role_access")
	}
	members := make([]roleaccess.PreviewMember, 0, len(workers))
	for _, worker := range workers {
		members = append(members, roleaccess.PreviewMember{Refs: []string{worker.WorkerRef, worker.WorkerID, worker.WorkerNumber}, Unit: worker.OrgUnit})
	}
	preview, err := roleaccess.ResolveAccessPreview(snapshot, fromRoleVisibility(req.GetProposed()), members)
	if err != nil {
		return nil, roleAccessError(err, principal, inv.RequestID(), "preview")
	}
	return toRoleAccessPreview(preview), nil
}

func toRoleAccessPreview(preview roleaccess.AccessPreview) *journeyv1.PreviewRoleAccessResponse {
	return &journeyv1.PreviewRoleAccessResponse{
		RoleId: preview.RoleID, RoleName: preview.RoleName,
		ExplicitRoles:         append([]string(nil), preview.ExplicitRoles...),
		InheritedRoles:        append([]string(nil), preview.InheritedRoles...),
		AdministratorOverride: preview.AdministratorOverride,
		Current:               toRoleAccessScope(preview.Current),
		Proposed:              toRoleAccessScope(preview.Proposed),
		AddedUnits:            append([]string(nil), preview.AddedUnits...),
		RemovedUnits:          append([]string(nil), preview.RemovedUnits...),
		HolderCount:           int32(preview.HolderCount),
		OverriddenHolderCount: int32(preview.OverriddenHolderCount),
	}
}

func toRoleAccessScope(scope roleaccess.PreviewScope) *journeyv1.RoleAccessScope {
	return &journeyv1.RoleAccessScope{Mode: scope.Mode, OrganizationUnits: append([]string(nil), scope.Units...), Relative: scope.Relative}
}
