package application

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

// Sharing a private answer to its channel is the asker's own act. The agent's
// task credential for the run expires minutes after the answer, so the server
// cannot post as the agent later: it posts the answer, as the asker, under the
// question, after the check a public answer needs. Every current member of the
// channel may read every document the answer links, now, and the agent is not
// set to answer privately. Anything that cannot be shown stays private.

// personaShareSource is one document the answer links, as its Sources row names it.
type personaShareSource struct{ DocumentID, VersionID, Anchor string }

// personaShareSources reads the Sources section of a private card. Every line
// must be a link to a document: a source the asker could not open (shown as
// text) or one that is not a document cannot be shown to be readable by anyone
// else, so the answer is not shared. The text before the section is returned.
func personaShareSources(body string) (answer string, sources []personaShareSource, ok bool) {
	const prefix = "\n\nSources\n"
	start := strings.LastIndex(body, prefix)
	if start < 0 {
		return "", nil, false
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(body[start+len(prefix):], "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimSpace(strings.SplitN(strings.TrimPrefix(line, "- "), " <!--", 2)[0])
		split := strings.LastIndex(line, "](")
		if !strings.HasPrefix(line, "[") || split < 1 || !strings.HasSuffix(line, ")") {
			return "", nil, false
		}
		parsed, err := url.Parse(line[split+2 : len(line)-1])
		if err != nil || parsed.Path != "/workspace/app/docs" || parsed.Query().Get("document") == "" || parsed.Query().Get("version") == "" {
			return "", nil, false
		}
		anchor, err := url.PathUnescape(parsed.Fragment)
		if err != nil {
			return "", nil, false
		}
		source := personaShareSource{DocumentID: parsed.Query().Get("document"), VersionID: parsed.Query().Get("version"), Anchor: anchor}
		if seen[source.DocumentID] {
			// The audience check takes one entry per document.
			continue
		}
		seen[source.DocumentID] = true
		sources = append(sources, source)
	}
	return strings.TrimSpace(body[:start]), sources, len(sources) > 0
}

var personaShareReadableFlag = regexp.MustCompile(` <!--chat\.agent\.source\.readable:(?:true|false)-->`)

// PersonaAnswerShareGate is the check made at the moment of sharing.
type PersonaAnswerShareGate struct {
	// Profile refuses an agent set to answer privately, for its profile or its
	// installation in this conversation. An error that carries
	// personaPrivateReasonError names that reason.
	Profile func(ctx context.Context, invocation agentinvoke.Invocation, conversation chat.Conversation) error
	// Audience resolves the conversation's members and the channel's data-class
	// ceiling.
	Audience chatrecipient.AudienceFloorAuthority
	// Documents decides, for one member, whether a document version may be read.
	Documents AgentAnnouncementDocumentAuthority
	// Cited resolves a linked document to the version and bytes it names, as the
	// asker may read them.
	Cited func(ctx context.Context, tenant, subject string, source personaShareSource) (AgentAnnouncementResolvedDocument, error)
	// BodyClasses classifies the answer's own text.
	BodyClasses PersonaPublicReplyTextClassificationSource
	// Fence holds the documents' read policy until fn returns. Optional.
	Fence func(ctx context.Context, tenant string, documentIDs []string, fn func() error) error
}

// Share checks that the answer may be read by the whole channel and, only then,
// calls post with the text to post, inside the documents' read fence. A refusal
// is an ErrPersonaShareRefused naming why.
func (g *PersonaAnswerShareGate) Share(ctx context.Context, asker chat.Principal, invocation agentinvoke.Invocation, conversation chat.Conversation, card string, post func(body string) (chat.Post, error)) (chat.Post, error) {
	if g == nil || ctx == nil || post == nil || g.Profile == nil || g.Cited == nil || isNilPersonaOutputPort(g.Audience) || isNilPersonaOutputPort(g.Documents) || isNilPersonaOutputPort(g.BodyClasses) {
		return chat.Post{}, ErrPersonaReplyDeliveryUnavailable
	}
	refused := func(reason string) (chat.Post, error) { return chat.Post{}, ErrPersonaShareRefused{Reason: reason} }
	if conversation.Kind != chat.PublicChannel || conversation.ID != invocation.ConversationID || conversation.TenantID != asker.TenantID {
		return refused(chat.PrivateReasonAudience)
	}
	if err := g.Profile(ctx, invocation, conversation); err != nil {
		return refused(personaPrivateReasonOf(err))
	}
	cleaned, _ := chat.SplitPrivateReason(card)
	answer, sources, ok := personaShareSources(cleaned)
	if !ok {
		return refused(chat.PrivateReasonAudience)
	}
	documents := make([]AgentAnnouncementResolvedDocument, 0, len(sources))
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		document, err := g.Cited(ctx, asker.TenantID, asker.SubjectID, source)
		if err != nil {
			return refused(chat.PrivateReasonAudience)
		}
		documents, ids = append(documents, document), append(ids, document.DocumentID)
	}
	bodyClass, err := personaRuntimePublicBodyClass(ctx, g.BodyClasses, asker.TenantID, answer)
	if err != nil {
		return refused(chat.PrivateReasonAudience)
	}
	var posted chat.Post
	run := func() error {
		snapshot, err := g.Audience.CurrentAudience(ctx, conversation)
		if err != nil {
			return ErrPersonaShareRefused{Reason: chat.PrivateReasonAudience}
		}
		allowed, _, err := authorizeAnnouncementAudience(ctx, asker.TenantID, conversation.ID, g.Documents, snapshot, documents, ids)
		if err != nil || !allowed {
			// CHATBUG-067: the refusal names the source that is not open to
			// everyone here, so the asker knows what keeps the answer private.
			return ErrPersonaShareRefused{Reason: chat.PrivateReasonAudience, Source: g.closedSource(ctx, asker.TenantID, conversation.ID, snapshot, documents)}
		}
		if g.Audience.AllowDataClass(ctx, conversation, bodyClass) != nil {
			return ErrPersonaShareRefused{Reason: chat.PrivateReasonAudience}
		}
		// The per-reader flag on a Sources line is rewritten at every read; the
		// stored text does not carry the asker's.
		posted, err = post(personaShareReadableFlag.ReplaceAllString(cleaned, ""))
		return err
	}
	if g.Fence != nil {
		err = g.Fence(ctx, asker.TenantID, ids, run)
	} else {
		err = run()
	}
	if err != nil {
		return chat.Post{}, err
	}
	return posted, nil
}

