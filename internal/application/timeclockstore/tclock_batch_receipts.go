package timeclockstore

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

// WorkflowReceiptLookupAdapter composes time-store receipt correlation with
// the separate core workflow evidence reader.
type WorkflowReceiptLookupAdapter struct {
	TimeStore     *timestore.Store
	Evidence      clockservice.PunchNodeEvidenceReader
	ResolveTenant func(string) (uuid.UUID, error)
}

// LookupWorkflowReceipt returns only matching durable time and core evidence.
func (a WorkflowReceiptLookupAdapter) LookupWorkflowReceipt(ctx context.Context, tenant, device string, sequence int64) (clockservice.WorkflowReceiptBinding, bool, error) {
	if a.TimeStore == nil || a.Evidence == nil || a.ResolveTenant == nil {
		return clockservice.WorkflowReceiptBinding{}, false, clockservice.ErrUnavailable
	}
	row, found, err := a.TimeStore.LookupWorkflowReceiptContext(ctx, tenant, device, sequence)
	if err != nil || !found {
		return clockservice.WorkflowReceiptBinding{}, found, err
	}
	tenantID, err := a.ResolveTenant(tenant)
	if err != nil || tenantID == uuid.Nil {
		return clockservice.WorkflowReceiptBinding{}, false, errors.New("timeclockstore: tenant resolution failed")
	}
	evidence, found, err := a.Evidence.LoadPunchNodeEvidence(ctx, tenantID, row.InstanceID, row.ObservationID)
	if err != nil || !found || evidence.InstanceID != row.InstanceID || evidence.TenantID != tenantID || evidence.SessionID != row.SessionID || evidence.PlanDigest != row.PlanDigest {
		return clockservice.WorkflowReceiptBinding{}, false, err
	}
	return clockservice.WorkflowReceiptBinding{TenantID: row.TenantID, DeviceSequence: row.DeviceSequence, ObservationID: row.ObservationID, SessionID: row.SessionID, InstanceID: row.InstanceID.String(), WorkflowID: row.WorkflowID, PlanDigest: row.PlanDigest, StartKey: row.StartKey, Committed: true, NodeID: evidence.NodeID, Attempt: evidence.Attempt, InstanceVersion: evidence.InstanceVersion, TraceID: evidence.TraceID}, true, nil
}

var _ clockservice.WorkflowReceiptLookup = WorkflowReceiptLookupAdapter{}
