package application

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

func TestTodo_AGENTP_008_Integration_PublicMentionScopeRereadsChatOwners(t *testing.T) {
	store := streamIntegrationStore(t)
	_, fixture, _, _, _ := personaPrivateScopeFixture(t)
	ctx, req := fixture.ctx, fixture.scope
	const room = "public-persona-scope"
	owner := chat.Principal{TenantID: req.Tenant.String(), SubjectID: req.User.Principal.Subject()}
	conversation := chat.Conversation{TenantID: owner.TenantID, ID: room, Kind: chat.PublicChannel, Name: "Public persona scope", OwnerID: "bob", Revision: 1}
	members := []chat.Membership{
		{TenantID: owner.TenantID, HomeTenantID: owner.TenantID, ConversationID: room, SubjectID: "bob", Role: chat.Manager, HistoryVisibility: chat.FullHistory},
		{TenantID: owner.TenantID, HomeTenantID: owner.TenantID, ConversationID: room, SubjectID: "alice", Role: chat.Member, HistoryVisibility: chat.FullHistory},
	}
	if _, err := store.CreateConversation(ctx, conversation, members, "public-scope-create"); err != nil {
		t.Fatal(err)
	}
	service := chat.NewService(store, func() time.Time { return req.At })
	service.SetAuthority(streamIntegrationAuthority{store: store})
	scope, err := NewPersonaChatScopeAuthorizer(service, store.Store, service)
	if err != nil {
		t.Fatal(err)
	}
	post, err := service.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: owner.TenantID, ConversationID: room, Body: "Explain the leave policy", IdempotencyKey: "public-scope-post"})
	if err != nil {
		t.Fatal(err)
	}
	caps := capability.NewRegistry()
	skills := agentskills.NewRegistry(caps)
	reply, err := bindPersonaChatReplySkill(caps, skills)
	if err != nil {
		t.Fatal(err)
	}
	search, err := bindPersonaPolicySearchSkill(caps, skills, personaPolicySearchHub31Fake{})
	if err != nil {
		t.Fatal(err)
	}
	user := req.User
	user.Population, user.Roles, user.OrganizationScopes = "members", []string{"member"}, []string{"org-a"}
	grants := agentgate.StaticGrants{}
	for _, pin := range []agentskills.SkillPin{reply, search} {
		grants = append(grants, agentgate.SkillGrant{ID: pin.ID, Tenant: req.Tenant, Skill: pin.Key(), Roles: user.Roles,
			Population: user.Population, OrganizationScopes: user.OrganizationScopes, Purposes: []string{req.Purpose}})
	}
	gate, err := agentgate.New(agentgate.Config{Skills: skills, Grants: grants, Now: func() time.Time { return req.At }})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := NewGateInvokerAuthoritySource(GateInvokerAuthoritySourceConfig{Gate: gate, Skills: skills, Current: sourceCurrent{user: user}, Purpose: req.Purpose, ChatScope: scope})
	if err != nil {
		t.Fatal(err)
	}
	bound := withPersonaChatAuthorityTuple(ctx, owner.TenantID, owner.SubjectID, room, post.ID, post.ID)
	if _, err := projection.ResolveInvokerAuthority(bound, owner.SubjectID, req.Tenant, req.Purpose, req.At); err == nil {
		t.Fatal("missing explicit public admission policy was accepted")
	}
	if _, err := store.Store.PutPublicAudiencePolicy(ctx, owner.TenantID, room, 0, chatstore.PublicAudiencePolicy{Classification: "INTERNAL", Principals: []chatstore.PublicAudiencePrincipal{
		{HomeTenantID: owner.TenantID, SubjectID: "alice"}, {HomeTenantID: owner.TenantID, SubjectID: "bob"}, {HomeTenantID: owner.TenantID, SubjectID: "future"},
	}}); err != nil {
		t.Fatal(err)
	}
	current, err := projection.ResolveInvokerAuthority(bound, owner.SubjectID, req.Tenant, req.Purpose, req.At)
	if err != nil || !current.Active || len(current.SkillAuthorities) != 2 || len(current.Authority.Fields) != 0 {
		t.Fatalf("public current authority=%#v err=%v", current, err)
	}
	for _, pin := range []agentskills.SkillPin{reply, search} {
		a := current.SkillAuthorities[pin.ID]
		if len(a.Resources) != 1 || a.Resources[0] != personaChatAuthorityResource(owner.TenantID, room, post.ID, post.ID) || len(a.Fields) != 0 {
			t.Fatalf("scope escaped the current visible tuple: %#v", a)
		}
	}
	foreign := withPersonaChatAuthorityTuple(ctx, owner.TenantID, owner.SubjectID, room, post.ID, "not-visible")
	if _, err := projection.ResolveInvokerAuthority(foreign, owner.SubjectID, req.Tenant, req.Purpose, req.At); err == nil {
		t.Fatal("foreign invoking post admitted")
	}
	if _, err := service.DeletePost(ctx, chat.DeletePostRequest{Principal: owner, TenantID: owner.TenantID, ConversationID: room, PostID: post.ID, ExpectedRevision: post.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := projection.ResolveInvokerAuthority(bound, owner.SubjectID, req.Tenant, req.Purpose, req.At); err == nil {
		t.Fatal("deleted invoking post retained authority")
	}
	post, err = service.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: owner.TenantID, ConversationID: room, Body: "Explain the leave policy again", IdempotencyKey: "public-scope-second-post"})
	if err != nil {
		t.Fatal(err)
	}
	bound = withPersonaChatAuthorityTuple(ctx, owner.TenantID, owner.SubjectID, room, post.ID, post.ID)
	if _, err := projection.ResolveInvokerAuthority(bound, owner.SubjectID, req.Tenant, req.Purpose, req.At); err != nil {
		t.Fatal(err)
	}
	membership, err := store.GetMembership(ctx, owner.TenantID, room, owner.TenantID, owner.SubjectID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RemoveMembership(ctx, chat.RemoveMembershipRequest{Principal: owner, TenantID: owner.TenantID, HomeTenantID: owner.TenantID,
		ConversationID: room, SubjectID: owner.SubjectID, ExpectedRevision: membership.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := projection.ResolveInvokerAuthority(bound, owner.SubjectID, req.Tenant, req.Purpose, req.At); err == nil {
		t.Fatal("former member retained invocation authority")
	}
}
