package roleaccess

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var ErrManagementRevision = errors.New("roleaccess: configuration revision precondition failed")

// Management adds explicit compare-and-set and retirement semantics to the
// persistence-neutral role access port. The port remains responsible for the
// durable append and tenant isolation.
type Management struct{ Store Store }

func (m Management) UpdateRole(ctx context.Context, tenant values.TenantId, scope string, role Role, ifMatch int64) (Role, error) {
	if m.Store == nil || ifMatch <= 0 {
		return Role{}, ErrManagementRevision
	}
	snapshot, err := m.Store.Load(ctx, tenant, scope)
	if err != nil {
		return Role{}, err
	}
	var existing Role
	found := false
	for _, candidate := range snapshot.Roles {
		if candidate.ID == role.ID {
			existing, found = candidate, true
			break
		}
	}
	if !found || existing.Version != ifMatch {
		return Role{}, ErrManagementRevision
	}
	if err := ValidateRoleUpdate(existing, role); err != nil {
		return Role{}, err
	}
	role.Version = ifMatch + 1
	return m.Store.SaveRole(ctx, tenant, scope, role)
}

func (m Management) RetireRole(ctx context.Context, tenant values.TenantId, scope, roleID string, ifMatch int64) (Role, error) {
	if m.Store == nil || ifMatch <= 0 {
		return Role{}, ErrManagementRevision
	}
	snapshot, err := m.Store.Load(ctx, tenant, scope)
	if err != nil {
		return Role{}, err
	}
	for _, role := range snapshot.Roles {
		if role.ID == roleID {
			if role.Version != ifMatch || role.System {
				return Role{}, ErrManagementRevision
			}
			role.Active, role.Version = false, ifMatch+1
			return m.Store.SaveRole(ctx, tenant, scope, role)
		}
	}
	return Role{}, ErrManagementRevision
}

func (m Management) UpdateAssignment(ctx context.Context, tenant values.TenantId, scope string, assignment Assignment, ifMatch int64) (Assignment, error) {
	if m.Store == nil || ifMatch <= 0 {
		return Assignment{}, ErrManagementRevision
	}
	snapshot, err := m.Store.Load(ctx, tenant, scope)
	if err != nil {
		return Assignment{}, err
	}
	for _, existing := range snapshot.Assignments {
		if existing.WorkerRef == assignment.WorkerRef {
			if existing.Version != ifMatch {
				return Assignment{}, ErrManagementRevision
			}
			assignment.Version = ifMatch + 1
			return m.Store.SaveAssignment(ctx, tenant, scope, assignment)
		}
	}
	return Assignment{}, ErrManagementRevision
}
