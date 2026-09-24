package application

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/siemstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/securityevidence"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/siem"
)

type siemEventAppender interface {
	Append(context.Context, uuid.UUID, siemstore.AppendRequest) (siemstore.Record, error)
}

// SIEMEventIngestor projects trusted SECARCH-008 signals and routed alerts
// into the durable tenant feed. Append persists each projection with its
// outbox dispatch intent atomically.
type SIEMEventIngestor struct{ Store siemEventAppender }

func (i SIEMEventIngestor) RecordSignal(ctx context.Context, tenant uuid.UUID, signal securityevidence.Signal) (siemstore.Record, error) {
	input, err := siem.EventInputFromSignal(tenant, signal)
	if err != nil {
		return siemstore.Record{}, err
	}
	return i.append(ctx, tenant, input)
}

func (i SIEMEventIngestor) RecordAlert(ctx context.Context, tenant uuid.UUID, alert TrustedRoutedAlert) (siemstore.Record, error) {
	if alert.tenant != tenant {
		return siemstore.Record{}, ErrSIEMExportInvalid
	}
	input, err := siem.EventInputFromAlert(tenant, alert.alert)
	if err != nil {
		return siemstore.Record{}, err
	}
	return i.append(ctx, tenant, input)
}

func (i SIEMEventIngestor) append(ctx context.Context, tenant uuid.UUID, input siem.EventInput) (siemstore.Record, error) {
	if i.Store == nil || tenant == uuid.Nil || input.Tenant != tenant.String() {
		return siemstore.Record{}, siem.ErrInvalidEvent
	}
	record, err := i.Store.Append(ctx, tenant, siemstore.AppendRequest{
		Type: string(input.Type), OccurredAt: input.OccurredAt, SourceRef: input.SourceRef,
		EvidenceDigest: input.EvidenceDigest, RuleID: input.RuleID, RuleVersion: input.RuleVersion,
	})
	if err != nil {
		return siemstore.Record{}, fmt.Errorf("application: persist SIEM event: %w", err)
	}
	return record, nil
}
