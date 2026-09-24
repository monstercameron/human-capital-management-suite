package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

type chat030Disclosure struct {
	err   error
	input DisclosureInput
	calls int
}

type chat030GuestAuthority struct{}

type chat030TenantStore struct{ *fakeStore }

type chat030AudienceStore struct{ *fakeStore }

func (s chat030AudienceStore) GetConversation(_ context.Context, tenant, id string) (Conversation, error) {
	kind := PrivateChannel
	if id == "public" {
		kind = PublicChannel
	}
	return Conversation{ID: id, TenantID: tenant, Kind: kind, Revision: 1}, nil
}

func (s chat030TenantStore) GetConversation(ctx context.Context, tenant, id string) (Conversation, error) {
	c, err := s.fakeStore.GetConversation(ctx, tenant, id)
	if err != nil {
		return Conversation{}, err
	}
	c.TenantID = tenant
	return c, nil
}

func (chat030GuestAuthority) Authorize(_ context.Context, p Principal, c Conversation, _ chatpolicy.Action, at time.Time) (chatpolicy.Input, error) {
	return chatpolicy.Input{
		Principal:     chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, AuthorityRevision: 1},
		Channel:       chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Private: true, Enabled: true, Revision: c.Revision, Classification: "internal", Residency: "US"},
		Membership:    chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: 1, GrantVersion: 1, JoinedAt: at.Add(-time.Hour)},
		HasMembership: true,
		Grant:         chatpolicy.Grant{ID: "grant-1", ConversationID: c.ID, HostTenant: c.TenantID, ConsumerTenant: p.TenantID, Version: 1, Scope: "conversation", Classification: "internal", Residency: "US", Proposed: true, AcceptedByHost: true, AcceptedByConsumer: true, ExpiresAt: at.Add(time.Hour)},
		HasGrant:      true, Now: at,
	}, nil
}

