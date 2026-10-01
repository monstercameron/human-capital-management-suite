package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

var errPersonaPrivateChatReplyBinding = errors.New("application: private persona reply binding unavailable")

// PersonaPrivateChatSkillAuthorizationResolver reprojects current private-chat
// and skill authority for the exact durable admission and run. Implementations
// must call Gate.ProjectPrivateChatSkillAuthorization on current owner facts.
type PersonaPrivateChatSkillAuthorizationResolver interface {
	ResolvePrivateChatSkillAuthorization(context.Context, agentrun.Record, runstate.Run) (agentgate.PrivateChatSkillAuthorization, error)
}

// PersonaPrivateChatScopeRequestBuilder derives the Gate request from the
// persisted run and current server-owned persona pin/invoker facts. It must
// return the pinned T0 reply skill and verified human principal, never
// request-body fields.
type PersonaPrivateChatScopeRequestBuilder interface {
	BuildPrivateChatScopeRequest(context.Context, agentrun.Record, runstate.Run) (agentgate.PrivateChatScopeRequest, error)
}

// PersonaPrivateChatSkillAuthorizationAdapter bridges a durable run tuple
// into the current skill-grant and chat-policy projection.
type PersonaPrivateChatSkillAuthorizationAdapter struct {
	Gate    *agentgate.Gate
	Builder PersonaPrivateChatScopeRequestBuilder
	Chat    agentgate.PrivateChatScopeAuthorizer
}

var _ PersonaPrivateChatSkillAuthorizationResolver = (*PersonaPrivateChatSkillAuthorizationAdapter)(nil)

// ResolvePrivateChatSkillAuthorization builds an exact request, verifies it
// names the accepted run's source identities, and asks Gate to recheck the
// current skill grant and private chat policy.
func (a *PersonaPrivateChatSkillAuthorizationAdapter) ResolvePrivateChatSkillAuthorization(ctx context.Context, record agentrun.Record, run runstate.Run) (agentgate.PrivateChatSkillAuthorization, error) {
	if a == nil || a.Gate == nil || isNilPersonaOutputPort(a.Builder) || isNilPersonaOutputPort(a.Chat) || ctx == nil || !personaPrivateChatRunTupleMatches(record, run) {
		return agentgate.PrivateChatSkillAuthorization{}, errPersonaPrivateChatReplyBinding
	}
	req, err := a.Builder.BuildPrivateChatScopeRequest(ctx, record, run)
	if err != nil {
		return agentgate.PrivateChatSkillAuthorization{}, fmt.Errorf("build private chat scope request: %w", err)
	}
	r := record.Request
	if req.Tenant.String() != r.Source.TenantID || req.Purpose != r.Purpose || req.ConversationID != r.Audience.ID || req.ThreadID != r.Context.ID || req.InvokingPostID != r.Source.Ref ||
		req.User.Principal == nil || req.User.Principal.Subject() != r.Principal.InvokerID || req.User.Principal.Tenant().String() != r.Source.TenantID {
		return agentgate.PrivateChatSkillAuthorization{}, errPersonaPrivateChatReplyBinding
	}
	projection, err := a.Gate.ProjectPrivateChatSkillAuthorization(ctx, req, a.Chat)
	if err != nil {
		return agentgate.PrivateChatSkillAuthorization{}, fmt.Errorf("project current private chat skill authority: %w", err)
	}
	if _, err := privateChatReplyBinding(record, run, projection); err != nil {
		return agentgate.PrivateChatSkillAuthorization{}, err
	}
	return projection, nil
}

// PersonaPrivateChatGatewayIdentityResolver supplies the verified workload
// identity and durable delegation chain for one admitted run. Implementations
// must load the tenant-scoped grant named by the durable request, verify its
// invoker, installation, run, target agent, purpose, revocation and validity,
// and derive persona.chat_reply with chat.current only from its exact
// persona.chat_reply skill scope. They must not derive authority from request
// text. The binding adapter narrows these values to that same tool and scope.
type PersonaPrivateChatGatewayIdentityResolver interface {
	ResolvePrivateChatGatewayIdentity(context.Context, agentrun.Record, runstate.Run) (agentsecurity.AgentIdentity, []agentsecurity.DelegationLink, error)
}

