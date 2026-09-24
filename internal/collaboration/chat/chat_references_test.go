package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

type referenceAuthority struct{}

type deniedReferenceAuthority struct{ action chatpolicy.Action }

func (a deniedReferenceAuthority) Authorize(ctx context.Context, p Principal, c Conversation, action chatpolicy.Action, at time.Time) (chatpolicy.Input, error) {
	if action == a.action {
		return chatpolicy.Input{}, chatpolicy.ErrNotAuthorized
	}
	return referenceAuthority{}.Authorize(ctx, p, c, action, at)
}

func (referenceAuthority) Authorize(_ context.Context, p Principal, c Conversation, _ chatpolicy.Action, at time.Time) (chatpolicy.Input, error) {
	if p.SubjectID == "other" {
		return chatpolicy.Input{}, chatpolicy.ErrNotAuthorized
	}
	return chatpolicy.Input{
		Principal:     chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, AuthorityRevision: 1},
		Channel:       chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Enabled: true, Revision: c.Revision},
		Membership:    chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: 1, JoinedAt: at.Add(-time.Hour)},
		HasMembership: true, Now: at,
	}, nil
}

type referenceDirectory struct{ candidates []ReferenceCandidate }

func (d referenceDirectory) People(context.Context, Principal, string, string, string) ([]ReferenceCandidate, error) {
	return d.candidates, nil
}
func (d referenceDirectory) Agents(context.Context, Principal, string, string, string) ([]ReferenceCandidate, error) {
	return d.candidates, nil
}
func (d referenceDirectory) Conversations(context.Context, Principal, string, string, string) ([]ReferenceCandidate, error) {
	return d.candidates, nil
}

type agentReferenceDirectory struct{ referenceDirectory }

func (agentReferenceDirectory) AgentEligible(context.Context, string, string, string) bool {
	return true
}

func TestTodo_CHAT_027_SecurityMentionCommitRechecksMembership(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1}, post: Post{ID: "p1"}}
	s := NewService(f, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(referenceAuthority{})
	s.SetReferenceDirectory(referenceDirectory{candidates: []ReferenceCandidate{{Reference: Reference{Kind: PersonMention, TenantID: "t1", ID: "u2", Display: "Outside"}, Eligible: true}}})
	got, err := s.SuggestReferences(context.Background(), SuggestReferencesRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Kind: PersonMention})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("suggestions = %#v, want no unauthorized audience member", got)
	}
}

func TestTodo_CHAT_029_SecurityLinkDoesNotGrantAccess(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1}, post: Post{ID: "p1"}}
	s := NewService(f, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(referenceAuthority{})
	link, err := s.CreateConversationLink(context.Background(), principal(), "t1", "c1", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ResolveConversationLink(context.Background(), Principal{TenantID: "t1", SubjectID: "other"}, link.URL[len("/chat/share/"):]); err == nil {
		t.Fatal("link granted access to an unauthorized principal")
	}
}

func TestTodo_CHAT_029_SecurityPreJoinPostCannotBeLocated(t *testing.T) {
	joined := time.Unix(30, 0).UTC()
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1, HistoryVisibility: FromJoin, JoinedAt: &joined}, post: Post{ID: "p1", TenantID: "t1", ConversationID: "c1", CreatedAt: time.Unix(20, 0).UTC()}}
	s := NewService(f, func() time.Time { return time.Unix(40, 0).UTC() })
	s.SetAuthority(referenceAuthority{})
	if _, err := s.CreateShareLink(context.Background(), principal(), "t1", "c1", "p1"); err != ErrNotFound {
		t.Fatalf("err = %v, want not found for pre-join source", err)
	}
}

func TestTodo_CHAT_029_SecurityMalformedJoinHistoryFailsClosed(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1, HistoryVisibility: FromJoin}, post: Post{ID: "p1", TenantID: "t1", ConversationID: "c1", CreatedAt: time.Unix(20, 0).UTC()}}
	s := NewService(f, func() time.Time { return time.Unix(40, 0).UTC() })
	s.SetAuthority(referenceAuthority{})
	if _, err := s.CreateShareLink(context.Background(), principal(), "t1", "c1", "p1"); err != ErrNotFound {
		t.Fatalf("err = %v, want not found for missing join instant", err)
	}
}

func TestTodo_CHAT_030_SecurityLinkResolveRechecksHistoryAndDeletion(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1, HistoryVisibility: FullHistory}, post: Post{ID: "p1", TenantID: "t1", ConversationID: "c1", Body: "private contents", CreatedAt: time.Unix(20, 0).UTC()}}
	s := NewService(f, func() time.Time { return time.Unix(40, 0).UTC() })
	s.SetAuthority(referenceAuthority{})
	link, err := s.CreateShareLink(context.Background(), principal(), "t1", "c1", "p1")
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(link.URL, "/chat/share/")
	if _, p, err := s.ResolveShareLink(context.Background(), principal(), token); err != nil || p == nil || p.Body != "private contents" {
		t.Fatalf("authorized resolution = %+v, %v", p, err)
	}
	f.membership.HistoryVisibility = NoHistory
	if _, p, err := s.ResolveShareLink(context.Background(), principal(), token); err != ErrNotFound || p != nil {
		t.Fatalf("revoked history resolution = %+v, %v", p, err)
	}
	f.membership.HistoryVisibility = FullHistory
	f.post.Deleted = true
	if _, p, err := s.ResolveShareLink(context.Background(), principal(), token); err != ErrNotFound || p != nil {
		t.Fatalf("deleted post resolution = %+v, %v", p, err)
	}
}

