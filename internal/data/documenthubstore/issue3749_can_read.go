package documenthubstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// CanRead says whether the reader may read the document, with the same
// existence and access checks the backlinks read makes first. A document that
// does not exist and one the reader may not read both answer ErrDenied, so
// the answer never confirms that a document exists.
func (s *Store) CanRead(ctx context.Context, tenantID, docID, readerKind, readerID string) error {
	if tenantID == "" || docID == "" || readerID == "" {
		return ErrDenied
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var exists int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM document WHERE tenant_id=$1 AND id=$2 AND lifecycle<>'DISPOSED'`, tenantID, docID).Scan(&exists); err != nil || exists == 0 {
			return ErrDenied
		}
		return authorizeTx(ctx, tx, tenantID, docID, readerKind, readerID, ActionRead)
	})
}
