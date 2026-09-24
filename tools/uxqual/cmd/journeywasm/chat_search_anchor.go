package main

import (
	"sort"
	"strconv"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
)

const chatSearchContextPageSize = 100

// chatSearchAnchorRequests builds the two bounded reads around a search hit.
// The exclusive sequence edges include the hit in both reads; mergeSearch
// removes that overlap while leaving room for equal context on either side.
func chatSearchAnchorRequests(tenantID, conversationID string, sequence uint64) (*chatv1.ListPostsRequest, *chatv1.ListPostsRequest) {
	older := &chatv1.ListPostsRequest{
		TenantId: tenantID, ConversationId: conversationID,
		PageSize: chatSearchContextPageSize, Descending: true,
		BeforeSequence: sequence + 1,
	}
	afterSequence := uint64(0)
	if sequence > 0 {
		afterSequence = sequence - 1
	}
	newer := &chatv1.ListPostsRequest{
		TenantId: tenantID, ConversationId: conversationID,
		PageSize: chatSearchContextPageSize, AfterSequence: afterSequence,
	}
	return older, newer
}

func mergeChatSearchAnchorPosts(older, newer []*chatv1.Post) []*chatv1.Post {
	posts := make([]*chatv1.Post, 0, len(older)+len(newer))
	seen := make(map[string]struct{}, len(older)+len(newer))
	for _, page := range [][]*chatv1.Post{older, newer} {
		for _, post := range page {
			if post == nil {
				continue
			}
			key := post.GetId()
			if key == "" {
				key = "sequence:" + strconv.FormatUint(post.GetSequence(), 10)
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			posts = append(posts, post)
		}
	}
	sort.SliceStable(posts, func(i, j int) bool { return posts[i].GetSequence() < posts[j].GetSequence() })
	return posts
}
