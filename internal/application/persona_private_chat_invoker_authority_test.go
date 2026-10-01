package application

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

type privateInvokerChatPolicy struct {
	denied bool
	calls  int
}

type privateInvokerUnrelatedWorkerScope struct{ user agentgate.UserContext }

func (s privateInvokerUnrelatedWorkerScope) Resolve(context.Context, *trust.Principal, string) (agentgate.UserContext, []agentgate.Subject, []authz.FieldID, error) {
	return s.user, []agentgate.Subject{{Ref: values.EntityRef{Tenant: "another-tenant", Kind: values.Kind("worker"), Id: "00000000-0000-4000-8000-000000000001"},
		Organization: authz.OrgUnitRef{Tenant: "another-tenant", ID: "another-org"}}}, []authz.FieldID{authz.FieldJobTitle}, nil
}

func (p *privateInvokerChatPolicy) AuthorizePrivateChat(_ context.Context, req agentgate.PrivateChatScopeRequest) (agentgate.PrivateChatScopeEvidence, error) {
	p.calls++
	return agentgate.PrivateChatScopeEvidence{Allowed: !p.denied, Tenant: req.Tenant, InvokerID: req.User.Principal.Subject(),
		ConversationID: req.ConversationID, ThreadID: req.ThreadID, InvokingPostID: req.InvokingPostID, InvokingPostAuthor: req.User.Principal.Subject(),
		PrivateConversation: true, ActiveMember: !p.denied, PostVisible: !p.denied, ConversationRev: 2, MembershipRev: 3,
		PostDigest: "sha256:" + strings.Repeat("a", 64), EvaluatedAt: req.At}, nil
}

