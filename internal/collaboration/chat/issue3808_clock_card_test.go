package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

func clockCardService(f *fakeStore) *Service {
	s := NewService(f, func() time.Time { return time.Unix(10, 0).UTC() })
	s.SetAuthority(verifiedAuthority{store: f})
	return s
}

func TestIssue3808_ClockCardBodyRoundTrips(t *testing.T) {
	card := ClockCard{Kind: ClockCardMissingPunch, Audience: ClockCardSelf, Subject: "maria", Day: "2026-11-17", Ref: "sess-1"}
	body, err := ClockCardBody(card)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ParseClockCard(body)
	if !ok || got != card {
		t.Fatalf("parsed %+v, %v from %q", got, ok, body)
	}
	for _, bad := range []ClockCard{
		{Kind: "other", Audience: ClockCardSelf, Subject: "m", Day: "2026-11-17", Ref: "r"},
		{Kind: ClockCardMissingPunch, Audience: "everyone", Subject: "m", Day: "2026-11-17", Ref: "r"},
		{Kind: ClockCardMissingPunch, Audience: ClockCardSelf, Subject: "", Day: "2026-11-17", Ref: "r"},
		{Kind: ClockCardMissingPunch, Audience: ClockCardSelf, Subject: "m|x", Day: "2026-11-17", Ref: "r"},
		{Kind: ClockCardMissingPunch, Audience: ClockCardSelf, Subject: "m", Day: "yesterday", Ref: "r"},
		{Kind: ClockCardMissingPunch, Audience: ClockCardSelf, Subject: "m", Day: "2026-11-17", Ref: ""},
	} {
		if _, err := ClockCardBody(bad); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("ClockCardBody(%+v) err = %v, want invalid", bad, err)
		}
	}
	if _, ok := ParseClockCard(ClockCardMarker + "missing-punch|self|m"); ok {
		t.Error("a truncated body parsed")
	}
}

// A person cannot type a card into a conversation.
func TestIssue3808_HandTypedClockCardIsRefused_Security(t *testing.T) {
	body, _ := ClockCardBody(ClockCard{Kind: ClockCardMissingPunch, Audience: ClockCardSelf, Subject: "u1", Day: "2026-11-17", Ref: "s"})
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Manager, JoinedAt: timePtr(time.Unix(1, 0))}}
	s := clockCardService(f)
	_, err := s.SendPost(context.Background(), SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "look: " + body, IdempotencyKey: "k"})
	if !errors.Is(err, ErrInvalidArgument) || f.mutations != 0 {
		t.Fatalf("send = %v, mutations %d", err, f.mutations)
	}
}

func TestIssue3808_PostClockCardWritesToTheRecipientsDirectConversation(t *testing.T) {
	f := &fakeStore{membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "dm", SubjectID: "system", Role: Manager, JoinedAt: timePtr(time.Unix(1, 0))}}
	s := clockCardService(f)
	card := ClockCard{Kind: ClockCardMissingPunch, Audience: ClockCardSelf, Subject: "maria", Day: "2026-11-17", Ref: "sess-1"}
	post, err := s.PostClockCard(context.Background(), ClockCardPost{TenantID: "t1", Sender: Principal{TenantID: "t1", SubjectID: "system"}, Recipient: "maria", Card: card, IdempotencyKey: "evt-7"})
	if err != nil {
		t.Fatal(err)
	}
	if f.conversation.Kind != Direct || len(f.created) != 2 {
		t.Fatalf("conversation = %+v members %+v, want a direct conversation of two", f.conversation, f.created)
	}
	got, ok := ParseClockCard(f.sent.Body)
	if !ok || got != card || post.ID == "" || f.sent.ConversationID != f.conversation.ID || f.sent.AuthorID != "system" {
		t.Fatalf("sent = %+v (%+v, %v)", f.sent, got, ok)
	}
	var recipient bool
	for _, m := range f.created {
		recipient = recipient || m.SubjectID == "maria"
	}
	if !recipient {
		t.Errorf("recipient is not a member: %+v", f.created)
	}
}

func TestIssue3808_PostClockCardRefusesBadRequests_Security(t *testing.T) {
	f := &fakeStore{}
	s := clockCardService(f)
	card := ClockCard{Kind: ClockCardMissingPunch, Audience: ClockCardSelf, Subject: "maria", Day: "2026-11-17", Ref: "s"}
	system := Principal{TenantID: "t1", SubjectID: "system"}
	for name, r := range map[string]ClockCardPost{
		"sender in another tenant": {TenantID: "t1", Sender: Principal{TenantID: "t2", SubjectID: "system"}, Recipient: "maria", Card: card, IdempotencyKey: "k"},
		"no recipient":             {TenantID: "t1", Sender: system, Card: card, IdempotencyKey: "k"},
		"sends to itself":          {TenantID: "t1", Sender: system, Recipient: "system", Card: card, IdempotencyKey: "k"},
		"no key":                   {TenantID: "t1", Sender: system, Recipient: "maria", Card: card},
		"bad card":                 {TenantID: "t1", Sender: system, Recipient: "maria", Card: ClockCard{}, IdempotencyKey: "k"},
	} {
		if _, err := s.PostClockCard(context.Background(), r); err == nil || f.mutations != 0 {
			t.Errorf("%s: err %v, mutations %d", name, err, f.mutations)
		}
	}
}

// A card is the recipient's to read, not a thing to search, hand to an agent as
// context or translate: only a reader that asks for system lines gets it.
func TestIssue3808_ClockCardIsLeftOutOfSearchAndContext_Security(t *testing.T) {
	body, err := ClockCardBody(ClockCard{Kind: ClockCardMissingPunch, Audience: ClockCardReview, Subject: "maria", Day: "2026-11-17", Ref: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	card := Post{ID: "card", ConversationID: "c1", TenantID: "t1", AuthorID: "system", Body: body, Sequence: 1}
	hello := Post{ID: "hello", ConversationID: "c1", TenantID: "t1", AuthorID: "u1", Body: "hello", Sequence: 2}
	base := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Role: Manager, JoinedAt: timePtr(time.Unix(1, 0))}}
	f := &linesStore{fakeStore: base, posts: []Post{card, hello}, results: []SearchResult{{Post: card, ConversationName: "room"}, {Post: hello, ConversationName: "room"}}}
	s := NewService(f, func() time.Time { return time.Unix(10, 0).UTC() })
	s.SetAuthority(verifiedAuthority{store: base})
	got, err := s.ListPosts(context.Background(), ListPostsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Page: Page{PageSize: 10}})
	if err != nil || len(got.Posts) != 1 || got.Posts[0].ID != "hello" {
		t.Errorf("a context reader sees %+v (%v)", got.Posts, err)
	}
	got, err = s.ListPosts(context.Background(), ListPostsRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Page: Page{PageSize: 10}, IncludeSystem: true})
	if err != nil || len(got.Posts) != 2 {
		t.Errorf("the recipient's own timeline sees %+v (%v)", got.Posts, err)
	}
	found, err := s.Search(context.Background(), SearchRequest{Principal: principal(), TenantID: "t1", Query: "punch", Page: Page{PageSize: 10}})
	if err != nil || len(found.Results) != 1 || found.Results[0].Post.ID != "hello" {
		t.Errorf("search finds %+v (%v)", found.Results, err)
	}
	if !IsSystemBody(body) || !IsSystemBody(MembershipAddedBody("u2")) || IsSystemBody("hello") {
		t.Error("IsSystemBody does not tell a system body from a message")
	}
}