// closedSource is the title of the first cited document that, taken alone, is
// not open to every current member of the conversation; empty when no single
// document explains the refusal.
func (g *PersonaAnswerShareGate) closedSource(ctx context.Context, tenant, conversationID string, snapshot chatrecipient.AudienceSnapshot, documents []AgentAnnouncementResolvedDocument) string {
	for _, document := range documents {
		allowed, _, err := authorizeAnnouncementAudience(ctx, tenant, conversationID, g.Documents, snapshot, []AgentAnnouncementResolvedDocument{document}, []string{document.DocumentID})
		if err != nil || !allowed {
			return strings.TrimSpace(document.Title)
		}
	}
	return ""
}

// personaShareExpired is the refusal for an answer whose private card is no
// longer held: there is nothing left to post.
const personaShareExpired = "expired"

// NewPersonaAnswerShareGate composes the production check from the same floor
// and documents hub that decide whether an agent's answer is posted publicly.
// It returns nil, and sharing is unavailable, when either is missing.
func NewPersonaAnswerShareGate(floor *PersonaRuntimeAudienceFloor, hub *documenthubstore.Store) *PersonaAnswerShareGate {
	if floor == nil || hub == nil || floor.Chat == nil || floor.Personas == nil || isNilPersonaOutputPort(floor.Authority) || isNilPersonaOutputPort(floor.BodyClasses) {
		return nil
	}
	hubAuthority, ok := floor.Documents.(AgentAnnouncementHubAuthority)
	if !ok {
		return nil
	}
	gate := &PersonaAnswerShareGate{
		Profile:     floor.shareProfileGate,
		Audience:    floor.Authority,
		Documents:   WorkspaceSectionDocumentAuthority{AgentAnnouncementHubAuthority: hubAuthority},
		BodyClasses: floor.BodyClasses,
		Fence:       hubAuthority.WithDocumentReadFence,
	}
	gate.Cited = func(ctx context.Context, tenant, subject string, source personaShareSource) (AgentAnnouncementResolvedDocument, error) {
		id, err := hubAuthority.versionID(ctx, tenant, source.DocumentID, source.VersionID)
		if err != nil {
			return AgentAnnouncementResolvedDocument{}, err
		}
		version, err := hub.ReadVersion(ctx, tenant, source.DocumentID, id, "person", subject)
		if err != nil {
			return AgentAnnouncementResolvedDocument{}, err
		}
		content := version.Markdown
		if source.Anchor != "" {
			content = agentDocumentSection(version.Markdown, source.Anchor)
		}
		if strings.TrimSpace(content) == "" {
			return AgentAnnouncementResolvedDocument{}, ErrAgentAnnouncementDenied
		}
		return AgentAnnouncementResolvedDocument{DocumentID: source.DocumentID, Version: id, Title: version.Title, Digest: personaRunT0ToolOutputDigest([]byte(content)), SectionAnchor: source.Anchor}, nil
	}
	return gate
}