func TestTodo_AGENTP_021_PrivateChatInvokerDomainAuthority(t *testing.T) {
	at := time.Date(2026, 9, 30, 18, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org-a", Roles: []string{"member"}, Purposes: []string{"persona-mention"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-a",
		CredentialDigest: "credential-a", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), principal)
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
	grants := agentgate.StaticGrants{}
	for _, pin := range []agentskills.SkillPin{reply, search} {
		grants = append(grants, agentgate.SkillGrant{ID: pin.ID, Tenant: principal.Tenant(), Skill: pin.Key(), Roles: []string{"member"},
			Population: "members", OrganizationScopes: []string{"org-a"}, Purposes: []string{"persona-mention"}})
	}
	gate, err := agentgate.New(agentgate.Config{Skills: skills, Grants: grants, Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	policy := &privateInvokerChatPolicy{}
	source := &PersonaPrivateChatInvokerAuthoritySource{Gate: gate, Chat: policy, Users: privateChatBuilderUserResolver{
		user: agentgate.UserContext{Principal: principal, Population: "members", Roles: []string{"member"}, OrganizationScopes: []string{"org-a"}}}}
	req := PersonaChatAuthorityRequest{Tenant: principal.Tenant(), InvokerID: "alice", Purpose: "persona-mention", ConversationID: "room-a",
		ThreadID: "thread-a", InvokingPostID: "post-a", Pins: []agentskills.SkillPin{reply, search}, At: at}
	current, err := source.ResolvePersonaChatInvokerAuthority(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !current.Active || len(current.SkillAuthorities) != 2 || len(current.Authority.Fields) != 0 || policy.calls != 2 {
		t.Fatalf("domain authority=%#v calls=%d", current, policy.calls)
	}
	for id, scope := range map[string]string{reply.ID: "chat.current", search.ID: "documents:search"} {
		a := current.SkillAuthorities[id]
		if !slices.Equal(a.Capabilities, []string{scope}) || len(a.Fields) != 0 || !slices.Equal(a.Resources, []string{`chat.current:["tenant-a","room-a","thread-a","post-a"]`}) {
			t.Fatalf("%s authority=%#v", id, a)
		}
	}
	projection, err := NewGateInvokerAuthoritySource(GateInvokerAuthoritySourceConfig{Gate: gate, Skills: skills,
		Current: sourceCurrent{user: source.Users.(privateChatBuilderUserResolver).user}, Purpose: req.Purpose, PrivateChat: policy})
	if err != nil {
		t.Fatal(err)
	}
	boundCtx := withPersonaChatAuthorityTuple(ctx, req.Tenant.String(), req.InvokerID, req.ConversationID, req.ThreadID, req.InvokingPostID)
	projected, err := projection.ResolveInvokerAuthority(boundCtx, req.InvokerID, req.Tenant, req.Purpose, at)
	if err != nil || !sameSkillAuthorities(projected.SkillAuthorities, current.SkillAuthorities) {
		t.Fatalf("served projection lost domain authority: %#v err=%v", projected, err)
	}
	projection.current = privateInvokerUnrelatedWorkerScope{user: source.Users.(privateChatBuilderUserResolver).user}
	if unrelated, err := projection.ResolveInvokerAuthority(boundCtx, req.InvokerID, req.Tenant, req.Purpose, at); err != nil || len(unrelated.Authority.Fields) != 0 || !sameSkillAuthorities(unrelated.SkillAuthorities, current.SkillAuthorities) {
		t.Fatalf("unrelated worker fields affected private chat scope: %#v err=%v", unrelated, err)
	}
	if _, err := projection.ResolveInvokerAuthority(ctx, req.InvokerID, req.Tenant, req.Purpose, at); err == nil {
		t.Fatal("unbound chat scope was projected")
	}
	discoverer := currentPersonaMentionSkills{projection: projection, now: func() time.Time { return at }}
	discovered, err := discoverer.ResolveInvokerSkills(boundCtx, principal, req.Purpose)
	if err != nil || len(discovered) != 2 || !slices.Equal(discovered[reply.ID], []string{"chat.current"}) || !slices.Equal(discovered[search.ID], []string{"documents:search"}) {
		t.Fatalf("served mention discovery=%#v err=%v", discovered, err)
	}
	foreignCtx := withPersonaChatAuthorityTuple(ctx, "tenant-b", req.InvokerID, req.ConversationID, req.ThreadID, req.InvokingPostID)
	if _, err := projection.ResolveInvokerAuthority(foreignCtx, req.InvokerID, req.Tenant, req.Purpose, at); err == nil {
		t.Fatal("foreign tuple was projected")
	}
	for _, tc := range []struct {
		name   string
		change func(*PersonaChatAuthorityRequest)
	}{
		{"foreign tenant", func(r *PersonaChatAuthorityRequest) { r.Tenant = "tenant-b" }},
		{"different invoker", func(r *PersonaChatAuthorityRequest) { r.InvokerID = "bob" }},
		{"missing post", func(r *PersonaChatAuthorityRequest) { r.InvokingPostID = "" }},
		{"expired identity", func(r *PersonaChatAuthorityRequest) { r.At = principal.ExpiresAt() }},
		{"missing pins", func(r *PersonaChatAuthorityRequest) { r.Pins = nil }},
		{"changed pin", func(r *PersonaChatAuthorityRequest) { r.Pins[0].Digest = "sha256:" + strings.Repeat("f", 64) }},
		{"duplicate pin", func(r *PersonaChatAuthorityRequest) { r.Pins = append(r.Pins, r.Pins[0]) }},
		{"unknown skill", func(r *PersonaChatAuthorityRequest) { r.Pins[0].ID = "worker.read" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := req
			changed.Pins = slices.Clone(req.Pins)
			tc.change(&changed)
			if got, err := source.ResolvePersonaChatInvokerAuthority(ctx, changed); err == nil || got.Active {
				t.Fatalf("unexpected authority: %#v err=%v", got, err)
			}
		})
	}
	policy.denied = true
	if _, err := source.ResolvePersonaChatInvokerAuthority(ctx, req); err == nil {
		t.Fatal("revoked membership was allowed")
	}
	policy.denied = false
	source.Users = privateChatBuilderUserResolver{err: errors.New("directory unavailable")}
	if _, err := source.ResolvePersonaChatInvokerAuthority(ctx, req); err == nil {
		t.Fatal("missing directory was allowed")
	}
}
