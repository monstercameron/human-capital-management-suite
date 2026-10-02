package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// chatcmd002FakeCards records what the HTTP surface asks of the card service.
type chatcmd002FakeCards struct {
	posted  []chat.Chatcmd002PostRequest
	read    []chat.Chatcmd002Request
	mutated []chat.Chatcmd002Request
	views   map[string]chat.Chatcmd002View
	err     error
}

func (f *chatcmd002FakeCards) Post(_ context.Context, r chat.Chatcmd002PostRequest) (chat.Post, error) {
	f.posted = append(f.posted, r)
	return chat.Post{ID: "new-post", Revision: 1}, f.err
}
func (f *chatcmd002FakeCards) Read(_ context.Context, r chat.Chatcmd002Request) (chat.Chatcmd002View, error) {
	f.read = append(f.read, r)
	view, ok := f.views[r.PostID]
	if !ok {
		return view, chat.ErrNotFound
	}
	return view, nil
}
func (f *chatcmd002FakeCards) Mutate(_ context.Context, r chat.Chatcmd002Request) (chat.Post, error) {
	f.mutated = append(f.mutated, r)
	return chat.Post{ID: r.PostID, Revision: r.ExpectedRevision + 1}, f.err
}

func chatcmd002HTTPContext(t *testing.T, kind trust.SubjectKind) context.Context {
	t.Helper()
	at := time.Now()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "alice", SubjectKind: kind, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "test-session-alice", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "test-verified-alice"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), principal)
}

func chatcmd002Call(ctx context.Context, cards Chatcmd002CardPort, method, action, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, Chatcmd002CardPath+"/"+action, strings.NewReader(body)).WithContext(ctx)
	Chatcmd002CardHandler{Cards: cards}.ServeHTTP(recorder, request)
	return recorder
}

// TestTodo_CHATBUG_057 covers the route the page uses to post a poll or a list
// from its preview and to vote, tick and close: the earlier work had the
// service and no way to reach it.
func TestTodo_CHATBUG_057(t *testing.T) {
	ctx := chatcmd002HTTPContext(t, trust.SubjectKindHuman)
	cards := &chatcmd002FakeCards{views: map[string]chat.Chatcmd002View{"p1": {CanManage: true, Revision: 4}, "p2": {Voted: true, Revision: 2}}}

	posted := chatcmd002Call(ctx, cards, http.MethodPost, "post", `{"conversation_id":"room","parent_id":"root","idempotency_key":"k1","card":{"kind":"poll","title":"Lunch?","poll":{"options":[{"Text":"Tacos"},{"Text":"Pho"}],"multiple":false,"anonymous":false,"results":"always","add_options":"author"}}}`)
	if posted.Code != http.StatusOK || len(cards.posted) != 1 {
		t.Fatalf("post %d %s", posted.Code, posted.Body)
	}
	sent := cards.posted[0]
	if !sent.Accepted || sent.Principal.TenantID != "host" || sent.Principal.SubjectID != "alice" || sent.TenantID != "host" || sent.ConversationID != "room" || sent.ParentID != "root" || sent.IdempotencyKey != "k1" || sent.Card.Title != "Lunch?" || len(sent.Card.Poll.Options) != 2 {
		t.Fatalf("posted request %+v", sent)
	}
	var reply Chatcmd002CardReply
	if err := json.Unmarshal(posted.Body.Bytes(), &reply); err != nil || reply.PostID != "new-post" || reply.Revision != 1 {
		t.Fatalf("post reply %+v %v", reply, err)
	}

	read := chatcmd002Call(ctx, cards, http.MethodPost, "read", `{"conversation_id":"room","host_tenant_id":"other-host","post_ids":["p1","gone","p2","p1"]}`)
	reply = Chatcmd002CardReply{}
	if err := json.Unmarshal(read.Body.Bytes(), &reply); read.Code != http.StatusOK || err != nil || len(reply.Views) != 2 || !reply.Views["p1"].CanManage || !reply.Views["p2"].Voted {
		t.Fatalf("read %d %+v %v", read.Code, reply, err)
	}
	if len(cards.read) != 3 || cards.read[0].TenantID != "other-host" || cards.read[0].ConversationID != "room" {
		t.Fatalf("read requests %+v", cards.read)
	}

	voted := chatcmd002Call(ctx, cards, http.MethodPost, "mutate", `{"conversation_id":"room","post_id":"p1","expected_revision":4,"mutation":{"operation":"VOTE","options":["a"],"completed":false}}`)
	reply = Chatcmd002CardReply{}
	if err := json.Unmarshal(voted.Body.Bytes(), &reply); voted.Code != http.StatusOK || err != nil || reply.Revision != 5 || reply.View == nil || !reply.View.CanManage {
		t.Fatalf("mutate %d %+v %v", voted.Code, reply, err)
	}
	if len(cards.mutated) != 1 || cards.mutated[0].Mutation.Operation != "VOTE" || strings.Join(cards.mutated[0].Mutation.Options, ",") != "a" || cards.mutated[0].ExpectedRevision != 4 || cards.mutated[0].PostID != "p1" {
		t.Fatalf("mutation %+v", cards.mutated)
	}
}

