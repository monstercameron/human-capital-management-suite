// Live audience eligibility for team/channel document placements (HUB-013),
// adapting chat's own current membership check to
// documenthubstore.AudienceEligibility without documenthubstore importing
// any chat package. GetConversation (which ListMemberships calls
// internally) is chat's own authorization for "can this principal currently
// see this conversation", so asking it about the subject themselves is
// exactly the live-eligibility recheck HUB-013 requires: a departed
// member's principal fails that check on its very next call, never on a
// cache's schedule.
package application

import (
	"context"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

// chatAudienceEligibility implements documenthubstore.AudienceEligibility
// over a live chat.ConversationService. Only scope_kind="placement" is
// meaningful here: scopeID names the team or channel conversation whose
// current membership gates the placement.
type chatAudienceEligibility struct {
	conversations chatcore.ConversationService
}

// Eligible reports whether subjectID currently belongs to the audience of
// scopeID (a team/channel conversation). Any refusal from chat --
// membership gone, conversation gone, or a store fault -- is treated as
// ineligible: a placement read must never widen access because a
// dependency failed open.
func (e chatAudienceEligibility) Eligible(ctx context.Context, tenantID, scopeKind, scopeID, subjectID string) (bool, error) {
	if e.conversations == nil || scopeID == "" || subjectID == "" {
		return false, nil
	}
	if _, err := e.conversations.ListMemberships(ctx, chatcore.ListMembershipsRequest{
		Principal:      chatcore.Principal{TenantID: tenantID, SubjectID: subjectID},
		TenantID:       tenantID,
		ConversationID: scopeID,
		Page:           chatcore.Page{PageSize: 1},
	}); err != nil {
		return false, nil
	}
	return true, nil
}

// documentAudienceEligibility builds the HUB-013 eligibility port from the
// composed chat runtime. A nil conversations service (chat not enabled)
// yields an authority that refuses every placement read, matching
// GetDocumentPlacement's own nil-authority refusal.
func documentAudienceEligibility(conversations chatcore.ConversationService) documenthubstore.AudienceEligibility {
	if conversations == nil {
		return nil
	}
	return chatAudienceEligibility{conversations: conversations}
}
