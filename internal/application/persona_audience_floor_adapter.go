package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// ErrPersonaAudienceFloorUnavailable identifies missing or incomplete
// authority needed to make a public persona delivery decision. Callers must
// route privately when this error is returned.
var ErrPersonaAudienceFloorUnavailable = errors.New("persona audience floor: unavailable")

// PersonaAudienceFloorSnapshot is the one read used by the production floor
// adapter. The source must obtain all fields from one authoritative read at
// Revision. In particular, EligibleFutureMembers must be an explicit public
// channel eligibility population; it must never be inferred from membership,
// roles, tenant IDs, or worker existence.
type PersonaAudienceFloorSnapshot struct {
	Revision              uint64
	CurrentMembers        []chatrecipient.AudiencePrincipal
	EligibleFutureMembers []chatrecipient.AudiencePrincipal
	CurrentComplete       bool
	EligibilityComplete   bool
	GuestExternalComplete bool
	// FenceComplete attests that Revision fences membership, future public
	// eligibility, and channel policy through the eventual public commit.
	// chat's membership-only revision is insufficient without this attestation.
	FenceComplete bool
}

// PersonaAudienceFloorSnapshotSource reads current audience state. A source
// must read the chat audience revision and all population rows under the same
// authoritative fence, or return an error.
type PersonaAudienceFloorSnapshotSource interface {
	ReadPersonaAudienceFloorSnapshot(context.Context, chat.Conversation) (PersonaAudienceFloorSnapshot, error)
}

// PersonaAudienceFloorDisclosureAuthorizer reauthorizes one disclosure for a
// particular audience principal against current record and field authority.
type PersonaAudienceFloorDisclosureAuthorizer interface {
	AuthorizePersonaAudienceDisclosure(context.Context, chatrecipient.AudiencePrincipal, chatrecipient.Disclosure) error
}

// PersonaAudienceFloorPolicy evaluates the channel's current data-class
// ceiling. Implementations must bind this decision to the same revision fence
// returned by the snapshot source.
type PersonaAudienceFloorPolicy interface {
	AllowPersonaAudienceDataClass(context.Context, chat.Conversation, dlp.DataClass) error
}

// PersonaAudienceFloorAdapter adapts authoritative application ports to the
// chatrecipient audience-floor authority. Missing ports and incomplete fence
// coverage fail closed.
type PersonaAudienceFloorAdapter struct {
	Snapshot      PersonaAudienceFloorSnapshotSource
	Disclosure    PersonaAudienceFloorDisclosureAuthorizer
	ChannelPolicy PersonaAudienceFloorPolicy
}

var _ chatrecipient.AudienceFloorAuthority = (*PersonaAudienceFloorAdapter)(nil)

// NewPersonaAudienceFloorAdapter constructs a fail-closed audience authority.
// The source must provide the exact chat audience revision and attest that the
// revision also fences future eligibility and policy at public commit.
func NewPersonaAudienceFloorAdapter(snapshot PersonaAudienceFloorSnapshotSource, disclosure PersonaAudienceFloorDisclosureAuthorizer, policy PersonaAudienceFloorPolicy) *PersonaAudienceFloorAdapter {
	return &PersonaAudienceFloorAdapter{Snapshot: snapshot, Disclosure: disclosure, ChannelPolicy: policy}
}

// CurrentAudience resolves current members and the complete future population
// for a public channel. Missing guest/external coverage or a membership-only
// fence returns an error, so EvaluateAudienceFloor routes privately.
func (a *PersonaAudienceFloorAdapter) CurrentAudience(ctx context.Context, conversation chat.Conversation) (chatrecipient.AudienceSnapshot, error) {
	if a == nil || a.Snapshot == nil || ctx == nil || !validFloorConversation(conversation) {
		return chatrecipient.AudienceSnapshot{}, ErrPersonaAudienceFloorUnavailable
	}
	snapshot, err := a.Snapshot.ReadPersonaAudienceFloorSnapshot(ctx, conversation)
	if err != nil {
		return chatrecipient.AudienceSnapshot{}, fmt.Errorf("%w: read snapshot: %v", ErrPersonaAudienceFloorUnavailable, err)
	}
	if !validFloorSnapshot(snapshot, conversation) {
		return chatrecipient.AudienceSnapshot{}, ErrPersonaAudienceFloorUnavailable
	}
	return chatrecipient.AudienceSnapshot{
		TenantID: conversation.TenantID, ConversationID: conversation.ID, Revision: snapshot.Revision,
		CurrentMembers:        cloneAudiencePrincipals(snapshot.CurrentMembers),
		EligibilityPopulation: cloneAudiencePrincipals(snapshot.EligibleFutureMembers),
		Complete:              snapshot.CurrentComplete, EligibilityComplete: snapshot.EligibilityComplete,
		GuestAndExternalComplete: snapshot.GuestExternalComplete,
	}, nil
}

