package application

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrewrite"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// ChattoneRenderingReader is the reader-side message selection the writing
// context reads through (ChatRenderingSurface.ReadRenderingSelection), so a
// message the writer sees masked is never sent to the model unmasked.
type ChattoneRenderingReader interface {
	ReadRenderingSelection(context.Context, chatstore.RenderingScope, string) (chatrender.Rendering, chatrender.Mark, error)
}

// ChattoneChatSource is the production ChattoneConversationSource: the few
// most recent messages the writer can read in this conversation, as the writer
// reads them, plus bounded facts about the conversation for the audience
// suggestion. It never reads another conversation.
type ChattoneChatSource struct {
	Chat       chat.ConversationService
	Renderings ChattoneRenderingReader
	Status     chat.ChannelStatusService
	Now        func() time.Time
}

// chattoneRecentScan is how many posts are looked at to find
// chatrewrite.MaxContextMessages readable ones.
const chattoneRecentScan = 12

func (s ChattoneChatSource) WritingContext(ctx context.Context, id chatrewrite.Identity) (chatrewrite.ConversationFacts, []string, error) {
	if ctx == nil || s.Chat == nil || s.Renderings == nil || !id.Valid() {
		return chatrewrite.ConversationFacts{}, nil, chatrewrite.ErrUnavailable
	}
	principal := chat.Principal{TenantID: id.Tenant, SubjectID: id.Person}
	room, err := s.Chat.GetConversation(ctx, chat.GetConversationRequest{Principal: principal, TenantID: id.Tenant, ConversationID: id.Conversation})
	if err != nil {
		return chatrewrite.ConversationFacts{}, nil, err
	}
	page, err := s.Chat.ListPosts(ctx, chat.ListPostsRequest{Principal: principal, TenantID: id.Tenant, ConversationID: id.Conversation, Descending: true, Page: chat.Page{PageSize: chattoneRecentScan}})
	if err != nil {
		return chatrewrite.ConversationFacts{}, nil, err
	}
	scope := chatstore.RenderingScope{Principal: principal, Tenant: id.Tenant, Conversation: id.Conversation}
	// The order within a page is the store's; the context is ordered by the
	// posts' own sequence, newest first here and oldest first when returned.
	posts := slices.Clone(page.Posts)
	slices.SortFunc(posts, func(a, b chat.Post) int { return cmp.Compare(b.Sequence, a.Sequence) })
	newestFirst := []string{}
	for _, post := range posts {
		if len(newestFirst) >= chatrewrite.MaxContextMessages {
			break
		}
		if post.Deleted || post.TenantID != id.Tenant || post.ConversationID != id.Conversation {
			continue
		}
		selected, _, err := s.Renderings.ReadRenderingSelection(ctx, scope, post.ID)
		if err != nil {
			// The context only informs register. A message that cannot be read as
			// this writer reads it (withheld, removed, not yet rendered, or a
			// store hiccup) contributes nothing; it never fails the writer's
			// request. A cancelled request still stops.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return chatrewrite.ConversationFacts{}, nil, ctxErr
			}
			continue
		}
		// A pending or withheld rendering has no text; it contributes nothing.
		if text := strings.TrimSpace(selected.Text); text != "" {
			newestFirst = append(newestFirst, text)
		}
	}
	recent := make([]string, 0, len(newestFirst))
	for i := len(newestFirst) - 1; i >= 0; i-- {
		recent = append(recent, newestFirst[i])
	}
	facts := chatrewrite.ConversationFacts{MemberCount: int(room.MemberCount), Purpose: chattonePurposeOf(room.Name, chatpolicy.ChannelStatus("")), Status: "ordinary"}
	for _, text := range recent {
		facts.RecentLengths = append(facts.RecentLengths, utf8.RuneCountInString(text))
		if chattoneFormalMessage(text) {
			facts.FormalMarkers++
		}
	}
	if s.Status != nil {
		if status, err := s.Status.GetChannelStatus(ctx, chat.GetConversationRequest{Principal: principal, TenantID: id.Tenant, ConversationID: id.Conversation}); err == nil {
			at := time.Now()
			if s.Now != nil {
				at = s.Now()
			}
			effective := status.Effective(at)
			facts.Purpose = chattonePurposeOf(room.Name, effective)
			switch effective {
			case chatpolicy.StatusLocked, chatpolicy.StatusArchived:
				facts.Status = "resolved"
			default:
				facts.Status = "active"
			}
		}
	}
	facts.CustomerFacing = chattoneNameHas(room.Name, "customer", "client", "external", "partner", "vendor")
	// A direct message or a small group of colleagues is the "close colleague" case.
	facts.CloseColleagues = room.Kind == chat.Direct || room.Kind == chat.Group
	return facts, recent, nil
}

func chattoneNameHas(name string, words ...string) bool {
	lower := strings.ToLower(name)
	for _, w := range words {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// chattonePurposeOf names the channel's purpose from its status and name. The
// registry's two special purposes are the only ones the suggestion uses.
func chattonePurposeOf(name string, status chatpolicy.ChannelStatus) string {
	switch {
	case status == chatpolicy.StatusAnnouncements || chattoneNameHas(name, "announce"):
		return "announcement"
	case chattoneNameHas(name, "incident", "outage", "sev1", "sev2", "oncall", "on-call"):
		return "incident"
	}
	return "discussion"
}

// chattoneFormalMessage counts a message as formal when it carries a greeting,
// a sign-off or a stock courteous phrase.
func chattoneFormalMessage(text string) bool {
	return chattoneNameHas(text, "regards", "sincerely", "dear ", "kindly", "please find", "thank you", "best wishes", "yours", "mit freundlichen", "cordialement", "saludos")
}

var _ ChattoneConversationSource = ChattoneChatSource{}
