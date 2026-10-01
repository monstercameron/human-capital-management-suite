package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_008_PrivateChatReplyAdmissionSealsExactDurableBinding(t *testing.T) {
	record, run, projection := personaPrivateChatBindingFixture(t)
	binding, err := privateChatReplyBinding(record, run, projection)
	if err != nil {
		t.Fatal(err)
	}
	if binding.TenantID != "tenant-a" || binding.ConversationID != "conversation-a" || binding.ThreadID != "thread-a" || binding.InvokingPostID != "post-a" ||
		binding.InvokerID != "alice" || binding.InstallationID != "install-a" || binding.RunID != record.ID || binding.SkillGrantID != "skill-grant-a" || binding.ConversationRevision != 7 || binding.MembershipRevision != 11 ||
		binding.InvokingPostDigest != projection.Evidence.PostDigest || binding.CurrentEvidenceDigest == "" {
		t.Fatalf("binding omitted a durable identity or current revision: %+v", binding)
	}
	call, err := privateChatReplyToolCall(binding, privateChatGatewayIdentity(record, run), privateChatGatewayDelegation(record, run), record)
	if err != nil {
		t.Fatal(err)
	}
	if call.Args["installation_id"] != binding.InstallationID {
		t.Fatalf("gateway args installation = %v, want %q", call.Args["installation_id"], binding.InstallationID)
	}
	gateway, err := agentsecurity.NewToolGateway([]agentsecurity.ToolDescriptor{PersonaChatReplyToolDescriptor()})
	if err != nil {
		t.Fatal(err)
	}
	admission, err := gateway.Admit(call)
	if err != nil {
		t.Fatalf("gateway admission: %v", err)
	}
	if admission.Tenant != binding.TenantID || admission.Nonce != binding.RunID || admission.ArgsDigest != call.ArgsDigest || admission.Capability != agentgate.PrivateChatReplyCapability || admission.Tool != "persona.chat_reply" {
		t.Fatalf("gateway admission lost private chat scope binding: %+v", admission)
	}
	call.Args["conversation_revision"] = float64(8)
	mutatedDigest, err := agentsecurity.DigestArguments(call.Args)
	if err != nil {
		t.Fatal(err)
	}
	if mutatedDigest == admission.ArgsDigest {
		t.Fatal("changing the current conversation revision did not change the gateway-sealed args digest")
	}
}

func TestTodo_AGENTP_008_PrivateChatReplyAdmissionRejectsTupleAndScopeDrift(t *testing.T) {
	tests := []struct {
		name   string
		change func(*agentrun.Record, *runstate.Run, *agentgate.PrivateChatSkillAuthorization, *agentsecurity.AgentIdentity, *[]agentsecurity.DelegationLink)
	}{
		{name: "run request digest changed", change: func(_ *agentrun.Record, run *runstate.Run, _ *agentgate.PrivateChatSkillAuthorization, _ *agentsecurity.AgentIdentity, _ *[]agentsecurity.DelegationLink) {
			run.RequestDigest = "sha256:changed"
		}},
		{name: "installation changed after durable admission", change: func(record *agentrun.Record, _ *runstate.Run, _ *agentgate.PrivateChatSkillAuthorization, _ *agentsecurity.AgentIdentity, _ *[]agentsecurity.DelegationLink) {
			record.Request.InstallationID = "install-b"
		}},
		{name: "different skill identity", change: func(_ *agentrun.Record, _ *runstate.Run, projection *agentgate.PrivateChatSkillAuthorization, _ *agentsecurity.AgentIdentity, _ *[]agentsecurity.DelegationLink) {
			projection.Skill.Definition.ID = "other.reply"
		}},
		{name: "reply skill exceeds T0", change: func(_ *agentrun.Record, _ *runstate.Run, projection *agentgate.PrivateChatSkillAuthorization, _ *agentsecurity.AgentIdentity, _ *[]agentsecurity.DelegationLink) {
			projection.Skill.Definition.SideEffectTier = agentskills.TierT1
		}},
		{name: "foreign conversation evidence", change: func(_ *agentrun.Record, _ *runstate.Run, p *agentgate.PrivateChatSkillAuthorization, _ *agentsecurity.AgentIdentity, _ *[]agentsecurity.DelegationLink) {
			p.Evidence.ConversationID = "conversation-b"
		}},
		{name: "missing current revision", change: func(_ *agentrun.Record, _ *runstate.Run, p *agentgate.PrivateChatSkillAuthorization, _ *agentsecurity.AgentIdentity, _ *[]agentsecurity.DelegationLink) {
			p.Evidence.MembershipRev = 0
		}},
		{name: "changed invoker", change: func(_ *agentrun.Record, _ *runstate.Run, _ *agentgate.PrivateChatSkillAuthorization, _ *agentsecurity.AgentIdentity, links *[]agentsecurity.DelegationLink) {
			(*links)[0].Delegator = "mallory"
		}},
		{name: "widened tool scope", change: func(_ *agentrun.Record, _ *runstate.Run, _ *agentgate.PrivateChatSkillAuthorization, identity *agentsecurity.AgentIdentity, _ *[]agentsecurity.DelegationLink) {
			identity.ToolSet = append(identity.ToolSet, "people.lookup")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record, run, projection := personaPrivateChatBindingFixture(t)
			identity, delegation := privateChatGatewayIdentity(record, run), privateChatGatewayDelegation(record, run)
			tc.change(&record, &run, &projection, &identity, &delegation)
			binding, err := privateChatReplyBinding(record, run, projection)
			if err == nil {
				_, err = privateChatReplyToolCall(binding, identity, delegation, record)
			}
			if !errors.Is(err, errPersonaPrivateChatReplyBinding) {
				t.Fatalf("drift error=%v, want fail-closed private reply binding", err)
			}
		})
	}
}

