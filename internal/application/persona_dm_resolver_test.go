package application

import (
	"context"
	"errors"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type personaDMStoreFake struct {
	conversations []chatcore.Conversation
	members       map[string][]chatcore.Membership
	err           error
}

func (f *personaDMStoreFake) ListConversations(context.Context, chatcore.Principal, string, chatcore.Page, chatcore.ConversationScope) (chatcore.ListConversationsResponse, error) {
	if f.err != nil {
		return chatcore.ListConversationsResponse{}, f.err
	}
	return chatcore.ListConversationsResponse{Conversations: f.conversations}, nil
}

func (f *personaDMStoreFake) ListMemberships(_ context.Context, _, conversation string, _ chatcore.Page) (chatcore.ListMembershipsResponse, error) {
	if f.err != nil {
		return chatcore.ListMembershipsResponse{}, f.err
	}
	return chatcore.ListMembershipsResponse{Memberships: f.members[conversation]}, nil
}

func personaDMFixture() PersonaDMResolver {
	return PersonaDMResolver{Store: &personaDMStoreFake{
		conversations: []chatcore.Conversation{{ID: "dm-1", TenantID: "tenant-a", Kind: chatcore.Direct}},
		members: map[string][]chatcore.Membership{"dm-1": {
			{TenantID: "tenant-a", ConversationID: "dm-1", HomeTenantID: "tenant-a", SubjectID: "human-1"},
			{TenantID: "tenant-a", ConversationID: "dm-1", HomeTenantID: "tenant-a", SubjectID: "persona-1"},
		}},
	}, Persona: chatcore.MemberRef{TenantID: "tenant-a", SubjectID: "persona-1"}}
}

func TestTodo_AGENTP_011_PersonaDMResolverResolvesExactActiveDirectPair(t *testing.T) {
	got, err := personaDMFixture().ResolvePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a")
	if err != nil || got != "dm-1" {
		t.Fatalf("resolved conversation=%q err=%v", got, err)
	}
}

func TestTodo_AGENTP_011_PersonaDMResolverRefusesMissingAndAmbiguousPairs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PersonaDMResolver)
		want   error
	}{
		{name: "nonmember", mutate: func(r *PersonaDMResolver) {}, want: chatcore.ErrPermissionDenied},
		{name: "ambiguous", mutate: func(r *PersonaDMResolver) {
			store := r.Store.(*personaDMStoreFake)
			store.conversations = append(store.conversations, chatcore.Conversation{ID: "dm-2", TenantID: "tenant-a", Kind: chatcore.Direct})
			store.members["dm-2"] = append([]chatcore.Membership(nil), store.members["dm-1"]...)
			for i := range store.members["dm-2"] {
				store.members["dm-2"][i].ConversationID = "dm-2"
			}
		}, want: errPersonaDMAmbiguous},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolver := personaDMFixture()
			if tc.name == "nonmember" {
				got, err := resolver.ResolvePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "other"}, "tenant-a")
				if got != "" || !errors.Is(err, tc.want) {
					t.Fatalf("got=%q err=%v", got, err)
				}
				return
			}
			tc.mutate(&resolver)
			got, err := resolver.ResolvePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a")
			if got != "" || !errors.Is(err, tc.want) {
				t.Fatalf("got=%q err=%v", got, err)
			}
		})
	}
}

func TestTodo_AGENTP_011_PersonaDMResolverRefusesWrongTenantAndNonDirect(t *testing.T) {
	resolver := personaDMFixture()
	if got, err := resolver.ResolvePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-b", SubjectID: "human-1"}, "tenant-a"); got != "" || !errors.Is(err, errPersonaDMResolverUnavailable) {
		t.Fatalf("wrong tenant got=%q err=%v", got, err)
	}
	store := resolver.Store.(*personaDMStoreFake)
	store.conversations[0].Kind = chatcore.Group
	if got, err := resolver.ResolvePersonaDM(context.Background(), chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a"); got != "" || !errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Fatalf("non-direct got=%q err=%v", got, err)
	}
}
