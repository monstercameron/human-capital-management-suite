package application

import (
	"context"
	"strconv"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// PersonaLiveCasePlacement is a pre-provisioned fixture-only conversation and
// candidate placement. Contexts are authenticated synthetic human identities;
// the evaluator cannot substitute their subject or tenant from request text.
type PersonaLiveCasePlacement struct {
	ConversationID, InstallationID string
	InvokerContext, PeerContext    context.Context
}

type PersonaLiveCaseSkillSource interface {
	ResolvePersonaEvaluationSkills(context.Context, agenteval.PersonaEvaluationTarget, string, string, string, string) (agentinvoke.SkillScopes, error)
}

type PersonaLiveChatPostWriter interface {
	SendPost(context.Context, chatcore.SendPostRequest) (chatcore.Post, error)
}

// PersonaLiveChatFixtureSource uses normal chat commits and the durable grant
// and invocation repositories. Fixtures contain input posts and placements,
// never outcomes, delivered recipients, pass flags or measured counters.
type PersonaLiveChatFixtureSource struct {
	Definitions *PersonaCandidateDefinitionSource
	Chat        PersonaLiveChatPostWriter
	Invocations agentinvoke.InvocationRepository
	Grants      agentinvoke.GrantIssuer
	Skills      PersonaLiveCaseSkillSource
	Placements  map[string]PersonaLiveCasePlacement
	Now         func() time.Time
	NewID       func() string
}

func (s *PersonaLiveChatFixtureSource) CreateSyntheticPersonaCase(ctx context.Context, target agenteval.PersonaEvaluationTarget, testCase agenteval.PersonaCase) (context.Context, agentinvoke.RunRequest, error) {
	if s == nil || ctx == nil || s.Definitions == nil || s.Definitions.Target != target || s.Chat == nil || s.Invocations == nil || s.Grants == nil || s.Skills == nil || s.Now == nil || s.NewID == nil {
		return nil, agentinvoke.RunRequest{}, agenteval.ErrPersonaEvaluation
	}
	placement, ok := s.Placements[testCase.ID]
	if !ok || !required(placement.ConversationID) || !required(placement.InstallationID) || placement.InvokerContext == nil {
		return nil, agentinvoke.RunRequest{}, agenteval.ErrPersonaEvaluation
	}
	invoker, ok := personaRunChatPrincipal(placement.InvokerContext, target.SyntheticTenantID, target.InvokerID)
	if !ok {
		return nil, agentinvoke.RunRequest{}, agenteval.ErrPersonaEvaluation
	}
	verifiedInvoker, _ := trust.FromContext(placement.InvokerContext)
	caseCtx := trust.WithPrincipal(ctx, verifiedInvoker)
	_, _, manifest, err := s.Definitions.Resolve(caseCtx)
	if err != nil {
		return nil, agentinvoke.RunRequest{}, err
	}
	invocationID := s.NewID()
	if !required(invocationID) {
		return nil, agentinvoke.RunRequest{}, agenteval.ErrPersonaEvaluation
	}
	parent := ""
	if testCase.PeerText != "" {
		peer, ok := personaEvaluationPeerPrincipal(placement.PeerContext, target)
		if !ok {
			return nil, agentinvoke.RunRequest{}, agenteval.ErrPersonaEvaluation
		}
		verifiedPeer, _ := trust.FromContext(placement.PeerContext)
		post, err := s.Chat.SendPost(trust.WithPrincipal(ctx, verifiedPeer), chatcore.SendPostRequest{Principal: peer, TenantID: target.SyntheticTenantID,
			ConversationID: placement.ConversationID, Body: testCase.PeerText, IdempotencyKey: "candidate-peer-" + invocationID})
		if err != nil || post.TenantID != target.SyntheticTenantID || post.ConversationID != placement.ConversationID || post.AuthorID != peer.SubjectID || !required(post.ID) {
			return nil, agentinvoke.RunRequest{}, agenteval.ErrPersonaEvaluation
		}
		parent = post.ID
	}
	post, err := s.Chat.SendPost(caseCtx, chatcore.SendPostRequest{Principal: invoker, TenantID: target.SyntheticTenantID,
		ConversationID: placement.ConversationID, ParentID: parent, Body: testCase.Prompt, IdempotencyKey: "candidate-invoker-" + invocationID})
	if err != nil || post.TenantID != target.SyntheticTenantID || post.ConversationID != placement.ConversationID || post.AuthorID != target.InvokerID || !required(post.ID) || post.Body != testCase.Prompt {
		return nil, agentinvoke.RunRequest{}, agenteval.ErrPersonaEvaluation
	}
	thread := parent
	if thread == "" {
		thread = post.ID
	}
	skills, err := s.Skills.ResolvePersonaEvaluationSkills(caseCtx, target, placement.ConversationID, placement.InstallationID, thread, post.ID)
	if err != nil || len(skills) == 0 {
		return nil, agentinvoke.RunRequest{}, agenteval.ErrPersonaEvaluation
	}
	version := strconv.FormatInt(target.PersonaVersion, 10)
	actor := agentinvoke.ActorChain{UserID: target.InvokerID, PersonaID: target.PersonaID, PersonaVersion: version,
		InstallationID: placement.InstallationID, ConversationID: placement.ConversationID, InvokingPostID: post.ID, InvocationID: invocationID}
	invocation, _, err := s.Invocations.Claim(caseCtx, agentinvoke.Invocation{ID: invocationID, TenantID: target.SyntheticTenantID,
		ConversationID: placement.ConversationID, ThreadID: thread, PostID: post.ID, InvokerID: target.InvokerID, PersonaID: target.PersonaID,
		PersonaVersion: version, InstallationID: placement.InstallationID, Mode: agentinvoke.OnBehalfOf, Skills: skills, Actor: actor})
	if err != nil {
		return nil, agentinvoke.RunRequest{}, err
	}
	grant, err := s.Grants.CreateOnBehalfOfGrant(caseCtx, agentinvoke.GrantRequest{InvocationID: invocation.ID, UserID: target.InvokerID,
		TenantID: target.SyntheticTenantID, AgentVersion: version, TargetAgentID: manifest.ID,
		PersonaID: target.PersonaID, PersonaVersion: version, InstallationID: placement.InstallationID, ConversationID: placement.ConversationID,
		ThreadID: thread, InvokingPostID: post.ID, Purpose: "persona-mention", Skills: skills, Mode: agentinvoke.OnBehalfOf, ExpiresAt: s.Now().UTC().Add(15 * time.Minute)})
	if err != nil {
		return nil, agentinvoke.RunRequest{}, err
	}
	invocation, err = s.Invocations.SetGrant(caseCtx, target.SyntheticTenantID, invocation.ID, grant)
	if err != nil {
		return nil, agentinvoke.RunRequest{}, err
	}
	if _, err := s.Invocations.MarkStarted(caseCtx, target.SyntheticTenantID, invocation.ID); err != nil {
		return nil, agentinvoke.RunRequest{}, err
	}
	request := agentinvoke.RunRequest{InvocationID: invocation.ID, TenantID: target.SyntheticTenantID, ConversationID: placement.ConversationID,
		ThreadID: thread, InvokingPostID: post.ID, InvokerID: target.InvokerID, PersonaID: target.PersonaID, PersonaVersion: version,
		InstallationID: placement.InstallationID, Mode: agentinvoke.OnBehalfOf, Skills: invocation.Skills, Grant: invocation.Grant, Actor: invocation.Actor}
	if !validPersonaChatRunRequest(request) {
		return nil, agentinvoke.RunRequest{}, agenteval.ErrPersonaEvaluation
	}
	return caseCtx, request, nil
}

func personaEvaluationPeerPrincipal(ctx context.Context, target agenteval.PersonaEvaluationTarget) (chatcore.Principal, bool) {
	if ctx == nil {
		return chatcore.Principal{}, false
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().String() != target.SyntheticTenantID || principal.Subject() == target.InvokerID {
		return chatcore.Principal{}, false
	}
	return chatcore.Principal{TenantID: principal.Tenant().String(), SubjectID: principal.Subject(), Roles: principal.Roles()}, true
}
