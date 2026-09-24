package roleaccessstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// LoadAt reconstructs the saved permission snapshot as it existed at at.
// Current control rows are read first, then append-only revisions newer than
// the requested instant are applied backwards in reverse commit-time order.
func (s *Store) LoadAt(ctx context.Context, tenant values.TenantId, organization string, at time.Time) (roleaccess.Snapshot, error) {
	if at.IsZero() {
		return roleaccess.Snapshot{}, roleaccess.ErrInvalid
	}
	snapshot, err := s.Load(ctx, tenant, organization)
	if err != nil {
		return roleaccess.Snapshot{}, err
	}
	err = s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		rows, err := tx.Query(ctx, `
			SELECT change_kind, role_id, worker_ref, organization_scope_id, page_id, feature_id, before_row
			FROM access_role_revision
			WHERE tenant_id=$1 AND recorded_at>$2
			ORDER BY recorded_at DESC, revision_id DESC`, tenantID, at.UTC())
		if err != nil {
			return fmt.Errorf("roleaccessstore: load permission revisions: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var kind, roleID, workerRef, organizationScopeID, pageID, featureID string
			var before []byte
			if err := rows.Scan(&kind, &roleID, &workerRef, &organizationScopeID, &pageID, &featureID, &before); err != nil {
				return err
			}
			if err := rewindSnapshot(&snapshot, organization, ChangeKind(kind), roleID, workerRef, organizationScopeID, pageID, featureID, before); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	return snapshot, err
}

func rewindSnapshot(snapshot *roleaccess.Snapshot, organization string, kind ChangeKind, roleID, workerRef, organizationScopeID, pageID, featureID string, before []byte) error {
	decode := func(dst any) error {
		if len(before) == 0 {
			return nil
		}
		if err := json.Unmarshal(before, dst); err != nil {
			return fmt.Errorf("roleaccessstore: decode %s history: %w", kind, err)
		}
		return nil
	}
	switch kind {
	case RevisionRole:
		index := indexRole(snapshot.Roles, roleID)
		if len(before) == 0 {
			if index >= 0 {
				snapshot.Roles = append(snapshot.Roles[:index], snapshot.Roles[index+1:]...)
			}
			return nil
		}
		var value roleaccess.Role
		if err := decode(&value); err != nil {
			return err
		}
		if index >= 0 {
			snapshot.Roles[index] = value
		} else {
			snapshot.Roles = append(snapshot.Roles, value)
		}
	case RevisionPagePermission:
		index := indexPagePermission(snapshot.PagePermissions, roleID, pageID)
		if len(before) == 0 {
			if index >= 0 {
				snapshot.PagePermissions = append(snapshot.PagePermissions[:index], snapshot.PagePermissions[index+1:]...)
			}
			return nil
		}
		var value roleaccess.PagePermission
		if err := decode(&value); err != nil {
			return err
		}
		if index >= 0 {
			snapshot.PagePermissions[index] = value
		} else {
			snapshot.PagePermissions = append(snapshot.PagePermissions, value)
		}
	case RevisionFeaturePermission:
		index := indexFeaturePermission(snapshot.FeaturePermissions, roleID, pageID, featureID)
		if len(before) == 0 {
			if index >= 0 {
				snapshot.FeaturePermissions = append(snapshot.FeaturePermissions[:index], snapshot.FeaturePermissions[index+1:]...)
			}
			return nil
		}
		var value roleaccess.FeaturePermission
		if err := decode(&value); err != nil {
			return err
		}
		if index >= 0 {
			snapshot.FeaturePermissions[index] = value
		} else {
			snapshot.FeaturePermissions = append(snapshot.FeaturePermissions, value)
		}
	case RevisionVisibility:
		if organizationScopeID != organization {
			return nil
		}
		index := indexVisibility(snapshot.Policies, roleID)
		if len(before) == 0 {
			if index >= 0 {
				snapshot.Policies = append(snapshot.Policies[:index], snapshot.Policies[index+1:]...)
			}
			return nil
		}
		var value roleaccess.VisibilityPolicy
		if err := decode(&value); err != nil {
			return err
		}
		if index >= 0 {
			snapshot.Policies[index] = value
		} else {
			snapshot.Policies = append(snapshot.Policies, value)
		}
	case RevisionAssignment:
		index := indexAssignment(snapshot.Assignments, workerRef)
		if index < 0 && len(before) == 0 {
			return nil
		}
		if index < 0 {
			snapshot.Assignments = append(snapshot.Assignments, roleaccess.Assignment{WorkerRef: workerRef})
			index = len(snapshot.Assignments) - 1
		}
		var value membershipImage
		if err := decode(&value); err != nil {
			return err
		}
		if value.Present {
			if !containsRole(snapshot.Assignments[index].RoleIDs, roleID) {
				snapshot.Assignments[index].RoleIDs = append(snapshot.Assignments[index].RoleIDs, roleID)
			}
		} else {
			snapshot.Assignments[index].RoleIDs = removeRole(snapshot.Assignments[index].RoleIDs, roleID)
		}
		snapshot.Assignments[index].Version = value.Version
		if len(snapshot.Assignments[index].RoleIDs) == 0 {
			snapshot.Assignments = append(snapshot.Assignments[:index], snapshot.Assignments[index+1:]...)
		} else {
			sort.Strings(snapshot.Assignments[index].RoleIDs)
		}
	default:
		return fmt.Errorf("roleaccessstore: unknown revision kind %q", kind)
	}
	return nil
}

func indexRole(values []roleaccess.Role, roleID string) int {
	for i := range values {
		if values[i].ID == roleID {
			return i
		}
	}
	return -1
}

func indexPagePermission(values []roleaccess.PagePermission, roleID, pageID string) int {
	for i := range values {
		if values[i].RoleID == roleID && values[i].PageID == pageID {
			return i
		}
	}
	return -1
}

func indexFeaturePermission(values []roleaccess.FeaturePermission, roleID, pageID, featureID string) int {
	for i := range values {
		if values[i].RoleID == roleID && values[i].PageID == pageID && values[i].FeatureID == featureID {
			return i
		}
	}
	return -1
}

func indexVisibility(values []roleaccess.VisibilityPolicy, roleID string) int {
	for i := range values {
		if values[i].RoleID == roleID {
			return i
		}
	}
	return -1
}

func indexAssignment(values []roleaccess.Assignment, workerRef string) int {
	for i := range values {
		if values[i].WorkerRef == workerRef {
			return i
		}
	}
	return -1
}

func removeRole(roleIDs []string, roleID string) []string {
	for i, value := range roleIDs {
		if value == roleID {
			return append(roleIDs[:i], roleIDs[i+1:]...)
		}
	}
	return roleIDs
}