// AuthorizeDisclosure delegates current record, field, and citation-title
// authorization. Unknown or malformed disclosures fail before delegation.
func (a *PersonaAudienceFloorAdapter) AuthorizeDisclosure(ctx context.Context, principal chatrecipient.AudiencePrincipal, disclosure chatrecipient.Disclosure) error {
	if a == nil || a.Disclosure == nil || ctx == nil || !validAudiencePrincipal(principal) || !validAudienceDisclosure(disclosure) {
		return ErrPersonaAudienceFloorUnavailable
	}
	if err := a.Disclosure.AuthorizePersonaAudienceDisclosure(ctx, principal, disclosure); err != nil {
		return fmt.Errorf("%w: disclosure authorization: %v", ErrPersonaAudienceFloorUnavailable, err)
	}
	return nil
}

// AllowDataClass delegates the current channel policy. Unknown classes and
// malformed conversations fail closed before the policy port is consulted.
func (a *PersonaAudienceFloorAdapter) AllowDataClass(ctx context.Context, conversation chat.Conversation, class dlp.DataClass) error {
	if a == nil || a.ChannelPolicy == nil || ctx == nil || !validFloorConversation(conversation) || !class.Valid() {
		return ErrPersonaAudienceFloorUnavailable
	}
	if err := a.ChannelPolicy.AllowPersonaAudienceDataClass(ctx, conversation, class); err != nil {
		return fmt.Errorf("%w: channel policy: %v", ErrPersonaAudienceFloorUnavailable, err)
	}
	return nil
}

func validFloorConversation(conversation chat.Conversation) bool {
	return strings.TrimSpace(conversation.ID) != "" && strings.TrimSpace(conversation.TenantID) != "" && conversation.Revision > 0 && !conversation.Archived
}

func validFloorSnapshot(snapshot PersonaAudienceFloorSnapshot, conversation chat.Conversation) bool {
	if snapshot.Revision == 0 || !snapshot.CurrentComplete || !snapshot.EligibilityComplete || !snapshot.GuestExternalComplete || !snapshot.FenceComplete || conversation.Kind != chat.PublicChannel {
		return false
	}
	if len(snapshot.CurrentMembers) == 0 || len(snapshot.EligibleFutureMembers) == 0 {
		return false
	}
	return validAudiencePrincipals(snapshot.CurrentMembers) && validAudiencePrincipals(snapshot.EligibleFutureMembers)
}

func validAudiencePrincipals(principals []chatrecipient.AudiencePrincipal) bool {
	seen := make(map[string]struct{}, len(principals))
	for _, principal := range principals {
		if !validAudiencePrincipal(principal) {
			return false
		}
		key := principal.TenantID + "\x00" + principal.SubjectID
		if _, ok := seen[key]; ok {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func validAudiencePrincipal(principal chatrecipient.AudiencePrincipal) bool {
	return strings.TrimSpace(principal.TenantID) != "" && strings.TrimSpace(principal.SubjectID) != ""
}

func validAudienceDisclosure(disclosure chatrecipient.Disclosure) bool {
	return strings.TrimSpace(disclosure.SourceID) != "" && strings.TrimSpace(disclosure.RecordID) != "" && strings.TrimSpace(disclosure.Field) != "" && disclosure.DataClass.Valid()
}

func cloneAudiencePrincipals(principals []chatrecipient.AudiencePrincipal) []chatrecipient.AudiencePrincipal {
	return append([]chatrecipient.AudiencePrincipal(nil), principals...)
}
