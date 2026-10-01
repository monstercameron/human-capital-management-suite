package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

// ValidateTaskBoundModelLease verifies the issuance metadata before the egress
// dispatcher consumes the lease. Custody remains responsible for expiry,
// revocation, destination binding and replay detection.
func (s *ModelLeaseSource) ValidateTaskBoundModelLease(ctx context.Context, tenant, taskID, agentID, provider string, value lease.CredentialLease) error {
	if s == nil || ctx == nil || ctx.Err() != nil {
		return ErrModelLeaseTaskBinding
	}
	s.mu.Lock()
	issued, ok := s.issued[value.ID]
	s.mu.Unlock()
	if !ok || issued.provider != provider || issued.task.TenantID != tenant || issued.task.TaskID != taskID || issued.task.AgentID != agentID || issued.task.Workload != value.Workload || value.Tenant != tenant || value.Handle.Region != issued.region {
		return ErrModelLeaseTaskBinding
	}
	return nil
}
