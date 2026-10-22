package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSegments struct {
	resolveErr error
	resolved   PostAudience
	signature  []string
	asked      []string
	resolvedBy string
}

func (f *fakeSegments) Resolve(_ context.Context, p Principal, _, _ string, segments []string, restricted bool) (PostAudience, error) {
	f.resolvedBy = p.SubjectID
	if f.resolveErr != nil {
		return PostAudience{}, f.resolveErr
	}
	out := f.resolved
	out.Segments, out.Restricted = segments, restricted
	return out, nil
}
func (f *fakeSegments) Signature(_ context.Context, _, _, person string, _ []string) ([]string, error) {
	f.asked = append(f.asked, person)
	return f.signature, nil
}

// audienceStore is the store a service reads and writes through in these tests:
// it keeps the context a post was sent with, and answers for post audiences.
type audienceStore struct {
	chatcmd002FixtureStore
	sentCtx  context.Context
	readable map[string]bool
	readErr  error
}

func (s *audienceStore) SendPost(ctx context.Context, r SendPostRequest, p Post) (Post, error) {
	s.sentCtx = ctx
	return s.chatcmd002FixtureStore.SendPost(ctx, r, p)
}
func (s *audienceStore) PostReadableBy(_ context.Context, _, _, _, person string) (bool, error) {
	return s.readable[person], s.readErr
}

func audienceFixture() (*fakeStore, *audienceStore, *Service) {
	f := &fakeStore{conversation: Conversation{ID: "room", TenantID: "t", Kind: PublicChannel, Revision: 1}, membership: Membership{TenantID: "t", HomeTenantID: "t", ConversationID: "room", SubjectID: "writer", HistoryVisibility: FullHistory}}
	store := &audienceStore{chatcmd002FixtureStore: chatcmd002FixtureStore{f}, readable: map[string]bool{"writer": true}}
	s := NewService(store, func() time.Time { return time.Now() })
	s.SetAuthority(chatcmd002FixtureAuthority{verifiedAuthority{store: f}})
	return f, store, s
}

