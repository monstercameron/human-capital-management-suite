package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaChatInvocation = errors.New("application: persona chat invocation unavailable")

// personaChatPostWriter is the durable chat write seam used by the coordinator.
type personaChatPostWriter interface {
	SendPost(context.Context, chatcore.SendPostRequest) (chatcore.Post, error)
}

// personaReferenceResolver returns only canonical persona identities represented
// by references already accepted by Chat. It must resolve by typed identity, never Display.
type personaReferenceResolver interface {
	ResolvePersonaMentions(context.Context, string, string, []chatcore.Reference) ([]agentinvoke.Mention, error)
}

// personaT0SkillPolicy is the server-owned catalog check for read-only skills.
type personaT0SkillPolicy interface {
	IsBoundT0Run(context.Context, agentinvoke.RunRequest) (bool, error)
}

// personaInvocationFailureSink records best-effort failures after the human
// post is already durable. Failure reporting must never fail the chat send.
type personaInvocationFailureSink interface {
	RecordPersonaInvocationFailure(context.Context, string, error)
}

// personaChatInvocationConfig contains the explicit ports needed at the
// post-commit boundary. Every run is constrained by the T0 skill policy.
type personaChatInvocationConfig struct {
	Chat       personaChatPostWriter
	References personaReferenceResolver
	Authority  agentinvoke.AuthorityResolver
	Grants     agentinvoke.GrantIssuer
	Runs       agentinvoke.RunStarter
	T0Skills   personaT0SkillPolicy
	Repository agentinvoke.InvocationRepository
	Failures   personaInvocationFailureSink
}

// personaChatInvocation coordinates a committed human chat post with the
// shared on-behalf-of invocation service. It does not own post durability.
type personaChatInvocation struct {
	chat     personaChatPostWriter
	refs     personaReferenceResolver
	resolver *agentinvoke.Service
	failures personaInvocationFailureSink
}

