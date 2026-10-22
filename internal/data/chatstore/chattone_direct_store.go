package chatstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ConversationIsDirect reports whether a conversation is a direct or group
// message (as opposed to a channel). A conversation that does not exist is not
// direct: whoever asks decides what a missing conversation means.
func (s *Store) ConversationIsDirect(ctx context.Context, tenant, conversation string) (bool, error) {
	direct := false
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var kind string
		err := tx.QueryRow(ctx, `SELECT kind FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, tenant, conversation).Scan(&kind)
		if err == dbport.ErrNoRows {
			return nil
		}
		direct = kind == "DIRECT" || kind == "GROUP"
		return err
	})
	return direct, err
}
