package application

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3795: form cards. An agent run posts a form card (chat.Service.
// PostFormCard); the person it names answers it here, once; the run reads the
// answer back with Result. Every decision is made here, before anything is
// written: who may read the card, who may answer it, what the answers must
// look like. The answers are private to the person who gave them and the
// agent that asked.

var (
	ErrChatFormInvalid     = errors.New("chat form: invalid request")
	ErrChatFormNotFound    = errors.New("chat form: card not found")
	ErrChatFormDenied      = errors.New("chat form: not allowed")
	ErrChatFormConflict    = errors.New("chat form: already answered")
	ErrChatFormUnavailable = errors.New("chat form: unavailable")
)

// ChatFormInvalidError is a refused answer: Fields names each refused field by
// the code the page turns into a sentence (chat.FormErr*).
type ChatFormInvalidError struct{ Fields map[string]string }

func (e *ChatFormInvalidError) Error() string { return ErrChatFormInvalid.Error() }
func (e *ChatFormInvalidError) Unwrap() error { return ErrChatFormInvalid }

// ChatFormChat is the conversation service as form cards use it.
type ChatFormChat interface {
	ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error)
	ListMemberships(context.Context, chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error)
}

// ChatFormStore keeps the answers.
type ChatFormStore interface {
	RecordFormSubmission(context.Context, chatstore.FormSubmission) (bool, error)
	GetFormSubmission(ctx context.Context, tenant, conversation, post string) (chatstore.FormSubmission, bool, error)
	FormSubmittedAt(ctx context.Context, tenant, conversation string, posts []string) (map[string]time.Time, error)
}

// ChatFormResult is an answer handed to the run that asked. Values are keyed by
// field id exactly as the card defined them.
type ChatFormResult struct {
	ConversationID string            `json:"conversation_id"`
	PostID         string            `json:"post_id"`
	FormRef        string            `json:"form_ref,omitempty"`
	RunRef         string            `json:"run_ref,omitempty"`
	SubmittedBy    string            `json:"submitted_by"`
	SubmittedAt    time.Time         `json:"submitted_at"`
	Values         map[string]string `json:"values"`
}

// ChatFormView is what one reader may see of one form card: whether it can be
// answered by them, whether it has been, and, for the person who answered it,
// their own answers.
type ChatFormView struct {
	PostID      string            `json:"post_id"`
	CanSubmit   bool              `json:"can_submit"`
	Submitted   bool              `json:"submitted"`
	SubmittedAt *time.Time        `json:"submitted_at,omitempty"`
	Mine        map[string]string `json:"mine,omitempty"`
}

// ChatFormCards is the use case behind the form-card routes and the agent hook.
type ChatFormCards struct {
	Chat  ChatFormChat
	Store ChatFormStore
	// OnSubmitted, when set, is told after an answer is recorded so a waiting run
	// can resume. The answer is already durable when it is called, so a failure
	// is logged and never taken back: the run can still read Result.
	OnSubmitted func(context.Context, ChatFormResult) error
}

const chatFormMaxViews = 50

func (c *ChatFormCards) ready() bool { return c != nil && c.Chat != nil && c.Store != nil }

// card reads a post the reader may open and says whether it is a form card. A
// post the reader cannot open and a post that is no form read the same.
func (c *ChatFormCards) card(ctx context.Context, p chat.Principal, conversation, postID string) (chat.Post, chat.FormCard, error) {
	if strings.TrimSpace(conversation) == "" || strings.TrimSpace(postID) == "" {
		return chat.Post{}, chat.FormCard{}, ErrChatFormInvalid
	}
	_, post, err := c.Chat.ReadAuthorizedReference(ctx, p, p.TenantID, conversation, postID)
	switch {
	case err == nil:
	case errors.Is(err, chat.ErrNotFound), errors.Is(err, chat.ErrPermissionDenied):
		return chat.Post{}, chat.FormCard{}, ErrChatFormNotFound
	default:
		return chat.Post{}, chat.FormCard{}, ErrChatFormUnavailable
	}
	if post == nil || post.ID != postID || post.ConversationID != conversation || post.Deleted {
		return chat.Post{}, chat.FormCard{}, ErrChatFormNotFound
	}
	card, ok := chat.ParseFormCard(post.Body)
	if !ok {
		return chat.Post{}, chat.FormCard{}, ErrChatFormNotFound
	}
	return *post, card, nil
}