func TestTodo_CHAT_030_Security(t *testing.T) {
	f := &fakeStore{
		conversation: conversation(),
		membership:   Membership{TenantID: "t1", HomeTenantID: "t2", ConversationID: "c1", SubjectID: "u1", Revision: 1},
		post:         Post{ID: "p1", TenantID: "t1", ConversationID: "c1", Body: "private contents", Revision: 1},
	}
	s := NewService(f, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(chat030GuestAuthority{})
	guest := Principal{TenantID: "t2", SubjectID: "u1"}

	link, err := s.CreateShareLink(context.Background(), guest, "t1", "c1", "p1")
	if err != nil {
		t.Fatalf("guest CreateShareLink: %v", err)
	}
	token := strings.TrimPrefix(link.URL, "/chat/share/")
	if strings.Contains(token, "private contents") {
		t.Fatal("share locator contains forwarded post content")
	}
	if _, post, err := s.ResolveShareLink(context.Background(), guest, token); err != nil || post == nil || post.Body != "private contents" {
		t.Fatalf("authorized guest resolution = %+v, %v", post, err)
	}
}

func (c *chat030Disclosure) Check(_ context.Context, in DisclosureInput) error {
	c.calls++
	c.input = in
	return c.err
}

func TestTodo_CHAT_030(t *testing.T) {
	f := &fakeStore{
		conversation: conversation(),
		membership:   Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "private", SubjectID: "u1", Revision: 1},
		post:         Post{ID: "p1", TenantID: "t1", ConversationID: "private", AuthorID: "u2", Body: "source", Revision: 3},
	}
	s := NewService(chat030AudienceStore{fakeStore: f}, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(referenceAuthority{})
	checker := &chat030Disclosure{}
	s.SetDisclosureChecker(checker)

	_, err := s.ForwardPost(context.Background(), ForwardPostRequest{
		Principal: principal(), SourceTenantID: "t1", SourceConversationID: "private", SourcePostID: "p1",
		DestinationTenantID: "t1", DestinationConversationID: "public", IdempotencyKey: "forward-1",
	})
	if err != nil {
		t.Fatalf("ForwardPost: %v", err)
	}
	if checker.calls != 1 || checker.input.SourcePost.Body != "source" || checker.input.Source.Kind != PrivateChannel || checker.input.Destination.Kind != PublicChannel {
		t.Fatalf("disclosure checks = %d, input = %+v; want source and destination checked once", checker.calls, checker.input)
	}
	if f.sent.SourceAttribution == nil || f.sent.SourceAttribution.PostID != "p1" || f.sent.SourceAttribution.PostRevision != 3 || f.sent.SourceAttribution.OriginalAuthorID != "u2" {
		t.Fatalf("persisted forwarded post attribution = %+v, want server-derived source provenance", f.sent.SourceAttribution)
	}
}

func TestTodo_CHAT_030_SecurityDisclosureMustAuthorizeBeforeWrite(t *testing.T) {
	f := &fakeStore{
		conversation: conversation(),
		membership:   Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1},
		post:         Post{ID: "p1", TenantID: "t1", ConversationID: "c1", Body: "source", Revision: 1},
	}
	s := NewService(f, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(referenceAuthority{})

	_, err := s.ForwardPost(context.Background(), ForwardPostRequest{
		Principal: principal(), SourceTenantID: "t1", SourceConversationID: "c1", SourcePostID: "p1",
		DestinationTenantID: "t1", DestinationConversationID: "c1", IdempotencyKey: "no-checker",
	})
	if err != nil || f.mutations != 1 {
		t.Fatalf("same-tenant forward without optional checker err = %v, mutations = %d; want one successful write", err, f.mutations)
	}

	checker := &chat030Disclosure{err: errors.New("destination disclosure refused")}
	s.SetDisclosureChecker(checker)
	_, err = s.ForwardPost(context.Background(), ForwardPostRequest{
		Principal: principal(), SourceTenantID: "t1", SourceConversationID: "c1", SourcePostID: "p1",
		DestinationTenantID: "t1", DestinationConversationID: "c1", IdempotencyKey: "denied-disclosure",
	})
	if !errors.Is(err, ErrPermissionDenied) || f.mutations != 1 || checker.calls != 1 {
		t.Fatalf("refused disclosure err = %v, mutations = %d, checks = %d; want denial before write", err, f.mutations, checker.calls)
	}
}

func TestTodo_CHAT_030_SecurityCrossTenantForwardNeedsChecker(t *testing.T) {
	f := &fakeStore{
		conversation: conversation(),
		membership:   Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "c1", SubjectID: "u1", Revision: 1},
		post:         Post{ID: "p1", TenantID: "t1", ConversationID: "c1", Body: "source", Revision: 1},
	}
	s := NewService(chat030TenantStore{fakeStore: f}, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(chat030GuestAuthority{})
	_, err := s.ForwardPost(context.Background(), ForwardPostRequest{
		Principal: principal(), SourceTenantID: "t1", SourceConversationID: "c1", SourcePostID: "p1",
		DestinationTenantID: "t2", DestinationConversationID: "c1", IdempotencyKey: "cross-tenant",
	})
	if !errors.Is(err, ErrPermissionDenied) || f.mutations != 0 {
		t.Fatalf("cross-tenant forward without checker err = %v, mutations = %d; want denial before write", err, f.mutations)
	}
}

func TestTodo_CHAT_030_SecurityPrivateToPublicNeedsDisclosureChecker(t *testing.T) {
	f := &fakeStore{
		membership: Membership{TenantID: "t1", HomeTenantID: "t1", ConversationID: "private", SubjectID: "u1", Revision: 1},
		post:       Post{ID: "p1", TenantID: "t1", ConversationID: "private", Body: "private source", Revision: 1},
	}
	s := NewService(chat030AudienceStore{fakeStore: f}, func() time.Time { return time.Unix(20, 0).UTC() })
	s.SetAuthority(referenceAuthority{})
	_, err := s.ForwardPost(context.Background(), ForwardPostRequest{
		Principal: principal(), SourceTenantID: "t1", SourceConversationID: "private", SourcePostID: "p1",
		DestinationTenantID: "t1", DestinationConversationID: "public", IdempotencyKey: "private-to-public",
	})
	if !errors.Is(err, ErrPermissionDenied) || f.mutations != 0 {
		t.Fatalf("private to public forward err = %v, mutations = %d; want disclosure denial before write", err, f.mutations)
	}
}
