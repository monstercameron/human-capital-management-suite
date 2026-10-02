package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaChatInvocation = errors.New("application: persona chat invocation unavailable")

// personaChatPostWriter is the durable chat write seam used by the coordinator.
type personaChatPostWriter interface {
	SendPost(context.Context, chatcore.SendPostRequest) (chatcore.Post, error)
}

type personaDirectConversationReader interface {
	GetConversation(context.Context, chatcore.GetConversationRequest) (chatcore.Conversation, error)
	ListMemberships(context.Context, chatcore.ListMembershipsRequest) (chatcore.ListMembershipsResponse, error)
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
	// DetachedAfterCommit is enabled by the served runtime so model work never
	// holds the HTTP response that committed the invoking post.
	DetachedAfterCommit bool
}

// personaChatInvocation coordinates a committed human chat post with the
// shared on-behalf-of invocation service. It does not own post durability.
type personaChatInvocation struct {
	chat     personaChatPostWriter
	refs     personaReferenceResolver
	resolver *agentinvoke.Service
	failures personaInvocationFailureSink
	detached bool
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
	_, servedWorker := cfg.Runs.(*PersonaRunModelWorker)
	return &personaChatInvocation{chat: cfg.Chat, refs: cfg.References, resolver: resolver, failures: cfg.Failures, detached: cfg.DetachedAfterCommit || servedWorker}, nil
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
	if c.detached {
		// Detach before the request is released. The admission itself has a
		// strict bound below, so this server-owned task cannot linger forever.
		detached := context.WithoutCancel(ctx)
		go c.afterCommit(detached, request, post)
	} else {
		c.afterCommit(ctx, request, post)
	}
	return post, nil
}

// personaMentionAdmissionTimeout bounds the admission work that follows a
// committed post that mentions an agent.
const personaMentionAdmissionTimeout = 60 * time.Second

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
	// Everything after commit, including canonical-reference resolution, keeps
	// request values but not request cancellation and is bounded independently.
	admissionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), personaMentionAdmissionTimeout)
	defer cancel()
	references := post.References
	if !hasTypedAgentReference(references) {
		if reference, ok := c.directAgentReference(admissionCtx, principal, post); ok {
			references = append(append([]chatcore.Reference(nil), references...), reference)
		}
	}
	mentions, err := c.refs.ResolvePersonaMentions(admissionCtx, post.TenantID, post.ConversationID, references)
	if err != nil {
		c.recordPostFailure(admissionCtx, post, err)
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
	// The post is committed. Admission of the mention is server work that must
	// not be abandoned because the sender's request was cancelled or timed out,
	// so it keeps the request's values (principal, trace) under its own bound.
	started := time.Now()
	ctx, _ = withAgentUXRunTiming(admissionCtx)
	ctx = withPersonaChatAuthorityTuple(ctx, post.TenantID, principal.Subject(), post.ConversationID, threadID, post.ID)
	defer func() {
		slog.InfoContext(ctx, "hcmnext.persona_mention_admission", "post_id", post.ID, "duration_ms", time.Since(started).Milliseconds(), "failed", err != nil)
		agentUXSpeedEmit(ctx, err != nil)
	}()
	done := agentUXSpeedStage(ctx, "admission_to_delivery")
	defer done()
	_, err = c.resolver.OnPostCommit(ctx, agentinvoke.PostCommit{
		TenantID: post.TenantID, ConversationID: post.ConversationID, ThreadID: threadID,
		PostID: post.ID, AuthorID: principal.Subject(), AuthorKind: agentinvoke.HumanAuthor,
		Body: post.Body, New: true, Mentions: canonical,
	})
	if err != nil {
		c.recordPostFailure(ctx, post, err)
	}
}

func hasTypedAgentReference(references []chatcore.Reference) bool {
	for _, reference := range references {
		if reference.Kind == chatcore.AgentMention {
			return true
		}
	}
	return false
}

// directAgentReference recognizes only a current, same-tenant, two-member
// direct conversation. The canonical resolver then proves that the other
// member is the active agent identity installed in this exact conversation.
func (c *personaChatInvocation) directAgentReference(ctx context.Context, principal *trust.Principal, post chatcore.Post) (chatcore.Reference, bool) {
	reader, ok := c.chat.(personaDirectConversationReader)
	if !ok || principal == nil {
		return chatcore.Reference{}, false
	}
	chatPrincipal := chatcore.Principal{TenantID: principal.Tenant().String(), SubjectID: principal.Subject()}
	room, err := reader.GetConversation(ctx, chatcore.GetConversationRequest{Principal: chatPrincipal, TenantID: post.TenantID, ConversationID: post.ConversationID})
	if err != nil || room.ID != post.ConversationID || room.TenantID != post.TenantID || room.Kind != chatcore.Direct || room.Archived {
		return chatcore.Reference{}, false
	}
	members, err := reader.ListMemberships(ctx, chatcore.ListMembershipsRequest{Principal: chatPrincipal, TenantID: post.TenantID, ConversationID: post.ConversationID, Page: chatcore.Page{PageSize: 3}})
	if err != nil || members.NextCursor != "" || len(members.Memberships) != 2 {
		return chatcore.Reference{}, false
	}
	foundHuman := false
	agentID := ""
	for _, member := range members.Memberships {
		if member.TenantID != post.TenantID || member.ConversationID != post.ConversationID || member.HomeTenantID != post.TenantID || member.JoinedAt == nil || member.LeftAt != nil {
			return chatcore.Reference{}, false
		}
		if member.SubjectID == principal.Subject() {
			if foundHuman {
				return chatcore.Reference{}, false
			}
			foundHuman = true
			continue
		}
		if agentID != "" || strings.TrimSpace(member.SubjectID) != member.SubjectID || member.SubjectID == "" {
			return chatcore.Reference{}, false
		}
		agentID = member.SubjectID
	}
	if !foundHuman || agentID == "" {
		return chatcore.Reference{}, false
	}
	return chatcore.Reference{Kind: chatcore.AgentMention, TenantID: post.TenantID, ID: agentID, ConversationID: post.ConversationID}, true
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
