package chat

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

type chat021RevokingAuthority struct {
	calls int
}

func (a *chat021RevokingAuthority) Authorize(_ context.Context, p Principal, c Conversation, _ chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	a.calls++
	if a.calls == 2 {
		return chatpolicy.Input{}, ErrPermissionDenied
	}
	return chatpolicy.Input{
		Principal:     chatpolicy.Principal{ID: p.SubjectID, Tenant: p.TenantID, Active: true, AuthorityRevision: uint64(a.calls)},
		Channel:       chatpolicy.Channel{ID: c.ID, HostTenant: c.TenantID, Enabled: true, Private: true, Revision: c.Revision},
		Membership:    chatpolicy.Membership{ConversationID: c.ID, PrincipalID: p.SubjectID, Tenant: p.TenantID, State: chatpolicy.MembershipCurrent, Revision: 1},
		HasMembership: true,
		Now:           now,
	}, nil
}

func TestTodo_CHAT_021_Security_RechecksCurrentAuthorizationForEachHit(t *testing.T) {
	f := &fakeStore{
		conversation: conversation(),
		search: SearchResponse{Results: []SearchResult{
			{Post: Post{ID: "p1", TenantID: "t1", ConversationID: "c1", Body: "first visible result"}},
			{Post: Post{ID: "p2", TenantID: "t1", ConversationID: "c1", Body: "must be hidden after revocation"}},
		}},
	}
	s := NewService(f, time.Now)
	authority := &chat021RevokingAuthority{}
	s.SetAuthority(authority)

	got, err := s.Search(context.Background(), SearchRequest{Principal: principal(), TenantID: "t1", Query: "result"})
	if err != nil {
		t.Fatal(err)
	}
	if authority.calls != 2 {
		t.Fatalf("authority checks = %d, want a fresh check for both hits", authority.calls)
	}
	if len(got.Results) != 1 || got.Results[0].Post.ID != "p1" {
		t.Fatalf("search results after mid-query revocation = %+v, want only first authorized hit", got.Results)
	}
}
