package chat

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// reactionBatchFake is a store that reads several posts' reactions at once and
// records how it was asked.
type reactionBatchFake struct {
	*fakeStore
	batchCalls  int
	singleCalls int
	posts       []string
	perPost     uint32
}

func (f *reactionBatchFake) ListReactionsForPosts(_ context.Context, _ Principal, tenantID, conversationID string, postIDs []string, perPost uint32) ([]Reaction, error) {
	f.batchCalls++
	f.posts, f.perPost = append([]string(nil), postIDs...), perPost
	out := make([]Reaction, 0, len(postIDs))
	for _, id := range postIDs {
		out = append(out, Reaction{TenantID: tenantID, ConversationID: conversationID, PostID: id, HomeTenantID: "t1", SubjectID: "u2", Emoji: "+1"})
	}
	return out, nil
}

func (f *reactionBatchFake) ListReactions(ctx context.Context, p Principal, t, cid, post string, page Page) (ListReactionsResponse, error) {
	f.singleCalls++
	return f.fakeStore.ListReactions(ctx, p, t, cid, post, page)
}

// reactionSingleFake is a store without the batch read; it counts the
// post-by-post reads the service falls back to.
type reactionSingleFake struct {
	*fakeStore
	asked []string
	size  []uint32
}

func (f *reactionSingleFake) ListReactions(_ context.Context, _ Principal, t, cid, post string, page Page) (ListReactionsResponse, error) {
	f.asked, f.size = append(f.asked, post), append(f.size, page.PageSize)
	return ListReactionsResponse{Reactions: []Reaction{{TenantID: t, ConversationID: cid, PostID: post, Emoji: "+1"}}, NextCursor: "more"}, nil
}

// TestTodo_CHATBUG_014_ReactionBatch: a request that names its posts is one
// read of the store, authorized once, and bounded the way a single-post read is.
func TestTodo_CHATBUG_014_ReactionBatch(t *testing.T) {
	ctx := context.Background()
	base := func() *fakeStore {
		return &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", HistoryVisibility: FullHistory}}
	}
	request := func(posts ...string) ListReactionsRequest {
		return ListReactionsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostIDs: posts}
	}

	batch := &reactionBatchFake{fakeStore: base()}
	s := NewService(batch, time.Now)
	s.SetAuthority(verifiedAuthority{store: batch.fakeStore})
	got, err := s.ListReactions(ctx, request("p1", "p2", "p1", "p3"))
	if err != nil {
		t.Fatal(err)
	}
	if batch.batchCalls != 1 || batch.singleCalls != 0 {
		t.Fatalf("three posts cost %d batch and %d single reads, want one batch read", batch.batchCalls, batch.singleCalls)
	}
	if !reflect.DeepEqual(batch.posts, []string{"p1", "p2", "p3"}) || batch.perPost != 200 {
		t.Fatalf("the store was asked for %v at %d per post, want each post once at the single-post bound", batch.posts, batch.perPost)
	}
	if len(got.Reactions) != 3 || got.Reactions[1].PostID != "p2" || got.NextCursor != "" {
		t.Fatalf("answer=%+v", got)
	}
	if _, err := s.ListReactions(ctx, ListReactionsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", PostIDs: []string{"p1"}, Page: Page{PageSize: 50}}); err != nil || batch.perPost != 50 {
		t.Fatalf("a named page size was not passed on: perPost=%d err=%v", batch.perPost, err)
	}

	// Refusals, each before the store is read.
	calls := batch.batchCalls
	tooMany := make([]string, ReactionBatchLimit+1)
	for i := range tooMany {
		tooMany[i] = "p" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	for name, r := range map[string]ListReactionsRequest{
		"more posts than the limit":  request(tooMany...),
		"an empty post id":           request("p1", ""),
		"a single post beside many":  {Principal: principal(), TenantID: "t1", ConversationID: "c1", PostID: "p9", PostIDs: []string{"p1"}},
		"a cursor":                   {Principal: principal(), TenantID: "t1", ConversationID: "c1", PostIDs: []string{"p1"}, Page: Page{Cursor: "x"}},
		"an unbounded page":          {Principal: principal(), TenantID: "t1", ConversationID: "c1", PostIDs: []string{"p1"}, Page: Page{PageSize: 201}},
		"no conversation":            {Principal: principal(), TenantID: "t1", PostIDs: []string{"p1"}},
		"a principal with no tenant": {Principal: Principal{SubjectID: "u1"}, TenantID: "t1", ConversationID: "c1", PostIDs: []string{"p1"}},
	} {
		if _, err := s.ListReactions(ctx, r); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if batch.batchCalls != calls {
		t.Fatalf("a refused request reached the store %d times", batch.batchCalls-calls)
	}

	// A reader who has left the conversation is refused, not answered empty.
	left := time.Unix(20, 0).UTC()
	batch.membership.LeftAt = &left
	if _, err := s.ListReactions(ctx, request("p1")); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("a revoked reader got %v", err)
	}
	if batch.batchCalls != calls {
		t.Fatal("a revoked reader's request reached the store")
	}

	// A store without the batch read is asked post by post, first page only.
	single := &reactionSingleFake{fakeStore: base()}
	s = NewService(single, time.Now)
	s.SetAuthority(verifiedAuthority{store: single.fakeStore})
	got, err = s.ListReactions(ctx, request("p1", "p2"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(single.asked, []string{"p1", "p2"}) || !reflect.DeepEqual(single.size, []uint32{200, 200}) {
		t.Fatalf("fallback asked %v at %v", single.asked, single.size)
	}
	if len(got.Reactions) != 2 || got.NextCursor != "" {
		t.Fatalf("fallback answer=%+v", got)
	}
}
