// Package roleaccessstore persists tenant roles, employee role assignments,
// and role-scoped organization visibility behind tenant-isolated transactions.
package roleaccessstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type Store struct {
	db     dbport.Beginner
	tenant func(values.TenantId) uuid.UUID
}

func New(db dbport.Beginner, tenant func(values.TenantId) uuid.UUID) *Store {
	return &Store{db: db, tenant: tenant}
}

func (s *Store) Bootstrap(ctx context.Context, tenant values.TenantId, actor string) error {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system:bootstrap"
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		for _, role := range roleaccess.DefaultRoles() {
			_, err := tx.Exec(ctx, `INSERT INTO access_role (tenant_id,role_id,version,name,description,system_role,active,updated_by) VALUES ($1,$2,1,$3,$4,true,true,$5) ON CONFLICT DO NOTHING`, tenantID, role.ID, role.Name, role.Description, actor)
			if err != nil {
				return fmt.Errorf("roleaccessstore: bootstrap %s: %w", role.ID, err)
			}
		}
		for _, permission := range roleaccess.DefaultPagePermissions() {
			_, err := tx.Exec(ctx, `INSERT INTO role_page_permission (tenant_id,role_id,page_id,version,can_view,can_create,can_update,can_delete,updated_by) VALUES ($1,$2,$3,1,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, tenantID, permission.RoleID, permission.PageID, permission.View, permission.Create, permission.Update, permission.Delete, actor)
			if err != nil {
				return fmt.Errorf("roleaccessstore: bootstrap page %s for %s: %w", permission.PageID, permission.RoleID, err)
			}
		}
		return nil
	})
}

// BootstrapLocalDevPersonaPermissions narrows only untouched version-one
// HarborCare demo grants. The payroll persona reviews assigned promotion work
// but does not initiate requests or browse the organization tree. A grant an
// administrator has edited (and therefore versioned) is never overwritten.
// This is called only by the local-dev composition, never for production.
func (s *Store) BootstrapLocalDevPersonaPermissions(ctx context.Context, tenant values.TenantId) error {
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		for _, roleID := range []string{"payroll_manager", "promotion_operator"} {
			for _, pageID := range []string{"organization", "org-explorer", "org-outline", "org-responsive"} {
				if _, err := tx.Exec(ctx, `UPDATE role_page_permission SET can_view=false,version=version+1,updated_by='system:local-dev-personas',updated_at=clock_timestamp()
					WHERE tenant_id=$1 AND role_id=$2 AND page_id=$3 AND version=1
					AND can_view=true AND can_create=false AND can_update=false AND can_delete=false`, tenantID, roleID, pageID); err != nil {
					return fmt.Errorf("roleaccessstore: narrow local-dev %s access for %s: %w", pageID, roleID, err)
				}
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE role_page_permission SET can_create=false,version=version+1,updated_by='system:local-dev-personas',updated_at=clock_timestamp()
			WHERE tenant_id=$1 AND role_id='payroll_manager' AND page_id='journeys' AND version=1
			AND can_view=true AND can_create=true AND can_update=true AND can_delete=false`, tenantID); err != nil {
			return fmt.Errorf("roleaccessstore: narrow local-dev payroll initiation: %w", err)
		}
		return nil
	})
}

