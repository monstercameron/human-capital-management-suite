package application

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type ChatmapExpirySweep struct {
	Repo chat.LocationRepository
	Now  chat.Clock
}

// RunOnce is the tenant-scoped task invoked by the trusted maintenance runner.
// It contains no position or person identifier in its result.
func (s ChatmapExpirySweep) RunOnce(ctx context.Context, tenantID string) (int64, error) {
	if s.Repo == nil {
		return 0, chat.ErrUnavailable
	}
	if strings.TrimSpace(tenantID) == "" {
		return 0, chat.ErrInvalidArgument
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	return s.Repo.SweepLocations(ctx, tenantID, now)
}
