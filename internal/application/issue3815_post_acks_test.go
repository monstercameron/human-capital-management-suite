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
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// issue3815Chat is a channel "room" of tenant-a owned by "owner", with one
// manager, two members and one person who left, holding one policy post by
// the owner and one plain post by a member.
type issue3815Chat struct {
	conv    chat.Conversation
	posts   map[string]chat.Post
	readers map[string]bool
	members []chat.Membership
}

func (f *issue3815Chat) GetConversation(_ context.Context, r chat.GetConversationRequest) (chat.Conversation, error) {
	if r.TenantID != "tenant-a" || r.ConversationID != "room" || !f.readers[r.Principal.SubjectID] {
		return chat.Conversation{}, chat.ErrNotFound
	}
	return f.conv, nil
}

func (f *issue3815Chat) ReadAuthorizedReference(ctx context.Context, p chat.Principal, tenant, conversation, post string) (chat.Conversation, *chat.Post, error) {
	c, err := f.GetConversation(ctx, chat.GetConversationRequest{Principal: p, TenantID: tenant, ConversationID: conversation})
	if err != nil {
		return chat.Conversation{}, nil, err
	}
	got, ok := f.posts[post]
	if !ok {
		return chat.Conversation{}, nil, chat.ErrNotFound
	}
	return c, &got, nil
}

func (f *issue3815Chat) ListMemberships(_ context.Context, r chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error) {
	if !f.readers[r.Principal.SubjectID] {
		return chat.ListMembershipsResponse{}, chat.ErrNotFound
	}
	return chat.ListMembershipsResponse{Memberships: f.members}, nil
}

type issue3815Store struct {
	reqs map[string]chatstore.AckRequirement
	acks map[string]map[string]chatstore.Ack
	fail error
}

func (s *issue3815Store) RequireAck(_ context.Context, r chatstore.AckRequirement) (bool, error) {
	if s.fail != nil {
		return false, s.fail
	}
	if _, done := s.reqs[r.PostID]; done {
		return false, nil
	}
	s.reqs[r.PostID] = r
	return true, nil
}
func (s *issue3815Store) AckRequirements(_ context.Context, _, _ string, posts []string) (map[string]chatstore.AckRequirement, error) {
	if s.fail != nil {
		return nil, s.fail
	}
	out := map[string]chatstore.AckRequirement{}
	for _, id := range posts {
		if r, ok := s.reqs[id]; ok {
			out[id] = r
		}
	}
	return out, nil
}
func (s *issue3815Store) RecordAck(_ context.Context, a chatstore.Ack) (bool, error) {
	if s.fail != nil {
		return false, s.fail
	}
	if _, ok := s.reqs[a.PostID]; !ok {
		return false, chat.ErrNotFound
	}
	if s.acks[a.PostID] == nil {
		s.acks[a.PostID] = map[string]chatstore.Ack{}
	}
	if _, done := s.acks[a.PostID][a.SubjectID]; done {
		return false, nil
	}
	s.acks[a.PostID][a.SubjectID] = a
	return true, nil
}
func (s *issue3815Store) AckCounts(_ context.Context, _, _ string, posts []string) (map[string]int, error) {
	out := map[string]int{}
	for _, id := range posts {
		if n := len(s.acks[id]); n > 0 {
			out[id] = n
		}
	}
	return out, nil
}
func (s *issue3815Store) MyAcks(_ context.Context, _, _ string, posts []string, subject string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	for _, id := range posts {
		if a, ok := s.acks[id][subject]; ok {
			out[id] = a.AcknowledgedAt
		}
	}
	return out, nil
}
func (s *issue3815Store) ListAcks(_ context.Context, _, _, post string) ([]chatstore.Ack, error) {
	var out []chatstore.Ack
	for _, a := range s.acks[post] {
		out = append(out, a)
	}
	return out, nil
}

func issue3815Fixture(t *testing.T) (*ChatPostAcks, *issue3815Store) {
	t.Helper()
	joined := time.Unix(1, 0)
	member := func(id string, role chat.MembershipRole) chat.Membership {
		return chat.Membership{SubjectID: id, HomeTenantID: "tenant-a", Role: role, JoinedAt: &joined}
	}
	fake := &issue3815Chat{
		conv: chat.Conversation{ID: "room", TenantID: "tenant-a", Kind: chat.PublicChannel, OwnerID: "owner"},
		posts: map[string]chat.Post{
			"policy": {ID: "policy", ConversationID: "room", TenantID: "tenant-a", AuthorID: "owner", Body: "New safety policy", Revision: 3},
			"chatty": {ID: "chatty", ConversationID: "room", TenantID: "tenant-a", AuthorID: "person", Body: "lunch?"},
			"card":   {ID: "card", ConversationID: "room", TenantID: "tenant-a", AuthorID: "owner", Body: chat.FormCardMarker + "{}"},
		},
		readers: map[string]bool{"owner": true, "person": true, "manager": true, "lurker": true, "gone": true},
		members: []chat.Membership{
			member("owner", chat.Manager), member("manager", chat.Manager), member("person", chat.Member), member("other", chat.Member),
			{SubjectID: "gone", HomeTenantID: "tenant-a", JoinedAt: &joined, LeftAt: &joined},
			{SubjectID: "guest", HomeTenantID: "tenant-z", JoinedAt: &joined},
		},
	}
	store := &issue3815Store{reqs: map[string]chatstore.AckRequirement{}, acks: map[string]map[string]chatstore.Ack{}}
	return &ChatPostAcks{Chat: fake, Store: store}, store
}

