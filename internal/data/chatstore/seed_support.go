package chatstore

// Support for the demo seeder, and only for it. The two operations here exist
// because a seed has to be re-runnable: it must be able to ask whether it has
// already run, and to remove exactly the rooms it created without touching
// anything else in the tenant.

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// ConversationExists reports whether one conversation row is present in a
// tenant. It is a existence check and nothing more: no membership, no policy, no
// content.
func (s *Store) ConversationExists(ctx context.Context, tenantID, conversationID string) (bool, error) {
	found := false
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var one int
		scanErr := tx.QueryRow(ctx, `SELECT 1 FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, tenantID, conversationID).Scan(&one)
		if scanErr == nil {
			found = true
			return nil
		}
		if scanErr == dbport.ErrNoRows {
			return nil
		}
		return scanErr
	})
	return found, err
}

// seedPurgeOrder is the delete order for one conversation's rows: children
// before parents, so no statement depends on a cascade that may or may not be
// declared.
//
// chat_audit_event, chat_outbox and chat_post_revision are deliberately absent.
// All three carry a forbid-mutation trigger, and a seeder is the last thing that
// should be allowed to rewrite an append-only log. Nothing depends on them by
// foreign key, and a reseed mints fresh post identifiers, so the retained rows
// neither collide with the new history nor make it unreadable.
var seedPurgeOrder = []struct{ table, column string }{
	{"chat_reaction", "post_id_in_conversation"},
	{"chat_pin", "conversation_id"},
	{"chat_idempotency", "conversation_id"},
	{"chat_cursor", "conversation_id"},
	{"chat_preference", "conversation_id"},
	{"chat_thread_follow", "conversation_id"},
	{"chat_post", "conversation_id"},
	{"chat_membership", "conversation_id"},
	{"chat_conversation_idempotency", "conversation_id"},
	{"chat_conversation", "id"},
}

// PurgeConversations removes the named conversations and everything hanging off
// them. It is scoped to one tenant and to an explicit identifier list; there is
// deliberately no "purge all" form, because a seeder's reset must not be one
// typo away from emptying a tenant.
func (s *Store) PurgeConversations(ctx context.Context, tenantID string, conversationIDs []string) error {
	if len(conversationIDs) == 0 {
		return nil
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		for _, spec := range seedPurgeOrder {
			var statement string
			switch spec.column {
			case "post_id_in_conversation":
				statement = fmt.Sprintf(`DELETE FROM %s WHERE tenant_id=$1 AND post_id IN (SELECT id FROM chat_post WHERE tenant_id=$1 AND conversation_id = ANY($2))`, spec.table)
			default:
				statement = fmt.Sprintf(`DELETE FROM %s WHERE tenant_id=$1 AND %s = ANY($2)`, spec.table, spec.column)
			}
			if _, err := tx.Exec(ctx, statement, tenantID, conversationIDs); err != nil {
				return fmt.Errorf("purge %s: %w", spec.table, err)
			}
		}
		return nil
	})
}
