package chatstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var _ chat.SavedLabelStore = (*Store)(nil)

// RenameSavedLabel renames one label on every saved item of the caller, in one
// statement. Labels that differ only in case are one label, so renaming into a
// name an item already carries merges the two and keeps the item's order. No
// saved item is deleted or moved.
func (s *Store) RenameSavedLabel(ctx context.Context, p chat.Principal, from, to string) (int, error) {
	if from == "" || to == "" {
		return 0, chat.ErrInvalidArgument
	}
	var changed int
	err := s.chatsaveTx(ctx, p, p.TenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE chat_saved_item SET labels=ARRAY(SELECT (array_agg(l ORDER BY o))[1] FROM unnest(array_replace(labels,(SELECT x FROM unnest(labels) x WHERE lower(x)=lower($4) LIMIT 1),$5)) WITH ORDINALITY u(l,o) GROUP BY lower(l) ORDER BY min(o)) WHERE tenant_id=$1 AND home_tenant_id=$2 AND person_id=$3 AND lower($4)=ANY(ARRAY(SELECT lower(x) FROM unnest(labels) x))`, p.TenantID, p.TenantID, p.SubjectID, from, to)
		if err != nil {
			return err
		}
		changed = int(n)
		return nil
	})
	return changed, err
}

// RemoveSavedLabel takes one label off every saved item of the caller.
func (s *Store) RemoveSavedLabel(ctx context.Context, p chat.Principal, label string) (int, error) {
	if label == "" {
		return 0, chat.ErrInvalidArgument
	}
	var changed int
	err := s.chatsaveTx(ctx, p, p.TenantID, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE chat_saved_item SET labels=ARRAY(SELECT x FROM unnest(labels) WITH ORDINALITY u(x,o) WHERE lower(x)<>lower($4) ORDER BY o) WHERE tenant_id=$1 AND home_tenant_id=$2 AND person_id=$3 AND lower($4)=ANY(ARRAY(SELECT lower(x) FROM unnest(labels) x))`, p.TenantID, p.TenantID, p.SubjectID, label)
		if err != nil {
			return err
		}
		changed = int(n)
		return nil
	})
	return changed, err
}
