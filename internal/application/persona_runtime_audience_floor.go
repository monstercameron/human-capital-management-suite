package application

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// PersonaRuntimeAudienceFloor maps opaque, sealed chat source disclosures to
// Chat's complete current/future audience check. Unknown business material or
// non-chat sources cannot be posted publicly by this adapter.
type PersonaRuntimeAudienceFloor struct {
	Chat        chat.ConversationService
	Personas    *agentpersonastore.Store
	Authority   chatrecipient.AudienceFloorAuthority
	Classes     PersonaPublicChatDisclosureClassificationSource
	BodyClasses PersonaPublicReplyTextClassificationSource
}

// PersonaPublicChatDisclosureClassificationSource resolves current source-owner
// classification for the exact cited bytes. A room's allowed class is not source
// classification; missing or edited source evidence refuses public disclosure.
type PersonaPublicChatDisclosureClassificationSource interface {
	PersonaPublicChatDisclosureClass(context.Context, string, string, string, string) (dlp.DataClass, error)
}

// PersonaPublicReplyTextClassificationSource inspects the generated text in
// addition to the independently classified sources. Missing body authority or
// a restricted finding prevents public delivery.
type PersonaPublicReplyTextClassificationSource interface {
	ClassifyPersonaReplyText(context.Context, string, string) (dlp.DataClass, error)
}

func (f *PersonaRuntimeAudienceFloor) AuthorizePersonaOutput(ctx context.Context, output agentsecurity.FinalOutputPersistence) (chat.PersonaAudienceDecision, error) {
	if f == nil || ctx == nil || f.Chat == nil || f.Personas == nil || isNilPersonaOutputPort(f.Authority) || isNilPersonaOutputPort(f.Classes) || isNilPersonaOutputPort(f.BodyClasses) || output.Digest() == "" {
		return chat.PersonaAudienceDecision{}, ErrPersonaAudienceFloorUnavailable
	}
	id := output.Identity()
	principal, ok := personaRunChatPrincipal(ctx, id.TenantID, id.InvokerID)
	if !ok {
		return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
	}
	conversation, err := f.Chat.GetConversation(ctx, chat.GetConversationRequest{Principal: principal, TenantID: id.TenantID, ConversationID: id.ConversationID})
	if err != nil {
		return chat.PersonaAudienceDecision{}, err
	}
	store, err := f.Personas.Scoped(values.TenantId(id.TenantID))
	if err != nil {
		return chat.PersonaAudienceDecision{}, err
	}
	version, installation, err := store.ReadCurrentPersonaAuthority(ctx, id.ConversationID, id.PersonaID)
	if err != nil || installation.InstallationID != id.InstallationID || !personaRunVersionMatches(version.Version, id.PersonaVersion) {
		return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
	}
	profile, err := personaRuntimePublicProfile(version, installation, id)
	if err != nil {
		return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
	}
	draft, answer, err := output.Payload()
	if err != nil {
		return chat.PersonaAudienceDecision{}, err
	}
	parts := make([]string, 0, len(answer.Parts))
	for _, part := range answer.Parts {
		if strings.TrimSpace(part.Text) != "" {
			parts = append(parts, strings.TrimSpace(part.Text))
		}
	}
	body := strings.Join(parts, "\n\n")
	if body == "" {
		body = strings.TrimSpace(draft.Narrative)
	}
	bodyClass, err := personaRuntimePublicBodyClass(ctx, f.BodyClasses, id.TenantID, body)
	if err != nil {
		return chat.PersonaAudienceDecision{}, err
	}
	for _, material := range output.Materials() {
		if material.Kind != agentsecurity.OutputSource || !strings.HasPrefix(material.ID, "chat:") {
			return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
		}
	}
	citations := output.Citations()
	if len(citations) == 0 {
		return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
	}
	disclosures := make([]chatrecipient.Disclosure, 0, len(citations))
	digests := make(map[string]string, len(citations))
	for _, citation := range citations {
		postID := strings.TrimPrefix(citation.SourceID, "chat:")
		if postID == citation.SourceID || !required(postID) || citation.Location != "conversation:"+id.ConversationID+"/"+postID || !personaRequestDigest(citation.Digest) {
			return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
		}
		if prior, ok := digests[postID]; ok && prior != citation.Digest {
			return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
		}
		digests[postID] = citation.Digest
		class, err := f.Classes.PersonaPublicChatDisclosureClass(ctx, id.TenantID, id.ConversationID, postID, citation.Digest)
		if err != nil || (class != dlp.ClassPublic && class != dlp.ClassInternal) {
			return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
		}
		disclosures = append(disclosures, chatrecipient.Disclosure{SourceID: citation.SourceID, RecordID: postID, Field: "body", DataClass: class})
	}
	ctx = WithPersonaPublicAudienceConversation(ctx, id.TenantID, id.ConversationID)
	ctx = WithPersonaPublicAudienceSourceDigests(ctx, digests)
	decision := chatrecipient.EvaluateAudienceFloor(ctx, chatrecipient.AudienceFloorRequest{Conversation: conversation, AlwaysPrivate: profile.AlwaysPrivate || installation.ChannelPolicy.AlwaysPrivate, Disclosures: disclosures}, f.Authority)
	if decision.Route != chatrecipient.RoutePublic || decision.SnapshotRevision == 0 {
		return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
	}
	if err := f.Authority.AllowDataClass(ctx, conversation, bodyClass); err != nil {
		return chat.PersonaAudienceDecision{}, chat.ErrPermissionDenied
	}
	return chat.PersonaAudienceDecision{Revision: decision.SnapshotRevision, Body: body, ParentID: id.ThreadID}, nil
}

func personaRuntimePublicBodyClass(ctx context.Context, source PersonaPublicReplyTextClassificationSource, tenant, body string) (dlp.DataClass, error) {
	if ctx == nil || isNilPersonaOutputPort(source) || !required(tenant) || strings.TrimSpace(body) == "" {
		return "", ErrPersonaAudienceFloorUnavailable
	}
	class, err := source.ClassifyPersonaReplyText(ctx, tenant, body)
	if err != nil || (class != dlp.ClassPublic && class != dlp.ClassInternal) {
		return "", chat.ErrPermissionDenied
	}
	return class, nil
}

func personaRuntimePublicProfile(version agentpersonastore.PersonaVersion, installation agentpersonastore.ActiveInstallation, id agentsecurity.FinalOutputIdentity) (agentpersona.PersonaProfile, error) {
	var profile agentpersona.PersonaProfile
	if version.TenantID.String() != id.TenantID || version.PersonaID != id.PersonaID || !personaRunVersionMatches(version.Version, id.PersonaVersion) ||
		installation.InstallationID != id.InstallationID || installation.ConversationID != id.ConversationID || installation.PersonaID != id.PersonaID || !personaRunVersionMatches(installation.PersonaVersion, id.PersonaVersion) ||
		json.Unmarshal(version.Profile, &profile) != nil || profile.PersonaID != id.PersonaID || !personaRunVersionMatches(int64(profile.Version), id.PersonaVersion) {
		return agentpersona.PersonaProfile{}, chat.ErrPermissionDenied
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != version.ContentDigest || profile.AlwaysPrivate || installation.ChannelPolicy.AlwaysPrivate {
		return agentpersona.PersonaProfile{}, chat.ErrPermissionDenied
	}
	return profile, nil
}

var _ chat.PersonaAudienceFloor = (*PersonaRuntimeAudienceFloor)(nil)