func segmentCard(t *testing.T, anonymous bool) Chatcmd002Card {
	d, err := Chatcmd003ParsePoll(`"Swap?" 1="Yes" 2="No"`, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	card := d.Card
	card.Audience = []string{"Night shift"}
	card.Poll.Anonymous = anonymous
	return card
}

// TestTodo_CHATGATE_010_Security: a card addressed to segments is posted only
// after the gate vouched for its author, is anonymous, and is voted on through a
// resolver bound to the person calling, not to anything in the request. The
// refusal of a vote by someone outside the segment is made where the card is
// read, in the store, and is proved there (TestTodo_CHATGATE_010_Integration).
func TestTodo_CHATGATE_010_Security(t *testing.T) {
	f, store, chat := audienceFixture()
	port := &fakeSegments{signature: []string{"Night shift"}}
	cards := &Chatcmd002Service{Chat: chat, Repository: chatcmd002FixtureRepository{}}
	post := Chatcmd002PostRequest{Accepted: true, SendPostRequest: SendPostRequest{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", IdempotencyKey: "addressed"}, Card: segmentCard(t, true)}

	// No gate to ask: refused, nothing sent.
	if _, err := cards.Post(context.Background(), post); !errors.Is(err, ErrUnavailable) || f.mutations != 0 {
		t.Fatalf("a card addressed with no gate: %v, %d sent", err, f.mutations)
	}
	// The gate refuses the author: nothing sent.
	cards.Segments = &fakeSegments{resolveErr: ErrPermissionDenied}
	if _, err := cards.Post(context.Background(), post); !errors.Is(err, ErrPermissionDenied) || f.mutations != 0 {
		t.Fatalf("a card addressed by someone the gate refuses: %v, %d sent", err, f.mutations)
	}
	// A named poll cannot be addressed: nobody reads one person's choice.
	named := post
	named.Card = segmentCard(t, false)
	cards.Segments = port
	if _, err := cards.Post(context.Background(), named); !errors.Is(err, ErrInvalidArgument) || f.mutations != 0 {
		t.Fatalf("a named poll addressed to a segment: %v", err)
	}
	// Accepted: the audience is in the card, and the gate was asked as the author.
	if _, err := cards.Post(context.Background(), post); err != nil {
		t.Fatal(err)
	}
	card, ok := Chatcmd002Decode(f.sent.Body)
	if !ok || len(card.Audience) != 1 || card.Audience[0] != "Night shift" || port.resolvedBy != "writer" {
		t.Fatalf("the posted card: %+v ok=%v, resolved by %q", card, ok, port.resolvedBy)
	}
	_ = store
	f.post = Post{ID: "p", TenantID: "t", ConversationID: "room", CreatedAt: time.Now().Add(-time.Hour)}

	// A vote or a tick is handed to the store with a resolver for the caller.
	var seen Chatcmd002Request
	cards.Repository = chatcmd002FixtureRepository{mutate: func(ctx context.Context, r Chatcmd002Request, a func(context.Context) error) (Post, error) {
		seen = r
		return Post{}, a(ctx)
	}}
	vote := Chatcmd002Request{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", PostID: "p", ExpectedRevision: 1, Mutation: Chatcmd002Mutation{Operation: "VOTE", Options: []string{"a"}}}
	if _, err := cards.Mutate(context.Background(), vote); err != nil {
		t.Fatal(err)
	}
	if seen.Segments == nil {
		t.Fatal("the store was given no way to ask the gate")
	}
	if got, err := seen.Segments(context.Background(), []string{"Night shift"}); err != nil || len(got) != 1 || len(port.asked) != 1 || port.asked[0] != "writer" {
		t.Fatalf("the resolver asked about %v: %v %v", port.asked, got, err)
	}
	// With no gate the store gets nothing and refuses an addressed card.
	cards.Segments = nil
	if _, err := cards.Mutate(context.Background(), vote); err != nil || seen.Segments != nil {
		t.Fatalf("a vote with no gate: %v, resolver %v", err, seen.Segments != nil)
	}
}

// TestTodo_CHATGATE_011_ReadPaths: the check every path that reads one post asks:
// a restricted post is read by its audience, a failed check hides the post, and a
// store with no audiences hides nothing.
func TestTodo_CHATGATE_011_ReadPaths(t *testing.T) {
	_, store, chat := audienceFixture()
	c := Conversation{ID: "room", TenantID: "t", Kind: PublicChannel}
	post := Post{ID: "p", TenantID: "t", ConversationID: "room", CreatedAt: time.Now().Add(-time.Hour)}
	writer, outsider := Principal{TenantID: "t", SubjectID: "writer"}, Principal{TenantID: "t", SubjectID: "outsider"}
	if !chat.postVisibleTo(context.Background(), writer, c, post) {
		t.Fatal("the audience cannot read its post")
	}
	if chat.postVisibleTo(context.Background(), outsider, c, post) {
		t.Fatal("a person outside the audience reads a restricted post")
	}
	store.readErr = errors.New("audience store down")
	if chat.postVisibleTo(context.Background(), writer, c, post) {
		t.Fatal("a failed audience check showed the post")
	}
	// A store that keeps no audiences hides nothing.
	f := &fakeStore{conversation: c, membership: Membership{TenantID: "t", HomeTenantID: "t", ConversationID: "room", SubjectID: "outsider", HistoryVisibility: FullHistory}}
	plain := NewService(f, nil)
	if !plain.postVisibleTo(context.Background(), outsider, c, post) {
		t.Fatal("a store with no audiences hid a post")
	}
}

// TestTodo_CHATGATE_011_Send: a post addressed to segments carries its resolved
// audience to the store in the context of the send, so the store writes it in the
// same transaction; no gate, or a refusal, sends nothing.
func TestTodo_CHATGATE_011_Send(t *testing.T) {
	f, store, chat := audienceFixture()
	request := SendPostRequest{Principal: Principal{TenantID: "t", SubjectID: "writer"}, TenantID: "t", ConversationID: "room", Body: "Night shift: swap on Friday", IdempotencyKey: "addressed", Audience: &PostAudienceRequest{Segments: []string{"Night shift"}, Restricted: true}}
	if _, err := chat.SendPost(context.Background(), request); !errors.Is(err, ErrUnavailable) || f.mutations != 0 {
		t.Fatalf("an addressed post with no gate: %v, %d sent", err, f.mutations)
	}
	chat.SetSegmentAudience(&fakeSegments{resolveErr: ErrPermissionDenied})
	if _, err := chat.SendPost(context.Background(), request); !errors.Is(err, ErrPermissionDenied) || f.mutations != 0 {
		t.Fatalf("a refused audience: %v, %d sent", err, f.mutations)
	}
	chat.SetSegmentAudience(&fakeSegments{resolved: PostAudience{Members: []string{"ana", "writer"}}})
	if _, err := chat.SendPost(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	audience, ok := PostAudienceFrom(store.sentCtx)
	if !ok || !audience.Restricted || len(audience.Members) != 2 || audience.Segments[0] != "Night shift" {
		t.Fatalf("the store was not handed the audience: %+v %v", audience, ok)
	}
	// A post that is not addressed carries none.
	plain := request
	plain.Audience, plain.IdempotencyKey = nil, "plain"
	if _, err := chat.SendPost(context.Background(), plain); err != nil {
		t.Fatal(err)
	}
	if _, ok := PostAudienceFrom(store.sentCtx); ok {
		t.Fatal("an ordinary post carries an audience")
	}
}
