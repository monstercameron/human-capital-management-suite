package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
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

// chatcmd002Ballots is what the ballot tables hold for one poll message.
type chatcmd002Ballots struct {
	counts map[string]int
	voters map[string][]chat.Chatcmd002Voter
	mine   []string
	voted  bool
}

// chatcmd002VoterNames is how many names one option lists; its count is always
// exact.
const chatcmd002VoterNames = 50

// chatcmd002ReadBallots counts a poll's votes from the ballot tables. The
// numbers in the message body are never trusted: a body can be posted by hand.
func chatcmd002ReadBallots(ctx context.Context, tx dbport.Tx, r chat.Chatcmd002Request, card chat.Chatcmd002Card) (chatcmd002Ballots, error) {
	out := chatcmd002Ballots{counts: map[string]int{}, voters: map[string][]chat.Chatcmd002Voter{}}
	rows, err := tx.Query(ctx, `SELECT home_tenant_id,subject_id,option_id FROM chat_post_card_vote WHERE tenant_id=$1 AND post_id=$2 ORDER BY voted_at,home_tenant_id,subject_id`, r.TenantID, r.PostID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var home, subject, option string
		if err := rows.Scan(&home, &subject, &option); err != nil {
			rows.Close()
			return out, err
		}
		own := home == r.Principal.TenantID && subject == r.Principal.SubjectID
		out.voted = out.voted || own
		if option == "" {
			continue
		}
		out.counts[option]++
		if len(out.voters[option]) < chatcmd002VoterNames {
			out.voters[option] = append(out.voters[option], chat.Chatcmd002Voter{HomeTenantID: home, SubjectID: subject})
		}
		if own {
			out.mine = append(out.mine, option)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	if !card.Poll.Anonymous {
		return out, nil
	}
	// An anonymous poll keeps its choices in the tally, which names nobody.
	out.counts, out.voters, out.mine = map[string]int{}, nil, nil
	tally, err := tx.Query(ctx, `SELECT option_id,votes FROM chat_post_card_tally WHERE tenant_id=$1 AND post_id=$2`, r.TenantID, r.PostID)
	if err != nil {
		return out, err
	}
	defer tally.Close()
	for tally.Next() {
		var option string
		var votes int
		if err := tally.Scan(&option, &votes); err != nil {
			return out, err
		}
		out.counts[option] = votes
	}
	return out, tally.Err()
}

// chatcmd002ResultsVisible applies the poll's setting: always, once the reader
// has voted, or once it is closed. A closed poll always shows its result.
func chatcmd002ResultsVisible(card chat.Chatcmd002Card, voted bool, now time.Time) bool {
	return card.Poll.Results == "always" || card.Closed(now) || (card.Poll.Results == "after-voting" && voted)
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
		now := time.Now()
		out.Card, out.CanManage, out.Revision = card, chatcmd002Own(post, r.Principal), uint64(post.Revision)
		out.CanAddOption = card.CanAddOption(now, out.CanManage)
		if card.Poll != nil {
			ballots, err := chatcmd002ReadBallots(ctx, tx, r, card)
			if err != nil {
				return err
			}
			out.MyOptions, out.Voted = ballots.mine, ballots.voted
			out.ResultsVisible = chatcmd002ResultsVisible(card, ballots.voted, now)
			for i := range out.Card.Poll.Options {
				out.Card.Poll.Options[i].Count = 0
				if out.ResultsVisible {
					out.Card.Poll.Options[i].Count = ballots.counts[out.Card.Poll.Options[i].ID]
				}
			}
			if out.ResultsVisible && !card.Poll.Anonymous {
				out.Voters = ballots.voters
			}
		}
		if card.Todo != nil {
			out.CanTick = map[string]bool{}
			for _, item := range card.Todo.Items {
				out.CanTick[item.ID] = !card.Closed(now) && chatcmd002CanTick(card, post, item, r.Principal)
			}
		}
		return nil
	})
	return out, err
}

