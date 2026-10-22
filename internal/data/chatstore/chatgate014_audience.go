package chatstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PostAudienceInfos is the audience of the posts among ids that have one, as
// one person may know it (CHATGATE-014). A restricted post the person cannot read
// is left out, by the same rule as the reads that list posts, so a tag never
// tells a reader that a message they cannot open exists.
func (s *Adapter) PostAudienceInfos(ctx context.Context, tenantID, person string, ids []string) (map[string]chat.PostAudienceInfo, error) {
	if tenantID == "" || person == "" {
		return nil, chat.ErrInvalidArgument
	}
	out := map[string]chat.PostAudienceInfo{}
	if len(ids) == 0 {
		return out, nil
	}
	if len(ids) > 500 {
		ids = ids[:500]
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT post_id,segments,restricted,cardinality(member_ids) FROM chat_post_audience au WHERE au.tenant_id=$1 AND au.post_id=ANY($2) AND NOT (au.restricted AND au.author_id<>$3 AND NOT ($3=ANY(au.member_ids)))`, tenantID, ids, person)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var info chat.PostAudienceInfo
			if err := rows.Scan(&id, &info.Segments, &info.Restricted, &info.People); err != nil {
				return err
			}
			if !info.Restricted {
				info.People = 0
			}
			out[id] = info
		}
		return rows.Err()
	})
	return out, err
}

var _ chat.PostAudienceInfoReader = (*Adapter)(nil)