// PersonaPrivateChatReplyAuthoritySource decorates the ordinary output
// authority source with a gateway admission sealed to current private-chat
// policy. It binds tenant, conversation, thread, invoking post, invoker, run,
// exact skill grant and pin, revisions, and post digest into gateway args.
type PersonaPrivateChatReplyAuthoritySource struct {
	Base       PersonaRunChatReplyAuthoritySource
	Projection PersonaPrivateChatSkillAuthorizationResolver
	Identity   PersonaPrivateChatGatewayIdentityResolver
}

var _ PersonaRunChatReplyAuthoritySource = (*PersonaPrivateChatReplyAuthoritySource)(nil)

// ResolvePersonaRunChatReplyAuthority rechecks current projection evidence,
// then issues a gateway admission whose argument digest commits the full
// durable private-chat binding. Missing or changed evidence denies output.
func (s *PersonaPrivateChatReplyAuthoritySource) ResolvePersonaRunChatReplyAuthority(ctx context.Context, record agentrun.Record, run runstate.Run) (PersonaRunChatReplyAuthority, error) {
	if s == nil || ctx == nil || isNilPersonaOutputPort(s.Base) || isNilPersonaOutputPort(s.Projection) || isNilPersonaOutputPort(s.Identity) {
		return PersonaRunChatReplyAuthority{}, errPersonaPrivateChatReplyBinding
	}
	base, err := s.Base.ResolvePersonaRunChatReplyAuthority(ctx, record, run)
	if err != nil {
		return PersonaRunChatReplyAuthority{}, err
	}
	projection, err := s.Projection.ResolvePrivateChatSkillAuthorization(ctx, record, run)
	if err != nil {
		return PersonaRunChatReplyAuthority{}, fmt.Errorf("recheck private persona skill authorization: %w", err)
	}
	binding, err := privateChatReplyBinding(record, run, projection)
	if err != nil {
		return PersonaRunChatReplyAuthority{}, err
	}
	identity, delegation, err := s.Identity.ResolvePrivateChatGatewayIdentity(ctx, record, run)
	if err != nil {
		return PersonaRunChatReplyAuthority{}, fmt.Errorf("resolve private persona gateway identity: %w", err)
	}
	call, err := privateChatReplyToolCall(binding, identity, delegation, record)
	if err != nil {
		return PersonaRunChatReplyAuthority{}, err
	}
	if base.Gateway == nil {
		return PersonaRunChatReplyAuthority{}, errPersonaPrivateChatReplyBinding
	}
	admission, err := base.Gateway.Admit(call)
	if err != nil {
		return PersonaRunChatReplyAuthority{}, fmt.Errorf("admit exact private persona reply: %w", err)
	}
	base.Admission = admission
	return base, nil
}

// PersonaPrivateChatReplyBinding is the canonical scope sealed into the
// gateway argument digest and later recomputed from durable run state.
type PersonaPrivateChatReplyBinding struct {
	TenantID              string `json:"tenant_id"`
	ConversationID        string `json:"conversation_id"`
	ThreadID              string `json:"thread_id"`
	InvokingPostID        string `json:"invoking_post_id"`
	InvokerID             string `json:"invoker_id"`
	InstallationID        string `json:"installation_id"`
	RunID                 string `json:"run_id"`
	SkillGrantID          string `json:"skill_grant_id"`
	SkillID               string `json:"skill_id"`
	SkillVersion          uint32 `json:"skill_version"`
	SkillDigest           string `json:"skill_digest"`
	PersonaDigest         string `json:"persona_digest"`
	ConversationRevision  uint64 `json:"conversation_revision"`
	MembershipRevision    uint64 `json:"membership_revision"`
	InvokingPostDigest    string `json:"invoking_post_digest"`
	CurrentEvidenceDigest string `json:"current_evidence_digest"`
}

