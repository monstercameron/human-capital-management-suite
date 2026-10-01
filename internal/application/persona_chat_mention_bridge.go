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

var errPersonaChatMentionBridge = errors.New("application: persona chat mention bridge unavailable")

// personaChatMentionBridge is the post-commit boundary between Chat's
// canonical post projection and the shared persona invocation service. Chat
// remains responsible for durability; agentinvoke remains responsible for
// admission, grants, and idempotent run start.
type personaChatMentionBridge struct {
	references personaReferenceResolver
	resolver   *agentinvoke.Service
}

type personaChatMentionBridgeConfig struct {
	References personaReferenceResolver
	Resolver   *agentinvoke.Service
}

func newPersonaChatMentionBridge(cfg personaChatMentionBridgeConfig) (*personaChatMentionBridge, error) {
	if cfg.References == nil || cfg.Resolver == nil {
		return nil, fmt.Errorf("%w: reference resolver and invocation service are required", errPersonaChatMentionBridge)
	}
	return &personaChatMentionBridge{references: cfg.References, resolver: cfg.Resolver}, nil
}

// HandlePostCommit resolves canonical persona references on one newly created
// human post. The principal is read from trusted context and must match both
// the post author and tenant; caller-supplied chat request fields are not used
// as authority. Replaying the same post is safe because agentinvoke keys its
// claim by post and persona.
func (b *personaChatMentionBridge) HandlePostCommit(ctx context.Context, post chatcore.Post) error {
	if b == nil || b.references == nil || b.resolver == nil {
		return errPersonaChatMentionBridge
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman {
		return fmt.Errorf("%w: trusted human principal is required", errPersonaChatMentionBridge)
	}
	if principal.Subject() != post.AuthorID || principal.Tenant().String() != post.TenantID {
		return fmt.Errorf("%w: principal does not match post author", errPersonaChatMentionBridge)
	}
	if err := validPersonaMentionPost(post); err != nil {
		return err
	}
	mentions, err := b.references.ResolvePersonaMentions(ctx, post.TenantID, post.ConversationID, post.References)
	if err != nil {
		return fmt.Errorf("%w: resolve canonical mentions: %v", errPersonaChatMentionBridge, err)
	}
	mentions = canonicalPersonaMentions(mentions)
	if len(mentions) == 0 {
		return nil
	}
	threadID := post.ID
	if post.ParentID != "" {
		threadID = post.ParentID
	}
	ctx = withPersonaChatAuthorityTuple(ctx, post.TenantID, principal.Subject(), post.ConversationID, threadID, post.ID)
	_, err = b.resolver.OnPostCommit(ctx, agentinvoke.PostCommit{
		TenantID: post.TenantID, ConversationID: post.ConversationID, ThreadID: threadID,
		PostID: post.ID, AuthorID: principal.Subject(), AuthorKind: agentinvoke.HumanAuthor,
		Body: post.Body, New: true, Mentions: mentions,
	})
	if err != nil {
		return fmt.Errorf("%w: invoke canonical mentions: %v", errPersonaChatMentionBridge, err)
	}
	return nil
}

func validPersonaMentionPost(post chatcore.Post) error {
	for name, value := range map[string]string{
		"post id": post.ID, "tenant id": post.TenantID, "conversation id": post.ConversationID, "author id": post.AuthorID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", errPersonaChatMentionBridge, name)
		}
	}
	if post.ID != strings.TrimSpace(post.ID) || post.Revision != 1 || post.Deleted || post.SourceAttribution != nil {
		return fmt.Errorf("%w: post is not a newly authored canonical post", errPersonaChatMentionBridge)
	}
	return nil
}

func canonicalPersonaMentions(mentions []agentinvoke.Mention) []agentinvoke.Mention {
	canonical := make([]agentinvoke.Mention, 0, len(mentions))
	seen := make(map[string]struct{}, len(mentions))
	for _, mention := range mentions {
		if mention.Kind != agentinvoke.PersonaMention || !mention.Canonical || strings.TrimSpace(mention.PersonaID) == "" {
			continue
		}
		if _, exists := seen[mention.PersonaID]; exists {
			continue
		}
		seen[mention.PersonaID] = struct{}{}
		mention.Display = ""
		canonical = append(canonical, mention)
	}
	return canonical
}
