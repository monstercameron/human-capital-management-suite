package chat

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// ReaderMasker is the optional half of a content policy that decides what a
// reader sees of messages other people wrote (CHATMOD-002). The stored post and
// its revisions are never changed: the masked text is derived on every read, so
// a rule that is switched off shows the original again and a rule switched on
// covers history.
//
// MaskBodies returns one body per post, in order. A refusal to decide returns
// the error together with the unmasked bodies so a caller can fail closed.
type ReaderMasker interface {
	MaskBodies(ctx context.Context, reader Principal, posts []Post) ([]string, error)
}

// maskedPlaceholder is shown instead of a body the filters could not be asked
// about. A reader never receives a possibly listed term because a lookup failed.
const maskedPlaceholder = "[removed word]"

// maskReaderBodies applies the content policy's mask to every post in place.
// The slice must be a copy the caller owns.
func (s *Service) maskReaderBodies(ctx context.Context, reader Principal, posts []Post) {
	masker, ok := s.contentPolicy.(ReaderMasker)
	if !ok || len(posts) == 0 {
		return
	}
	bodies, err := masker.MaskBodies(ctx, reader, posts)
	if err != nil || len(bodies) != len(posts) {
		for i := range posts {
			if !posts[i].Deleted && strings.TrimSpace(posts[i].Body) != "" {
				posts[i].Body = maskedPlaceholder
			}
		}
		return
	}
	for i := range posts {
		posts[i].Body = bodies[i]
	}
}

// MaskWatchEvent is the reader's masked view of one live watch event. The
// served stream calls it for every event, whether or not the document hub has
// bound the fuller projection.
func (s *Service) MaskWatchEvent(ctx context.Context, p Principal, tenant, conversation string, event WatchEvent) WatchEvent {
	if event.Event.Post != nil {
		posts := []Post{*event.Event.Post}
		s.maskReaderBodies(ctx, p, posts)
		event.Event.Post = &posts[0]
	}
	if event.Event.Pin != nil && event.Event.Pin.Post != nil {
		posts := []Post{*event.Event.Pin.Post}
		s.maskReaderBodies(ctx, p, posts)
		pin := *event.Event.Pin
		pin.Post = &posts[0]
		event.Event.Pin = &pin
	}
	return event
}

// maskOwnPost masks one post for the person who just wrote it: the author sees
// what readers see, never a silently different text.
func (s *Service) maskOwnPost(ctx context.Context, p Principal, post Post) Post {
	posts := []Post{post}
	s.maskReaderBodies(ctx, p, posts)
	return posts[0]
}

// MaskBodies implements ReaderMasker for the filter policy. Work is bounded by
// the number of distinct conversations and, for a message that a rule does
// match, the number of distinct authors on the page: a page nothing matches
// costs one evaluation per post and no author or conversation lookups beyond
// the first per conversation.
func (p *FilterContentPolicy) MaskBodies(ctx context.Context, reader Principal, posts []Post) ([]string, error) {
	out := make([]string, len(posts))
	for i := range posts {
		out[i] = posts[i].Body
	}
	if p == nil || p.Filters == nil {
		return out, chatfilter.ErrUnavailable
	}
	type conversationKey struct{ tenant, id string }
	direct := map[conversationKey]bool{}
	active := map[conversationKey]bool{}
	type authorKey struct{ home, id string }
	authors := map[authorKey]chatfilter.Input{}
	for i, post := range posts {
		if post.Deleted || strings.TrimSpace(post.Body) == "" {
			continue
		}
		// Nothing is enabled by default: skip every post of a conversation whose
		// workspace has no rule on, without compiling or matching anything.
		ak := conversationKey{post.TenantID, post.ConversationID}
		on, asked := active[ak]
		if !asked {
			var err error
			if on, err = p.Filters.HasActive(ctx, post.TenantID, post.ConversationID); err != nil {
				return out, err
			}
			active[ak] = on
		}
		if !on {
			continue
		}
		input := chatfilter.Input{Tenant: post.TenantID, Channel: post.ConversationID, Subject: post.AuthorID, Body: post.Body}
		result, err := p.Filters.Evaluate(ctx, input, false)
		if err != nil {
			return out, err
		}
		if result.Masked == post.Body {
			continue
		}
		// Something matched, so the remaining facts are worth a lookup. In a
		// direct or group conversation only the workspace's hard rules apply.
		ck := conversationKey{post.TenantID, post.ConversationID}
		isDirect, known := direct[ck]
		if !known {
			c := Conversation{ID: post.ConversationID, TenantID: post.TenantID}
			if p.Conversations != nil {
				c, err = p.Conversations.GetConversation(ctx, post.TenantID, post.ConversationID)
				if err != nil {
					return out, err
				}
			}
			isDirect = c.Kind == Direct || c.Kind == Group
			direct[ck] = isDirect
		}
		input.Direct = isDirect
		// Exemptions (roles, named agents) belong to the author as the server
		// knows them now, never to the reader asking.
		if p.AuthorIdentity != nil {
			ak := authorKey{post.AuthorHomeTenantID, post.AuthorID}
			author, ok := authors[ak]
			if !ok {
				author, err = p.AuthorIdentity.FilterAuthorInput(ctx, post)
				if err != nil {
					return out, err
				}
				authors[ak] = author
			}
			if author.Tenant == post.AuthorHomeTenantID && author.Subject == post.AuthorID && post.AuthorHomeTenantID == post.TenantID {
				input.Roles, input.Agent = author.Roles, author.Agent
			}
		}
		if input.Direct || len(input.Roles) > 0 || input.Agent {
			result, err = p.Filters.Evaluate(ctx, input, false)
			if err != nil {
				return out, err
			}
		}
		out[i] = result.Masked
	}
	return out, nil
}

// CheckAgentPost is the server-owned check for text an agent writes without a
// person's request (a public answer, a scheduled announcement). An agent's
// output passes the same filters as a person's; the author is the agent, and a
// rule may exempt a named agent. A refusal is the same typed blocked error.
func (p *FilterContentPolicy) CheckAgentPost(ctx context.Context, tenant, conversation, authorID, body string) error {
	if p == nil || p.Filters == nil {
		return chatfilter.ErrUnavailable
	}
	if strings.TrimSpace(body) == "" {
		return nil
	}
	c := Conversation{ID: conversation, TenantID: tenant}
	if p.Conversations != nil {
		var err error
		c, err = p.Conversations.GetConversation(ctx, tenant, conversation)
		if err != nil {
			return err
		}
	}
	input := chatfilter.Input{Tenant: tenant, Channel: conversation, Subject: authorID, Body: strings.TrimSpace(body), Agent: true, Direct: c.Kind == Direct || c.Kind == Group}
	result, err := p.Filters.Evaluate(ctx, input, true)
	if err != nil {
		return err
	}
	return result.Refusal()
}
