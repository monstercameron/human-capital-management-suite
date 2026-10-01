package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errTenantPersonaSkillGrant = errors.New("application: tenant persona skill grant unavailable")

// TenantPersonaSkillGrantResolver reads current exact-version grants for one
// tenant and checks audiences against the original grant tuples.
type TenantPersonaSkillGrantResolver struct {
	provider agentgate.GrantProvider
	tenant   values.TenantId
}

// NewTenantPersonaSkillGrantResolver constructs a tenant-bound current-grant resolver.
func NewTenantPersonaSkillGrantResolver(provider agentgate.GrantProvider, tenant values.TenantId) (*TenantPersonaSkillGrantResolver, error) {
	if provider == nil || tenant.Validate() != nil {
		return nil, errTenantPersonaSkillGrant
	}
	return &TenantPersonaSkillGrantResolver{provider: provider, tenant: tenant}, nil
}

// AudienceGrant returns a safe legacy projection from one current grant row.
// Tuple-aware consumers should use AllowsAudience to retain row correlations.
func (r *TenantPersonaSkillGrantResolver) AudienceGrant(pin agentskills.SkillPin) (agentpersona.SkillAudienceGrant, error) {
	grants, err := r.current(pin)
	if err != nil || len(grants) == 0 {
		return agentpersona.SkillAudienceGrant{}, err
	}
	grant := grants[0]
	return agentpersona.SkillAudienceGrant{
		Roles: slices.Clone(grant.Roles), Populations: []string{grant.Population}, OrganizationScopes: slices.Clone(grant.OrganizationScopes),
	}, nil
}

// AllowsAudience requires every requested axis combination to be covered by
// at least one current grant row, so separate rows are never unioned into a
// broader Cartesian audience.
func (r *TenantPersonaSkillGrantResolver) AllowsAudience(pin agentskills.SkillPin, audience agentpersona.Audience) (bool, error) {
	grants, err := r.current(pin)
	if err != nil {
		return false, err
	}
	if len(audience.Roles) == 0 || len(audience.Populations) == 0 || len(audience.OrganizationScopes) == 0 || len(grants) == 0 {
		return false, nil
	}
	for _, role := range audience.Roles {
		for _, population := range audience.Populations {
			for _, organization := range audience.OrganizationScopes {
				if !tupleGrantCovers(grants, role, population, organization) {
					return false, nil
				}
			}
		}
	}
	return true, nil
}

func (r *TenantPersonaSkillGrantResolver) current(pin agentskills.SkillPin) ([]agentgate.SkillGrant, error) {
	if r == nil || r.provider == nil || r.tenant.Validate() != nil || strings.TrimSpace(pin.ID) == "" || pin.ID != strings.TrimSpace(pin.ID) || pin.Version == 0 || strings.TrimSpace(pin.Digest) == "" {
		return nil, errTenantPersonaSkillGrant
	}
	grants, err := r.provider.Grants(context.Background(), r.tenant, pin.Key())
	if err != nil {
		return nil, fmt.Errorf("%w: current grant query failed", errTenantPersonaSkillGrant)
	}
	current := make([]agentgate.SkillGrant, 0, len(grants))
	for _, grant := range grants {
		if !validPersonaSkillGrant(grant, r.tenant, pin.Key()) {
			continue
		}
		current = append(current, grant)
	}
	sort.Slice(current, func(i, j int) bool { return current[i].ID < current[j].ID })
	return current, nil
}

func validPersonaSkillGrant(grant agentgate.SkillGrant, tenant values.TenantId, key agentskills.SkillKey) bool {
	return strings.TrimSpace(grant.ID) != "" && grant.ID == strings.TrimSpace(grant.ID) && grant.Tenant == tenant && grant.Skill == key &&
		validGrantDimension(grant.Roles) && strings.TrimSpace(grant.Population) != "" && grant.Population == strings.TrimSpace(grant.Population) &&
		validGrantDimension(grant.OrganizationScopes) && validGrantDimension(grant.Purposes)
}

func validGrantDimension(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return false
		}
	}
	return true
}

func tupleGrantCovers(grants []agentgate.SkillGrant, role, population, organization string) bool {
	for _, grant := range grants {
		if (grant.Population == population || grant.Population == agentgate.AnyScope) && grantDimensionCovers(grant.Roles, role) && grantDimensionCovers(grant.OrganizationScopes, organization) {
			return true
		}
	}
	return false
}

func grantDimensionCovers(granted []string, requested string) bool {
	for _, value := range granted {
		if value == requested || value == agentgate.AnyScope {
			return true
		}
	}
	return false
}

var _ agentpersona.SkillGrantResolver = (*TenantPersonaSkillGrantResolver)(nil)
var _ agentpersona.SkillGrantAudienceMatcher = (*TenantPersonaSkillGrantResolver)(nil)
