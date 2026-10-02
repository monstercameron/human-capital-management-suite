package chat

import (
	"context"
	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"strconv"
	"strings"
)

type Chatcmd002Request struct {
	Principal                        Principal
	TenantID, ConversationID, PostID string
	ExpectedRevision                 uint64
	Mutation                         Chatcmd002Mutation
}
type Chatcmd002Repository interface {
	Chatcmd002Read(context.Context, Chatcmd002Request, func(context.Context) error) (Chatcmd002View, error)
	Chatcmd002Mutate(context.Context, Chatcmd002Request, func(context.Context) error) (Post, error)
}
type Chatcmd002Service struct {
	Chat       *Service
	Repository Chatcmd002Repository
	// Sender posts the card's message. The served composition gives it the
	// routed and audited conversation service, so a card travels the same path
	// as any other message; nil posts through Chat.
	Sender Chatcmd002Sender
	// Commands is the command registry the service enforces: a card is posted
	// by /poll or /todo, and a command that is not allowed in a conversation is
	// refused here as well as left out of the menu. The zero value is the
	// product's own registry.
	Commands Chatcmd001Registry
}

// Chatcmd002Sender is the one conversation call a card needs to be posted.
type Chatcmd002Sender interface {
	SendPost(context.Context, SendPostRequest) (Post, error)
}
type Chatcmd002PostRequest struct {
	SendPostRequest
	Card     Chatcmd002Card
	Accepted bool
}

func (s *Chatcmd002Service) authorize(ctx context.Context, r Chatcmd002Request, write bool) error {
	if s == nil || s.Chat == nil || s.Repository == nil || r.Principal.SubjectID == "" || r.Principal.TenantID == "" || r.TenantID == "" || r.ConversationID == "" {
		return ErrInvalidArgument
	}
	c, err := s.Chat.store.GetConversation(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return err
	}
	m, err := s.Chat.store.GetMembership(ctx, r.TenantID, r.ConversationID, r.Principal.TenantID, r.Principal.SubjectID)
	if err != nil || !currentReferenceMembership(m, r.TenantID, r.ConversationID, r.Principal.TenantID, r.Principal.SubjectID) {
		return ErrPermissionDenied
	}
	action := chatpolicy.ActionRead
	if write {
		action = chatpolicy.ActionPost
		// A vote is a reaction to the card, not a post: members of an
		// announcements-only channel may vote, while a locked or archived
		// channel still refuses it, as it refuses a reaction.
		if r.Mutation.Operation == "VOTE" {
			ctx = WithChannelStatusAction(ctx, chatpolicy.StatusReact)
		}
	}
	if err := s.Chat.authorize(ctx, r.Principal, c, action); err != nil {
		return err
	}
	if r.PostID != "" {
		p, err := s.Chat.store.GetPost(ctx, r.TenantID, r.ConversationID, r.PostID)
		if err != nil || p.Deleted || !s.Chat.postVisibleTo(ctx, r.Principal, c, p) {
			return ErrNotFound
		}
	}
	return nil
}
func (s *Chatcmd002Service) Post(ctx context.Context, r Chatcmd002PostRequest) (Post, error) {
	if !r.Accepted || r.IdempotencyKey == "" || r.Card.Validate() != nil || r.Card.ClosedAt != nil || r.Card.Interacted {
		return Post{}, ErrInvalidArgument
	}
	if err := s.authorize(ctx, Chatcmd002Request{Principal: r.Principal, TenantID: r.TenantID, ConversationID: r.ConversationID}, true); err != nil {
		return Post{}, err
	}
	conversation, err := s.Chat.store.GetConversation(ctx, r.TenantID, r.ConversationID)
	if err != nil {
		return Post{}, err
	}
	commands := s.Commands
	if len(commands.commands) == 0 {
		commands = Chatcmd001Defaults()
	}
	if command, ok := commands.Lookup(r.Card.Kind); !ok || !command.AllowedIn(Chatcmd001Place{Kind: conversation.Kind}) {
		return Post{}, ErrPermissionDenied
	}
	card := chatcmd003Clone(r.Card)
	if card.Poll != nil {
		for i := range card.Poll.Options {
			card.Poll.Options[i].ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(r.TenantID+"\x00"+r.ConversationID+"\x00"+r.Principal.TenantID+"\x00"+r.Principal.SubjectID+"\x00"+r.IdempotencyKey+"\x00option:"+strconv.Itoa(i))).String()
			card.Poll.Options[i].Count = 0
		}
	}
	refs := append([]Reference(nil), r.References...)
	if card.Todo != nil {
		for i := range card.Todo.Items {
			task := &card.Todo.Items[i]
			text := task.Text
			task.ChannelTodoItem = ChannelTodoItem{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(r.TenantID+"\x00"+r.ConversationID+"\x00"+r.Principal.TenantID+"\x00"+r.Principal.SubjectID+"\x00"+r.IdempotencyKey+"\x00task:"+strconv.Itoa(i))).String(), Text: text, CreatedBy: r.Principal.SubjectID, CreatedByHomeTenantID: r.Principal.TenantID}
			if task.AssigneeID != "" {
				member, err := s.Chat.store.GetMembership(ctx, r.TenantID, r.ConversationID, task.AssigneeHomeTenantID, task.AssigneeID)
				if err != nil || !currentReferenceMembership(member, r.TenantID, r.ConversationID, task.AssigneeHomeTenantID, task.AssigneeID) {
					return Post{}, ErrPermissionDenied
				}
				refs = append(refs, Reference{Kind: PersonMention, TenantID: task.AssigneeHomeTenantID, ID: task.AssigneeID, Display: task.AssigneeName, ConversationID: r.ConversationID})
			}
		}
	}
	body, err := card.Body()
	if err != nil {
		return Post{}, err
	}
	request := r.SendPostRequest
	request.Body = body
	request.References = refs
	ctx = withCardAuthority(ctx)
	if s.Sender != nil {
		return s.Sender.SendPost(ctx, request)
	}
	return s.Chat.SendPost(ctx, request)
}
func (s *Chatcmd002Service) Read(ctx context.Context, r Chatcmd002Request) (Chatcmd002View, error) {
	if err := s.authorize(ctx, r, false); err != nil {
		return Chatcmd002View{}, err
	}
	return s.Repository.Chatcmd002Read(ctx, r, func(ctx context.Context) error { return s.authorize(ctx, r, false) })
}
func (s *Chatcmd002Service) Mutate(ctx context.Context, r Chatcmd002Request) (Post, error) {
	if r.ExpectedRevision == 0 || strings.TrimSpace(r.PostID) == "" {
		return Post{}, ErrInvalidArgument
	}
	if err := s.authorize(ctx, r, true); err != nil {
		return Post{}, err
	}
	if r.Mutation.Card != nil || r.Mutation.Operation == "ADD_OPTION" {
		// Words a person adds or rewrites pass the same filters as a message.
		words := r.Mutation.Text
		if r.Mutation.Card != nil {
			if r.Mutation.Card.Validate() != nil {
				return Post{}, ErrInvalidArgument
			}
			words = r.Mutation.Card.SearchText()
		}
		c, err := s.Chat.store.GetConversation(ctx, r.TenantID, r.ConversationID)
		if err != nil {
			return Post{}, err
		}
		if err := s.Chat.checkContent(ctx, ContentInput{Principal: r.Principal, Conversation: c, Body: words, Edit: true}); err != nil {
			return Post{}, err
		}
	}
	return s.Repository.Chatcmd002Mutate(ctx, r, func(ctx context.Context) error { return s.authorize(ctx, r, true) })
}