func TestIssue3815_OwnerRequiresAndMembersAcknowledgeOnce_Integration(t *testing.T) {
	acks, store := issue3815Fixture(t)
	ctx := context.Background()
	owner := issue3795Person(t, "owner", trust.SubjectKindHuman)
	person := issue3795Person(t, "person", trust.SubjectKindHuman)

	view, err := acks.Require(ctx, owner, "room", "policy")
	if err != nil || !view.Required || !view.CanManage || view.Total != 3 || view.AckCount != 0 || len(view.Pending) != 3 {
		t.Fatalf("require = %+v, %v (the audience is the three active members of this company other than the sender)", view, err)
	}
	if got := store.reqs["policy"]; got.RequiredByID != "owner" || got.PostRevision != 3 {
		t.Fatalf("requirement stored = %+v", got)
	}
	// Reading is not acknowledging.
	reply, err := acks.Views(ctx, person, "room", []string{"policy", "chatty"})
	views := reply.Views
	if err != nil || len(views) != 1 || !views["policy"].CanAcknowledge || views["policy"].Acknowledged || views["policy"].CanManage {
		t.Fatalf("a member's view before = %+v, %v", views, err)
	}
	if views["policy"].Pending != nil || views["policy"].Total != 0 {
		t.Fatalf("a member was shown the roll: %+v", views["policy"])
	}
	if len(store.acks["policy"]) != 0 {
		t.Fatal("reading the post recorded an acknowledgement")
	}
	done, err := acks.Acknowledge(ctx, person, "room", "policy")
	if err != nil || !done.Acknowledged || done.AcknowledgedAt == nil || done.CanAcknowledge {
		t.Fatalf("acknowledge = %+v, %v", done, err)
	}
	if got := store.acks["policy"]["person"]; got.PostRevision != 3 {
		t.Fatalf("the revision the person saw was not kept: %+v", got)
	}
	// A second tap is the same answer and writes nothing new.
	again, err := acks.Acknowledge(ctx, person, "room", "policy")
	if err != nil || !again.Acknowledged || len(store.acks["policy"]) != 1 {
		t.Fatalf("second acknowledge = %+v, %v", again, err)
	}
	manager := issue3795Person(t, "manager", trust.SubjectKindHuman)
	reply, err = acks.Views(ctx, manager, "room", []string{"policy"})
	got := reply.Views["policy"]
	if err != nil || !got.CanManage || got.Total != 3 || got.AckCount != 1 || len(got.Pending) != 2 {
		t.Fatalf("manager status = %+v, %v", got, err)
	}
	for _, p := range got.Pending {
		if p == "person" || p == "gone" || p == "guest" {
			t.Fatalf("pending names %q", p)
		}
	}
}

func TestIssue3815_OnlyAnAuthorizedSenderMayRequire_Security(t *testing.T) {
	acks, store := issue3815Fixture(t)
	ctx := context.Background()
	person := issue3795Person(t, "person", trust.SubjectKindHuman)
	// A plain member cannot require it of their own post.
	if _, err := acks.Require(ctx, person, "room", "chatty"); !errors.Is(err, ErrChatAckDenied) {
		t.Fatalf("member require = %v", err)
	}
	// A manager cannot require it of somebody else's post.
	manager := issue3795Person(t, "manager", trust.SubjectKindHuman)
	if _, err := acks.Require(ctx, manager, "room", "policy"); !errors.Is(err, ErrChatAckDenied) {
		t.Fatalf("manager require of another's post = %v", err)
	}
	// A system card is not a policy.
	owner := issue3795Person(t, "owner", trust.SubjectKindHuman)
	if _, err := acks.Require(ctx, owner, "room", "card"); !errors.Is(err, ErrChatAckInvalid) {
		t.Fatalf("card require = %v", err)
	}
	// An agent never requires or acknowledges on a person's behalf.
	agent := issue3795Person(t, "owner", trust.SubjectKindAgent)
	if _, err := acks.Require(ctx, agent, "room", "policy"); !errors.Is(err, ErrChatAckDenied) {
		t.Fatalf("agent require = %v", err)
	}
	// Somebody who cannot open the room learns nothing.
	stranger := issue3795Person(t, "stranger", trust.SubjectKindHuman)
	for name, call := range map[string]func() error{
		"require": func() error { _, err := acks.Require(ctx, stranger, "room", "policy"); return err },
		"ack":     func() error { _, err := acks.Acknowledge(ctx, stranger, "room", "policy"); return err },
	} {
		if err := call(); !errors.Is(err, ErrChatAckNotFound) {
			t.Errorf("%s by a stranger = %v; want not found", name, err)
		}
	}
	if len(store.reqs) != 0 {
		t.Fatalf("a refused call wrote %v", store.reqs)
	}
}

