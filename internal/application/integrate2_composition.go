package application

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type integrate2RenderingPolicy struct {
	Store    *chatstore.Store
	Chat     chat.ConversationService
	Filter   *chat.FilterContentPolicy
	Personas personaReferenceLookup
	// Languages answers whether translation is on for a conversation (CHATLANG-006).
	Languages *ChatlangGovernance
}

func (s integrate2RenderingPolicy) AuthorizeRendering(ctx context.Context, scope chatstore.RenderingScope, action string) error {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || p.Subject() != scope.Principal.SubjectID || p.Tenant().String() != scope.Principal.TenantID || !time.Now().Before(p.ExpiresAt()) {
		return chatrender.ErrDenied
	}
	if scope.Conversation == "" {
		if action != "settings" || scope.Tenant != scope.Principal.TenantID {
			return chatrender.ErrDenied
		}
		return nil
	}
	_, err := s.Chat.GetConversation(ctx, chat.GetConversationRequest{Principal: scope.Principal, TenantID: scope.Tenant, ConversationID: scope.Conversation})
	return err
}

func (s integrate2RenderingPolicy) RenderingPolicy(ctx context.Context, scope chatstore.RenderingScope, id string) (chatrender.Policy, error) {
	post, err := integrate2ReadPost(ctx, s.Chat, scope.Principal, scope.Tenant, scope.Conversation, id)
	if err != nil {
		return chatrender.Policy{}, err
	}
	authored := post.Body
	post, err = s.visibleSource(ctx, post)
	if err != nil {
		return chatrender.Policy{}, err
	}
	detection, err := s.Store.RevisionLanguage(ctx, scope, id, post.Revision)
	if errors.Is(err, chat.ErrNotFound) || (err == nil && authored != post.Body) {
		detection = chatrender.Detect(post.Body)
		err = nil
	}
	if err != nil {
		return chatrender.Policy{}, err
	}
	original := chatrender.Rendering{Tenant: scope.Tenant, Message: id, Revision: post.Revision, Tone: chatrender.AsWritten, Text: post.Body, Language: detection.Language, SourceLanguage: detection.Language}
	policy := chatrender.Policy{Original: original, AllowOriginal: true, AllowedKinds: []chatrender.Kind{chatrender.Mask}}
	// Mask selection is synchronous and keeps authored bytes outside reader views.
	if s.Filter != nil {
		masked, err := s.Filter.MaskedBody(ctx, post, scope.Principal)
		if err != nil {
			return chatrender.Policy{}, err
		}
		if masked != post.Body {
			policy.AllowOriginal = false
			policy.RequireMask = true
		}
	}
	// CHATLANG-003: a translated rendering is offered only where the workspace,
	// the channel and the composed engine allow it, and never for text the
	// filters mask.
	if !policy.RequireMask && s.Languages.Allows(ctx, scope.Tenant, scope.Conversation) {
		policy.AllowedKinds = append(policy.AllowedKinds, chatrender.Translate)
	}
	return policy, nil
}

func integrate2ReadPost(ctx context.Context, reader chat.ConversationService, p chat.Principal, tenant, conversation, id string) (chat.Post, error) {
	if id == "" {
		return chat.Post{}, chat.ErrInvalidArgument
	}
	if references, ok := reader.(chat.AuthorizedReferenceReader); ok {
		_, post, err := references.ReadAuthorizedReference(ctx, p, tenant, conversation, id)
		if err != nil {
			return chat.Post{}, err
		}
		if post != nil && !post.Deleted {
			return *post, nil
		}
		return chat.Post{}, chat.ErrNotFound
	}
	page := chat.Page{PageSize: 200}
	seen := map[string]bool{}
	for {
		result, err := reader.ListPosts(ctx, chat.ListPostsRequest{Principal: p, TenantID: tenant, ConversationID: conversation, Page: page})
		if err != nil {
			return chat.Post{}, err
		}
		for _, post := range result.Posts {
			if post.ID == id && post.TenantID == tenant && post.ConversationID == conversation && !post.Deleted {
				return post, nil
			}
		}
		if result.NextCursor == "" {
			return chat.Post{}, chat.ErrNotFound
		}
		if seen[result.NextCursor] {
			return chat.Post{}, chat.ErrUnavailable
		}
		seen[result.NextCursor] = true
		page.Cursor = result.NextCursor
	}
}