func (s *Store) Load(ctx context.Context, tenant values.TenantId, organization string) (roleaccess.Snapshot, error) {
	organization = strings.TrimSpace(organization)
	if organization == "" {
		return roleaccess.Snapshot{}, roleaccess.ErrInvalid
	}
	var result roleaccess.Snapshot
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT version,role_id,name,description,system_role,active FROM access_role WHERE tenant_id=$1 ORDER BY lower(name),role_id`, tenantID)
		if err != nil {
			return fmt.Errorf("roleaccessstore: load roles: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var role roleaccess.Role
			if err := rows.Scan(&role.Version, &role.ID, &role.Name, &role.Description, &role.System, &role.Active); err != nil {
				return err
			}
			result.Roles = append(result.Roles, role)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `SELECT s.version,s.worker_ref,a.role_id FROM worker_access_role_set s JOIN worker_access_role_assignment a USING (tenant_id,worker_ref) WHERE s.tenant_id=$1 ORDER BY lower(s.worker_ref),a.role_id`, tenantID)
		if err != nil {
			return fmt.Errorf("roleaccessstore: load assignments: %w", err)
		}
		defer rows.Close()
		assignmentIndex := map[string]int{}
		for rows.Next() {
			var version int64
			var workerRef, roleID string
			if err := rows.Scan(&version, &workerRef, &roleID); err != nil {
				return err
			}
			key := strings.ToLower(workerRef)
			index, ok := assignmentIndex[key]
			if !ok {
				result.Assignments = append(result.Assignments, roleaccess.Assignment{Version: version, WorkerRef: workerRef})
				index = len(result.Assignments) - 1
				assignmentIndex[key] = index
			}
			result.Assignments[index].RoleIDs = append(result.Assignments[index].RoleIDs, roleID)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `SELECT version,role_id,mode,organization_units FROM role_organization_visibility WHERE tenant_id=$1 AND organization_scope_id=$2 ORDER BY role_id`, tenantID, organization)
		if err != nil {
			return fmt.Errorf("roleaccessstore: load visibility: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var policy roleaccess.VisibilityPolicy
			if err := rows.Scan(&policy.Version, &policy.RoleID, &policy.Mode, &policy.OrganizationUnits); err != nil {
				return err
			}
			result.Policies = append(result.Policies, roleaccess.NormalizeVisibility(policy))
		}
		if err := rows.Err(); err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `SELECT version,role_id,page_id,can_view,can_create,can_update,can_delete FROM role_page_permission WHERE tenant_id=$1 ORDER BY role_id,page_id`, tenantID)
		if err != nil {
			return fmt.Errorf("roleaccessstore: load page permissions: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var permission roleaccess.PagePermission
			if err := rows.Scan(&permission.Version, &permission.RoleID, &permission.PageID, &permission.View, &permission.Create, &permission.Update, &permission.Delete); err != nil {
				return err
			}
			result.PagePermissions = append(result.PagePermissions, roleaccess.NormalizePagePermission(permission))
		}
		return rows.Err()
	})
	return result, err
}

func (s *Store) SavePagePermission(ctx context.Context, tenant values.TenantId, actor string, permission roleaccess.PagePermission) (roleaccess.PagePermission, error) {
	actor = strings.TrimSpace(actor)
	permission = roleaccess.NormalizePagePermission(permission)
	if actor == "" || roleaccess.ValidatePagePermission(permission) != nil {
		return roleaccess.PagePermission{}, roleaccess.ErrInvalid
	}
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT active FROM access_role WHERE tenant_id=$1 AND role_id=$2`, tenantID, permission.RoleID).Scan(&active); err != nil || !active {
			return roleaccess.ErrInvalid
		}
		if permission.Version == 0 {
			affected, err := tx.Exec(ctx, `INSERT INTO role_page_permission (tenant_id,role_id,page_id,version,can_view,can_create,can_update,can_delete,updated_by) VALUES ($1,$2,$3,1,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, tenantID, permission.RoleID, permission.PageID, permission.View, permission.Create, permission.Update, permission.Delete, actor)
			if err != nil {
				return err
			}
			if affected != 1 {
				return roleaccess.ErrVersionConflict
			}
			permission.Version = 1
			return nil
		}
		affected, err := tx.Exec(ctx, `UPDATE role_page_permission SET version=version+1,can_view=$4,can_create=$5,can_update=$6,can_delete=$7,updated_by=$8,updated_at=clock_timestamp() WHERE tenant_id=$1 AND role_id=$2 AND page_id=$3 AND version=$9`, tenantID, permission.RoleID, permission.PageID, permission.View, permission.Create, permission.Update, permission.Delete, actor, permission.Version)
		if err != nil {
			return err
		}
		if affected != 1 {
			return roleaccess.ErrVersionConflict
		}
		permission.Version++
		return nil
	})
	return permission, err
}

func (s *Store) SaveRole(ctx context.Context, tenant values.TenantId, actor string, role roleaccess.Role) (roleaccess.Role, error) {
	actor = strings.TrimSpace(actor)
	role = roleaccess.NormalizeRole(role)
	if actor == "" || roleaccess.ValidateRole(role) != nil {
		return roleaccess.Role{}, roleaccess.ErrInvalid
	}
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		if role.Version == 0 {
			affected, err := tx.Exec(ctx, `INSERT INTO access_role (tenant_id,role_id,version,name,description,system_role,active,updated_by) VALUES ($1,$2,1,$3,$4,false,true,$5) ON CONFLICT DO NOTHING`, tenantID, role.ID, role.Name, role.Description, actor)
			if err != nil {
				return err
			}
			if affected != 1 {
				return roleaccess.ErrVersionConflict
			}
			role.Version, role.System, role.Active = 1, false, true
			return nil
		}
		affected, err := tx.Exec(ctx, `UPDATE access_role SET version=version+1,name=$4,description=$5,active=$6,updated_by=$7,updated_at=clock_timestamp() WHERE tenant_id=$1 AND role_id=$2 AND version=$3`, tenantID, role.ID, role.Version, role.Name, role.Description, role.Active, actor)
		if err != nil {
			return err
		}
		if affected != 1 {
			return roleaccess.ErrVersionConflict
		}
		role.Version++
		return nil
	})
	return role, err
}

func (s *Store) SaveAssignment(ctx context.Context, tenant values.TenantId, actor string, assignment roleaccess.Assignment) (roleaccess.Assignment, error) {
	actor = strings.TrimSpace(actor)
	assignment = roleaccess.NormalizeAssignment(assignment)
	if actor == "" || roleaccess.ValidateAssignment(assignment) != nil {
		return roleaccess.Assignment{}, roleaccess.ErrInvalid
	}
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		for _, roleID := range assignment.RoleIDs {
			var active bool
			if err := tx.QueryRow(ctx, `SELECT active FROM access_role WHERE tenant_id=$1 AND role_id=$2`, tenantID, roleID).Scan(&active); err != nil || !active {
				return roleaccess.ErrInvalid
			}
		}
		if assignment.Version == 0 {
			affected, err := tx.Exec(ctx, `INSERT INTO worker_access_role_set (tenant_id,worker_ref,version,updated_by) VALUES ($1,$2,1,$3) ON CONFLICT DO NOTHING`, tenantID, assignment.WorkerRef, actor)
			if err != nil {
				return err
			}
			if affected != 1 {
				return roleaccess.ErrVersionConflict
			}
			assignment.Version = 1
		} else {
			affected, err := tx.Exec(ctx, `UPDATE worker_access_role_set SET version=version+1,updated_by=$4,updated_at=clock_timestamp() WHERE tenant_id=$1 AND worker_ref=$2 AND version=$3`, tenantID, assignment.WorkerRef, assignment.Version, actor)
			if err != nil {
				return err
			}
			if affected != 1 {
				return roleaccess.ErrVersionConflict
			}
			assignment.Version++
		}
		if _, err := tx.Exec(ctx, `DELETE FROM worker_access_role_assignment WHERE tenant_id=$1 AND worker_ref=$2`, tenantID, assignment.WorkerRef); err != nil {
			return err
		}
		for _, roleID := range assignment.RoleIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO worker_access_role_assignment (tenant_id,worker_ref,role_id) VALUES ($1,$2,$3)`, tenantID, assignment.WorkerRef, roleID); err != nil {
				return err
			}
		}
		return nil
	})
	return assignment, err
}