// Views reads what the person may see of each of the given posts. A post that
// is not a form card they can open is left out of the answer.
func (c *ChatFormCards) Views(ctx context.Context, human *trust.Principal, conversation string, postIDs []string) (map[string]ChatFormView, error) {
	p, err := chatWorkPrincipal(human)
	if err != nil {
		return nil, ErrChatFormDenied
	}
	if !c.ready() {
		return nil, ErrChatFormUnavailable
	}
	if strings.TrimSpace(conversation) == "" || len(postIDs) == 0 || len(postIDs) > chatFormMaxViews {
		return nil, ErrChatFormInvalid
	}
	cards := map[string]chat.FormCard{}
	var ids []string
	for _, id := range postIDs {
		if _, seen := cards[id]; seen {
			continue
		}
		_, card, err := c.card(ctx, p, conversation, id)
		switch {
		case err == nil:
			cards[id] = card
			ids = append(ids, id)
		case errors.Is(err, ErrChatFormNotFound):
		default:
			return nil, err
		}
	}
	out := map[string]ChatFormView{}
	if len(ids) == 0 {
		return out, nil
	}
	answered, err := c.Store.FormSubmittedAt(ctx, p.TenantID, conversation, ids)
	if err != nil {
		return nil, ErrChatFormUnavailable
	}
	for _, id := range ids {
		view := ChatFormView{PostID: id}
		if at, done := answered[id]; done {
			view.Submitted, view.SubmittedAt = true, &at
			// Only the person who answered is shown what they answered.
			if cards[id].Addressee == p.SubjectID {
				if sub, found, err := c.Store.GetFormSubmission(ctx, p.TenantID, conversation, id); err == nil && found && sub.SubmittedByID == p.SubjectID {
					view.Mine = sub.Answers
				}
			}
		} else {
			view.CanSubmit = cards[id].Addressee == p.SubjectID
		}
		out[id] = view
	}
	return out, nil
}

// isMember answers whether a subject may be named in a person field: an active
// member of the conversation, read as the person answering.
func (c *ChatFormCards) isMember(ctx context.Context, p chat.Principal, conversation string) func(string) (bool, error) {
	var members map[string]bool
	return func(subject string) (bool, error) {
		if members == nil {
			members = map[string]bool{}
			cursor := ""
			for page := 0; page < 50; page++ {
				res, err := c.Chat.ListMemberships(ctx, chat.ListMembershipsRequest{Principal: p, TenantID: p.TenantID, ConversationID: conversation, Page: chat.Page{Cursor: cursor, PageSize: 200}})
				if err != nil {
					return false, ErrChatFormUnavailable
				}
				for _, m := range res.Memberships {
					if m.JoinedAt != nil && m.LeftAt == nil && m.HomeTenantID == p.TenantID {
						members[m.SubjectID] = true
					}
				}
				if res.NextCursor == "" {
					break
				}
				cursor = res.NextCursor
			}
		}
		return members[subject], nil
	}
}

