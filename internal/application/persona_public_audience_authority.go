package application

import (
	"context"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type personaPublicAudienceStore interface {
	CapturePublicAudienceSnapshot(context.Context, string, string) (chatstore.PublicAudienceSnapshot, error)
	CapturePersonaChannelPolicy(context.Context, string, string, string) (chatstore.PersonaChannelPolicySnapshot, error)
	AuthorizePublicChatDisclosure(context.Context, string, string, string, string, string, string, string, string, string) error
	PublicChatDisclosureClass(context.Context, string, string, string, string) (dlp.DataClass, error)
	ReadableChatDisclosureClass(context.Context, string, string, string, string, string, string) (dlp.DataClass, error)
}

// PersonaPublicAudienceAuthority uses chat-owned admission population and
// administered policy. Its revision fences policy, eligibility, membership,
// lifecycle, and grounded source edits through the public reply commit.
type PersonaPublicAudienceAuthority struct{ Store personaPublicAudienceStore }

func NewPersonaPublicAudienceAuthority(store *chatstore.Store) *PersonaPublicAudienceAuthority {
	return &PersonaPublicAudienceAuthority{Store: store}
}

type personaPublicAudienceConversationKey struct{}
type personaPublicAudienceConversation struct{ tenantID, conversationID string }
type personaPublicAudienceSourceDigestsKey struct{}

// WithPersonaPublicAudienceSourceDigests binds per-source disclosure checks to
// citation bytes from the sealed output, including edits before snapshot read.
func WithPersonaPublicAudienceSourceDigests(ctx context.Context, digests map[string]string) context.Context {
	copy := make(map[string]string, len(digests))
	for postID, digest := range digests {
		copy[postID] = digest
	}
	return context.WithValue(ctx, personaPublicAudienceSourceDigestsKey{}, copy)
}

// WithPersonaPublicAudienceConversation binds disclosure checks to the sealed
// output's destination. The same-conversation restriction is necessary because
// one room's audience revision cannot fence another room's source changes.
func WithPersonaPublicAudienceConversation(ctx context.Context, tenantID, conversationID string) context.Context {
	return context.WithValue(ctx, personaPublicAudienceConversationKey{}, personaPublicAudienceConversation{tenantID, conversationID})
}

var _ PersonaAudienceFloorSnapshotSource = (*PersonaPublicAudienceAuthority)(nil)
var _ PersonaAudienceFloorDisclosureAuthorizer = (*PersonaPublicAudienceAuthority)(nil)
var _ PersonaAudienceFloorPolicy = (*PersonaPublicAudienceAuthority)(nil)
var _ PersonaAdminPlacementSource = (*PersonaPublicAudienceAuthority)(nil)

// PersonaPublicChatDisclosureClass returns only a trusted classifier assertion
// bound to the exact citation bytes; channel ceilings never supply its value.
func (a *PersonaPublicAudienceAuthority) PersonaPublicChatDisclosureClass(ctx context.Context, tenantID, conversationID, postID, expectedDigest string) (dlp.DataClass, error) {
	if a == nil || isNilPersonaOutputPort(a.Store) || ctx == nil {
		return "", ErrPersonaAudienceFloorUnavailable
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.Tenant().String() != tenantID {
		return "", ErrPersonaAudienceFloorUnavailable
	}
	class, err := a.Store.ReadableChatDisclosureClass(ctx, tenantID, principal.Tenant().String(), principal.Subject(), conversationID, postID, expectedDigest)
	if err != nil || !class.Valid() {
		return "", ErrPersonaAudienceFloorUnavailable
	}
	return class, nil
}

func (a *PersonaPublicAudienceAuthority) ReadPersonaAudienceFloorSnapshot(ctx context.Context, conversation chat.Conversation) (PersonaAudienceFloorSnapshot, error) {
	if a == nil || isNilPersonaOutputPort(a.Store) || ctx == nil || !validFloorConversation(conversation) || conversation.Kind != chat.PublicChannel {
		return PersonaAudienceFloorSnapshot{}, ErrPersonaAudienceFloorUnavailable
	}
	snapshot, err := a.Store.CapturePublicAudienceSnapshot(ctx, conversation.TenantID, conversation.ID)
	if err != nil || snapshot.TenantID != conversation.TenantID || snapshot.ConversationID != conversation.ID || snapshot.Revision == 0 || snapshot.PolicyRevision == 0 || len(snapshot.Current) == 0 || len(snapshot.Eligible) == 0 {
		return PersonaAudienceFloorSnapshot{}, ErrPersonaAudienceFloorUnavailable
	}
	policy, err := a.Store.CapturePersonaChannelPolicy(ctx, conversation.TenantID, conversation.ID, "")
	if err != nil || policy.TenantID != conversation.TenantID || policy.ConversationID != conversation.ID || policy.Kind != string(chat.PublicChannel) || policy.PolicyRevision <= 0 || policy.Revision != snapshot.Revision || !slices.Contains(policy.Policy.AllowedChannelClasses, string(agentpersonastore.ConversationPublic)) {
		return PersonaAudienceFloorSnapshot{}, ErrPersonaAudienceFloorUnavailable
	}
	out := PersonaAudienceFloorSnapshot{Revision: snapshot.Revision, CurrentComplete: true, EligibilityComplete: true, GuestExternalComplete: true, FenceComplete: true}
	convert := func(members []chatstore.PublicAudiencePrincipal) []chatrecipient.AudiencePrincipal {
		out := make([]chatrecipient.AudiencePrincipal, 0, len(members))
		for _, member := range members {
			out = append(out, chatrecipient.AudiencePrincipal{TenantID: member.HomeTenantID, SubjectID: member.SubjectID, Guest: member.Guest, External: member.HomeTenantID != conversation.TenantID})
		}
		return out
	}
	out.CurrentMembers, out.EligibleFutureMembers = convert(snapshot.Current), convert(snapshot.Eligible)
	if !validFloorSnapshot(out, conversation) || !consistentPersonaRunAudience(out.CurrentMembers, out.EligibleFutureMembers) {
		return PersonaAudienceFloorSnapshot{}, ErrPersonaAudienceFloorUnavailable
	}
	return out, nil
}

func (a *PersonaPublicAudienceAuthority) AuthorizePersonaAudienceDisclosure(ctx context.Context, recipient chatrecipient.AudiencePrincipal, disclosure chatrecipient.Disclosure) error {
	if a == nil || isNilPersonaOutputPort(a.Store) || ctx == nil || !validAudiencePrincipal(recipient) || !validAudienceDisclosure(disclosure) || disclosure.SourceID != "chat:"+disclosure.RecordID {
		return ErrPersonaAudienceFloorUnavailable
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return ErrPersonaAudienceFloorUnavailable
	}
	conversation, ok := ctx.Value(personaPublicAudienceConversationKey{}).(personaPublicAudienceConversation)
	if !ok || conversation.tenantID != principal.Tenant().String() || strings.TrimSpace(conversation.conversationID) == "" {
		return ErrPersonaAudienceFloorUnavailable
	}
	digests, ok := ctx.Value(personaPublicAudienceSourceDigestsKey{}).(map[string]string)
	if !ok || strings.TrimSpace(digests[disclosure.RecordID]) == "" {
		return ErrPersonaAudienceFloorUnavailable
	}
	if err := a.Store.AuthorizePublicChatDisclosure(ctx, principal.Tenant().String(), conversation.conversationID, recipient.TenantID, recipient.SubjectID, disclosure.RecordID, digests[disclosure.RecordID], disclosure.Field, disclosure.CitationTitle, string(disclosure.DataClass)); err != nil {
		return ErrPersonaAudienceFloorUnavailable
	}
	return nil
}

func (a *PersonaPublicAudienceAuthority) AllowPersonaAudienceDataClass(ctx context.Context, conversation chat.Conversation, class dlp.DataClass) error {
	if a == nil || isNilPersonaOutputPort(a.Store) || ctx == nil || !validFloorConversation(conversation) || (class != dlp.ClassPublic && class != dlp.ClassInternal) {
		return ErrPersonaAudienceFloorUnavailable
	}
	policy, err := a.Store.CapturePersonaChannelPolicy(ctx, conversation.TenantID, conversation.ID, "")
	if err != nil || policy.TenantID != conversation.TenantID || policy.ConversationID != conversation.ID || policy.Kind != string(chat.PublicChannel) || policy.PolicyRevision <= 0 || policy.Policy.AlwaysPrivate || !slices.Contains(policy.Policy.AllowedDataClasses, string(class)) || !slices.Contains(policy.Policy.AllowedChannelClasses, string(agentpersonastore.ConversationPublic)) {
		return ErrPersonaAudienceFloorUnavailable
	}
	return nil
}

func (a *PersonaPublicAudienceAuthority) ResolvePersonaAdminPlacement(ctx context.Context, actor PersonaAdminCommandActor, conversationID string) (PersonaAdminPlacementFacts, error) {
	if a == nil || isNilPersonaOutputPort(a.Store) || ctx == nil || !personaAdminActorBound(ctx, actor) || strings.TrimSpace(conversationID) == "" {
		return PersonaAdminPlacementFacts{}, ErrPersonaAdminLifecycleUnavailable
	}
	snapshot, err := a.Store.CapturePersonaChannelPolicy(ctx, actor.Tenant.String(), conversationID, actor.Subject)
	if err != nil || snapshot.TenantID != actor.Tenant.String() || snapshot.ConversationID != conversationID || snapshot.Revision == 0 || snapshot.PolicyRevision <= 0 || snapshot.ManagerID != actor.Subject {
		return PersonaAdminPlacementFacts{}, ErrPersonaAdminLifecycleUnavailable
	}
	var class agentpersonastore.ConversationClass
	switch snapshot.Kind {
	case string(chat.PublicChannel):
		class = agentpersonastore.ConversationPublic
	case string(chat.PrivateChannel):
		class = agentpersonastore.ConversationPrivate
	case string(chat.Group):
		class = agentpersonastore.ConversationGroupDM
	case string(chat.Direct):
		class = agentpersonastore.ConversationOneToOne
	default:
		return PersonaAdminPlacementFacts{}, ErrPersonaAdminLifecycleUnavailable
	}
	policy := snapshot.Policy
	classes := make([]agentpersonastore.ConversationClass, 0, len(policy.AllowedChannelClasses))
	for _, allowed := range policy.AllowedChannelClasses {
		classes = append(classes, agentpersonastore.ConversationClass(allowed))
	}
	return PersonaAdminPlacementFacts{Tenant: actor.Tenant, ConversationID: conversationID, Class: class, PlacementClass: policy.PlacementClass, ManagerID: snapshot.ManagerID, Revision: snapshot.Revision, ExternalMembers: snapshot.ExternalMembers || snapshot.GuestMembers, CrossCompanyMembers: snapshot.ExternalMembers, Policy: agentpersonastore.ChannelPolicy{PlacementClass: policy.PlacementClass, MaxTier: policy.MaxTier, AllowedDataClasses: slices.Clone(policy.AllowedDataClasses), AlwaysPrivate: policy.AlwaysPrivate, ConversationSearchAllowed: policy.ConversationSearchAllowed, AllowedChannelClasses: classes, AllowExternalMembers: policy.AllowExternalMembers, AllowCrossCompanyMembers: policy.AllowCrossCompanyMembers}}, nil
}
