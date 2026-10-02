package chat

import "context"

// CHATBUG-014: opening a conversation read its reactions one post at a time,
// thirty-seven calls for the page of messages on screen. A request that names
// the posts gets all of them in one call.

// ReactionBatchLimit is how many posts one reaction read may name.
const ReactionBatchLimit = 100

// reactionBatchPageSize is the per-post bound when the caller names none: the
// same bound a single-post read applies.
const reactionBatchPageSize = 200

// ReactionBatchStore is a store that reads the reactions of several posts of
// one conversation in one query. A store without it is asked post by post.
type ReactionBatchStore interface {
	// ListReactionsForPosts returns up to perPost reactions of each named post
	// the principal may read, ordered by post and then as ListReactions orders
	// them. A post that is tombstoned, outside the principal's history window or
	// not in the conversation contributes nothing.
	ListReactionsForPosts(ctx context.Context, principal Principal, tenantID, conversationID string, postIDs []string, perPost uint32) ([]Reaction, error)
}

// listReactionsForPosts answers a ListReactions request that names its posts.
// The conversation is authorized once, exactly as for a single post.
func (s *Service) listReactionsForPosts(ctx context.Context, r ListReactionsRequest) (ListReactionsResponse, error) {
	if err := validatePrincipal(r.Principal, r.TenantID); err != nil {
		return ListReactionsResponse{}, err
	}
	if r.ConversationID == "" || r.PostID != "" || r.Page.Cursor != "" || len(r.PostIDs) > ReactionBatchLimit {
		return ListReactionsResponse{}, ErrInvalidArgument
	}
	if err := validatePage(r.Page); err != nil {
		return ListReactionsResponse{}, err
	}
	posts := make([]string, 0, len(r.PostIDs))
	seen := make(map[string]struct{}, len(r.PostIDs))
	for _, id := range r.PostIDs {
		if id == "" {
			return ListReactionsResponse{}, ErrInvalidArgument
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		posts = append(posts, id)
	}
	if _, err := s.GetConversation(ctx, GetConversationRequest{Principal: r.Principal, TenantID: r.TenantID, ConversationID: r.ConversationID}); err != nil {
		return ListReactionsResponse{}, err
	}
	perPost := r.Page.PageSize
	if perPost == 0 {
		perPost = reactionBatchPageSize
	}
	if batch, ok := s.store.(ReactionBatchStore); ok {
		reactions, err := batch.ListReactionsForPosts(ctx, r.Principal, r.TenantID, r.ConversationID, posts, perPost)
		return ListReactionsResponse{Reactions: reactions}, err
	}
	var out ListReactionsResponse
	for _, id := range posts {
		page, err := s.store.ListReactions(ctx, r.Principal, r.TenantID, r.ConversationID, id, Page{PageSize: perPost})
		if err != nil {
			return ListReactionsResponse{}, err
		}
		out.Reactions = append(out.Reactions, page.Reactions...)
	}
	return out, nil
}
