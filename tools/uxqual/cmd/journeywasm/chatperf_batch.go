package main

import (
	"context"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"google.golang.org/grpc"
)

// CHATBUG-014: two reads the page made once per row are made once per page.
// Opening a conversation asked for its reactions post by post (thirty-seven
// calls for #general on the review data) and the sidebar asked for its unread
// counts conversation by conversation (nineteen, and again after every new
// message). Both requests can name many rows; these are the client's halves.

const (
	// chatperfReactionBatch is how many posts one reaction read names; the
	// server's bound.
	chatperfReactionBatch = 100
	// chatperfReactionPageSize is how many reactions are read for each post,
	// as before.
	chatperfReactionPageSize = 100
	// chatperfCountsBatch is how many conversations one counts read names; the
	// server's bound.
	chatperfCountsBatch = 100
)

// chatperfChunks cuts ids into runs of at most size, in order.
func chatperfChunks(ids []string, size int) [][]string {
	if size <= 0 || len(ids) == 0 {
		return nil
	}
	chunks := make([][]string, 0, (len(ids)+size-1)/size)
	for len(ids) > size {
		chunks = append(chunks, ids[:size])
		ids = ids[size:]
	}
	return append(chunks, ids)
}

// chatperfReactionLister is the one call the reaction read needs.
type chatperfReactionLister interface {
	ListReactions(ctx context.Context, in *chatv1.ListReactionsRequest, opts ...grpc.CallOption) (*chatv1.ListReactionsResponse, error)
}

// chatperfReadReactions reads the reactions of the named posts of one
// conversation: one call per hundred posts. The answer has an entry for every
// post asked about, empty for a post nobody has reacted to, so a reaction that
// was removed disappears. A failed call fails the read: the caller keeps what
// it was showing.
func chatperfReadReactions(ctx context.Context, client chatperfReactionLister, tenant, conversation string, ids []string) (map[string][]*chatv1.Reaction, error) {
	byPost := make(map[string][]*chatv1.Reaction, len(ids))
	for _, chunk := range chatperfChunks(ids, chatperfReactionBatch) {
		result, err := client.ListReactions(ctx, &chatv1.ListReactionsRequest{TenantId: tenant, ConversationId: conversation, PostIds: chunk, PageSize: chatperfReactionPageSize})
		if err != nil {
			return nil, err
		}
		for _, id := range chunk {
			byPost[id] = nil
		}
		for _, reaction := range result.GetReactions() {
			if reaction == nil {
				continue
			}
			// Only posts that were asked about: an answer is never allowed to
			// put a reaction on a post this read did not name.
			if _, asked := byPost[reaction.GetPostId()]; asked {
				byPost[reaction.GetPostId()] = append(byPost[reaction.GetPostId()], reaction)
			}
		}
	}
	return byPost, nil
}

// chatperfCountsReader is the one call the sidebar's counts need.
type chatperfCountsReader interface {
	GetCounts(ctx context.Context, in *chatv1.GetCountsRequest, opts ...grpc.CallOption) (*chatv1.GetCountsResponse, error)
}

// chatperfReadCounts reads the unread and mention counts of a sidebar: one call
// per host tenant (and per hundred conversations of it). hosts maps each
// conversation to the tenant that hosts it, and previous is what the last read
// answered. A conversation whose call fails keeps its previous counts: a read
// that did not get an answer is not an answer of zero.
func chatperfReadCounts(ctx context.Context, client chatperfCountsReader, hosts map[string]string, previous map[string]*chatv1.ChatCounts) map[string]*chatv1.ChatCounts {
	byHost := make(map[string][]string)
	order := make([]string, 0, 1)
	for id, host := range hosts {
		if id == "" || host == "" {
			continue
		}
		if _, seen := byHost[host]; !seen {
			order = append(order, host)
		}
		byHost[host] = append(byHost[host], id)
	}
	counts := make(map[string]*chatv1.ChatCounts, len(hosts))
	for _, host := range order {
		for _, chunk := range chatperfChunks(byHost[host], chatperfCountsBatch) {
			result, err := client.GetCounts(ctx, &chatv1.GetCountsRequest{TenantId: host, ConversationIds: chunk})
			if err != nil {
				for _, id := range chunk {
					if kept := previous[id]; kept != nil {
						counts[id] = kept
					}
				}
				continue
			}
			for _, row := range result.GetAllCounts() {
				if row != nil && hosts[row.GetConversationId()] == host {
					counts[row.GetConversationId()] = row
				}
			}
		}
	}
	return counts
}
