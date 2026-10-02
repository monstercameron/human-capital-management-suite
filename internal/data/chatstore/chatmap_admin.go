package chatstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ListLocationJurisdictions returns the country rows of a workspace for its
// settings page, "*" (everyone) first.
func (s *Store) ListLocationJurisdictions(ctx context.Context, tenant string) ([]chat.LocationJurisdiction, error) {
	rows := []chat.LocationJurisdiction{}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		result, err := tx.Query(ctx, `SELECT country,enabled,basis FROM chat_location_jurisdiction WHERE tenant_id=$1 ORDER BY (country<>'*'),country LIMIT 300`, tenant)
		if err != nil {
			return err
		}
		defer result.Close()
		for result.Next() {
			var j chat.LocationJurisdiction
			if err = result.Scan(&j.Country, &j.Enabled, &j.Basis); err != nil {
				return err
			}
			rows = append(rows, j)
		}
		return result.Err()
	})
	return rows, err
}
