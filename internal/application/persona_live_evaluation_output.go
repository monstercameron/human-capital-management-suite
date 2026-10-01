package application

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PersonaCandidateOutputAuthority reconstructs output authority from the same
// current owners used by admission and model routing. Grounding is read through
// the invoker's authorized thread reader; provider text cannot create evidence.
type PersonaCandidateOutputAuthority struct {
	authority   agentrun.Authority
	definitions *PersonaCandidateDefinitionSource
	threads     agentinvoke.ThreadReader
	route       PersonaRunModelRoute
	now         func() time.Time
	grants      PersonaGrantTenantStoreFactory
	worker      PersonaPrivateChatWorkloadIdentitySource
	tools       PersonaRuntimeToolGroundingSource
}

type PersonaCandidateOutputAuthorityConfig struct {
	Authority   agentrun.Authority
	Definitions *PersonaCandidateDefinitionSource
	Threads     agentinvoke.ThreadReader
	Route       PersonaRunModelRoute
	Grants      PersonaGrantTenantStoreFactory
	Worker      PersonaPrivateChatWorkloadIdentitySource
	Tools       PersonaRuntimeToolGroundingSource
	Now         func() time.Time
}

func NewPersonaCandidateOutputAuthority(c PersonaCandidateOutputAuthorityConfig) (*PersonaCandidateOutputAuthority, error) {
	if c.Authority == nil || c.Definitions == nil || c.Threads == nil || c.Grants == nil || c.Worker == nil || c.Now == nil || c.Definitions.Target.ModelDigest != "sha256:"+c.Route.Route.Pin.Primary.ProfileDigest {
		return nil, ErrPersonaRunOutputValidatorUnavailable
	}
	return &PersonaCandidateOutputAuthority{authority: c.Authority, definitions: c.Definitions, threads: c.Threads, route: c.Route, grants: c.Grants, worker: c.Worker, tools: c.Tools, now: c.Now}, nil
}

func (s *PersonaCandidateOutputAuthority) ResolvePersonaRunChatReplyAuthority(ctx context.Context, record agentrun.Record, run runstate.Run) (PersonaRunChatReplyAuthority, error) {
	if s == nil || ctx == nil || s.authority == nil || s.definitions == nil || s.threads == nil || s.now == nil || s.grants == nil || s.worker == nil || validatePersonaModelWorkBinding(record, run) != nil {
		return PersonaRunChatReplyAuthority{}, ErrPersonaRunOutputValidatorUnavailable
	}
	ctx = WithPersonaBackgroundAdmission(ctx, record)
	current, err := s.authority.VerifyAdmission(ctx, record.Request)
	if err != nil || !reflect.DeepEqual(current, record.Authority) {
		return PersonaRunChatReplyAuthority{}, ErrPersonaRunOutputValidatorUnavailable
	}
	_, _, manifest, err := s.definitions.Resolve(ctx)
	if err != nil || record.Request.Source.TenantID != s.definitions.Target.SyntheticTenantID || record.Request.Persona == nil || record.Request.Persona.Digest != s.definitions.Target.ProfileDigest || record.Request.Principal.InvokerID != s.definitions.Target.InvokerID || s.definitions.Target.ModelDigest != "sha256:"+s.route.Route.Pin.Primary.ProfileDigest {
		return PersonaRunChatReplyAuthority{}, ErrPersonaRunOutputValidatorUnavailable
	}
	store, err := s.grants.ForTenant(ctx, values.TenantId(record.Request.Source.TenantID))
	if err != nil || isNilPersonaOutputPort(store) {
		return PersonaRunChatReplyAuthority{}, ErrPersonaRunOutputValidatorUnavailable
	}
	grant, err := store.Get(record.Request.Principal.DelegatedCredentialRef)
	now := s.now().UTC()
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
	posts, err := s.threads.ReadThread(ctx, agentinvoke.ThreadReadRequest{TenantID: record.Request.Source.TenantID, ConversationID: record.Request.Audience.ID,
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
		ModelDigest: s.route.Route.Pin.Primary.ProfileDigest, Gateway: gateway, Admission: admitted, Grounding: grounding}, nil
}

var _ PersonaRunChatReplyAuthoritySource = (*PersonaCandidateOutputAuthority)(nil)
