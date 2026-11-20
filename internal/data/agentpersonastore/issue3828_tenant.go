package agentpersonastore

import "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"

// Tenant is the workspace this scoped store reads and writes. Local demo
// preparation uses it to pick that workspace's seeded administrator.
func (s *TenantStore) Tenant() values.TenantId { return s.tenant }
