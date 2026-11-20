package chatstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// issue3947ChangeTask applies CHANGE_TASK (#3947) to a loaded card inside the
// mutation's transaction, with the post row already locked and the expected
// revision already checked. Only the list's author may change a task. A person
// given the task must be a current member of the host conversation who can read
// this post, so the task is never shown to someone outside it; the card's own
// audience stays as posted, and a card addressed to segments takes no new
// assignee because the gate vouched for the people it was posted to. changed is
// false when the task already reads as asked.
func (s *Store) issue3947ChangeTask(ctx context.Context, tx dbport.Tx, r chat.Chatcmd002Request, post Post, card chat.Chatcmd002Card, now time.Time) (chat.Chatcmd002Card, bool, error) {
	if !chatcmd002Own(post, r.Principal) {
		return card, false, chat.ErrPermissionDenied
	}
	change := r.Mutation.TaskChange()
	next, changed, err := card.ChangeTask(change, r.Principal, now)
	if err != nil || !changed {
		return card, false, err
	}
	if change.SetAssignee && change.AssigneeID != "" && strings.Contains(taskChangeKind(next, change.ItemID), "assignee") {
		if len(card.Audience) > 0 {
			return card, false, chat.ErrPermissionDenied
		}
		var visibility string
		var joined time.Time
		err := tx.QueryRow(ctx, `SELECT history_visibility,joined_at FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND state='active' AND left_at IS NULL FOR SHARE`, r.TenantID, r.ConversationID, change.AssigneeHomeTenantID, change.AssigneeID).Scan(&visibility, &joined)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return card, false, chat.ErrPermissionDenied
			}
			return card, false, err
		}
		if visibility == "NO_HISTORY" || visibility == "NONE" || (visibility == "FROM_JOIN" && post.CreatedAt.Before(joined)) {
			return card, false, chat.ErrPermissionDenied
		}
	}
	return next, true, nil
}

func taskChangeKind(card chat.Chatcmd002Card, item string) string {
	for _, task := range card.Todo.Items {
		if task.ID == item {
			return task.Changed
		}
	}
	return ""
}
