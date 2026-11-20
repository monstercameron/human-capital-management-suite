package chatstore

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3735: delivery of a due saved-message reminder.
//
// chat_saved_item is fenced to one person at a time, so the scheduler cannot
// ask it "who has a reminder due". ListSavedReminderPeople asks it person by
// person, for the people the workspace has chat memberships for (any state, so
// someone who has since left every channel is still reminded).

// SavedReminderNotice names one reminder to deliver. It carries no message text
// and no note.
type SavedReminderNotice struct {
	Home, Person, Host, Conversation, Post, Key string
}

// ListSavedReminderPeople returns the people of one workspace with at least one
// saved to-do whose due time has passed and which has not been delivered.
func (s *Store) ListSavedReminderPeople(ctx context.Context, tenant string) ([]string, error) {
	if strings.TrimSpace(tenant) == "" {
		return nil, chat.ErrInvalidArgument
	}
	out := []string{}
	err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT member_id FROM chat_membership WHERE tenant_id=$1 AND home_tenant_id=$1 ORDER BY member_id`, tenant)
		if err != nil {
			return err
		}
		var candidates []string
		for rows.Next() {
			var who string
			if err = rows.Scan(&who); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, who)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, who := range candidates {
			if _, err = tx.Exec(ctx, `SELECT set_config('hcmnext.saved_home_tenant_id',$1,true),set_config('hcmnext.saved_person_id',$2,true)`, tenant, who); err != nil {
				return err
			}
			var due bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM chat_saved_item WHERE tenant_id=$1 AND home_tenant_id=$1 AND person_id=$2 AND state='todo' AND due_at IS NOT NULL AND due_at<=now() AND reminded_at IS NULL)`, tenant, who).Scan(&due); err != nil {
				return err
			}
			if due {
				out = append(out, who)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// NotifySavedReminder writes the reminder as a private notice for its owner in
// the owner's own workspace. The key is the reminder's identity (it changes with
// the due time), so a replay is the same notice. The conversation is recorded
// only when it lives in the owner's workspace.
func (s *Store) NotifySavedReminder(ctx context.Context, n SavedReminderNotice) error {
	if strings.TrimSpace(n.Home) == "" || strings.TrimSpace(n.Person) == "" || n.Key == "" || n.Post == "" {
		return chat.ErrInvalidArgument
	}
	conversation := n.Conversation
	if n.Host != n.Home {
		conversation = ""
	}
	return s.RunTenantTx(ctx, n.Home, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_notice(tenant_id,id,conversation_id,post_id,home_tenant_id,subject_id,reason,outcome,at_time) VALUES($1,$2,$3,$4,$1,$5,'',$6,now()) ON CONFLICT DO NOTHING`, n.Home, n.Key, conversation, n.Post, n.Person, chat.ModerationOutcomeSavedReminder)
		return err
	})
}
