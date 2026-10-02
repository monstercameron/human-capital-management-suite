package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"time"
)

// Message cards use the post revision/outbox lifecycle. Task completion uses
// the standing list's mutation function and permission predicate.
func (s *Store) chatcmd002Load(ctx context.Context, tx dbport.Tx, r chat.Chatcmd002Request) (Post, chat.Chatcmd002Card, error) {
	if _, err := currentChannelTodoSelection(ctx, tx, r.TenantID, r.Principal.TenantID, r.ConversationID, r.Principal.SubjectID); err != nil {
		return Post{}, chat.Chatcmd002Card{}, err
	}
	post, err := s.loadPost(ctx, tx, r.TenantID, r.PostID)
	if err != nil || post.ConversationID != r.ConversationID || post.Tombstoned {
		return Post{}, chat.Chatcmd002Card{}, chat.ErrNotFound
	}
	var visibility string
	var joined time.Time
	if err := tx.QueryRow(ctx, `SELECT history_visibility,joined_at FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4`, r.TenantID, r.ConversationID, r.Principal.TenantID, r.Principal.SubjectID).Scan(&visibility, &joined); err != nil {
		return Post{}, chat.Chatcmd002Card{}, err
	}
	if visibility == "NO_HISTORY" || visibility == "NONE" || (visibility == "FROM_JOIN" && post.CreatedAt.Before(joined)) {
		return Post{}, chat.Chatcmd002Card{}, chat.ErrNotFound
	}
	card, ok := chat.Chatcmd002Decode(post.Body)
	if !ok {
		return Post{}, card, chat.ErrInvalidArgument
	}
	return post, card, nil
}
func chatcmd002Own(post Post, p chat.Principal) bool {
	return post.AuthorID == p.SubjectID && post.AuthorHomeTenantID == p.TenantID
}
func chatcmd002CanTick(card chat.Chatcmd002Card, post Post, task chat.Chatcmd002Task, p chat.Principal) bool {
	item := ChannelTodoItem{CreatedBy: post.AuthorID, CreatedByHomeTenantID: post.AuthorHomeTenantID, CompletionMode: "EVERYONE"}
	switch card.Todo.Tick {
	case "author":
		item.CompletionMode = "ME"
	case "assignee":
		item.CompletionMode = "ME"
		item.CreatedBy, item.CreatedByHomeTenantID = task.AssigneeID, task.AssigneeHomeTenantID
		if item.CreatedBy == "" {
			return false
		}
	}
	return canToggleChannelTodo(item, post.TenantID, ChannelTodoSelectedMember{HomeTenantID: p.TenantID, SubjectID: p.SubjectID})
}
func (s *Store) Chatcmd002Read(ctx context.Context, r chat.Chatcmd002Request, authorize func(context.Context) error) (chat.Chatcmd002View, error) {
	out := chat.Chatcmd002View{}
	if s == nil || authorize == nil || r.TenantID == "" || r.PostID == "" {
		return out, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, r.TenantID, func(tx dbport.Tx) error {
		post, card, err := s.chatcmd002Load(ctx, tx, r)
		if err != nil {
			return err
		}
		if err := authorize(ctx); err != nil {
			return err
		}
		out.Card, out.CanManage = card, chatcmd002Own(post, r.Principal)
		out.ResultsVisible = card.Poll != nil && (card.Poll.Results == "always" || card.ClosedAt != nil)
		if card.Todo != nil {
			out.CanTick = map[string]bool{}
			for _, item := range card.Todo.Items {
				out.CanTick[item.ID] = card.ClosedAt == nil && chatcmd002CanTick(card, post, item, r.Principal)
			}
		}
		return nil
	})
	return out, err
}
func (s *Store) Chatcmd002Mutate(ctx context.Context, r chat.Chatcmd002Request, authorize func(context.Context) error) (chat.Post, error) {
	var result chat.Post
	if s == nil || authorize == nil || r.ExpectedRevision == 0 || r.TenantID == "" || r.PostID == "" {
		return result, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, r.TenantID, func(tx dbport.Tx) error {
		if err := fenceContextWrite(ctx, tx, r.TenantID, r.ConversationID); err != nil {
			return err
		}
		post, card, err := s.chatcmd002Load(ctx, tx, r)
		if err != nil {
			return err
		}
		if uint64(post.Revision) != r.ExpectedRevision {
			return chat.ErrConflict
		}
		if err := authorize(ctx); err != nil {
			return err
		}
		mutation := r.Mutation
		switch mutation.Operation {
		case "VOTE":
			// The existing ballot store needs a message key before this can be
			// enabled; refusing avoids creating a second vote domain or fake counts.
			return chat.ErrUnavailable
		case "TICK":
			if card.Todo == nil || card.ClosedAt != nil {
				return chat.ErrPermissionDenied
			}
			found := false
			for i := range card.Todo.Items {
				task := &card.Todo.Items[i]
				if task.ID != mutation.ItemID {
					continue
				}
				found = true
				if !chatcmd002CanTick(card, post, *task, r.Principal) {
					return chat.ErrPermissionDenied
				}
				if task.Completed == mutation.Completed {
					result, err = chatPost(post)
					return err
				}
				list := ChannelTodoList{Items: []ChannelTodoItem{{ID: task.ID, Text: task.Text, Completed: task.Completed}}}
				if err := applyChannelTodoMutation(&list, r.Principal.TenantID, r.Principal.SubjectID, ChannelTodoMutation{Operation: "SET_COMPLETED", ItemID: task.ID, Completed: mutation.Completed}); err != nil {
					return err
				}
				task.Completed = list.Items[0].Completed
				task.CompletedBySubjectID = list.Items[0].CompletedBySubjectID
				task.CompletedByHomeTenantID = list.Items[0].CompletedByHomeTenantID
				task.CompletedAtUnix = list.Items[0].CompletedAtUnix
				card.Interacted = true
				break
			}
			if !found {
				return chat.ErrNotFound
			}
		case "CLOSE", "REOPEN":
			if !chatcmd002Own(post, r.Principal) {
				return chat.ErrPermissionDenied
			}
			if mutation.Operation == "CLOSE" {
				at := time.Now().UTC()
				card.ClosedAt = &at
			} else {
				card.ClosedAt = nil
				if card.Poll != nil {
					card.Poll.ClosesAt = nil
				}
			}
		case "EDIT":
			if !chatcmd002Own(post, r.Principal) || card.Interacted || mutation.Card == nil {
				return chat.ErrPermissionDenied
			}
			encoded, err := json.Marshal(mutation.Card)
			if err != nil {
				return chat.ErrInvalidArgument
			}
			var next chat.Chatcmd002Card
			if json.Unmarshal(encoded, &next) != nil {
				return chat.ErrInvalidArgument
			}
			if next.Kind != card.Kind || next.Validate() != nil || next.ClosedAt != nil || next.Interacted {
				return chat.ErrInvalidArgument
			}
			if card.Poll != nil {
				if len(next.Poll.Options) != len(card.Poll.Options) {
					return chat.ErrInvalidArgument
				}
				for i := range next.Poll.Options {
					next.Poll.Options[i].ID = card.Poll.Options[i].ID
					next.Poll.Options[i].Count = 0
				}
			} else {
				if len(next.Todo.Items) != len(card.Todo.Items) {
					return chat.ErrInvalidArgument
				}
				for i := range next.Todo.Items {
					if next.Todo.Items[i].AssigneeID != card.Todo.Items[i].AssigneeID || next.Todo.Items[i].AssigneeHomeTenantID != card.Todo.Items[i].AssigneeHomeTenantID {
						return chat.ErrInvalidArgument
					}
					task := card.Todo.Items[i]
					task.Text = next.Todo.Items[i].Text
					next.Todo.Items[i] = task
				}
			}
			next.ClosedAt = card.ClosedAt
			card = next
		default:
			return chat.ErrInvalidArgument
		}
		body, err := card.Body()
		if err != nil {
			return err
		}
		post.Body = body
		post.Revision++
		if _, err := tx.Exec(ctx, `UPDATE chat_post SET body=$1,revision=$2,updated_at=now() WHERE tenant_id=$3 AND conversation_id=$4 AND id=$5 AND tombstoned=false`, body, post.Revision, r.TenantID, r.ConversationID, r.PostID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chat_post_revision(tenant_id,post_id,revision,author_id,body,parent_id,references_json,source_attribution,tombstoned) VALUES($1,$2,$3,$4,$5,$6,$7,$8,false)`, r.TenantID, r.PostID, post.Revision, r.Principal.SubjectID, body, post.ParentID, post.References, post.SourceAttribution); err != nil {
			return err
		}
		if err := RecordRenderingRevisionTx(ctx, tx, r.TenantID, r.PostID, uint64(post.Revision), nil); err != nil {
			return err
		}
		var policyRevision, eventSequence int64
		if err := tx.QueryRow(ctx, `UPDATE chat_conversation SET event_sequence=event_sequence+1 WHERE tenant_id=$1 AND id=$2 RETURNING settings_revision,event_sequence`, r.TenantID, r.ConversationID).Scan(&policyRevision, &eventSequence); err != nil {
			return err
		}
		result, err = chatPost(post)
		if err != nil {
			return err
		}
		return writeOutbox(ctx, tx, outboxWrite{TenantID: r.TenantID, ConversationID: r.ConversationID, AggregateID: r.PostID, EventType: "post.edited", ActorHomeTenantID: r.Principal.TenantID, ActorID: r.Principal.SubjectID, TargetID: r.PostID, RecordID: "post:" + r.PostID, RecordKind: "EDIT", SourceID: r.PostID, Revision: result.Revision, PolicyRevision: policyRevision, EventSequence: eventSequence, Value: result})
	})
	if err != nil && errors.Is(err, dbport.ErrNoRows) {
		err = chat.ErrNotFound
	}
	return result, err
}
