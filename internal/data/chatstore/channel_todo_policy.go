package chatstore

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func channelTodoItemByID(items []ChannelTodoItem, id string) *ChannelTodoItem {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

// currentChannelTodoSelection captures the exact membership and accepted
// bilateral grant generation. A leave/rejoin or renewed grant never restores
// a prior selection without a new creator action.
func currentChannelTodoSelection(ctx context.Context, tx dbport.Tx, host, home, conversation, subject string) (ChannelTodoSelectedMember, error) {
	out := ChannelTodoSelectedMember{HomeTenantID: home, SubjectID: subject}
	var joined time.Time
	err := tx.QueryRow(ctx, `SELECT revision,joined_at FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND state='active' AND left_at IS NULL FOR SHARE`, host, conversation, home, subject).Scan(&out.MembershipRevision, &joined)
	if errors.Is(err, dbport.ErrNoRows) {
		return out, chat.ErrPermissionDenied
	}
	if err != nil {
		return out, err
	}
	out.JoinedAtUnixNano = joined.UnixNano()
	if host != home {
		err = tx.QueryRow(ctx, `SELECT id,version FROM chat_share_grant WHERE tenant_id=$1 AND conversation_id=$2 AND consumer_tenant=$3 AND accepted_at IS NOT NULL AND revoked_at IS NULL AND expires_at>now() FOR SHARE`, host, conversation, home).Scan(&out.GrantID, &out.GrantVersion)
		if errors.Is(err, dbport.ErrNoRows) {
			return out, chat.ErrPermissionDenied
		}
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func channelTodoCreator(item ChannelTodoItem, host string, actor ChannelTodoSelectedMember) bool {
	creatorHome := item.CreatedByHomeTenantID
	if creatorHome == "" {
		creatorHome = host
	}
	return creatorHome == actor.HomeTenantID && item.CreatedBy == actor.SubjectID
}

func canToggleChannelTodo(item ChannelTodoItem, host string, actor ChannelTodoSelectedMember) bool {
	switch item.CompletionMode {
	case "", "EVERYONE":
		return true
	case "ME":
		return channelTodoCreator(item, host, actor)
	case "ME_AND_SELECTED":
		if channelTodoCreator(item, host, actor) {
			return true
		}
		for _, selected := range item.SelectedCompleters {
			if sameChannelTodoSelection(selected, actor) {
				return true
			}
		}
	}
	return false
}

func sameChannelTodoSelection(a, b ChannelTodoSelectedMember) bool {
	return a.HomeTenantID == b.HomeTenantID && a.SubjectID == b.SubjectID && a.MembershipRevision == b.MembershipRevision && a.JoinedAtUnixNano == b.JoinedAtUnixNano && a.GrantID == b.GrantID && a.GrantVersion == b.GrantVersion
}

func bindChannelTodoSelections(ctx context.Context, tx dbport.Tx, host, conversation string, creator ChannelTodoSelectedMember, m ChannelTodoMutation) ([]ChannelTodoSelectedMember, error) {
	if m.CompletionMode != "ME_AND_SELECTED" {
		return nil, nil
	}
	selected := make([]ChannelTodoSelectedMember, 0, len(m.SelectedCompleters))
	seen := make(map[string]bool, len(m.SelectedCompleters))
	for _, candidate := range m.SelectedCompleters {
		if candidate.HomeTenantID == "" || candidate.SubjectID == "" {
			return nil, chat.ErrInvalidArgument
		}
		key := candidate.HomeTenantID + "\x00" + candidate.SubjectID
		if seen[key] || (candidate.HomeTenantID == creator.HomeTenantID && candidate.SubjectID == creator.SubjectID) {
			return nil, chat.ErrInvalidArgument
		}
		seen[key] = true
		bound, err := currentChannelTodoSelection(ctx, tx, host, candidate.HomeTenantID, conversation, candidate.SubjectID)
		if err != nil {
			return nil, err
		}
		selected = append(selected, bound)
	}
	return selected, nil
}
