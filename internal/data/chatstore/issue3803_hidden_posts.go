package chatstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PostsHiddenFrom names the posts among ids that one person may not read
// (#3803): a restricted post whose author is someone else and whose frozen
// audience leaves the person out. It is the rule of PostReadableBy for a list.
func (s *Adapter) PostsHiddenFrom(ctx context.Context, tenantID, person string, ids []string) (map[string]bool, error) {
	if tenantID == "" || person == "" {
		return nil, chat.ErrInvalidArgument
	}
	hidden := map[string]bool{}
	if len(ids) == 0 {
		return hidden, nil
	}
	if len(ids) > 500 {
		return nil, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT au.post_id FROM chat_post_audience au WHERE au.tenant_id=$1 AND au.post_id=ANY($2) AND au.restricted AND au.author_id<>$3 AND NOT ($3=ANY(au.member_ids))`, tenantID, ids, person)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			hidden[id] = true
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return hidden, nil
}

var _ chat.PostAudienceHider = (*Adapter)(nil)