// shareProfileGate is the floor's own test of the agent's privacy setting,
// applied to the agent and installation the answer came from, with the
// channel's current policy.
func (f *PersonaRuntimeAudienceFloor) shareProfileGate(ctx context.Context, invocation agentinvoke.Invocation, conversation chat.Conversation) error {
	store, err := f.Personas.Scoped(values.TenantId(invocation.TenantID))
	if err != nil {
		return err
	}
	version, installation, err := store.ReadCurrentPersonaAuthority(ctx, conversation.ID, invocation.PersonaID)
	if err != nil || installation.InstallationID != invocation.InstallationID || !personaRunVersionMatches(version.Version, invocation.PersonaVersion) {
		return chat.ErrPermissionDenied
	}
	var policySource personaCurrentChannelPolicySource
	if source, ok := f.Chat.(personaCurrentChannelPolicySource); ok {
		policySource = source
	} else {
		policySource = personaPolicySourceFromAudienceFloor(f.Authority)
	}
	if current, supported, policyErr := currentPersonaChannelPolicy(ctx, policySource, invocation.TenantID, conversation.ID, installation.ChannelPolicy); supported {
		if policyErr != nil {
			return chat.ErrPermissionDenied
		}
		installation.ChannelPolicy = current
	}
	_, err = personaRuntimePublicProfile(version, installation, personaShareIdentity(invocation))
	return err
}

func personaShareIdentity(invocation agentinvoke.Invocation) agentsecurity.FinalOutputIdentity {
	return agentsecurity.FinalOutputIdentity{TenantID: invocation.TenantID, PersonaID: invocation.PersonaID, PersonaVersion: invocation.PersonaVersion, InstallationID: invocation.InstallationID, ConversationID: invocation.ConversationID, InvokerID: invocation.InvokerID}
}