func TestForwardPostUsesSendPost(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1}, post: Post{ID: "p1", TenantID: "t1", ConversationID: "c1", AuthorID: "u2", Body: "source", Revision: 3}}
	s := NewService(f, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(referenceAuthority{})
	s.SetDisclosureChecker(&chat030Disclosure{})
	_, err := s.ForwardPost(context.Background(), ForwardPostRequest{Principal: principal(), SourceTenantID: "t1", SourceConversationID: "c1", SourcePostID: "p1", DestinationConversationID: "c1", IdempotencyKey: "forward-1", SourceAttribution: SourceAttribution{PostRevision: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if f.mutations == 0 {
		t.Fatal("forward did not reach durable SendPost")
	}
	if f.sent.Body != "source" || f.sent.SourceAttribution == nil || f.sent.SourceAttribution.PostID != "p1" || f.sent.SourceAttribution.PostRevision != 3 || f.sent.SourceAttribution.OriginalAuthorID != "u2" {
		t.Fatalf("forwarded post = %+v, want source body and server-derived attribution", f.sent)
	}
}

func TestTodo_CHAT_030_SecurityForwardRequiresBothGrants(t *testing.T) {
	for _, action := range []chatpolicy.Action{chatpolicy.ActionRead, chatpolicy.ActionPost} {
		t.Run(fmt.Sprint(action), func(t *testing.T) {
			f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1}, post: Post{ID: "p1", TenantID: "t1", ConversationID: "c1", Body: "source", Revision: 1}}
			s := NewService(f, func() time.Time { return time.Unix(20, 0).UTC() })
			s.SetAuthority(deniedReferenceAuthority{action: action})
			_, err := s.ForwardPost(context.Background(), ForwardPostRequest{Principal: principal(), SourceTenantID: "t1", SourceConversationID: "c1", SourcePostID: "p1", DestinationTenantID: "t1", DestinationConversationID: "c1", IdempotencyKey: "forward-denied"})
			if err != ErrPermissionDenied || f.mutations != 0 {
				t.Fatalf("forward err = %v, mutations = %d; want denied before write", err, f.mutations)
			}
		})
	}
}

func TestTodo_CHAT_030_SecurityDirectSendRejectsForgedSource(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1}}
	s := NewService(f, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(referenceAuthority{})
	_, err := s.SendPost(context.Background(), SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "forged", IdempotencyKey: "forged-1", SourceAttribution: &SourceAttribution{TenantID: "t1", ConversationID: "secret", PostID: "p1"}})
	if err != ErrPermissionDenied {
		t.Fatalf("err = %v, want permission denied", err)
	}
	if f.mutations != 0 {
		t.Fatalf("store mutations = %d, want zero", f.mutations)
	}
}

func TestTodo_CHAT_027_ReferenceKindsAndCommitValidation(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1}, post: Post{ID: "p1"}}
	s := NewService(f, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(referenceAuthority{})
	s.SetReferenceDirectory(agentReferenceDirectory{referenceDirectory{candidates: []ReferenceCandidate{
		{Reference: Reference{Kind: AgentMention, TenantID: "t1", ID: "agent-1", Display: "Agent"}, Eligible: true},
		{Reference: Reference{Kind: ConversationMention, TenantID: "t1", ID: "c1", ConversationID: "c1", Display: "Channel"}, Eligible: true},
	}}})
	for _, kind := range []ReferenceKind{AgentMention, ConversationMention} {
		got, err := s.SuggestReferences(context.Background(), SuggestReferencesRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Kind: kind})
		if err != nil || len(got) != 1 {
			t.Fatalf("kind %s suggestions = %#v, err=%v", kind, got, err)
		}
	}
	_, err := s.SendPostWithReferences(context.Background(), SendPostWithReferencesRequest{SendPostRequest: SendPostRequest{Principal: principal(), TenantID: "t1", ConversationID: "c1", Body: "x", IdempotencyKey: "ref-1"}, References: []Reference{{Kind: PersonMention, TenantID: "t1", ID: "missing"}}})
	if err != ErrPermissionDenied {
		t.Fatalf("invalid selected reference err = %v", err)
	}
}

func TestTodo_CHAT_030_LinkWithoutPostAndForwardRevisionConflict(t *testing.T) {
	f := &fakeStore{conversation: conversation(), membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1}, post: Post{ID: "p1", TenantID: "t1", ConversationID: "c1", Body: "source", Revision: 3}}
	s := NewService(f, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(referenceAuthority{})
	link, err := s.CreateShareLink(context.Background(), principal(), "t1", "c1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, post, err := s.ResolveShareLink(context.Background(), principal(), link.URL[len("/chat/share/"):]); err != nil || post != nil {
		t.Fatalf("conversation-only link = post %#v, err %v", post, err)
	}
	_, err = s.ForwardPost(context.Background(), ForwardPostRequest{Principal: principal(), SourceTenantID: "t1", SourceConversationID: "c1", SourcePostID: "p1", DestinationConversationID: "c1", IdempotencyKey: "forward-stale", SourceAttribution: SourceAttribution{PostRevision: 2}})
	if err != ErrConflict {
		t.Fatalf("stale forward err = %v", err)
	}
}