func TestIssue3815_OnlyAudienceMembersAcknowledge_Security(t *testing.T) {
	acks, store := issue3815Fixture(t)
	ctx := context.Background()
	if _, err := acks.Require(ctx, issue3795Person(t, "owner", trust.SubjectKindHuman), "room", "policy"); err != nil {
		t.Fatal(err)
	}
	// A reader of a public channel who has not joined is not in the audience, and a
	// person who left is not either.
	for _, who := range []string{"lurker", "gone"} {
		p := issue3795Person(t, who, trust.SubjectKindHuman)
		reply, err := acks.Views(ctx, p, "room", []string{"policy"})
		if v := reply.Views["policy"]; err != nil || v.CanAcknowledge || v.CanManage {
			t.Errorf("%s view = %+v, %v", who, v, err)
		}
		if _, err := acks.Acknowledge(ctx, p, "room", "policy"); !errors.Is(err, ErrChatAckDenied) {
			t.Errorf("%s acknowledge = %v", who, err)
		}
	}
	// A post that requires nothing cannot be acknowledged.
	if _, err := acks.Acknowledge(ctx, issue3795Person(t, "person", trust.SubjectKindHuman), "room", "chatty"); !errors.Is(err, ErrChatAckNotFound) {
		t.Errorf("ack of a plain post = %v", err)
	}
	if _, err := acks.Acknowledge(ctx, issue3795Person(t, "person", trust.SubjectKindAgent), "room", "policy"); !errors.Is(err, ErrChatAckDenied) {
		t.Errorf("agent ack = %v", err)
	}
	if len(store.acks["policy"]) != 0 {
		t.Fatalf("refused calls wrote %v", store.acks)
	}
}

func TestIssue3815_StorageFailureIsUnavailable(t *testing.T) {
	acks, store := issue3815Fixture(t)
	store.fail = errors.New("db down")
	if _, err := acks.Require(context.Background(), issue3795Person(t, "owner", trust.SubjectKindHuman), "room", "policy"); !errors.Is(err, ErrChatAckUnavailable) {
		t.Fatalf("require = %v", err)
	}
	if _, err := acks.Views(context.Background(), issue3795Person(t, "person", trust.SubjectKindHuman), "room", []string{"policy"}); !errors.Is(err, ErrChatAckUnavailable) {
		t.Fatalf("views = %v", err)
	}
}

func TestIssue3815_RoutesRefuseWithShortCodes_Security(t *testing.T) {
	acks, _ := issue3815Fixture(t)
	handler := ChatAckHandler{Acks: acks}
	person := issue3795Person(t, "person", trust.SubjectKindHuman)
	owner := issue3795Person(t, "owner", trust.SubjectKindHuman)
	call := func(p *trust.Principal, method, path string) (int, map[string]any) {
		req := httptest.NewRequest(method, path, nil)
		if p != nil {
			req = req.WithContext(trust.WithPrincipal(req.Context(), p))
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}
	if code, _ := call(nil, http.MethodGet, "/api/chat/conversations/room/acks?post=policy"); code != http.StatusUnauthorized {
		t.Errorf("no session: %d", code)
	}
	if code, body := call(person, http.MethodPost, "/api/chat/conversations/room/acks/chatty/require"); code != http.StatusForbidden || body["code"] != "permission_denied" {
		t.Errorf("member require: %d %v", code, body)
	}
	if code, body := call(owner, http.MethodPost, "/api/chat/conversations/room/acks/policy/require"); code != http.StatusOK || body["view"] == nil {
		t.Fatalf("owner require: %d %v", code, body)
	}
	if code, body := call(person, http.MethodPost, "/api/chat/conversations/room/acks/policy/acknowledge"); code != http.StatusOK || body["view"] == nil {
		t.Fatalf("acknowledge: %d %v", code, body)
	}
	code, body := call(person, http.MethodGet, "/api/chat/conversations/room/acks?post=policy&post=chatty")
	views, _ := body["views"].(map[string]any)
	if code != http.StatusOK || len(views) != 1 || body["can_require"] != false {
		t.Fatalf("list: %d %v", code, body)
	}
	if strings.Contains(issue3815JSON(t, body), "owner") {
		t.Errorf("a member's answer names the sender: %v", body)
	}
	if code, _ := call(person, http.MethodGet, "/api/chat/conversations/room/acks/policy/acknowledge"); code != http.StatusMethodNotAllowed {
		t.Errorf("wrong method: %d", code)
	}
	if code, body := call(person, http.MethodPost, "/api/chat/conversations/other/acks/policy/acknowledge"); code != http.StatusNotFound || body["code"] != "not_found" {
		t.Errorf("unknown room: %d %v", code, body)
	}
	if code, body := call(issue3795Person(t, "person", trust.SubjectKindAgent), http.MethodGet, "/api/chat/conversations/room/acks?post=policy"); code != http.StatusForbidden || body["code"] != "permission_denied" {
		t.Errorf("agent: %d %v", code, body)
	}
}

func issue3815JSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