func TestTodo_CHATBUG_057_Security(t *testing.T) {
	human := chatcmd002HTTPContext(t, trust.SubjectKindHuman)
	cards := &chatcmd002FakeCards{}
	vote := `{"conversation_id":"room","post_id":"p1","expected_revision":1,"mutation":{"operation":"VOTE","options":["a"],"completed":false}}`
	for name, tc := range map[string]struct {
		ctx            context.Context
		port           Chatcmd002CardPort
		method, action string
		body           string
		status         int
	}{
		"no verified person":      {context.Background(), cards, http.MethodPost, "mutate", vote, http.StatusUnauthorized},
		"an agent":                {chatcmd002HTTPContext(t, trust.SubjectKindAgent), cards, http.MethodPost, "mutate", vote, http.StatusForbidden},
		"service not composed":    {human, nil, http.MethodPost, "mutate", vote, http.StatusServiceUnavailable},
		"reading by GET":          {human, cards, http.MethodGet, "read", "", http.StatusMethodNotAllowed},
		"unknown action":          {human, cards, http.MethodPost, "delete", vote, http.StatusNotFound},
		"unknown field":           {human, cards, http.MethodPost, "mutate", `{"conversation_id":"room","post_id":"p1","principal":"bob"}`, http.StatusBadRequest},
		"two bodies":              {human, cards, http.MethodPost, "mutate", vote + vote, http.StatusBadRequest},
		"post without a card":     {human, cards, http.MethodPost, "post", `{"conversation_id":"room","idempotency_key":"k"}`, http.StatusBadRequest},
		"mutate without a change": {human, cards, http.MethodPost, "mutate", `{"conversation_id":"room","post_id":"p1"}`, http.StatusBadRequest},
		"read of nothing":         {human, cards, http.MethodPost, "read", `{"conversation_id":"room"}`, http.StatusBadRequest},
	} {
		if got := chatcmd002Call(tc.ctx, tc.port, tc.method, tc.action, tc.body); got.Code != tc.status {
			t.Errorf("%s: status %d, want %d (%s)", name, got.Code, tc.status, got.Body)
		}
	}
	if len(cards.posted)+len(cards.read)+len(cards.mutated) != 0 {
		t.Fatalf("a refused request reached the service: %+v", cards)
	}
	// What the service refuses is answered with its own status and no cause.
	for err, status := range map[error]int{chat.ErrPermissionDenied: http.StatusForbidden, chat.ErrConflict: http.StatusConflict, chat.ErrNotFound: http.StatusNotFound, chat.ErrInvalidArgument: http.StatusBadRequest, chat.ErrUnavailable: http.StatusServiceUnavailable} {
		got := chatcmd002Call(human, &chatcmd002FakeCards{err: err}, http.MethodPost, "mutate", vote)
		if got.Code != status || strings.Contains(got.Body.String(), err.Error()) {
			t.Errorf("%v: status %d body %s", err, got.Code, got.Body)
		}
	}
	// The overlay leaves every other path, and the tidy route, to the next handler.
	passed := 0
	overlay := OverlayChatcmd002Cards(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { passed++ }), cards, transport.Config{})
	for _, path := range []string{"/api/chat/saved", Chatcmd003TidyPath, Chatcmd002CardPath} {
		overlay.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
	}
	if passed != 3 {
		t.Fatalf("overlay kept %d of 3 foreign paths", 3-passed)
	}
	denied := httptest.NewRecorder()
	overlay.ServeHTTP(denied, httptest.NewRequest(http.MethodPost, Chatcmd002CardPath+"/mutate", strings.NewReader(vote)))
	if denied.Code < 400 || passed != 3 || len(cards.mutated) != 0 {
		t.Fatalf("unadmitted request: status %d", denied.Code)
	}
	if chatcmd002CardPort(composedChat{}) != nil {
		t.Fatal("a card service was composed without Chat")
	}
}

