package agenttriggerstore

import (
	"context"
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type AcknowledgedDelivery struct {
	Delivery scheduled.Delivery
	Receipt  scheduled.Receipt
}

// AcknowledgedPage lets independent Agent restore verify source acknowledgements
// against its restored inbox. A source acknowledgement is retained even when
// its original admitted deadline has elapsed.
func (s *Store) AcknowledgedPage(ctx context.Context, tenant, cursor string, limit int) ([]AcknowledgedDelivery, string, error) {
	if err := s.scoped(tenant); err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > 1000 {
		return nil, "", scheduled.ErrInvalidFiring
	}
	var result []AcknowledgedDelivery
	err := s.runner.RunTenantTx(ctx, s.tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT o.source_key,o.request_digest,o.payload,r.payload FROM agent_schedule_outbox o JOIN agent_schedule_receipt r ON r.tenant_id=o.tenant_id AND r.source_key=o.source_key WHERE o.tenant_id=$1 AND o.source_key>$2 ORDER BY o.source_key LIMIT $3`, s.tenantID, cursor, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key, digest string
			var deliveryBytes, receiptBytes []byte
			var row AcknowledgedDelivery
			if err := rows.Scan(&key, &digest, &deliveryBytes, &receiptBytes); err != nil {
				return err
			}
			if err := json.Unmarshal(deliveryBytes, &row.Delivery); err != nil {
				return err
			}
			if err := json.Unmarshal(receiptBytes, &row.Receipt); err != nil {
				return err
			}
			actual, err := agentrun.AdmissionRequestDigest(row.Delivery.Request)
			if err != nil || actual != digest || row.Delivery.TenantID != tenant || row.Delivery.Key != key || row.Receipt.SourceKey != key || row.Receipt.RequestDigest != digest || row.Receipt.RunRequestID == "" {
				return scheduled.ErrReceiptConflict
			}
			result = append(result, row)
		}
		return rows.Err()
	})
	next := ""
	if len(result) == limit {
		next = result[len(result)-1].Delivery.Key
	}
	return result, next, err
}
