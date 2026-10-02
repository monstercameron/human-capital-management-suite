package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
)

// The native scheduler outbox is shared with other announcement sources.
// This adapter claims only the ambient schedules supplied by its workload.
type agentUXAmbientScheduleOutbox struct {
	scheduled.Outbox
	ids map[string]bool
}

func (s agentUXAmbientScheduleOutbox) Pending(ctx context.Context, tenant string, limit int) ([]scheduled.Delivery, error) {
	if limit < 1 || limit > 1000 {
		return nil, ErrAgentUXAmbientInvalid
	}
	all, err := s.Outbox.Pending(ctx, tenant, 1000)
	if err != nil {
		return nil, err
	}
	result := []scheduled.Delivery{}
	for _, d := range all {
		if s.ids[d.ScheduleID] && d.TenantID == tenant {
			result = append(result, d)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}
