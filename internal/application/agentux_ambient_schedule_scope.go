package application

import (
	"context"
	"strings"

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

// agentUXAmbientScheduleOwned reports whether a schedule belongs to a reminder
// somebody set from a message. Those share the announcements' trigger store and
// outbox but are planned, delivered and retired only by the ambient agents'
// workload; the announcement worker leaves them alone (AGENTUX-068).
func agentUXAmbientScheduleOwned(id string) bool { return strings.HasPrefix(id, "ambient-") }