func composeIntegrate2Chat(store *chatstore.Store, adapter *chatstore.Adapter, core *chat.Service, routed chat.ConversationService, filters *chatfilter.Service, facts ChatAuthorityFacts, input ChatComposition, now chat.Clock) (composedChat, error) {
	filters.Authority = ChatFilterAuthority{Facts: facts, Conversations: routed, Membership: adapter, Now: now}
	policy := integrate2RenderingPolicy{Store: store, Chat: routed, Personas: input.PersonaReferences, Filter: &chat.FilterContentPolicy{Filters: filters, Conversations: adapter, AuthorIdentity: chatFilterIdentity{facts: facts, now: now, personas: input.PersonaReferences}}}
	languages := &ChatlangGovernance{Store: store, Admins: filters.Authority, Now: now}
	policy.Languages = languages
	surface := &ChatRenderingSurface{Store: store, Policy: policy, Languages: languages}
	rowAuthority := func(ctx context.Context, a chatsearch.Actor, row chatsearch.Row) (bool, error) {
		if row.ID == "" {
			return true, nil
		}
		// Object grants are independent; absent artifact authorities never grant files.
		if row.Kind == chatsearch.File || row.Kind == chatsearch.SourceTitle {
			return false, nil
		}
		if row.Target.MessageID == "" {
			return true, nil
		}
		_, err := integrate2ReadPost(ctx, routed, chat.Principal{TenantID: a.HomeTenantID, SubjectID: a.PersonID}, a.TenantID, row.Target.ConversationID, row.Target.MessageID)
		if errors.Is(err, chat.ErrNotFound) || errors.Is(err, chat.ErrPermissionDenied) {
			return false, nil
		}
		return err == nil, err
	}
	rendering := func(ctx context.Context, a chatsearch.Actor, row chatsearch.Row) (string, error) {
		if row.Kind != chatsearch.Message && row.Kind != chatsearch.Thread && row.Kind != chatsearch.Pin && row.Kind != chatsearch.AgentAnswer {
			return row.Text, nil
		}
		scope := chatstore.RenderingScope{Principal: chat.Principal{TenantID: a.HomeTenantID, SubjectID: a.PersonID}, Tenant: a.TenantID, Conversation: row.Target.ConversationID}
		value, _, err := surface.ReadRenderingSelection(ctx, scope, row.Target.MessageID)
		return value.Text, err
	}
	registry, err := NewChatSearchRendering(store, routed, rowAuthority, rendering)
	if err != nil {
		return composedChat{}, err
	}
	if err := integrate2RegisterFilters(registry, filters); err != nil {
		return composedChat{}, err
	}
	if err := integrate2RegisterVoice(registry, adapter, routed, policy.Filter); err != nil {
		return composedChat{}, err
	}
	result := composedChat{status: nil, renderings: surface, filters: filters, search: ChatSearchHTTP{Port: registry, History: chatsearch.NewRecent()}, store: store}
	result.status, _ = routed.(chat.ChannelStatusService)
	result.locations, result.locationPictures = integrate2ComposeLocations(routed, store, input, now)

	core.SetChannelGate(nil)
	if !isNilPersonaOutputPort(input.GateAuthority) {
		gates := &chatgate.Service{Repository: &chatstore.GateRepository{Store: store, Membership: adapter.GateMembership}, Authority: input.GateAuthority, Registry: chatgate.NewRegistry(), Clock: now}
		core.SetChannelGate(ChatgateJoinAdapter{Service: gates})
		routes, _ := routed.(interface {
			ChatWriteContext(context.Context, string, string) (context.Context, error)
		})
		result.gates = integrate2GateSurface{ChatgateSurface: &ChatgateApplication{Clock: now, Service: gates, Directory: input.GateDirectory}, Routes: routes, Filter: policy.Filter}
	}
	return result, nil
}

func (s integrate2RenderingPolicy) MaskedRendering(ctx context.Context, scope chatstore.RenderingScope, policy chatrender.Policy) (chatrender.Rendering, error) {
	post, err := integrate2ReadPost(ctx, s.Chat, scope.Principal, scope.Tenant, scope.Conversation, policy.Original.Message)
	if err != nil {
		return chatrender.Rendering{}, err
	}
	post, err = s.visibleSource(ctx, post)
	if err != nil {
		return chatrender.Rendering{}, err
	}
	masked, err := s.Filter.MaskedBody(ctx, post, scope.Principal)
	value := policy.Original
	value.Text = masked
	value.Kinds = []chatrender.Kind{chatrender.Mask}
	value.Checks = chatrender.Checks{Meaning: true, Placeholders: true}
	return value, err
}

func (s integrate2RenderingPolicy) visibleSource(ctx context.Context, post chat.Post) (chat.Post, error) {
	text := chatui.AuthoredReaderText(post.Body)
	if text == post.Body {
		return post, nil
	}
	if s.Personas == nil {
		return post, chat.ErrUnavailable
	}
	facts, err := s.Personas.LookupPersonaReference(ctx, post.TenantID, post.ConversationID, post.AuthorID)
	if errors.Is(err, errPersonaReferenceNotPersona) {
		return post, nil
	}
	if err != nil {
		return post, err
	}
	if !currentPersonaReference(facts, post.TenantID, post.ConversationID, post.AuthorID, time.Now()) {
		return post, chat.ErrPermissionDenied
	}
	post.Body = text
	return post, nil
}
