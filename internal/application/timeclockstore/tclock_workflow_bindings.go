package timeclockstore

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

// WorkflowBindingAdapter persists runtime identities while resolving the
// canonical logical tenant key explicitly at the composition boundary.
type WorkflowBindingAdapter struct {
	Store         *timestore.Store
	ResolveTenant func(uuid.UUID) (string, error)
}

// Save persists the immutable workflow identity for a clock session.
func (a WorkflowBindingAdapter) Save(ctx context.Context, binding clockservice.TimeClockRunBinding) error {
	if a.Store == nil {
		return clockservice.ErrUnavailable
	}
	if binding.TenantID == uuid.Nil || binding.TenantKey == "" {
		return errors.New("timeclockstore: tenant is required")
	}
	if a.ResolveTenant != nil {
		key, err := a.ResolveTenant(binding.TenantID)
		if err != nil || key != binding.TenantKey {
			return errors.New("timeclockstore: tenant mapping mismatch")
		}
	}
	return a.Store.BindWorkflowSessionRun(ctx, timestore.WorkflowSessionRun{
		TenantID: binding.TenantKey, SessionID: binding.SessionID, InstanceID: binding.InstanceID,
		WorkflowID: binding.WorkflowID, PlanDigest: binding.PlanDigest, StartKey: binding.StartKey,
		CorrelationID: binding.CorrelationID, CreatedAt: binding.CreatedAt,
	})
}

// Load retrieves the immutable workflow identity for a clock session.
func (a WorkflowBindingAdapter) Load(ctx context.Context, tenant uuid.UUID, session string) (clockservice.TimeClockRunBinding, error) {
	if a.Store == nil {
		return clockservice.TimeClockRunBinding{}, clockservice.ErrUnavailable
	}
	if a.ResolveTenant == nil {
		return clockservice.TimeClockRunBinding{}, errors.New("timeclockstore: tenant resolver is required")
	}
	key, err := a.ResolveTenant(tenant)
	if err != nil {
		return clockservice.TimeClockRunBinding{}, err
	}
	row, err := a.Store.LoadWorkflowSessionRun(ctx, key, session)
	if err != nil {
		return clockservice.TimeClockRunBinding{}, err
	}
	return clockservice.TimeClockRunBinding{TenantID: tenant, TenantKey: key, SessionID: row.SessionID, InstanceID: row.InstanceID,
		WorkflowID: row.WorkflowID, PlanDigest: row.PlanDigest, StartKey: row.StartKey, CorrelationID: row.CorrelationID, CreatedAt: row.CreatedAt}, nil
}

var _ clockservice.RunBindingStore = WorkflowBindingAdapter{}