func newPersonaChatInvocation(cfg personaChatInvocationConfig) (*personaChatInvocation, error) {
	if cfg.Chat == nil || cfg.References == nil || cfg.Authority == nil || cfg.Grants == nil || cfg.Runs == nil || cfg.T0Skills == nil || cfg.Repository == nil {
		return nil, fmt.Errorf("%w: chat, reference resolver, authority, grants, runner, T0 policy and repository are required", errPersonaChatInvocation)
	}
	var runs agentinvoke.RunStarter = personaT0RunStarter{next: cfg.Runs, policy: cfg.T0Skills}
	if _, ok := cfg.Runs.(agentinvoke.TargetAgentResolver); ok {
		runs = personaTargetedT0RunStarter{personaT0RunStarter: personaT0RunStarter{next: cfg.Runs, policy: cfg.T0Skills}}
	}
	resolver, err := agentinvoke.NewService(agentinvoke.Config{
		Authority: cfg.Authority, Grants: cfg.Grants,
		Runs:       runs,
		Repository: cfg.Repository,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: create invocation resolver: %v", errPersonaChatInvocation, err)
	}
	return &personaChatInvocation{chat: cfg.Chat, refs: cfg.References, resolver: resolver, failures: cfg.Failures}, nil
}

// SendPost commits the human message first, then attempts persona admission.
// Post-commit invocation failures are reported out of band and cannot turn a
// successful durable human post into a failed SendPost response.
func (c *personaChatInvocation) SendPost(ctx context.Context, request chatcore.SendPostRequest) (chatcore.Post, error) {
	if c == nil || c.chat == nil {
		return chatcore.Post{}, errPersonaChatInvocation
	}
	var post chatcore.Post
	var err error
	if len(request.References) > 0 {
		if referenceService, ok := c.chat.(chatcore.ReferenceService); ok {
			post, err = referenceService.SendPostWithReferences(ctx, chatcore.SendPostWithReferencesRequest{
				SendPostRequest: request,
				References:      append([]chatcore.Reference(nil), request.References...),
			})
		} else {
			post, err = c.chat.SendPost(ctx, request)
		}
	} else {
		post, err = c.chat.SendPost(ctx, request)
	}
	if err != nil {
		var classification *PersonaHumanPostClassificationError
		if errors.As(err, &classification) && classification.PostID != "" && classification.PostID == post.ID {
			c.recordPostFailure(ctx, post, err)
			return post, nil
		}
		return post, err
	}
	c.afterCommit(ctx, request, post)
	return post, nil
}

func (c *personaChatInvocation) afterCommit(ctx context.Context, request chatcore.SendPostRequest, post chatcore.Post) {
	if c == nil || c.resolver == nil || c.refs == nil {
		return
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman ||
		principal.Subject() != request.Principal.SubjectID || principal.Tenant().String() != request.Principal.TenantID {
		return
	}
	if post.ID == "" || post.ID != strings.TrimSpace(post.ID) || post.TenantID != request.TenantID ||
		post.ConversationID != request.ConversationID || post.AuthorID != principal.Subject() ||
		post.Revision != 1 || post.Deleted || post.SourceAttribution != nil {
		return
	}
	mentions, err := c.refs.ResolvePersonaMentions(ctx, post.TenantID, post.ConversationID, post.References)
	if err != nil {
		c.recordPostFailure(ctx, post, err)
		return
	}
	canonical := make([]agentinvoke.Mention, 0, len(mentions))
	for _, mention := range mentions {
		if mention.Kind != agentinvoke.PersonaMention || !mention.Canonical || strings.TrimSpace(mention.PersonaID) == "" {
			continue
		}
		mention.Display = "" // Identity and authority never depend on display text.
		canonical = append(canonical, mention)
	}
	if len(canonical) == 0 {
		return
	}
	threadID := post.ID
	if post.ParentID != "" {
		threadID = post.ParentID
	}
	ctx = withPersonaChatAuthorityTuple(ctx, post.TenantID, principal.Subject(), post.ConversationID, threadID, post.ID)
	_, err = c.resolver.OnPostCommit(ctx, agentinvoke.PostCommit{
		TenantID: post.TenantID, ConversationID: post.ConversationID, ThreadID: threadID,
		PostID: post.ID, AuthorID: principal.Subject(), AuthorKind: agentinvoke.HumanAuthor,
		Body: post.Body, New: true, Mentions: canonical,
	})
	if err != nil {
		c.recordPostFailure(ctx, post, err)
	}
}

func (c *personaChatInvocation) recordFailure(ctx context.Context, postID string, err error) {
	if c.failures != nil && err != nil {
		c.failures.RecordPersonaInvocationFailure(ctx, postID, err)
	}
}

type personaT0RunStarter struct {
	next   agentinvoke.RunStarter
	policy personaT0SkillPolicy
}

type personaTargetedT0RunStarter struct{ personaT0RunStarter }

func (s personaTargetedT0RunStarter) ResolveTargetAgentID(ctx context.Context, request agentinvoke.RunRequest) (string, error) {
	resolver, ok := s.next.(agentinvoke.TargetAgentResolver)
	if !ok || isNilPersonaOutputPort(resolver) {
		return "", errPersonaChatInvocation
	}
	return resolver.ResolveTargetAgentID(ctx, request)
}

func (s personaT0RunStarter) Start(ctx context.Context, request agentinvoke.RunRequest) error {
	if s.next == nil || s.policy == nil || request.Mode != agentinvoke.OnBehalfOf || len(request.Skills) == 0 {
		return errPersonaChatInvocation
	}
	for _, scopes := range request.Skills {
		if len(scopes) == 0 {
			return errPersonaChatInvocation
		}
	}
	bound := request
	bound.Skills = request.Skills.Clone()
	allowed, err := s.policy.IsBoundT0Run(ctx, bound)
	if err != nil {
		return fmt.Errorf("%w: T0 bound run lookup: %v", errPersonaChatInvocation, err)
	}
	if !allowed {
		return fmt.Errorf("%w: bound run is outside the T0 boundary", errPersonaChatInvocation)
	}
	return s.next.Start(ctx, request)
}