// ShareAnswer posts the private card of one of the caller's own answers to the
// channel it was asked in, under the question.
func (s *PersonaChatSurface) ShareAnswer(ctx context.Context, invocationID, idempotencyKey string) (personachat.ShareResult, error) {
	if s == nil || s.Invocations == nil || s.Receipts == nil || s.Share == nil || s.Chat == nil {
		return personachat.ShareResult{}, personachat.ErrUnavailable
	}
	if !validPersonaActionInput(invocationID, idempotencyKey) {
		return personachat.ShareResult{}, personachat.ErrInvalid
	}
	p, ok := personaSurfacePrincipal(ctx)
	if !ok {
		if p != nil {
			return personachat.ShareResult{}, personachat.ErrDenied
		}
		return personachat.ShareResult{}, personachat.ErrUnauthenticated
	}
	invocation, err := s.Invocations.Lookup(ctx, p.Tenant().String(), p.Subject(), invocationID)
	if err != nil || !ownedPersonaInvocation(invocation, p.Tenant().String(), p.Subject(), invocationID) {
		return personachat.ShareResult{}, personachat.ErrDenied
	}
	_, room, err := s.member(ctx, invocation.ConversationID)
	if err != nil {
		return personachat.ShareResult{}, err
	}
	principal := chat.Principal{TenantID: room.TenantID, SubjectID: p.Subject()}
	card, err := s.privateCardFor(ctx, principal, room, invocationID)
	if err != nil {
		return personachat.ShareResult{}, err
	}
	post, err := s.Share.Share(ctx, principal, invocation, room, card, func(body string) (chat.Post, error) {
		return s.Chat.SendPost(ctx, chat.SendPostRequest{Principal: principal, TenantID: room.TenantID, ConversationID: room.ID, ParentID: invocation.ThreadID, Body: body, IdempotencyKey: "persona-share:" + invocationID})
	})
	if err != nil {
		var refused ErrPersonaShareRefused
		switch {
		case errors.As(err, &refused):
			return personachat.ShareResult{}, &personachat.FinalStateConflict{State: refused.Reason, Detail: refused.Source}
		case errors.Is(err, chat.ErrPermissionDenied), errors.Is(err, chat.ErrNotFound):
			return personachat.ShareResult{}, personachat.ErrDenied
		case errors.Is(err, chat.ErrInvalidArgument):
			return personachat.ShareResult{}, personachat.ErrInvalid
		}
		return personachat.ShareResult{}, personachat.ErrUnavailable
	}
	return personachat.ShareResult{InvocationID: invocationID, PostID: post.ID}, nil
}

// privateCardFor reads the private card the asker was delivered for this
// invocation, as the asker reads it: a source they may not open is text.
func (s *PersonaChatSurface) privateCardFor(ctx context.Context, principal chat.Principal, room chat.Conversation, invocationID string) (string, error) {
	receipts, err := s.Receipts.ListReplyReceipts(ctx, room.TenantID, principal.SubjectID, room.ID)
	if err != nil {
		return "", personachat.ErrUnavailable
	}
	cardID := ""
	for _, receipt := range receipts {
		if receipt.InvocationID == invocationID && receipt.InvokerID == principal.SubjectID && receipt.ConversationID == room.ID && receipt.EphemeralPostID != "" {
			cardID = receipt.EphemeralPostID
			break
		}
	}
	lister, ok := s.privateCards()
	if !ok {
		// The composition gave the surface no way to read private cards. That is
		// not the asker's doing and is not a refusal.
		return "", personachat.ErrUnavailable
	}
	if cardID == "" {
		return "", &personachat.FinalStateConflict{State: personaShareExpired}
	}
	var after uint64
	for {
		posts, next, err := lister.ListEphemeralPosts(ctx, chat.ListEphemeralPostsRequest{Principal: principal, TenantID: room.TenantID, ConversationID: room.ID, AfterSequence: after, PageSize: 100})
		if err != nil {
			return "", surfaceChatError(err)
		}
		for _, post := range posts {
			if post.ID == cardID && post.RecipientSubjectID == principal.SubjectID && post.RecipientHomeTenantID == principal.TenantID {
				return post.Body, nil
			}
		}
		if next <= after {
			// The card is on record and no longer held: too old to share.
			return "", &personachat.FinalStateConflict{State: personaShareExpired}
		}
		after = next
	}
}

var _ personachat.ShareSurface = (*PersonaChatSurface)(nil)
