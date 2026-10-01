package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// ErrAgentDirectoryUnavailable identifies a missing authoritative directory
// fact. Callers must treat it as a denial, never as an empty or tenant-wide
// result.
var ErrAgentDirectoryUnavailable = errors.New("application: agent directory fact unavailable")

// AgentDirectoryDB reads the core tenant database for current agent policy
// facts. It opens a tenant-scoped read transaction for every method call.
// TenantUUID is required because the application tenant identifier is an
// opaque value while the core tables use UUIDs.
type AgentDirectoryDB struct {
	DB         dbport.Beginner
	TenantUUID func(values.TenantId) uuid.UUID
}

// NewAgentDirectoryDB constructs a production directory reader.
func NewAgentDirectoryDB(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID) *AgentDirectoryDB {
	return &AgentDirectoryDB{DB: db, TenantUUID: tenantUUID}
}

// NewAgentDirectoryDatabase is an explicit spelling retained for composition
// roots that call the core database a database rather than a DB.
func NewAgentDirectoryDatabase(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID) *AgentDirectoryDB {
	return NewAgentDirectoryDB(db, tenantUUID)
}

var _ AgentRoleDirectory = (*AgentDirectoryDB)(nil)
var _ AgentPopulationDirectory = (*AgentDirectoryDB)(nil)
var _ AgentOrganizationDirectory = (*AgentDirectoryDB)(nil)
var _ AgentSubjectDirectory = (*AgentDirectoryDB)(nil)
var _ AgentFieldPolicy = (*AgentDirectoryDB)(nil)

// CurrentRoles reads active role assignments for subject from the durable
// role-access projection. A missing assignment is unavailable, not all roles.
func (d *AgentDirectoryDB) CurrentRoles(ctx context.Context, tenant values.TenantId, subject string) ([]string, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, fmt.Errorf("%w: subject is required", ErrAgentDirectoryUnavailable)
	}
	var roles []string
	err := d.read(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT a.role_id FROM worker_access_role_assignment a JOIN worker_access_role_set s ON s.tenant_id=a.tenant_id AND s.worker_ref=a.worker_ref JOIN access_role r ON r.tenant_id=a.tenant_id AND r.role_id=a.role_id WHERE a.tenant_id=$1 AND a.worker_ref=$2 AND r.active ORDER BY a.role_id`, tenantID, subject)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var role string
			if err := rows.Scan(&role); err != nil {
				return err
			}
			roles = append(roles, role)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read current roles: %w", err)
	}
	if len(roles) == 0 {
		return nil, fmt.Errorf("%w: no active role assignment", ErrAgentDirectoryUnavailable)
	}
	return roles, nil
}

// CurrentPopulation reads the one current named population for a subject. A
// subject with no fact or more than one current fact is unavailable: callers
// must never turn ambiguity into a broad or guessed audience.
func (d *AgentDirectoryDB) CurrentPopulation(ctx context.Context, tenant values.TenantId, subject string) (string, error) {
	populations, err := d.CurrentPopulations(ctx, tenant, subject)
	if err != nil {
		return "", err
	}
	if len(populations) != 1 {
		return "", fmt.Errorf("%w: current population is ambiguous", ErrAgentDirectoryUnavailable)
	}
	return populations[0], nil
}

// CurrentPopulations reads all current named population facts for a subject.
// This concrete seam is intentionally narrower than the application
// interface: persona audience discovery can consume multiple exact IDs while
// the legacy singular interface remains fail-closed for ambiguity.
func (d *AgentDirectoryDB) CurrentPopulations(ctx context.Context, tenant values.TenantId, subject string) ([]string, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, fmt.Errorf("%w: subject is required", ErrAgentDirectoryUnavailable)
	}
	var populations []string
	err := d.read(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT population_id FROM agent_current_population WHERE tenant_id=$1 AND subject_ref=$2 AND effective_from <= CURRENT_TIMESTAMP AND (effective_until IS NULL OR CURRENT_TIMESTAMP < effective_until) AND revoked_at IS NULL AND superseded_at IS NULL ORDER BY population_id`, tenantID, subject)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var population string
			if err := rows.Scan(&population); err != nil {
				return err
			}
			population = strings.TrimSpace(population)
			if population == "" {
				return fmt.Errorf("%w: empty population authority", ErrAgentDirectoryUnavailable)
			}
			if len(populations) > 0 && populations[len(populations)-1] == population {
				return fmt.Errorf("%w: duplicate current population authority", ErrAgentDirectoryUnavailable)
			}
			populations = append(populations, population)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read current populations: %w", err)
	}
	if len(populations) == 0 {
		return nil, fmt.Errorf("%w: no current population assignment", ErrAgentDirectoryUnavailable)
	}
	return populations, nil
}

