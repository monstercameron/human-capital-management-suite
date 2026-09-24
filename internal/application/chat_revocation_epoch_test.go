package application

import (
	"context"
	"errors"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatauthority"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_CHAT_020_CachedPolicyEpochRefreshesAfterPolicyUpdate proves that a
// derived authorization made under one persisted policy revision cannot keep
// authorizing reads after a narrower policy revision commits.
func TestTodo_CHAT_020_CachedPolicyEpochRefreshesAfterPolicyUpdate(t *testing.T) {
	at := time.Now().UTC()
	db := &policyRevocationDB{policy: &chatpolicy.Channel{
		ID:         "conversation",
		HostTenant: "host",
		RoleMode:   chatpolicy.RolesAny,
		Revision:   1,
	}}
	cache := newChatAuthorityCache(time.Minute, nil)
	members := &policyReadStore{}
	authority := newChatCurrentAuthority(policyReadFacts{}, members, chatauthority.New(db), cache)
	principal := chatcore.Principal{TenantID: "host", SubjectID: "member"}
	conversation := chatcore.Conversation{ID: "conversation", TenantID: "host", Kind: chatcore.PrivateChannel, Revision: 1}
	memberPrincipal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "host", Subject: "member", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-member", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour),
		CredentialDigest: "chat-revocation-epoch",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), memberPrincipal)

	before, err := authority.Authorize(ctx, principal, conversation, chatpolicy.ActionRead, at)
	if err != nil {
		t.Fatalf("initial authority: %v", err)
	}
	initial, err := chatpolicy.Evaluate(chatpolicy.ActionRead, before)
	if err != nil || !initial.Allowed {
		t.Fatalf("initial policy decision=%+v err=%v; want allowed", initial, err)
	}
	if cached, ok := cachedChatGet(cache, cache.policies, chatCacheKey("host", "conversation")); !ok || cached.channel.Revision != 1 {
		t.Fatalf("initial cached policy=%+v ok=%t; want revision 1", cached, ok)
	}

	grants := NewChatCompanyGrants(chatAdminFacts{role: "hcm_admin"}, chatauthority.New(db)).withRevocation(cache, nil)
	if err := grants.SetChannelPolicy(policyAdminContext(t, at), "host", "conversation", []string{"manager"}, nil, nil, nil, chatpolicy.RolesAll, "confidential", "US", 1, at); err != nil {
		t.Fatalf("commit narrower policy: %v", err)
	}
	if _, ok := cachedChatGet(cache, cache.policies, chatCacheKey("host", "conversation")); ok {
		t.Fatal("old policy revision remained cached after update returned")
	}

	after, err := authority.Authorize(ctx, principal, conversation, chatpolicy.ActionRead, at)
	if err != nil {
		t.Fatalf("resolve current authority after update: %v", err)
	}
	decision, err := chatpolicy.Evaluate(chatpolicy.ActionRead, after)
	if !errors.Is(err, chatpolicy.ErrNotAuthorized) || decision.Allowed {
		t.Fatalf("current policy decision=%+v err=%v; want denied by new role requirement", decision, err)
	}
	if after.Channel.Revision == before.Channel.Revision || len(after.Channel.RequiredRoles) != 1 || after.Channel.RequiredRoles[0] != "manager" {
		t.Fatalf("current channel policy=%+v; want a distinct revision requiring manager", after.Channel)
	}
	if cached, ok := cachedChatGet(cache, cache.policies, chatCacheKey("host", "conversation")); !ok || cached.channel.Revision != 2 {
		t.Fatalf("refreshed cached policy=%+v ok=%t; want revision 2", cached, ok)
	}
}
