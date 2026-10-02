//go:build !(js && wasm)

package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"google.golang.org/grpc"
)

// chatperfCalls stands in for the tunnel and records every request made on it.
type chatperfCalls struct {
	reactions []*chatv1.ListReactionsRequest
	counts    []*chatv1.GetCountsRequest
	failHost  string
	fail      error
}

func (c *chatperfCalls) ListReactions(_ context.Context, in *chatv1.ListReactionsRequest, _ ...grpc.CallOption) (*chatv1.ListReactionsResponse, error) {
	c.reactions = append(c.reactions, in)
	if c.fail != nil {
		return nil, c.fail
	}
	out := &chatv1.ListReactionsResponse{}
	for _, id := range in.GetPostIds() {
		if id == "quiet" {
			continue
		}
		out.Reactions = append(out.Reactions, &chatv1.Reaction{PostId: id, SubjectId: "ann", Emoji: "+1"}, &chatv1.Reaction{PostId: id, SubjectId: "sam", Emoji: "+1"})
	}
	// A reaction for a post nobody asked about must not be folded in.
	out.Reactions = append(out.Reactions, &chatv1.Reaction{PostId: "stray", SubjectId: "zed", Emoji: "eyes"}, nil)
	return out, nil
}

func (c *chatperfCalls) GetCounts(_ context.Context, in *chatv1.GetCountsRequest, _ ...grpc.CallOption) (*chatv1.GetCountsResponse, error) {
	c.counts = append(c.counts, in)
	if in.GetTenantId() == c.failHost {
		return nil, errors.New("unavailable")
	}
	out := &chatv1.GetCountsResponse{}
	for _, id := range in.GetConversationIds() {
		if id == "revoked" {
			continue
		}
		out.AllCounts = append(out.AllCounts, &chatv1.ChatCounts{TenantId: in.GetTenantId(), ConversationId: id, UnreadCount: 2})
	}
	// A row for a conversation that was not asked about is not this sidebar's.
	out.AllCounts = append(out.AllCounts, &chatv1.ChatCounts{TenantId: "elsewhere", ConversationId: "stray", UnreadCount: 99})
	return out, nil
}

// TestTodo_CHATBUG_014_RequestCount: a page of posts is one reaction request
// and a sidebar is one counts request per host tenant, however many rows there
// are, up to the server's bound per request.
func TestTodo_CHATBUG_014_RequestCount(t *testing.T) {
	ctx := context.Background()

	// Thirty-seven posts, the review data's #general: one request, not 37.
	posts := make([]string, 0, 37)
	for i := 0; i < 36; i++ {
		posts = append(posts, fmt.Sprintf("p%02d", i))
	}
	posts = append(posts, "quiet")
	calls := &chatperfCalls{}
	byPost, err := chatperfReadReactions(ctx, calls, "tenant", "room", posts)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls.reactions) != 1 {
		t.Fatalf("%d posts cost %d reaction requests, want 1", len(posts), len(calls.reactions))
	}
	request := calls.reactions[0]
	if request.GetPostId() != "" || !reflect.DeepEqual(request.GetPostIds(), posts) || request.GetConversationId() != "room" || request.GetTenantId() != "tenant" || request.GetPageSize() != chatperfReactionPageSize {
		t.Fatalf("the reaction request was %+v", request)
	}
	if len(byPost) != len(posts) {
		t.Fatalf("%d posts have an answer, want all %d (a post with no reactions answers empty, so a removed reaction disappears)", len(byPost), len(posts))
	}
	if len(byPost["p00"]) != 2 || len(byPost["quiet"]) != 0 {
		t.Fatalf("p00=%d quiet=%d", len(byPost["p00"]), len(byPost["quiet"]))
	}
	if _, stray := byPost["stray"]; stray {
		t.Fatal("a reaction for a post that was not asked about was folded in")
	}

	// More posts than one request may name are asked for in runs of the bound.
	many := make([]string, 2*chatperfReactionBatch+1)
	for i := range many {
		many[i] = fmt.Sprintf("m%03d", i)
	}
	calls = &chatperfCalls{}
	if _, err := chatperfReadReactions(ctx, calls, "tenant", "room", many); err != nil || len(calls.reactions) != 3 {
		t.Fatalf("%d posts cost %d requests (err %v), want 3", len(many), len(calls.reactions), err)
	}
	for i, request := range calls.reactions {
		if len(request.GetPostIds()) > chatperfReactionBatch {
			t.Fatalf("request %d names %d posts, over the server's bound", i, len(request.GetPostIds()))
		}
	}
	if got := chatperfChunks(nil, 10); got != nil {
		t.Fatalf("nothing to ask about made %d requests", len(got))
	}

	// A failed read is a failure, never "no reactions".
	calls = &chatperfCalls{fail: errors.New("unavailable")}
	if byPost, err := chatperfReadReactions(ctx, calls, "tenant", "room", posts); err == nil || byPost != nil {
		t.Fatalf("a failed read answered %d posts", len(byPost))
	}

	// Nineteen conversations on one host: one counts request, not 19.
	hosts := map[string]string{"revoked": "host"}
	for i := 0; i < 18; i++ {
		hosts[fmt.Sprintf("c%02d", i)] = "host"
	}
	calls = &chatperfCalls{}
	counts := chatperfReadCounts(ctx, calls, hosts, nil)
	if len(calls.counts) != 1 {
		t.Fatalf("%d conversations cost %d counts requests, want 1", len(hosts), len(calls.counts))
	}
	asked := append([]string(nil), calls.counts[0].GetConversationIds()...)
	sort.Strings(asked)
	if calls.counts[0].GetConversationId() != "" || len(asked) != len(hosts) || asked[0] != "c00" {
		t.Fatalf("the counts request was %+v", calls.counts[0])
	}
	if len(counts) != 18 || counts["c00"].GetUnreadCount() != 2 || counts["revoked"] != nil || counts["stray"] != nil {
		t.Fatalf("counts=%v", counts)
	}

	// Two hosts: one request each. The host that fails keeps what it showed.
	hosts = map[string]string{"a": "one", "b": "one", "c": "two", "": "two", "d": ""}
	calls = &chatperfCalls{failHost: "two"}
	previous := map[string]*chatv1.ChatCounts{"c": {ConversationId: "c", UnreadCount: 7}, "a": {ConversationId: "a", UnreadCount: 9}}
	counts = chatperfReadCounts(ctx, calls, hosts, previous)
	if len(calls.counts) != 2 {
		t.Fatalf("two hosts cost %d requests", len(calls.counts))
	}
	if counts["a"].GetUnreadCount() != 2 || counts["b"].GetUnreadCount() != 2 {
		t.Fatalf("the host that answered: %v", counts)
	}
	if counts["c"].GetUnreadCount() != 7 {
		t.Fatalf("a failed read replaced the last good count: %v", counts["c"])
	}
}