// CurrentOrganizationScopes returns scopes explicitly granted to any of the
// supplied active roles. An empty result means no organization grant.
func (d *AgentDirectoryDB) CurrentOrganizationScopes(ctx context.Context, tenant values.TenantId, subject string, roles []string) ([]string, error) {
	if strings.TrimSpace(subject) == "" || roles == nil {
		return nil, fmt.Errorf("%w: subject and server-resolved roles are required", ErrAgentDirectoryUnavailable)
	}
	roles = normalizeFacts(roles)
	if len(roles) == 0 {
		return []string{}, nil
	}
	var scopes []string
	err := d.read(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT v.organization_scope_id FROM role_organization_visibility v JOIN access_role r ON r.tenant_id=v.tenant_id AND r.role_id=v.role_id WHERE v.tenant_id=$1 AND r.active AND v.role_id = ANY($2::text[]) ORDER BY v.organization_scope_id`, tenantID, roles)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var scope string
			if err := rows.Scan(&scope); err != nil {
				return err
			}
			scopes = append(scopes, scope)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read organization scopes: %w", err)
	}
	return normalizeFacts(scopes), nil
}

// CurrentSubjects resolves exact workers from current durable facts. It
// returns self and manager-chain records, and organization-matched records for
// HR/admin roles. It never emits a population wildcard.
func (d *AgentDirectoryDB) CurrentSubjects(ctx context.Context, tenant values.TenantId, subject, purpose string, roles, organizations []string) ([]agentgate.Subject, error) {
	return d.currentSubjects(ctx, tenant, subject, purpose, roles, organizations, false)
}

// personaHomeOrganizationSubjects resolves subject scope from current home
// organization facts, matching the identities used by persona credentials and
// grants. Workforce organization unit codes are not authority identifiers.
type personaHomeOrganizationSubjects struct{ directory *AgentDirectoryDB }

func (d personaHomeOrganizationSubjects) CurrentSubjects(ctx context.Context, tenant values.TenantId, subject, purpose string, roles, organizations []string) ([]agentgate.Subject, error) {
	if d.directory == nil {
		return nil, ErrAgentDirectoryUnavailable
	}
	return d.directory.currentSubjects(ctx, tenant, subject, purpose, roles, organizations, true)
}

func (d *AgentDirectoryDB) currentSubjects(ctx context.Context, tenant values.TenantId, subject, purpose string, roles, organizations []string, homeOrganizations bool) ([]agentgate.Subject, error) {
	if strings.TrimSpace(subject) == "" || strings.TrimSpace(purpose) == "" || roles == nil || organizations == nil {
		return nil, fmt.Errorf("%w: subject, purpose, roles and organizations are required", ErrAgentDirectoryUnavailable)
	}
	roles = normalizeFacts(roles)
	organizations = normalizeFacts(organizations)
	roleSet := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		roleSet[role] = struct{}{}
	}
	var out []agentgate.Subject
	// Self-service is deliberately limited to the caller's own record. Other
	// purposes may use the relationship and organization facts below; the
	// downstream field policy still decides which fields are disclosable.
	relationshipPurpose := purpose != authz.PurposeSelfService
	managerRole := false
	if _, ok := roleSet["manager"]; ok {
		managerRole = relationshipPurpose
	}
	organizationRole := false
	if _, ok := roleSet["hr_partner"]; ok {
		organizationRole = relationshipPurpose
	}
	if _, ok := roleSet["comp_admin"]; ok {
		organizationRole = relationshipPurpose
	}
	err := d.read(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		query := `SELECT worker_id::text,worker_key,org_unit,manager_relationship_ref FROM journey_worker WHERE tenant_id=$1 AND (worker_id::text=$2 OR worker_key=$2 OR ($3::boolean AND manager_relationship_ref=$2) OR ($4::boolean AND org_unit = ANY($5::text[]))) ORDER BY worker_id`
		if homeOrganizations {
			query = `SELECT w.worker_id::text,w.worker_key,
				COALESCE((SELECT h.organization_scope_id FROM agent_current_home_organization h
					WHERE h.tenant_id=w.tenant_id AND h.subject_ref=w.worker_key
					AND h.effective_from <= CURRENT_TIMESTAMP AND (h.effective_until IS NULL OR CURRENT_TIMESTAMP < h.effective_until)
					AND h.revoked_at IS NULL AND h.superseded_at IS NULL),''),w.manager_relationship_ref
				FROM journey_worker w WHERE w.tenant_id=$1 AND
				(w.worker_id::text=$2 OR w.worker_key=$2 OR ($3::boolean AND w.manager_relationship_ref=$2)
				OR ($4::boolean AND EXISTS (SELECT 1 FROM agent_current_home_organization h
					WHERE h.tenant_id=w.tenant_id AND h.subject_ref=w.worker_key AND h.organization_scope_id=ANY($5::text[])
					AND h.effective_from <= CURRENT_TIMESTAMP AND (h.effective_until IS NULL OR CURRENT_TIMESTAMP < h.effective_until)
					AND h.revoked_at IS NULL AND h.superseded_at IS NULL))) ORDER BY w.worker_id`
		}
		rows, err := tx.Query(ctx, query, tenantID, subject, managerRole, organizationRole, organizations)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, key, org, manager string
			if err := rows.Scan(&id, &key, &org, &manager); err != nil {
				return err
			}
			workerID, err := uuid.Parse(id)
			if err != nil {
				return fmt.Errorf("invalid worker id: %w", err)
			}
			ref := values.EntityRef{Tenant: tenant, Kind: values.Kind("worker"), Id: workerID.String()}
			if err := ref.Validate(); err != nil {
				return err
			}
			orgRef := authz.OrgUnitRef{Tenant: tenant, ID: strings.TrimSpace(org)}
			if orgRef.ID == "" {
				return fmt.Errorf("%w: worker organization is unresolved", ErrAgentDirectoryUnavailable)
			}
			out = append(out, agentgate.Subject{Ref: ref, Organization: orgRef})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read current subjects: %w", err)
	}
	return out, nil
}

// CurrentFields derives the field mask from the server-owned authz policy.
// Requested fields do not exist in this interface and therefore cannot widen
// the result; exact subjects are validated for tenant identity before policy.
func (d *AgentDirectoryDB) CurrentFields(_ context.Context, principal *trust.Principal, purpose string, roles, organizations []string, subjects []agentgate.Subject) ([]authz.FieldID, error) {
	if principal == nil || strings.TrimSpace(purpose) == "" || roles == nil || organizations == nil {
		return nil, fmt.Errorf("%w: field policy input is incomplete", ErrAgentDirectoryUnavailable)
	}
	for _, subject := range subjects {
		if err := subject.Ref.Validate(); err != nil || subject.Ref.Tenant != principal.Tenant() || subject.Organization.IsZero() || subject.Organization.Tenant != principal.Tenant() {
			return nil, fmt.Errorf("%w: subject is not tenant-scoped", ErrAgentDirectoryUnavailable)
		}
	}
	fields := make([]authz.FieldID, 0, len(authz.FieldRegistry))
	for field := range authz.FieldRegistry {
		fields = append(fields, field)
	}
	slices.Sort(fields)
	decision, err := authz.ResolveFieldsWithRoles(principal, normalizeFacts(roles), purpose, fields, nil)
	if err != nil {
		return nil, fmt.Errorf("resolve current field policy: %w", err)
	}
	allowed := make([]authz.FieldID, 0, len(fields))
	for _, field := range fields {
		if ruling := decision.Rulings[field]; ruling.Effect == authz.EffectAllow {
			allowed = append(allowed, field)
		}
	}
	return allowed, nil
}

func (d *AgentDirectoryDB) read(ctx context.Context, tenant values.TenantId, fn func(dbport.Tx, uuid.UUID) error) error {
	if d == nil || d.DB == nil || d.TenantUUID == nil || tenant.Validate() != nil {
		return ErrAgentDirectoryUnavailable
	}
	tenantID := d.TenantUUID(tenant)
	if tenantID == uuid.Nil {
		return ErrAgentDirectoryUnavailable
	}
	tx, err := d.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin directory read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return fmt.Errorf("scope directory read: %w", err)
	}
	return fn(tx, tenantID)
}
