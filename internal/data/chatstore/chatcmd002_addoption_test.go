package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func chatcmd002AddOption(r chat.Chatcmd002Request, subject, text string) chat.Chatcmd002Request {
	r.Principal.SubjectID = subject
	r.Mutation = chat.Chatcmd002Mutation{Operation: "ADD_OPTION", Text: text}
	return r
}

// TestTodo_CHATCMD_002_AddOption: a poll that lets members add options takes
// one from any member, and a poll that does not takes one from its author only.
// The new option is a real option (it can be voted on at once), a repeated or
// empty one is refused, and a twelfth is the last.
func TestTodo_CHATCMD_002_AddOption(t *testing.T) {
	ctx := context.Background()
	t.Run("members", func(t *testing.T) {
		s, r, post := chatcmd002PollFixture(t, func(p *chat.Chatcmd002Poll) { p.AddOptions = "members" })
		view, err := s.Chatcmd002Read(ctx, r, todoAllow)
		if err != nil || !view.CanAddOption || view.CanManage {
			t.Fatalf("a member of a poll that allows it reads %+v %v", view, err)
		}
		// The request carries a revision the card has since moved past: adding an
		// option does not depend on what others did meanwhile.
		first, err := s.Chatcmd002Mutate(ctx, chatcmd002AddOption(r, "worker", "  Salad "), todoAllow)
		if err != nil || first.Revision != post.Revision+1 {
			t.Fatalf("add %+v %v", first, err)
		}
		second, err := s.Chatcmd002Mutate(ctx, chatcmd002AddOption(r, "third", "Ramen"), todoAllow)
		if err != nil || second.Revision != first.Revision+1 {
			t.Fatalf("a second member adding against the first revision: %+v %v", second, err)
		}
		card, ok := chat.Chatcmd002Decode(second.Body)
		if !ok || len(card.Poll.Options) != 4 || card.Poll.Options[2].Text != "Salad" || card.Poll.Options[3].Text != "Ramen" || card.Poll.Options[2].ID == "" || card.Interacted {
			t.Fatalf("options after two additions %+v", card)
		}
		// The addition is votable at once and starts at no votes.
		voted, err := s.Chatcmd002Mutate(ctx, chatcmd002As(r, "owner", card.Poll.Options[2].ID), todoAllow)
		if err != nil || voted.Revision != second.Revision+1 {
			t.Fatalf("vote on the new option %+v %v", voted, err)
		}
		counts, _ := chatcmd002Counts(t, s, r)
		if counts[card.Poll.Options[2].ID] != 1 || counts["tacos"] != 0 {
			t.Fatalf("counts %v", counts)
		}
		for text, want := range map[string]error{"salad": chat.ErrConflict, "  ": chat.ErrInvalidArgument, "": chat.ErrInvalidArgument} {
			if _, err := s.Chatcmd002Mutate(ctx, chatcmd002AddOption(r, "worker", text), todoAllow); !errors.Is(err, want) {
				t.Errorf("adding %q: %v, want %v", text, err, want)
			}
		}
	})
	t.Run("author only", func(t *testing.T) {
		s, r, post := chatcmd002PollFixture(t, nil)
		view, err := s.Chatcmd002Read(ctx, r, todoAllow)
		if err != nil || view.CanAddOption {
			t.Fatalf("a member of an author-only poll reads %+v %v", view, err)
		}
		if _, err := s.Chatcmd002Mutate(ctx, chatcmd002AddOption(r, "worker", "Salad"), todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
			t.Fatalf("a member added to an author-only poll: %v", err)
		}
		if view, _ := s.Chatcmd002Read(ctx, chatcmd002AddOption(r, "owner", ""), todoAllow); !view.CanAddOption {
			t.Fatal("the author may add")
		}
		added, err := s.Chatcmd002Mutate(ctx, chatcmd002AddOption(r, "owner", "Salad"), todoAllow)
		if err != nil || added.Revision != post.Revision+1 {
			t.Fatalf("author added %+v %v", added, err)
		}
	})
	t.Run("closed and full", func(t *testing.T) {
		s, r, _ := chatcmd002PollFixture(t, func(p *chat.Chatcmd002Poll) {
			p.AddOptions = "members"
			for _, text := range []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"} {
				p.Options = append(p.Options, chat.ChannelPollOption{ID: "id-" + text, Text: text})
			}
		})
		last, err := s.Chatcmd002Mutate(ctx, chatcmd002AddOption(r, "worker", "K"), todoAllow)
		if err != nil {
			t.Fatalf("the twelfth option: %v", err)
		}
		if view, _ := s.Chatcmd002Read(ctx, r, todoAllow); view.CanAddOption {
			t.Fatal("a poll of twelve offers no more")
		}
		if _, err := s.Chatcmd002Mutate(ctx, chatcmd002AddOption(r, "worker", "L"), todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
			t.Fatalf("a thirteenth option: %v", err)
		}
		closed := chatcmd002AddOption(r, "owner", "")
		closed.ExpectedRevision, closed.Mutation = last.Revision, chat.Chatcmd002Mutation{Operation: "CLOSE"}
		if _, err := s.Chatcmd002Mutate(ctx, closed, todoAllow); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Chatcmd002Mutate(ctx, chatcmd002AddOption(r, "worker", "Z"), todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
			t.Fatalf("an option added to a closed poll: %v", err)
		}
	})
}

// TestTodo_CHATCMD_002_EditBeforeFirstVote: the author rewords a card until
// somebody votes or ticks; after that the wording is fixed, and nobody else
// may reword it at all.
func TestTodo_CHATCMD_002_EditBeforeFirstVote(t *testing.T) {
	ctx := context.Background()
	s, r, post := chatcmd002PollFixture(t, nil)
	original, _ := chat.Chatcmd002Decode(post.Body)
	next := original
	poll := *original.Poll
	poll.Options = []chat.ChannelPollOption{{Text: "Tacos please"}, {Text: "Pho"}}
	next.Title, next.Poll = "Where for lunch?", &poll
	edit := func(subject string, revision uint64, card chat.Chatcmd002Card) chat.Chatcmd002Request {
		q := chatcmd002As(r, subject)
		q.ExpectedRevision, q.Mutation = revision, chat.Chatcmd002Mutation{Operation: "EDIT", Card: &card}
		return q
	}
	if _, err := s.Chatcmd002Mutate(ctx, edit("worker", post.Revision, next), todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("a member reworded the card: %v", err)
	}
	edited, err := s.Chatcmd002Mutate(ctx, edit("owner", post.Revision, next), todoAllow)
	card, ok := chat.Chatcmd002Decode(edited.Body)
	if err != nil || !ok || card.Title != "Where for lunch?" || card.Poll.Options[0].Text != "Tacos please" || card.Poll.Options[0].ID != "tacos" || card.Interacted {
		t.Fatalf("edit %+v %+v %v", edited, card, err)
	}
	if _, err := s.Chatcmd002Mutate(ctx, edit("owner", post.Revision, next), todoAllow); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("an edit made against a stale revision: %v", err)
	}
	voted, err := s.Chatcmd002Mutate(ctx, r, todoAllow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Chatcmd002Mutate(ctx, edit("owner", voted.Revision, next), todoAllow); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("the card was reworded after its first vote: %v", err)
	}
}
