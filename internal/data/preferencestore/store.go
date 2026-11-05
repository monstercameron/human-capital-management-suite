// Package preferencestore adapts the presentation-preference port to the
// tenant-isolated PostgreSQL tables created by migration 00132.
package preferencestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/preferences"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type Store struct {
	db     dbport.Beginner
	tenant func(values.TenantId) uuid.UUID
}

func New(db dbport.Beginner, tenant func(values.TenantId) uuid.UUID) *Store {
	return &Store{db: db, tenant: tenant}
}

func (s *Store) Load(ctx context.Context, tenant values.TenantId, organizationScopeID, principal string) (preferences.Snapshot, error) {
	result := preferences.DefaultSnapshot()
	organizationScopeID = strings.TrimSpace(organizationScopeID)
	principal = strings.TrimSpace(principal)
	if organizationScopeID == "" || principal == "" {
		return result, preferences.ErrInvalid
	}
	result.Theme.OrganizationScopeID = organizationScopeID
	result.OrganizationVisibility.OrganizationScopeID = organizationScopeID
	visibilityLoaded := false
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var userJSON, themeJSON, visibilityJSON []byte
		if err := tx.QueryRow(ctx, `SELECT version, preferences FROM user_presentation_preference WHERE tenant_id=$1 AND principal_id=$2`, tenantID, principal).Scan(&result.User.Version, &userJSON); err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("preferencestore: load user: %w", err)
		} else if err == nil && json.Unmarshal(userJSON, &result.User) != nil {
			return fmt.Errorf("%w: corrupt user preferences", preferences.ErrInvalid)
		}
		if err := tx.QueryRow(ctx, `SELECT version, theme FROM tenant_appearance_preference WHERE tenant_id=$1 AND organization_scope_id=$2`, tenantID, organizationScopeID).Scan(&result.Theme.Version, &themeJSON); err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("preferencestore: load theme: %w", err)
		} else if err == nil && json.Unmarshal(themeJSON, &result.Theme.Theme) != nil {
			return fmt.Errorf("%w: corrupt tenant theme", preferences.ErrInvalid)
		}
		if err := tx.QueryRow(ctx, `SELECT version, policy FROM organization_visibility_preference WHERE tenant_id=$1 AND organization_scope_id=$2`, tenantID, organizationScopeID).Scan(&result.OrganizationVisibility.Version, &visibilityJSON); err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("preferencestore: load organization visibility: %w", err)
		} else if err == nil {
			visibilityLoaded = true
			if json.Unmarshal(visibilityJSON, &result.OrganizationVisibility) != nil || preferences.ValidateOrganizationVisibility(result.OrganizationVisibility) != nil {
				return fmt.Errorf("%w: corrupt organization visibility", preferences.ErrInvalid)
			}
		}
		return nil
	})
	if visibilityLoaded && err != nil {
		return result, err
	}
	result.User = preferences.NormalizeUser(result.User)
	result.OrganizationVisibility = preferences.NormalizeOrganizationVisibility(result.OrganizationVisibility)
	result.OrganizationVisibility.OrganizationScopeID = organizationScopeID
	return result, err
}

func (s *Store) SaveUser(ctx context.Context, tenant values.TenantId, principal string, value preferences.User) (preferences.User, error) {
	if strings.TrimSpace(principal) == "" {
		return preferences.User{}, preferences.ErrInvalid
	}
	value = preferences.NormalizeUser(value)
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		body, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("preferencestore: encode user: %w", err)
		}
		if value.Version == 0 {
			affected, insertErr := tx.Exec(ctx, `INSERT INTO user_presentation_preference (tenant_id,principal_id,version,preferences) VALUES ($1,$2,1,$3) ON CONFLICT DO NOTHING`, tenantID, principal, body)
			if insertErr != nil {
				return insertErr
			}
			if affected != 1 {
				return preferences.ErrVersionConflict
			}
			value.Version = 1
			return nil
		}
		affected, err := tx.Exec(ctx, `UPDATE user_presentation_preference SET version=version+1, preferences=$4, updated_at=clock_timestamp() WHERE tenant_id=$1 AND principal_id=$2 AND version=$3`, tenantID, principal, value.Version, body)
		if err != nil {
			return fmt.Errorf("preferencestore: update user: %w", err)
		}
		if affected != 1 {
			return preferences.ErrVersionConflict
		}
		value.Version++
		return nil
	})
	return value, err
}

func (s *Store) SaveTheme(ctx context.Context, tenant values.TenantId, organizationScopeID, actor string, value preferences.TenantTheme) (preferences.TenantTheme, error) {
	organizationScopeID = strings.TrimSpace(organizationScopeID)
	actor = strings.TrimSpace(actor)
	if organizationScopeID == "" || actor == "" {
		return preferences.TenantTheme{}, preferences.ErrInvalid
	}
	value.OrganizationScopeID = organizationScopeID
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		body, err := json.Marshal(value.Theme)
		if err != nil {
			return fmt.Errorf("preferencestore: encode theme: %w", err)
		}
		if value.Version == 0 {
			affected, err := tx.Exec(ctx, `INSERT INTO tenant_appearance_preference (tenant_id,organization_scope_id,version,theme,updated_by) VALUES ($1,$2,1,$3,$4) ON CONFLICT DO NOTHING`, tenantID, organizationScopeID, body, actor)
			if err != nil {
				return err
			}
			if affected != 1 {
				return preferences.ErrVersionConflict
			}
			value.Version = 1
			return nil
		}
		affected, err := tx.Exec(ctx, `UPDATE tenant_appearance_preference SET version=version+1, theme=$4, updated_by=$5, updated_at=clock_timestamp() WHERE tenant_id=$1 AND organization_scope_id=$2 AND version=$3`, tenantID, organizationScopeID, value.Version, body, actor)
		if err != nil {
			return fmt.Errorf("preferencestore: update theme: %w", err)
		}
		if affected != 1 {
			return preferences.ErrVersionConflict
		}
		value.Version++
		return nil
	})
	return value, err
}