func TestTodo_AGENTP_008_PrivateChatReplyAuthoritySourceBindsGatewayAdmission(t *testing.T) {
	record, run, projection := personaPrivateChatBindingFixture(t)
	descriptor := PersonaChatReplyToolDescriptor()
	gateway, err := agentsecurity.NewToolGateway([]agentsecurity.ToolDescriptor{descriptor})
	if err != nil {
		t.Fatal(err)
	}
	base := PersonaRunChatReplyAuthority{
		Schema:  PersonaRunOutputSchemaRef{ID: PersonaChatReplySchema, Version: 1, Digest: PersonaChatReplySchemaDigest, PersonaDigest: record.Request.Persona.Digest},
		Gateway: gateway,
	}
	projections := privateChatProjectionResolverFake{projection: projection}
	identities := privateChatGatewayIdentityFake{identity: privateChatGatewayIdentity(record, run), delegation: privateChatGatewayDelegation(record, run)}
	source := PersonaPrivateChatReplyAuthoritySource{Base: privateChatAuthoritySourceFake{authority: base}, Projection: &projections, Identity: &identities}
	got, err := source.ResolvePersonaRunChatReplyAuthority(t.Context(), record, run)
	if err != nil {
		t.Fatalf("resolve output authority: %v", err)
	}
	wantBinding, err := privateChatReplyBinding(record, run, projection)
	if err != nil {
		t.Fatal(err)
	}
	wantCall, err := privateChatReplyToolCall(wantBinding, identities.identity, identities.delegation, record)
	if err != nil {
		t.Fatal(err)
	}
	if got.Admission.ArgsDigest != wantCall.ArgsDigest || got.Admission.Nonce != run.ID || got.Admission.Tenant != record.Request.Source.TenantID || projections.calls != 1 || identities.calls != 1 {
		t.Fatalf("output authority admission=%+v want args=%s projection calls=%d identity calls=%d", got.Admission, wantCall.ArgsDigest, projections.calls, identities.calls)
	}
	firstDigest := got.Admission.ArgsDigest
	projection.Evidence.MembershipRev++
	projections.projection = projection
	changed, err := source.ResolvePersonaRunChatReplyAuthority(t.Context(), record, run)
	if err != nil {
		t.Fatalf("freshly reauthorized membership revision: %v", err)
	}
	if changed.Admission.ArgsDigest == firstDigest {
		t.Fatal("changed current membership revision reused the previous gateway admission digest")
	}
}

type privateChatProjectionResolverFake struct {
	projection agentgate.PrivateChatSkillAuthorization
	calls      int
	err        error
}

func (f *privateChatProjectionResolverFake) ResolvePrivateChatSkillAuthorization(context.Context, agentrun.Record, runstate.Run) (agentgate.PrivateChatSkillAuthorization, error) {
	f.calls++
	return f.projection, f.err
}

type privateChatGatewayIdentityFake struct {
	identity   agentsecurity.AgentIdentity
	delegation []agentsecurity.DelegationLink
	calls      int
	err        error
}

