package agenteval

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var ErrSyntheticScopeMap = errors.New("agenteval: invalid trusted synthetic tenant scope map")

// NewStaticSyntheticTenantStorageScopeResolver creates an exact, immutable
// mapping from canonical suite tenant IDs to their provisioned agent-store
// UUIDs. It never derives UUIDs from tenant names.
func NewStaticSyntheticTenantStorageScopeResolver(bindings map[values.TenantId]uuid.UUID) (SyntheticTenantStorageScopeResolver, error) {
	if len(bindings) == 0 {
		return nil, ErrSyntheticScopeMap
	}
	byTenant := make(map[values.TenantId]uuid.UUID, len(bindings))
	byUUID := make(map[uuid.UUID]values.TenantId, len(bindings))
	for tenant, id := range bindings {
		if err := tenant.Validate(); err != nil || id == uuid.Nil || strings.TrimSpace(string(tenant)) != string(tenant) {
			return nil, fmt.Errorf("%w: tenant and UUID must be canonical", ErrSyntheticScopeMap)
		}
		if prior, exists := byUUID[id]; exists {
			return nil, fmt.Errorf("%w: tenants %q and %q share one storage UUID", ErrSyntheticScopeMap, prior, tenant)
		}
		byTenant[tenant] = id
		byUUID[id] = tenant
	}
	return func(ctx context.Context, tenant values.TenantId) (uuid.UUID, error) {
		if ctx == nil || ctx.Err() != nil {
			if ctx == nil {
				return uuid.Nil, ErrSyntheticScopeMap
			}
			return uuid.Nil, ctx.Err()
		}
		if err := tenant.Validate(); err != nil {
			return uuid.Nil, ErrSyntheticScopeMap
		}
		id, ok := byTenant[tenant]
		if !ok {
			return uuid.Nil, ErrSyntheticScopeMap
		}
		return id, nil
	}, nil
}