// Submit records the person's answers to a card. Only the person the card
// names may answer, once; every answer is checked against the card's own
// fields before anything is written.
func (c *ChatFormCards) Submit(ctx context.Context, human *trust.Principal, conversation, postID string, answers map[string]string) (ChatFormView, error) {
	p, err := chatWorkPrincipal(human)
	if err != nil {
		return ChatFormView{}, ErrChatFormDenied
	}
	if !c.ready() {
		return ChatFormView{}, ErrChatFormUnavailable
	}
	post, card, err := c.card(ctx, p, conversation, postID)
	if err != nil {
		return ChatFormView{}, err
	}
	if card.Addressee != p.SubjectID {
		return ChatFormView{}, ErrChatFormDenied
	}
	if _, done, err := c.Store.GetFormSubmission(ctx, p.TenantID, conversation, postID); err != nil {
		return ChatFormView{}, ErrChatFormUnavailable
	} else if done {
		return ChatFormView{}, ErrChatFormConflict
	}
	clean, err := card.CheckAnswers(answers, c.isMember(ctx, p, conversation))
	var invalid *chat.FormValidationError
	switch {
	case err == nil:
	case errors.As(err, &invalid):
		return ChatFormView{}, &ChatFormInvalidError{Fields: invalid.Fields}
	default:
		return ChatFormView{}, ErrChatFormUnavailable
	}
	sub := chatstore.FormSubmission{TenantID: p.TenantID, ConversationID: conversation, PostID: postID, SubmittedByID: p.SubjectID, RequestedByID: post.AuthorID, Answers: clean, SubmittedAt: time.Now().UTC()}
	created, err := c.Store.RecordFormSubmission(ctx, sub)
	if err != nil {
		return ChatFormView{}, ErrChatFormUnavailable
	}
	if !created {
		return ChatFormView{}, ErrChatFormConflict
	}
	if c.OnSubmitted != nil {
		result := ChatFormResult{ConversationID: conversation, PostID: postID, FormRef: card.FormRef, RunRef: card.RunRef, SubmittedBy: p.SubjectID, SubmittedAt: sub.SubmittedAt, Values: clean}
		if err := c.OnSubmitted(ctx, result); err != nil {
			slog.Warn("hcmnext.chat_form_resume_failed", "post", postID)
		}
	}
	at := sub.SubmittedAt
	return ChatFormView{PostID: postID, Submitted: true, SubmittedAt: &at, Mine: clean}, nil
}

// Result is the read hook of the run that asked: it returns the answers to a
// card that run (the card's author) posted, or ErrChatFormNotFound while the
// card is unanswered. Nobody else is handed the answers here.
func (c *ChatFormCards) Result(ctx context.Context, asker chat.Principal, conversation, postID string) (ChatFormResult, error) {
	if !c.ready() {
		return ChatFormResult{}, ErrChatFormUnavailable
	}
	post, card, err := c.card(ctx, asker, conversation, postID)
	if err != nil {
		return ChatFormResult{}, err
	}
	if post.AuthorID != asker.SubjectID {
		return ChatFormResult{}, ErrChatFormDenied
	}
	sub, found, err := c.Store.GetFormSubmission(ctx, asker.TenantID, conversation, postID)
	switch {
	case err != nil:
		return ChatFormResult{}, ErrChatFormUnavailable
	case !found:
		return ChatFormResult{}, ErrChatFormNotFound
	}
	return ChatFormResult{ConversationID: conversation, PostID: postID, FormRef: card.FormRef, RunRef: card.RunRef, SubmittedBy: sub.SubmittedByID, SubmittedAt: sub.SubmittedAt, Values: sub.Answers}, nil
}

type chatFormChatPair struct {
	links interface {
		ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error)
	}
	members interface {
		ListMemberships(context.Context, chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error)
	}
}

func (p chatFormChatPair) ReadAuthorizedReference(ctx context.Context, who chat.Principal, tenant, conversation, post string) (chat.Conversation, *chat.Post, error) {
	return p.links.ReadAuthorizedReference(ctx, who, tenant, conversation, post)
}

func (p chatFormChatPair) ListMemberships(ctx context.Context, r chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error) {
	return p.members.ListMemberships(ctx, r)
}

// NewChatFormCards composes form cards over the served conversation service
// and Chat store, or returns nil when either is not composed.
func NewChatFormCards(links interface {
	ReadAuthorizedReference(context.Context, chat.Principal, string, string, string) (chat.Conversation, *chat.Post, error)
}, service chat.ConversationService, store *chatstore.Store) *ChatFormCards {
	if links == nil || service == nil || store == nil {
		return nil
	}
	return &ChatFormCards{Chat: chatFormChatPair{links: links, members: service}, Store: store}
}
