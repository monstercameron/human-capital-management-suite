package timestore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// LookupPunchReceipt reads one immutable device receipt for replay handling.
// The lookup is intentionally separate from preparation so a retry can return
// its original rejection without re-resolving a worker or session.
func (s *Store) LookupPunchReceipt(ctx context.Context, tenant, device string, sequence int64) (ReceiptRow, bool, error) {
	if tenant == "" || device == "" || sequence <= 0 {
		return ReceiptRow{}, false, ErrInvalid
	}
	var result ReceiptRow
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		row, found, err := readBatchReceipt(ctx, tx, tenant, device, sequence)
		if err != nil {
			return err
		}
		if found {
			result = row
		}
		return nil
	})
	return result, result.DeviceSequence != 0, err
}
