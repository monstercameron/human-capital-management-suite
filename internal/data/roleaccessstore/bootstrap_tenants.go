package roleaccessstore

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// BootstrapTenants bootstraps default roles, pages and features for every
// listed tenant. Bootstrap itself only inserts missing rows, so provisioning
// every tenant through here is idempotent: a tenant that already has its
// defaults keeps them untouched. Composition roots must pass every tenant
// they provision, not just the configured one, or an unlisted tenant is
// served with no permission rows.
func (s *Store) BootstrapTenants(ctx context.Context, tenants []values.TenantId, actor string) error {
	for _, tenant := range tenants {
		if err := s.Bootstrap(ctx, tenant, actor); err != nil {
			return fmt.Errorf("roleaccessstore: bootstrap tenant %s: %w", tenant, err)
		}
	}
	return nil
}