// chatcmd002Vote records one person's ballot and reports whether anything
// changed. A named ballot may be changed or withdrawn while the poll is open;
// an anonymous one is final, because no record says what it was.
func chatcmd002Vote(ctx context.Context, tx dbport.Tx, r chat.Chatcmd002Request, card chat.Chatcmd002Card) (bool, error) {
	known := map[string]bool{}
	for _, option := range card.Poll.Options {
		known[option.ID] = option.ID != ""
	}
	chosen := map[string]bool{}
	var options []string
	for _, id := range r.Mutation.Options {
		if !known[id] {
			return false, chat.ErrNotFound
		}
		if !chosen[id] {
			chosen[id] = true
			options = append(options, id)
		}
	}
	if len(options) > 1 && !card.Poll.Multiple {
		return false, chat.ErrInvalidArgument
	}
	rows, err := tx.Query(ctx, `SELECT option_id FROM chat_post_card_vote WHERE tenant_id=$1 AND post_id=$2 AND home_tenant_id=$3 AND subject_id=$4 FOR UPDATE`, r.TenantID, r.PostID, r.Principal.TenantID, r.Principal.SubjectID)
	if err != nil {
		return false, err
	}
	prior := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		prior[id] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()
	if card.Poll.Anonymous {
		if len(prior) > 0 {
			return false, chat.ErrConflict
		}
		if len(options) == 0 {
			return false, chat.ErrInvalidArgument
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chat_post_card_vote(tenant_id,conversation_id,post_id,home_tenant_id,subject_id,option_id) VALUES($1,$2,$3,$4,$5,'')`, r.TenantID, r.ConversationID, r.PostID, r.Principal.TenantID, r.Principal.SubjectID); err != nil {
			return false, err
		}
		for _, id := range options {
			if _, err := tx.Exec(ctx, `INSERT INTO chat_post_card_tally(tenant_id,conversation_id,post_id,option_id,votes) VALUES($1,$2,$3,$4,1) ON CONFLICT(tenant_id,post_id,option_id) DO UPDATE SET votes=chat_post_card_tally.votes+1`, r.TenantID, r.ConversationID, r.PostID, id); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	changed := false
	for id := range prior {
		if !chosen[id] {
			changed = true
			if _, err := tx.Exec(ctx, `DELETE FROM chat_post_card_vote WHERE tenant_id=$1 AND post_id=$2 AND home_tenant_id=$3 AND subject_id=$4 AND option_id=$5`, r.TenantID, r.PostID, r.Principal.TenantID, r.Principal.SubjectID, id); err != nil {
				return false, err
			}
		}
	}
	for _, id := range options {
		if !prior[id] {
			changed = true
			if _, err := tx.Exec(ctx, `INSERT INTO chat_post_card_vote(tenant_id,conversation_id,post_id,home_tenant_id,subject_id,option_id) VALUES($1,$2,$3,$4,$5,$6)`, r.TenantID, r.ConversationID, r.PostID, r.Principal.TenantID, r.Principal.SubjectID, id); err != nil {
				return false, err
			}
		}
	}
	return changed, nil
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
		// One change to a card at a time: two votes would otherwise read the same
		// revision, and the second would write a revision that already exists.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM chat_post WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, r.TenantID, r.PostID); err != nil {
			return err
		}
		post, card, err := s.chatcmd002Load(ctx, tx, r)
		if err != nil {
			return err
		}
		mutation := r.Mutation
		// A vote or a tick does not depend on what the others did meanwhile, so it
		// is not refused for a revision the voter has not seen yet, nor is another
		// member adding an option. Closing, reopening and editing are decisions
		// about the card as its author saw it.
		if mutation.Operation != "VOTE" && mutation.Operation != "TICK" && mutation.Operation != "ADD_OPTION" && uint64(post.Revision) != r.ExpectedRevision {
			return chat.ErrConflict
		}
		if err := authorize(ctx); err != nil {
			return err
		}
		now := time.Now()
		switch mutation.Operation {
		case "VOTE":
			if card.Poll == nil || card.Closed(now) {
				return chat.ErrPermissionDenied
			}
			changed, err := chatcmd002Vote(ctx, tx, r, card)
			if err != nil {
				return err
			}
			if !changed {
				result, err = chatPost(post)
				return err
			}
			ballots, err := chatcmd002ReadBallots(ctx, tx, r, card)
			if err != nil {
				return err
			}
			// The body repeats the counts only for a named poll whose result is
			// always shown. A result held back until voting or closing is read
			// through Chatcmd002Read, which applies that setting to each reader. An
			// anonymous poll's body never changes with a vote: the revision and
			// event written below name the voter, and a count moving beside that
			// name would say what they chose.
			for i := range card.Poll.Options {
				card.Poll.Options[i].Count = 0
				if card.Poll.Results == "always" && !card.Poll.Anonymous {
					card.Poll.Options[i].Count = ballots.counts[card.Poll.Options[i].ID]
				}
			}
			card.Interacted = true
		case "TICK":
			if card.Todo == nil || card.Closed(now) {
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
		case "ADD_OPTION":
			// A poll that lets members add options takes one from any reader; any
			// other poll takes one from its author only. The card says which, and
			// the reader's own standing is read from the post, never the request.
			if !card.CanAddOption(now, chatcmd002Own(post, r.Principal)) {
				return chat.ErrPermissionDenied
			}
			card, err = card.AddOption(uuid.NewString(), mutation.Text)
			if err != nil {
				return err
			}
		case "CLOSE", "REOPEN":
			if !chatcmd002Own(post, r.Principal) {
				return chat.ErrPermissionDenied
			}
			if mutation.Operation == "CLOSE" {
				at := now.UTC()
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
