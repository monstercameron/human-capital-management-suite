package application

import (
	"context"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DatabaseCommonAgentStores keeps admission and execution in the isolated
// agent database and maps both to the same fixed tenant projection.
type DatabaseCommonAgentStores struct {
	Agents     *agentstore.Store
	TenantUUID func(values.TenantId) uuid.UUID
	SourceKeys agentrunstore.AdmissionSourceKeyResolver
}

func (s DatabaseCommonAgentStores) ForTenant(ctx context.Context, tenant string) (CommonAgentAdmissionStore, runstate.Store, error) {
	if ctx == nil || s.Agents == nil || s.TenantUUID == nil {
		return nil, nil, agentrunstore.ErrAdmissionNotFound
	}
	var inbox *agentrunstore.AdmissionRepository
	var err error
	if s.SourceKeys == nil {
		inbox, err = agentrunstore.NewAdmissionRepository(s.Agents, s.TenantUUID(values.TenantId(tenant)), values.TenantId(tenant))
	} else {
		inbox, err = agentrunstore.NewAdmissionRepositoryWithSourceResolver(s.Agents, s.TenantUUID(values.TenantId(tenant)), values.TenantId(tenant), s.SourceKeys)
	}
	if err != nil {
		return nil, nil, err
	}
	executions, err := agentrunstate.New(s.Agents, func(ref string) uuid.UUID { return s.TenantUUID(values.TenantId(ref)) })
	if err != nil {
		return nil, nil, err
	}
	store, err := executions.ForTenant(tenant)
	return inbox, store, err
}