func (s *Store) SaveOrganizationVisibility(ctx context.Context, tenant values.TenantId, organizationScopeID, actor string, value preferences.OrganizationVisibility) (preferences.OrganizationVisibility, error) {
	organizationScopeID = strings.TrimSpace(organizationScopeID)
	actor = strings.TrimSpace(actor)
	if organizationScopeID == "" || actor == "" {
		return preferences.OrganizationVisibility{}, preferences.ErrInvalid
	}
	if err := preferences.ValidateOrganizationVisibility(value); err != nil {
		return preferences.OrganizationVisibility{}, err
	}
	value = preferences.NormalizeOrganizationVisibility(value)
	value.OrganizationScopeID = organizationScopeID
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		body, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("preferencestore: encode organization visibility: %w", err)
		}
		if value.Version == 0 {
			affected, err := tx.Exec(ctx, `INSERT INTO organization_visibility_preference (tenant_id,organization_scope_id,version,policy,updated_by) VALUES ($1,$2,1,$3,$4) ON CONFLICT DO NOTHING`, tenantID, organizationScopeID, body, actor)
			if err != nil {
				return err
			}
			if affected != 1 {
				return preferences.ErrVersionConflict
			}
			value.Version = 1
			return nil
		}
		affected, err := tx.Exec(ctx, `UPDATE organization_visibility_preference SET version=version+1, policy=$4, updated_by=$5, updated_at=clock_timestamp() WHERE tenant_id=$1 AND organization_scope_id=$2 AND version=$3`, tenantID, organizationScopeID, value.Version, body, actor)
		if err != nil {
			return fmt.Errorf("preferencestore: update organization visibility: %w", err)
		}
		if affected != 1 {
			return preferences.ErrVersionConflict
		}
		value.Version++
		return nil
	})
	return value, err
}

func (s *Store) RecordWorkflowUse(ctx context.Context, tenant values.TenantId, principal, workflow string) (preferences.User, error) {
	workflow = strings.TrimSpace(workflow)
	if workflow == "" {
		return preferences.User{}, preferences.ErrInvalid
	}
	for attempts := 0; attempts < 3; attempts++ {
		user, err := s.loadUser(ctx, tenant, principal)
		if err != nil {
			return preferences.User{}, err
		}
		// WFPAGE-004: the value is the workflow's last-use sequence, a
		// per-principal Lamport clock, so ordering by value descending
		// yields the viewer's last distinct starts. A bare count cannot
		// order distinct workflows by recency; legacy count values keep
		// working as ranks and self-heal to sequence values on their next
		// recorded use.
		var last int64
		for _, rank := range user.WorkflowUses {
			if rank > last {
				last = rank
			}
		}
		user.WorkflowUses[workflow] = last + 1
		updated, err := s.SaveUser(ctx, tenant, principal, user)
		if !errors.Is(err, preferences.ErrVersionConflict) {
			return updated, err
		}
	}
	return preferences.User{}, preferences.ErrVersionConflict
}

func (s *Store) loadUser(ctx context.Context, tenant values.TenantId, principal string) (preferences.User, error) {
	principal = strings.TrimSpace(principal)
	result := preferences.NormalizeUser(preferences.User{})
	if principal == "" {
		return result, preferences.ErrInvalid
	}
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var body []byte
		if err := tx.QueryRow(ctx, `SELECT version, preferences FROM user_presentation_preference WHERE tenant_id=$1 AND principal_id=$2`, tenantID, principal).Scan(&result.Version, &body); err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("preferencestore: load user: %w", err)
		} else if err == nil && json.Unmarshal(body, &result) != nil {
			return fmt.Errorf("%w: corrupt user preferences", preferences.ErrInvalid)
		}
		return nil
	})
	return preferences.NormalizeUser(result), err
}

func (s *Store) withTenant(ctx context.Context, tenant values.TenantId, fn func(dbport.Tx, uuid.UUID) error) error {
	if s == nil || s.db == nil || s.tenant == nil {
		return preferences.ErrUnavailable
	}
	if strings.TrimSpace(tenant.String()) == "" {
		return preferences.ErrInvalid
	}
	tenantID := s.tenant(tenant)
	if tenantID == uuid.Nil {
		return preferences.ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("preferencestore: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err = tenancy.WithTenant(ctx, tx, tenantID); err == nil {
		err = fn(tx, tenantID)
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("preferencestore: commit: %w", err)
	}
	return nil
}

var _ preferences.Store = (*Store)(nil)