// chatcmd002LeaseRepository records the write lease a card change arrives with.
type chatcmd002LeaseRepository struct {
	leases int
	shard  string
}

func (r *chatcmd002LeaseRepository) Chatcmd002Read(context.Context, chat.Chatcmd002Request, func(context.Context) error) (chat.Chatcmd002View, error) {
	return chat.Chatcmd002View{}, nil
}
func (r *chatcmd002LeaseRepository) Chatcmd002Mutate(ctx context.Context, _ chat.Chatcmd002Request, _ func(context.Context) error) (chat.Post, error) {
	if lease, ok := chatrouting.WriteLeaseFromContext(ctx); ok {
		r.leases++
		r.shard = lease.Route.ShardID
	}
	return chat.Post{ID: "p"}, nil
}

// TestTodo_CHATBUG_057_Integration: a vote reaches the store directly, so it
// takes the conversation's write lease first, as every other direct writer
// does. Without one the store refuses the write of a routed conversation, and
// the vote would be answered "unavailable".
func TestTodo_CHATBUG_057_Integration(t *testing.T) {
	ctx := context.Background()
	directory := chatrouting.NewMemoryDirectory()
	reserved, err := directory.Reserve(ctx, chatrouting.ReserveRequest{ConversationID: "room", HostTenantID: "host", ShardID: "shard-1", PlacementPolicy: "test", PlacementPolicyVersion: 1, IdempotencyKey: "reserve-room"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := directory.Activate(ctx, "room", "host", reserved.Epoch); err != nil {
		t.Fatal(err)
	}
	inner := &chatcmd002LeaseRepository{}
	routed := chatcmd002RoutedRepository{Chatcmd002Repository: inner, Directory: directory, Cache: chatrouting.NewRouteCache(time.Second), Now: time.Now}
	request := chat.Chatcmd002Request{TenantID: "host", ConversationID: "room", PostID: "p", ExpectedRevision: 1}
	if _, err := routed.Chatcmd002Mutate(ctx, request, func(context.Context) error { return nil }); err != nil || inner.leases != 1 || inner.shard != "shard-1" {
		t.Fatalf("vote in a routed conversation: %v, leases %d, shard %q", err, inner.leases, inner.shard)
	}
	// A conversation the directory does not know is not found, and one of
	// another tenant is not written.
	request.ConversationID = "elsewhere"
	if _, err := routed.Chatcmd002Mutate(ctx, request, func(context.Context) error { return nil }); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("unknown conversation: %v", err)
	}
	request.ConversationID, request.TenantID = "room", "other-tenant"
	if _, err := routed.Chatcmd002Mutate(ctx, request, func(context.Context) error { return nil }); err == nil || inner.leases != 1 {
		t.Fatalf("another tenant wrote with this conversation's lease: %v (%d)", err, inner.leases)
	}
	// A read takes no lease.
	if _, err := routed.Chatcmd002Read(ctx, request, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
}
