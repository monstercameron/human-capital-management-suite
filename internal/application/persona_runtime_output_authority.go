package application

import (
	"context"
	"fmt"
	"reflect"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// personaRuntimeOutputAuthority reconstructs output authority from the same
// current owners used by admission and model routing. Grounding is read through
// the invoker's authorized thread reader; provider text cannot create evidence.
type personaRuntimeOutputAuthority struct {
	authority agentrun.Authority
	work      *DatabasePersonaRunModelWorkSource
	grants    PersonaGrantTenantStoreFactory
	worker    PersonaPrivateChatWorkloadIdentitySource
	tools     PersonaRuntimeToolGroundingSource
}

// PersonaRuntimeToolGroundingSource reloads journaled tool results through
// their current source owners before turning them into gateway evidence.
type PersonaRuntimeToolGroundingSource interface {
	ReadPersonaRunToolGrounding(context.Context, agentrun.Record, runstate.Run, *agentsecurity.ToolGateway) ([]agentsecurity.Datum, error)
}

func (s *personaRuntimeOutputAuthority) ResolvePersonaRunChatReplyAuthority(ctx context.Context, record agentrun.Record, run runstate.Run) (PersonaRunChatReplyAuthority, error) {
	if s == nil || ctx == nil || s.authority == nil || s.work == nil || s.grants == nil || s.worker == nil || validatePersonaModelWorkBinding(record, run) != nil {
		return PersonaRunChatReplyAuthority{}, ErrPersonaRunOutputValidatorUnavailable
	}
	ctx = WithPersonaBackgroundAdmission(ctx, record)
	current, err := s.authority.VerifyAdmission(ctx, record.Request)
	if err != nil || !reflect.DeepEqual(current, record.Authority) {
		return PersonaRunChatReplyAuthority{}, ErrPersonaRunOutputValidatorUnavailable
	}
	profile, manifest, err := s.work.resolveProfileAndManifest(ctx, record)
	if err != nil {
		return PersonaRunChatReplyAuthority{}, err
	}
	policy, err := s.work.budgets.Resolve(ctx, record.Request.Source.TenantID, record.Request.LegalEntity)
	if err != nil {
		return PersonaRunChatReplyAuthority{}, err
	}
	route, err := s.work.resolveRoute(ctx, record, run, manifest, profile, policy)
	if err != nil {
		return PersonaRunChatReplyAuthority{}, err
	}
	store, err := s.grants.ForTenant(ctx, values.TenantId(record.Request.Source.TenantID))
	if err != nil || isNilPersonaOutputPort(store) {
		return PersonaRunChatReplyAuthority{}, ErrPersonaRunOutputValidatorUnavailable
	}
	grant, err := store.Get(record.Request.Principal.DelegatedCredentialRef)
	now := s.work.now().UTC()
	if err != nil || !privatePersonaReplyGrantMatches(grant, record, now) || store.CurrentRevocationEpoch(grant.Tenant, grant.UserID) != grant.RevocationEpoch {
		return PersonaRunChatReplyAuthority{}, ErrPersonaRunOutputValidatorUnavailable
	}
	worker, err := s.worker.ResolvePersonaChatWorker(ctx)
	if err != nil || !privatePersonaReplyWorkerMatches(worker, now) {
		return PersonaRunChatReplyAuthority{}, ErrPersonaRunOutputValidatorUnavailable
	}
	gateway, err := agentsecurity.NewToolGateway([]agentsecurity.ToolDescriptor{PersonaChatReplyToolDescriptor()})
	if err != nil {
		return PersonaRunChatReplyAuthority{}, err
	}
	identity := agentsecurity.AgentIdentity{Identity: "workload:" + worker.Issuer() + "/" + worker.Subject() + "#" + worker.Fingerprint(), AgentID: record.Request.Agent.AgentID,
		Tenant: grant.Tenant.String(), Purpose: grant.Purpose, ToolSet: []string{"persona.chat_reply"}, DataScope: []string{"chat.current"}, Budget: 1}
	call := agentsecurity.ToolCall{Agent: identity, Delegation: []agentsecurity.DelegationLink{{GrantID: grant.GrantID, Delegator: grant.UserID, Delegate: grant.TargetAgentID,
		Tenant: grant.Tenant.String(), Purpose: grant.Purpose, ToolSet: identity.ToolSet, DataScope: identity.DataScope, Budget: 1}},
		Tenant: grant.Tenant.String(), Purpose: grant.Purpose, Tool: "persona.chat_reply", Capability: "persona.reply", Version: 1,
		Nonce: record.Request.Source.Key, Args: map[string]any{"request_digest": record.RequestDigest, "run_id": run.ID, "policy_digest": current.PolicyDigest},
		InputTaint: []string{string(agentsecurity.TaintDerived)}, Provenance: []string{PersonaChatReplyProvenance, "chat.current"}, CostBudget: 1, DataScope: []string{"chat.current"}}
	call.ArgsDigest, err = agentsecurity.DigestArguments(call.Args)
	if err != nil {
		return PersonaRunChatReplyAuthority{}, err
	}
	admitted, err := gateway.Admit(call)
	if err != nil {
		return PersonaRunChatReplyAuthority{}, err
	}
	posts, err := s.work.threads.ReadThread(ctx, agentinvoke.ThreadReadRequest{TenantID: record.Request.Source.TenantID, ConversationID: record.Request.Audience.ID,
		ThreadID: record.Request.Context.ID, InvokingPostID: record.Request.Source.Ref, InvokerID: record.Request.Principal.InvokerID, Limit: agentinvoke.MaxThreadPosts})
	if err != nil || len(posts) == 0 {
		return PersonaRunChatReplyAuthority{}, ErrPersonaRunOutputValidatorUnavailable
	}
	grounding := make([]agentsecurity.Datum, 0, len(posts))
	for _, post := range posts {
		if post.TenantID != record.Request.Source.TenantID || post.ConversationID != record.Request.Audience.ID || post.ThreadID != record.Request.Context.ID || post.ID == "" {
			return PersonaRunChatReplyAuthority{}, ErrPersonaRunOutputValidatorUnavailable
		}
		datum, err := gateway.Observe(agentsecurity.SourceChat, post.Body, agentsecurity.KindObservation, agentsecurity.Citation{SourceID: "chat:" + post.ID,
			Location: fmt.Sprintf("conversation:%s/%s", post.ConversationID, post.ID), Digest: digestPersonaThreadPost(post)})
		if err != nil {
			return PersonaRunChatReplyAuthority{}, err
		}
		grounding = append(grounding, datum)
	}
	if !isNilPersonaOutputPort(s.tools) {
		toolGrounding, err := s.tools.ReadPersonaRunToolGrounding(ctx, record, run, gateway)
		if err != nil {
			return PersonaRunChatReplyAuthority{}, err
		}
		grounding = append(grounding, toolGrounding...)
	}
	return PersonaRunChatReplyAuthority{Schema: PersonaRunOutputSchemaRef{ID: manifest.OutputSchema.ID, Version: uint32(manifest.OutputSchema.Version), Digest: manifest.OutputSchema.Digest, PersonaDigest: record.Request.Persona.Digest},
		ModelDigest: route.Route.Pin.Primary.ProfileDigest, Gateway: gateway, Admission: admitted, Grounding: grounding}, nil
}

var _ PersonaRunChatReplyAuthoritySource = (*personaRuntimeOutputAuthority)(nil)

// BindPersonaRuntimeTools installs execution and its independently rechecked
// grounding together, so a tool result can never bypass final output evidence.
func BindPersonaRuntimeTools(cfg *PersonaInvocationProductionConfig, tools interface {
	PersonaRunT0ToolExecutionPort
	PersonaRuntimeToolGroundingSource
}) error {
	if cfg == nil || isNilPersonaOutputPort(tools) {
		return ErrPersonaRunExecutorUnavailable
	}
	validator, ok := cfg.Run.Output.(*SealedPersonaRunOutputValidator)
	if !ok || validator == nil {
		return ErrPersonaRunExecutorUnavailable
	}
	authority, ok := validator.authority.(*personaRuntimeOutputAuthority)
	if !ok || authority == nil || !isNilPersonaOutputPort(cfg.Run.Tools) || !isNilPersonaOutputPort(authority.tools) {
		return ErrPersonaRunExecutorUnavailable
	}
	authority.tools = tools
	cfg.Run.Tools = tools
	return nil
}