func (f *privateChatGatewayIdentityFake) ResolvePrivateChatGatewayIdentity(context.Context, agentrun.Record, runstate.Run) (agentsecurity.AgentIdentity, []agentsecurity.DelegationLink, error) {
	f.calls++
	return f.identity, f.delegation, f.err
}

type privateChatAuthoritySourceFake struct{ authority PersonaRunChatReplyAuthority }

func (f privateChatAuthoritySourceFake) ResolvePersonaRunChatReplyAuthority(context.Context, agentrun.Record, runstate.Run) (PersonaRunChatReplyAuthority, error) {
	return f.authority, nil
}

func personaPrivateChatBindingFixture(t *testing.T) (agentrun.Record, runstate.Run, agentgate.PrivateChatSkillAuthorization) {
	t.Helper()
	at := time.Date(2026, 9, 30, 13, 0, 0, 0, time.UTC)
	request := agentrun.Request{
		Source:  agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourcePersonaMention, Key: "invocation-a", Ref: "post-a"},
		Persona: &agentrun.PersonaRef{ID: "persona-a", Version: "v2", Digest: "sha256:" + "a" + repeatPersonaBinding("a", 63)},
		Agent:   agentrun.VersionRef{AgentID: "agent-a", Version: "v3", Digest: "sha256:" + repeatPersonaBinding("b", 64)}, InstallationID: "install-a",
		Principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, InvokerID: "alice", DelegatedCredentialRef: "runtime-grant-a"}, Purpose: "persona-chat",
		Audience: agentrun.AudienceScope{ID: "conversation-a"}, Context: agentrun.ContextScope{ID: "thread-a"},
	}
	id, err := agentrun.AdmissionRequestID(request.Source)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := agentrun.AdmissionRequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	record := agentrun.Record{ID: id, RequestDigest: digest, Decision: agentrun.DecisionAccepted, Request: request}
	run := runstate.Run{ID: record.ID, AdmissionID: record.ID, RequestDigest: record.RequestDigest, TenantID: "tenant-a", ActorID: "alice", AgentID: "agent-a", AgentVersion: "v3", AgentDigest: record.Request.Agent.Digest}
	key := agentskills.SkillKey{ID: "persona.chat_reply", Version: 1}
	projection := agentgate.PrivateChatSkillAuthorization{
		Skill:      agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: key.ID, Version: key.Version, SideEffectTier: agentskills.TierT0}, Digest: "sha256:" + repeatPersonaBinding("c", 64), Status: agentskills.StatusActive},
		Grant:      agentgate.SkillGrant{ID: "skill-grant-a", Tenant: values.TenantId("tenant-a"), Skill: key, Purposes: []string{"persona-chat"}},
		Capability: capability.Key{ID: agentgate.PrivateChatReplyCapability, Version: 1}, Scope: agentgate.PrivateChatReplyScope, EvaluatedAt: at, Purpose: "persona-chat",
		Evidence: agentgate.PrivateChatScopeEvidence{Allowed: true, Tenant: values.TenantId("tenant-a"), InvokerID: "alice", ConversationID: "conversation-a", ThreadID: "thread-a", InvokingPostID: "post-a", InvokingPostAuthor: "alice", PrivateConversation: true, ActiveMember: true, PostVisible: true, ConversationRev: 7, MembershipRev: 11, PostDigest: "sha256:" + repeatPersonaBinding("d", 64), EvaluatedAt: at},
	}
	return record, run, projection
}

func privateChatGatewayIdentity(record agentrun.Record, _ runstate.Run) agentsecurity.AgentIdentity {
	return agentsecurity.AgentIdentity{Identity: "workload:agent-a", AgentID: record.Request.Agent.AgentID, Tenant: record.Request.Source.TenantID, Purpose: record.Request.Purpose, ToolSet: []string{"persona.chat_reply"}, DataScope: []string{"chat.current"}, Budget: 1}
}

func privateChatGatewayDelegation(record agentrun.Record, _ runstate.Run) []agentsecurity.DelegationLink {
	return []agentsecurity.DelegationLink{{GrantID: record.Request.Principal.DelegatedCredentialRef, Delegator: record.Request.Principal.InvokerID, Delegate: record.Request.Agent.AgentID, Tenant: record.Request.Source.TenantID, Purpose: record.Request.Purpose, ToolSet: []string{"persona.chat_reply"}, DataScope: []string{"chat.current"}, Budget: 1}}
}

func repeatPersonaBinding(char string, count int) string {
	return strings.Repeat(char, count)
}