func privateChatReplyBinding(record agentrun.Record, run runstate.Run, p agentgate.PrivateChatSkillAuthorization) (PersonaPrivateChatReplyBinding, error) {
	r := record.Request
	e := p.Evidence
	requestID, idErr := agentrun.AdmissionRequestID(r.Source)
	requestDigest, digestErr := agentrun.AdmissionRequestDigest(r)
	if !personaPrivateChatRunTupleMatches(record, run) ||
		idErr != nil || digestErr != nil || requestID != record.ID || requestDigest != record.RequestDigest ||
		r.Source.Kind != agentrun.SourcePersonaMention || r.Source.TenantID == "" || r.Source.Key == "" || r.Source.Ref == "" ||
		r.InstallationID == "" || r.Principal.Mode != agentrun.ModeOnBehalfOf || r.Principal.InvokerID == "" ||
		r.Audience.ID == "" || r.Context.ID == "" || r.Purpose == "" || r.Persona == nil || run.AgentID == "" || p.Scope != agentgate.PrivateChatReplyScope ||
		p.Capability.ID != agentgate.PrivateChatReplyCapability || p.Capability.Version != 1 || p.Purpose != r.Purpose || p.Grant.ID == "" ||
		!validPersonaPrivateChatDigest(p.Skill.Digest) || !validPersonaPrivateChatDigest(r.Persona.Digest) || p.Skill.Status != "ACTIVE" ||
		p.Skill.Definition.ID != "persona.chat_reply" || p.Skill.Definition.SideEffectTier != agentskills.TierT0 || p.Skill.Definition.Version == 0 ||
		p.Grant.Tenant != e.Tenant || p.Grant.Skill != p.Skill.Definition.Key() || !slices.Contains(p.Grant.Purposes, r.Purpose) || !e.EvaluatedAt.Equal(p.EvaluatedAt) {
		return PersonaPrivateChatReplyBinding{}, errPersonaPrivateChatReplyBinding
	}
	if !e.Allowed || e.Tenant.String() != r.Source.TenantID || e.InvokerID != r.Principal.InvokerID || e.ConversationID != r.Audience.ID || e.ThreadID != r.Context.ID ||
		e.InvokingPostID != r.Source.Ref || e.InvokingPostAuthor != r.Principal.InvokerID || !e.PrivateConversation || !e.ActiveMember || !e.PostVisible ||
		e.ConversationRev == 0 || e.MembershipRev == 0 || !validPersonaPrivateChatDigest(e.PostDigest) || e.EvaluatedAt.IsZero() {
		return PersonaPrivateChatReplyBinding{}, errPersonaPrivateChatReplyBinding
	}
	material := struct {
		Tenant               string `json:"tenant"`
		Invoker              string `json:"invoker"`
		Conversation         string `json:"conversation"`
		Thread               string `json:"thread"`
		Post                 string `json:"post"`
		Installation         string `json:"installation"`
		ConversationRevision uint64 `json:"conversation_revision"`
		MembershipRevision   uint64 `json:"membership_revision"`
		PostDigest           string `json:"post_digest"`
		PersonaDigest        string `json:"persona_digest"`
		EvaluatedAt          string `json:"evaluated_at"`
	}{r.Source.TenantID, r.Principal.InvokerID, r.Audience.ID, r.Context.ID, r.Source.Ref, r.InstallationID, e.ConversationRev, e.MembershipRev, e.PostDigest, r.Persona.Digest, e.EvaluatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")}
	encoded, err := json.Marshal(material)
	if err != nil {
		return PersonaPrivateChatReplyBinding{}, errPersonaPrivateChatReplyBinding
	}
	sum := sha256.Sum256(append([]byte("hcm-next-private-chat-evidence/v1\x00"), encoded...))
	return PersonaPrivateChatReplyBinding{
		TenantID: r.Source.TenantID, ConversationID: r.Audience.ID, ThreadID: r.Context.ID, InvokingPostID: r.Source.Ref,
		InvokerID: r.Principal.InvokerID, InstallationID: r.InstallationID, RunID: record.ID, SkillGrantID: p.Grant.ID, SkillID: p.Skill.Definition.ID,
		SkillVersion: p.Skill.Definition.Version, SkillDigest: p.Skill.Digest, PersonaDigest: r.Persona.Digest, ConversationRevision: e.ConversationRev,
		MembershipRevision: e.MembershipRev, InvokingPostDigest: e.PostDigest, CurrentEvidenceDigest: "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}

func privateChatReplyToolCall(binding PersonaPrivateChatReplyBinding, identity agentsecurity.AgentIdentity, delegation []agentsecurity.DelegationLink, record agentrun.Record) (agentsecurity.ToolCall, error) {
	request := record.Request
	if identity.Identity == "" || identity.AgentID != record.Request.Agent.AgentID || identity.Tenant != binding.TenantID || identity.Purpose != request.Purpose ||
		!samePersonaReplyStrings(identity.ToolSet, []string{"persona.chat_reply"}) || !samePersonaReplyStrings(identity.DataScope, []string{"chat.current"}) || identity.Budget < 1 ||
		len(delegation) == 0 || request.Principal.DelegatedCredentialRef == "" {
		return agentsecurity.ToolCall{}, errPersonaPrivateChatReplyBinding
	}
	leaf := delegation[len(delegation)-1]
	if leaf.GrantID != request.Principal.DelegatedCredentialRef || leaf.Delegator != binding.InvokerID || leaf.Delegate != identity.AgentID || leaf.Tenant != binding.TenantID || leaf.Purpose != request.Purpose ||
		!samePersonaReplyStrings(leaf.ToolSet, []string{"persona.chat_reply"}) || !samePersonaReplyStrings(leaf.DataScope, []string{"chat.current"}) || leaf.Budget != 1 {
		return agentsecurity.ToolCall{}, errPersonaPrivateChatReplyBinding
	}
	args := map[string]any{
		"tenant_id": binding.TenantID, "conversation_id": binding.ConversationID, "thread_id": binding.ThreadID,
		"invoking_post_id": binding.InvokingPostID, "invoker_id": binding.InvokerID, "installation_id": binding.InstallationID, "run_id": binding.RunID,
		"skill_grant_id": binding.SkillGrantID, "skill_id": binding.SkillID, "skill_version": binding.SkillVersion,
		"skill_digest": binding.SkillDigest, "persona_digest": binding.PersonaDigest, "conversation_revision": binding.ConversationRevision,
		"membership_revision": binding.MembershipRevision, "invoking_post_digest": binding.InvokingPostDigest,
		"current_evidence_digest": binding.CurrentEvidenceDigest,
	}
	digest, err := agentsecurity.DigestArguments(args)
	if err != nil {
		return agentsecurity.ToolCall{}, errPersonaPrivateChatReplyBinding
	}
	call := agentsecurity.ToolCall{
		Agent: identity, Delegation: append([]agentsecurity.DelegationLink(nil), delegation...), Tenant: binding.TenantID,
		Purpose: request.Purpose, Tool: "persona.chat_reply", Capability: agentgate.PrivateChatReplyCapability, Version: 1,
		Nonce: binding.RunID, Args: args, ArgsDigest: digest, InputTaint: []string{string(agentsecurity.TaintDerived)},
		Provenance: []string{PersonaChatReplyProvenance, agentgate.PrivateChatReplyScope}, CostBudget: 1,
		DataScope: []string{agentgate.PrivateChatReplyScope},
	}
	return call, nil
}

func personaPrivateChatRunTupleMatches(record agentrun.Record, run runstate.Run) bool {
	r := record.Request
	return record.Decision == agentrun.DecisionAccepted && record.ID != "" && record.RequestDigest != "" && run.ID == record.ID && run.AdmissionID == record.ID &&
		run.RequestDigest == record.RequestDigest && run.TenantID == r.Source.TenantID && run.ActorID == r.Principal.InvokerID && run.AgentID == r.Agent.AgentID &&
		run.AgentVersion == r.Agent.Version && run.AgentDigest == r.Agent.Digest && r.Source.TenantID != "" && r.Source.Key != "" &&
		r.Source.Ref != "" && r.Principal.InvokerID != "" && r.Audience.ID != "" && r.Context.ID != "" && r.Persona != nil && r.Persona.Digest != ""
}

func samePersonaReplyStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func validPersonaPrivateChatDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}
