package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
)

type agentScheduleRecoveryInbox interface {
	scheduled.Inbox
	GetAdmission(context.Context, string, string) (agentrun.Record, error)
	Recover(context.Context, string, string) (runstate.Run, error)
}

// reconcileAcknowledged runs only on explicit source recovery. It detects an
// Agent-only restore that lost an admission already acknowledged by Scheduling.
// Missing expired/refused work is repair evidence; original source receipts
// are immutable and never replaced with a newly refused decision.
func (s *AgentScheduleService) reconcileAcknowledged(ctx context.Context, tenant string, limit int) (int, error) {
	store, _, worker, err := s.ports(tenant)
	if err != nil {
		return 0, err
	}
	s.mu.RLock()
	inbox, ok := s.inbox.(agentScheduleRecoveryInbox)
	s.mu.RUnlock()
	if !ok {
		return 0, agentRunScheduleRepair("RECOVERY_INBOX_UNAVAILABLE", agentrun.ErrAuthorityMissing)
	}
	var failures []error
	repaired := 0
	cursor := ""
	for {
		page, next, err := store.AcknowledgedPage(ctx, tenant, cursor, limit)
		if err != nil {
			return repaired, errors.Join(append(failures, err)...)
		}
		for _, row := range page {
			record, err := inbox.GetAdmission(ctx, tenant, row.Receipt.RunRequestID)
			if errors.Is(err, agentrunstore.ErrAdmissionNotFound) {
				if row.Receipt.Decision != agentrun.DecisionAccepted || !row.Delivery.Request.Deadline.After(s.cfg.Now()) {
					failures = append(failures, agentRunScheduleRepair("ACKNOWLEDGED_ADMISSION_MISSING", scheduled.ErrReceiptConflict))
					continue
				}
				if err := worker.CheckRequest(ctx, row.Delivery.Request); err != nil {
					failures = append(failures, agentRunScheduleRepair("ACKNOWLEDGED_SOURCE_NO_LONGER_CURRENT", err))
					continue
				}
				var created bool
				record, created, err = inbox.Admit(ctx, row.Delivery.Request)
				if err == nil && created && record.Decision == agentrun.DecisionAccepted {
					repaired++
				}
			}
			if err != nil {
				failures = append(failures, err)
				continue
			}
			digest, digestErr := agentrun.AdmissionRequestDigest(record.Request)
			if digestErr != nil || agentrun.ValidateAdmissionRecord(record) != nil || record.ID != row.Receipt.RunRequestID || record.RequestDigest != row.Receipt.RequestDigest || digest != row.Receipt.RequestDigest || record.Request.Source != row.Delivery.Request.Source || record.Decision != row.Receipt.Decision || record.RefusalCode != row.Receipt.RefusalCode {
				failures = append(failures, agentRunScheduleRepair("ACKNOWLEDGED_ADMISSION_CONFLICT", scheduled.ErrReceiptConflict))
				continue
			}
			if record.Decision == agentrun.DecisionAccepted {
				if _, err := inbox.Recover(ctx, tenant, record.ID); err != nil {
					failures = append(failures, err)
				}
			}
		}
		if next == "" {
			break
		}
		cursor = next
	}
	return repaired, errors.Join(failures...)
}

func agentRunScheduleRepair(code string, cause error) error {
	return fmt.Errorf("schedule restore repair %s: %w", code, cause)
}