func (s *Store) SaveVisibility(ctx context.Context, tenant values.TenantId, organization, actor string, policy roleaccess.VisibilityPolicy) (roleaccess.VisibilityPolicy, error) {
	organization, actor = strings.TrimSpace(organization), strings.TrimSpace(actor)
	policy = roleaccess.NormalizeVisibility(policy)
	if organization == "" || actor == "" || roleaccess.ValidateVisibility(policy) != nil {
		return roleaccess.VisibilityPolicy{}, roleaccess.ErrInvalid
	}
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT active FROM access_role WHERE tenant_id=$1 AND role_id=$2`, tenantID, policy.RoleID).Scan(&active); err != nil || !active {
			return roleaccess.ErrInvalid
		}
		if policy.Version == 0 {
			affected, err := tx.Exec(ctx, `INSERT INTO role_organization_visibility (tenant_id,organization_scope_id,role_id,version,mode,organization_units,updated_by) VALUES ($1,$2,$3,1,$4,$5,$6) ON CONFLICT DO NOTHING`, tenantID, organization, policy.RoleID, policy.Mode, policy.OrganizationUnits, actor)
			if err != nil {
				return err
			}
			if affected != 1 {
				return roleaccess.ErrVersionConflict
			}
			policy.Version = 1
			return nil
		}
		affected, err := tx.Exec(ctx, `UPDATE role_organization_visibility SET version=version+1,mode=$5,organization_units=$6,updated_by=$7,updated_at=clock_timestamp() WHERE tenant_id=$1 AND organization_scope_id=$2 AND role_id=$3 AND version=$4`, tenantID, organization, policy.RoleID, policy.Version, policy.Mode, policy.OrganizationUnits, actor)
		if err != nil {
			return err
		}
		if affected != 1 {
			return roleaccess.ErrVersionConflict
		}
		policy.Version++
		return nil
	})
	return policy, err
}

func (s *Store) withTenant(ctx context.Context, tenant values.TenantId, fn func(dbport.Tx, uuid.UUID) error) error {
	if s == nil || s.tenant == nil {
		return roleaccess.ErrUnavailable
	}
	tenantID := s.tenant(tenant)
	if tenantID == uuid.Nil {
		return roleaccess.ErrInvalid
	}
	if s.db == nil {
		return roleaccess.ErrUnavailable
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("roleaccessstore: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, tenantID); err == nil {
		err = fn(tx, tenantID)
	}
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("roleaccessstore: commit: %w", err)
	}
	return nil
}

var _ roleaccess.Store = (*Store)(nil)
