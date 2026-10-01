package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaRunOwnerFacts = errors.New("application: persona run owner facts unavailable")

type personaRunLegalEntityResolver interface {
	Resolve(context.Context, agentinvoke.RunRequest) (string, error)
}

type personaRunAgentPrincipalResolver interface {
	Resolve(context.Context, values.TenantId, string, int64) (string, error)
}

type personaRunAudienceResolver interface {
	ResolvePersonaRunAudience(context.Context, agentinvoke.RunRequest) (agentrun.AudienceScope, error)
}

type personaRunThreadSnapshotReader interface {
	CaptureThreadSnapshot(context.Context, chat.ThreadSnapshotRequest) (chat.ThreadSnapshot, error)
}

type personaRunEffectivePolicyResolver interface {
	Resolve(context.Context, string, string) (PersonaRunEffectivePolicy, error)
}

// PersonaRunOwnerFactsComposer combines current authorities owned by workforce,
// trust, chat, and agent policy into the facts required for one admission.
type PersonaRunOwnerFactsComposer struct {
	LegalEntity personaRunLegalEntityResolver
	Principal   personaRunAgentPrincipalResolver
	Audience    personaRunAudienceResolver
	Threads     personaRunThreadSnapshotReader
	Policy      personaRunEffectivePolicyResolver
}

var _ PersonaRunOwnerFactsSource = (*PersonaRunOwnerFactsComposer)(nil)

// NewPersonaRunOwnerFactsComposer requires every authoritative owner. Missing
// sources are rejected at construction and again at use.
func NewPersonaRunOwnerFactsComposer(legal personaRunLegalEntityResolver, principal personaRunAgentPrincipalResolver, audience personaRunAudienceResolver, threads personaRunThreadSnapshotReader, policy personaRunEffectivePolicyResolver) (*PersonaRunOwnerFactsComposer, error) {
	if legal == nil || principal == nil || audience == nil || threads == nil || policy == nil {
		return nil, errPersonaRunOwnerFacts
	}
	return &PersonaRunOwnerFactsComposer{LegalEntity: legal, Principal: principal, Audience: audience, Threads: threads, Policy: policy}, nil
}

// NewDatabasePersonaRunRequestBuilder composes the tenant-scoped persona and
// manifest readers with current owner facts into the served admission builder.
func NewDatabasePersonaRunRequestBuilder(personas personaRunAuthorityFactory, manifests personaRunManifestResolverFactory, ownerFacts PersonaRunOwnerFactsSource) (*PersonaRunRequestBuilder, error) {
	if personas == nil || manifests == nil || ownerFacts == nil {
		return nil, errPersonaRunRequestBuilder
	}
	return NewPersonaRunRequestBuilder(&DatabasePersonaRunRequestSource{Personas: personas, Manifests: manifests, OwnerFacts: ownerFacts})
}

// ResolvePersonaRunOwnerFacts reads the invoker's current legal entity, the
// persona version's bound service principal, current audience and thread image,
// and the active legal-entity policy. It never trusts request labels as facts.
func (s *PersonaRunOwnerFactsComposer) ResolvePersonaRunOwnerFacts(ctx context.Context, invocation agentinvoke.RunRequest) (PersonaRunOwnerFacts, error) {
	if s == nil || s.LegalEntity == nil || s.Principal == nil || s.Audience == nil || s.Threads == nil || s.Policy == nil || ctx == nil || invocation.Mode != agentinvoke.OnBehalfOf {
		return PersonaRunOwnerFacts{}, errPersonaRunOwnerFacts
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Tenant().String() != invocation.TenantID || verified.Subject() != invocation.InvokerID {
		return PersonaRunOwnerFacts{}, fmt.Errorf("%w: verified invoker required", errPersonaRunOwnerFacts)
	}
	version, err := personaRunVersionNumber(invocation.PersonaVersion)
	if err != nil {
		return PersonaRunOwnerFacts{}, fmt.Errorf("%w: exact persona version required", errPersonaRunOwnerFacts)
	}
	legal, err := s.LegalEntity.Resolve(ctx, invocation)
	if err != nil {
		return PersonaRunOwnerFacts{}, fmt.Errorf("%w: resolve invoker legal entity: %w", errPersonaRunOwnerFacts, err)
	}
	principal, err := s.Principal.Resolve(ctx, values.TenantId(invocation.TenantID), invocation.PersonaID, version)
	if err != nil {
		return PersonaRunOwnerFacts{}, fmt.Errorf("%w: resolve persona service principal: %w", errPersonaRunOwnerFacts, err)
	}
	audience, err := s.Audience.ResolvePersonaRunAudience(ctx, invocation)
	if err != nil {
		return PersonaRunOwnerFacts{}, fmt.Errorf("%w: resolve current audience: %w", errPersonaRunOwnerFacts, err)
	}
	thread, err := s.currentThreadSnapshot(ctx, invocation, verified)
	if err != nil {
		return PersonaRunOwnerFacts{}, err
	}
	policy, err := s.Policy.Resolve(ctx, invocation.TenantID, legal)
	if err != nil {
		return PersonaRunOwnerFacts{}, fmt.Errorf("%w: resolve active policy: %w", errPersonaRunOwnerFacts, err)
	}
	return PersonaRunOwnerFacts{
		LegalEntityID: legal, AgentPrincipal: principal, Audience: audience,
		Context:  agentrun.ContextScope{ID: invocation.ThreadID, SnapshotID: thread.SnapshotID, Digest: thread.Digest},
		Deadline: policy.Deadline, Budget: policy.Budget,
	}, nil
}

func (s *PersonaRunOwnerFactsComposer) currentThreadSnapshot(ctx context.Context, invocation agentinvoke.RunRequest, verified *trust.Principal) (chat.ThreadSnapshot, error) {
	limit := agentinvoke.MaxThreadPosts
	request := chat.ThreadSnapshotRequest{
		Principal: chat.Principal{TenantID: verified.Tenant().String(), SubjectID: verified.Subject(), Roles: verified.Roles()},
		TenantID:  invocation.TenantID, ConversationID: invocation.ConversationID, ThreadID: invocation.ThreadID,
		InvokingPostID: invocation.InvokingPostID, Limit: limit,
	}
	snapshot, err := s.Threads.CaptureThreadSnapshot(ctx, request)
	if err != nil {
		return chat.ThreadSnapshot{}, fmt.Errorf("%w: capture current thread: %w", errPersonaRunOwnerFacts, err)
	}
	readRequest := agentinvoke.ThreadReadRequest{
		TenantID: invocation.TenantID, ConversationID: invocation.ConversationID, ThreadID: invocation.ThreadID,
		InvokingPostID: invocation.InvokingPostID, InvokerID: invocation.InvokerID, Limit: limit,
	}
	if err := validatePersonaThreadSnapshot(snapshot, readRequest, request.Principal, limit); err != nil {
		return chat.ThreadSnapshot{}, fmt.Errorf("%w: thread snapshot scope or digest is invalid", errPersonaRunOwnerFacts)
	}
	return snapshot, nil
}

func personaRunVersionNumber(label string) (int64, error) {
	canonical := strings.TrimPrefix(label, "v")
	version, err := strconv.ParseInt(canonical, 10, 64)
	if err != nil || version <= 0 || strconv.FormatInt(version, 10) != canonical {
		return 0, errPersonaRunOwnerFacts
	}
	return version, nil
}
