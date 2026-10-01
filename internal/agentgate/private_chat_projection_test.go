package agentgate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const privateChatPurpose = "persona-mention"

type privateChatPolicyFake struct {
	evidence PrivateChatScopeEvidence
	err      error
	req      PrivateChatScopeRequest
	calls    int
}

func (f *privateChatPolicyFake) AuthorizePrivateChat(_ context.Context, req PrivateChatScopeRequest) (PrivateChatScopeEvidence, error) {
	f.calls++
	f.req = req
	return f.evidence, f.err
}

func privateChatFixture(t *testing.T) (*Gate, PrivateChatScopeRequest, *privateChatPolicyFake, *mutableGrants) {
	t.Helper()
	at := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)
	tenant := values.TenantId("tenant-chat")
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: tenant, Subject: "alice", SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org-a",
		Roles: []string{"member"}, Purposes: []string{privateChatPurpose}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: "session-chat", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "credential-chat",
	})
	if err != nil {
		t.Fatal(err)
	}
	capDef := capability.Definition{
		ID: PrivateChatReplyCapability, Version: 1, OwnerDomain: "chat",
		RequestSchema:  capability.SchemaRef{SchemaID: "persona.reply.request", Version: 1, ProtobufFullName: "test.PersonaReplyRequest"},
		ResponseSchema: capability.SchemaRef{SchemaID: "persona.reply.response", Version: 1, ProtobufFullName: "test.PersonaReplyResponse"},
		ErrorSchema:    capability.SchemaRef{SchemaID: "persona.reply.error", Version: 1, ProtobufFullName: "test.PersonaReplyError"},
		EffectClass:    capability.EffectPure, RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AgentEligible: true,
		AuthZScopeRef: PrivateChatReplyScope, LegalBasisRef: "legal.private-chat.reply.v1", EntitlementRef: "entitlement.chat.v1",
		SLOClassRef: "slo.interactive.v1", TestRef: "test:private-chat-reply",
	}
	caps := capability.NewRegistry()
	if err := caps.Register(capDef, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	skills := agentskills.NewRegistry(caps)
	skillDef := agentskills.SkillDefinition{
		ID: "persona.chat_reply", Version: 1, Owner: "chat", Description: "Prepare a private grounded persona response.",
		InputSchema: []byte(`{"type":"object"}`), OutputSchema: []byte(`{"type":"object"}`),
		Operations:     []agentskills.OperationRef{{Kind: agentskills.OperationCapability, Capability: capDef.Key()}},
		SideEffectTier: agentskills.TierT0, RequiredPurposes: []string{privateChatPurpose},
		IdempotencyRule: "read-only", CostClass: "LOW",
	}
	if err := skills.Publish(skillDef); err != nil {
		t.Fatal(err)
	}
	key := skillDef.Key()
	grants := &mutableGrants{grants: []SkillGrant{{
		ID: "grant-persona-reply", Tenant: tenant, Skill: key, Roles: []string{"member"}, Population: "members",
		OrganizationScopes: []string{"org-a"}, Purposes: []string{privateChatPurpose},
	}}}
	gate, err := New(Config{Skills: skills, Grants: grants, Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	request := PrivateChatScopeRequest{
		User:  UserContext{Principal: principal, Population: "members", Roles: []string{"member"}, OrganizationScopes: []string{"org-a"}},
		Skill: agentskills.SkillPin{ID: key.ID, Version: key.Version}, Purpose: privateChatPurpose,
		Tenant: tenant, ConversationID: "conv-7", ThreadID: "thread-2", InvokingPostID: "post-9", At: at,
	}
	pin, err := skills.Pin(key)
	if err != nil {
		t.Fatal(err)
	}
	request.Skill = pin
	policy := &privateChatPolicyFake{evidence: PrivateChatScopeEvidence{
		Allowed: true, Tenant: tenant, InvokerID: "alice", ConversationID: "conv-7", ThreadID: "thread-2", InvokingPostID: "post-9",
		InvokingPostAuthor: "alice", PrivateConversation: true, ActiveMember: true, PostVisible: true,
		ConversationRev: 4, MembershipRev: 8, PostDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", EvaluatedAt: at,
	}}
	return gate, request, policy, grants
}

func TestPrivateChatProjectionRequiresCurrentGrantAndExactChatPolicy(t *testing.T) {
	gate, req, policy, grants := privateChatFixture(t)
	fieldPDP := &recordingPDP{}
	gate.pdp = fieldPDP
	projection, err := gate.ProjectPrivateChatSkillAuthorization(context.Background(), req, policy)
	if err != nil {
		t.Fatalf("project private chat skill: %v", err)
	}
	if projection.Grant.ID != "grant-persona-reply" || projection.Capability != (capability.Key{ID: "persona.reply", Version: 1}) || projection.Scope != "chat.current" {
		t.Fatalf("projection authority = %#v", projection)
	}
	if projection.Evidence.ConversationID != req.ConversationID || projection.Evidence.ThreadID != req.ThreadID || projection.Evidence.InvokingPostID != req.InvokingPostID || projection.Evidence.InvokingPostAuthor != "alice" {
		t.Fatalf("projection is not bound to exact private post: %#v", projection.Evidence)
	}
	if policy.calls != 1 || policy.req.Skill != req.Skill || !policy.req.At.Equal(req.At) {
		t.Fatalf("policy request = %#v calls=%d", policy.req, policy.calls)
	}
	if len(fieldPDP.calls) != 0 {
		t.Fatalf("private chat projection invoked record/field PDP with substituted workforce fields: %#v", fieldPDP.calls)
	}
	grants.grants[0].Roles = []string{"other-role"}
	if _, err := gate.ProjectPrivateChatSkillAuthorization(context.Background(), req, policy); deniedCode(t, err) != DenyRole {
		t.Fatalf("stale current skill grant was not rechecked: %v", err)
	}
}

func TestTodo_AGENTP_021_PrivateDocumentSearchProjection(t *testing.T) {
	gate, req, policy, grants := privateChatFixture(t)
	caps := capability.NewRegistry()
	def := capability.Definition{
		ID: "hcmnext.agent.document_search", Version: 1, OwnerDomain: "documents",
		RequestSchema:  capability.SchemaRef{SchemaID: "search.request", Version: 1, ProtobufFullName: "test.SearchRequest"},
		ResponseSchema: capability.SchemaRef{SchemaID: "search.response", Version: 1, ProtobufFullName: "test.SearchResponse"},
		ErrorSchema:    capability.SchemaRef{SchemaID: "search.error", Version: 1, ProtobufFullName: "test.SearchError"},
		EffectClass:    capability.EffectReadOnly, ReadData: capability.DataDomainFieldSet{DataDomains: []string{"policy_document"}},
		RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AgentEligible: true,
		AuthZScopeRef: "documents:search", LegalBasisRef: "legal.search.v1", EntitlementRef: "entitlement.search.v1",
		SLOClassRef: "slo.interactive.v1", TestRef: "test:private-document-search",
	}
	if err := caps.Register(def, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	skills := agentskills.NewRegistry(caps)
	skill := agentskills.SkillDefinition{ID: "hcmnext.skill.knowledge_search_with_citations", Version: 1, Owner: "documents", Description: "Search current policy documents.",
		InputSchema: []byte(`{"type":"object"}`), OutputSchema: []byte(`{"type":"object"}`),
		Operations:     []agentskills.OperationRef{{Kind: agentskills.OperationCapability, Capability: def.Key()}},
		SideEffectTier: agentskills.TierT0, RequiredPurposes: []string{privateChatPurpose}, IdempotencyRule: "read-only", CostClass: "LOW"}
	if err := skills.Publish(skill); err != nil {
		t.Fatal(err)
	}
	pin, err := skills.Pin(skill.Key())
	if err != nil {
		t.Fatal(err)
	}
	gate.skills, req.Skill = skills, pin
	grants.grants[0].Skill = skill.Key()
	pdp := &recordingPDP{}
	gate.pdp = pdp
	projection, err := gate.ProjectPrivateChatDocumentSearchAuthorization(context.Background(), req, policy)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Scope != "documents:search" || projection.Capability != def.Key() || projection.Evidence.InvokingPostID != req.InvokingPostID || len(pdp.calls) != 0 {
		t.Fatalf("search authority lost its domain or current chat binding: %#v, PDP calls=%d", projection, len(pdp.calls))
	}
	policy.evidence.PrivateConversation, policy.evidence.PublicConversation, policy.evidence.PublicPolicyRev = false, true, 1
	public, err := gate.ProjectPersonaChatSkillAuthorization(context.Background(), req, personaChatPolicyFake{policy})
	if err != nil || public.Scope != "documents:search" || public.Capability != def.Key() || !public.Evidence.PublicConversation || len(pdp.calls) != 0 {
		t.Fatalf("public search lost its domain or invoking post: %#v err=%v", public, err)
	}
	policy.evidence.PrivateConversation, policy.evidence.PublicConversation, policy.evidence.PublicPolicyRev = true, false, 0
	policy.evidence.PostVisible = false
	if _, err := gate.ProjectPrivateChatDocumentSearchAuthorization(context.Background(), req, policy); deniedCode(t, err) != DenySubject {
		t.Fatalf("invisible post: %v", err)
	}
	policy.evidence.PostVisible = true
	grants.grants[0].Roles = []string{"another-role"}
	if _, err := gate.ProjectPrivateChatDocumentSearchAuthorization(context.Background(), req, policy); deniedCode(t, err) != DenyRole {
		t.Fatalf("revoked grant: %v", err)
	}
	if _, err := gate.ProjectPrivateChatSkillAuthorization(context.Background(), req, policy); deniedCode(t, err) != DenyCapability {
		t.Fatalf("search was admitted as a reply: %v", err)
	}
	changed := agentskills.SkillRecord{ResolvedOperations: []agentskills.ResolvedOperation{{HasCapability: true, Capability: capability.Record{Definition: def, Status: capability.StatusActive}}}}
	changed.ResolvedOperations[0].Capability.Definition.ReadData.DataDomains = []string{"worker"}
	if _, ok := privatePolicySearchCapability(changed); ok {
		t.Fatal("workforce search was admitted as policy search")
	}
}

func TestPrivateChatProjectionRejectsMismatchedOrStaleEvidence(t *testing.T) {
	tests := []struct {
		name   string
		change func(*PrivateChatScopeEvidence)
	}{
		{name: "wrong conversation", change: func(e *PrivateChatScopeEvidence) { e.ConversationID = "conv-other" }},
		{name: "wrong thread", change: func(e *PrivateChatScopeEvidence) { e.ThreadID = "thread-other" }},
		{name: "wrong post", change: func(e *PrivateChatScopeEvidence) { e.InvokingPostID = "post-other" }},
		{name: "post not authored by invoker", change: func(e *PrivateChatScopeEvidence) { e.InvokingPostAuthor = "mallory" }},
		{name: "not private", change: func(e *PrivateChatScopeEvidence) { e.PrivateConversation = false }},
		{name: "membership revoked", change: func(e *PrivateChatScopeEvidence) { e.ActiveMember = false }},
		{name: "post no longer visible", change: func(e *PrivateChatScopeEvidence) { e.PostVisible = false }},
		{name: "stale conversation revision", change: func(e *PrivateChatScopeEvidence) { e.ConversationRev = 0 }},
		{name: "stale membership revision", change: func(e *PrivateChatScopeEvidence) { e.MembershipRev = 0 }},
		{name: "missing post digest", change: func(e *PrivateChatScopeEvidence) { e.PostDigest = "" }},
		{name: "stale evaluation time", change: func(e *PrivateChatScopeEvidence) { e.EvaluatedAt = e.EvaluatedAt.Add(-time.Second) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gate, req, policy, _ := privateChatFixture(t)
			tc.change(&policy.evidence)
			if _, err := gate.ProjectPrivateChatSkillAuthorization(context.Background(), req, policy); deniedCode(t, err) != DenySubject {
				t.Fatalf("mismatched evidence error = %v", err)
			}
		})
	}
}

func TestPrivateChatProjectionRejectsWrongScopeAndMissingAuthorizer(t *testing.T) {
	gate, req, policy, _ := privateChatFixture(t)
	record, err := gate.skills.ResolvePin(req.Skill)
	if err != nil {
		t.Fatal(err)
	}
	record.ResolvedOperations[0].Capability.Definition.AuthZScopeRef = "scope:people.read"
	wrongScope := &privateChatCatalog{record: record}
	gate.skills = wrongScope
	if _, err := gate.ProjectPrivateChatSkillAuthorization(context.Background(), req, policy); deniedCode(t, err) != DenyCapability {
		t.Fatalf("wrong scope error = %v", err)
	}
	gate, req, _, _ = privateChatFixture(t)
	if _, err := gate.ProjectPrivateChatSkillAuthorization(context.Background(), req, nil); deniedCode(t, err) != DenyInvalid {
		t.Fatalf("missing policy owner error = %v", err)
	}
}

func TestPrivateChatProjectionCollapsesPolicyErrors(t *testing.T) {
	gate, req, policy, _ := privateChatFixture(t)
	policy.err = errors.New("private conversation details must not escape")
	_, err := gate.ProjectPrivateChatSkillAuthorization(context.Background(), req, policy)
	if deniedCode(t, err) != DenySubject || errors.Is(err, policy.err) {
		t.Fatalf("policy error was not safely collapsed: %v", err)
	}
}

type privateChatCatalog struct{ record agentskills.SkillRecord }

func (c *privateChatCatalog) List() []agentskills.SkillRecord {
	return []agentskills.SkillRecord{c.record}
}

func (c *privateChatCatalog) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	if pin.ID != c.record.Definition.ID || pin.Version != c.record.Definition.Version || pin.Digest != c.record.Digest {
		return agentskills.SkillRecord{}, agentskills.ErrDigestMismatch
	}
	return c.record, nil
}
