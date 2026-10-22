package chatstore

// Support for the demo seeder, and only for it. The two operations here exist
// because a seed has to be re-runnable: it must be able to ask whether it has
// already run, and to remove exactly the rooms it created without touching
// anything else in the tenant.

import (
	"context"
	"errors"
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
//
// Every table in the chat schema is accounted for in exactly one of four places:
// this list, seedPurgeCascade, seedPurgeImmutable or seedPurgeKept.
// TestTodo_CHATSEED_003_PurgeCoversEveryChatTable walks the migrated schema and
// fails when a migration adds a table that is in none of them, or a foreign key
// that would stop a conversation delete, so a reset cannot break silently again.
var seedPurgeOrder = []struct{ table, column string }{
	{"chat_reaction", "post_id_in_conversation"},
	{"chat_pin", "conversation_id"},
	{"chat_idempotency", "conversation_id"},
	// A removal made by the moderation seed (CHATSEED-002) points at its room
	// by a foreign key. Reports, notices, actions and filter hits carry no such
	// key, and the last three are append-only, so they stay behind and read as
	// items about messages that no longer exist.
	{"chat_admin_removal", "conversation_id"},
	// The persona classifier writes one row per seeded post, keyed to both the
	// post and its room, with no cascade on either.
	{"chat_persona_source_classification", "conversation_id"},
	{"chat_cursor", "conversation_id"},
	{"chat_preference", "conversation_id"},
	{"chat_thread_follow", "conversation_id"},
	// Tables that point at the room by a foreign key without a cascade. The
	// mutable ones are removed here; the append-only ones are in
	// seedPurgeImmutable and stop the purge before it starts.
	{"chat_gate_answer", "conversation_id"},
	{"chat_gate_state", "conversation_id"},
	{"chat_public_audience_principal", "conversation_id"},
	{"chat_public_audience_policy", "conversation_id"},
	{"chat_persona_channel_policy", "conversation_id"},
	{"chat_share_grant", "conversation_id"},
	{"agentux_ambient_grant", "conversation_id"},
	{"chat_post", "conversation_id"},
	{"chat_channel_policy", "conversation_id"},
	{"chat_membership", "conversation_id"},
	{"chat_conversation_idempotency", "conversation_id"},
	{"chat_conversation", "id"},
}

// seedPurgeCascade names the tables that go with their room or post because the
// foreign key says ON DELETE CASCADE. The catalogue test checks the cascade is
// really declared, so a later migration cannot turn one into a blocker unseen.
var seedPurgeCascade = []string{
	"chat_channel_poll", "chat_channel_poll_revision", "chat_channel_poll_vote",
	"chat_channel_todo", "chat_channel_todo_revision",
	"chat_channel_widget", "chat_channel_widget_revision",
	"chat_ephemeral_post", "chat_location_share", "chat_post_audience",
	"chat_post_card_segment_tally", "chat_post_card_tally", "chat_post_card_vote",
	"chat_voice_transcript", "chatrender_job", "chatrender_rendering", "chatrender_report",
	"chatscale_read_state", "chattone_reword_exemption",
}

// seedPurgeImmutable names the append-only tables that hold a foreign key to the
// room without a cascade. A room with a row in one of them cannot be deleted by
// any statement the runtime may issue, so PurgeConversations says so up front
// instead of failing on a constraint halfway through.
var seedPurgeImmutable = []string{
	"chat_gate_membership_basis", "chat_gate_read_audit",
	"chat_gate_submission_revision", "chat_gate_version",
}

// seedImmutableHistoryQuery names the first seedPurgeImmutable table that holds
// a row for any of the rooms. It is one literal so the statement catalogue the
// load tooling builds from this package resolves it; the catalogue test checks
// it mentions every table in the list.
const seedImmutableHistoryQuery = `SELECT holder FROM (
	SELECT 'chat_gate_membership_basis' AS holder FROM chat_gate_membership_basis WHERE tenant_id=$1 AND conversation_id = ANY($2)
	UNION ALL SELECT 'chat_gate_read_audit' FROM chat_gate_read_audit WHERE tenant_id=$1 AND conversation_id = ANY($2)
	UNION ALL SELECT 'chat_gate_submission_revision' FROM chat_gate_submission_revision WHERE tenant_id=$1 AND conversation_id = ANY($2)
	UNION ALL SELECT 'chat_gate_version' FROM chat_gate_version WHERE tenant_id=$1 AND conversation_id = ANY($2)
) held LIMIT 1`

// ErrSeedPurgeBlocked reports a room that holds append-only history, which a
// reset must not and cannot remove.
var ErrSeedPurgeBlocked = errors.New("conversation holds append-only history")

const (
	keptAppendOnly   = "append-only log; a seeder never rewrites it and nothing depends on it by foreign key"
	keptSetting      = "tenant or workspace setting, not owned by a room"
	keptPerPerson    = "belongs to a person or an installation, not to the seeded history"
	keptNoForeignKey = "carries a room identifier but no foreign key; left behind and reads as about a removed room"
)

// seedPurgeKept names every table a reset leaves alone, with the reason.
var seedPurgeKept = map[string]string{
	"agentux_ambient_offer": keptNoForeignKey, "agentux_ambient_optout": keptNoForeignKey,
	"agentux_ambient_read": keptAppendOnly, "agentux_ambient_task": keptNoForeignKey,
	"chat_agent_reaction_policy": keptSetting,
	"chat_app_event":             keptNoForeignKey, "chat_app_event_seen": keptPerPerson, "chat_app_installation": keptNoForeignKey,
	"chat_audit_event":       keptAppendOnly,
	"chat_filter_enablement": keptSetting, "chat_filter_hit": keptAppendOnly,
	"chat_filter_hit_review": keptAppendOnly, "chat_filter_version": keptAppendOnly,
	"chat_location_jurisdiction": keptSetting, "chat_location_policy": keptSetting,
	"chat_moderation_action": keptAppendOnly, "chat_moderation_notice": keptAppendOnly,
	"chat_moderation_permission": keptSetting, "chat_moderation_report": keptNoForeignKey,
	"chat_moderation_role": keptSetting,
	"chat_outbox":          keptAppendOnly, "chat_outbox_cursor": keptSetting, "chat_outbox_receipt": keptAppendOnly,
	"chat_personal_sidebar": keptPerPerson, "chat_post_revision": keptAppendOnly,
	"chat_quiet_hours": keptPerPerson, "chat_record_export": keptNoForeignKey,
	"chat_record_hold": keptNoForeignKey, "chat_record_inventory": keptNoForeignKey,
	"chat_retention_policy": keptSetting, "chat_saved_item": keptPerPerson,
	"chat_voice_setting": keptSetting,
	"chatlang_glossary":  keptSetting, "chatlang_pair_stat": keptSetting,
	"chatlang_report_resolution": keptNoForeignKey, "chatlang_setting": keptSetting,
	"chatlang_usage":         keptAppendOnly,
	"chatstate_permission":   keptSetting,
	"chattone_reader_choice": keptPerPerson, "chattone_reword_setting": keptSetting,
	"chattone_usage": keptAppendOnly, "chattone_workspace_setting": keptSetting,
	"goose_db_version": "the migration journal",
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
		var holder string
		switch err := tx.QueryRow(ctx, seedImmutableHistoryQuery, tenantID, conversationIDs).Scan(&holder); {
		case err == nil:
			return fmt.Errorf("%w: rows in %s", ErrSeedPurgeBlocked, holder)
		case err != dbport.ErrNoRows:
			return fmt.Errorf("check append-only history: %w", err)
		}
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
