package chatrecipient

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// SurfaceKind names a non-chat surface whose answers the audience floor
// routes: a workflow case page or a documentation-hub document.
type SurfaceKind string

const (
	// SurfaceWorkflowCase is the page of one workflow case; its audience
	// is the case participants.
	SurfaceWorkflowCase SurfaceKind = "WORKFLOW_CASE"
	// SurfaceHubDocument is one documentation-hub document; its audience
	// is the document readers.
	SurfaceHubDocument SurfaceKind = "HUB_DOCUMENT"
)

// Valid reports whether the kind is a known surface.
func (k SurfaceKind) Valid() bool {
	return k == SurfaceWorkflowCase || k == SurfaceHubDocument
}

// SurfaceRef identifies one versioned surface resource under discussion.
// Revision is the caller's view; the evaluator returns the authority's
// current revision for the committer to compare atomically, as on chat.
type SurfaceRef struct {
	TenantID   string
	Kind       SurfaceKind
	ResourceID string
	Revision   uint64
	Archived   bool
}

// SurfaceAudienceSource is the optional surface half of an audience-floor
// authority. Disclosure authorization stays on AudienceFloorAuthority, so
// chat, workflow pages and documents share one evaluator and one
// reauthorization rule; only the audience read and the data-class policy
// differ by surface.
type SurfaceAudienceSource interface {
	CurrentSurfaceAudience(context.Context, SurfaceRef) (AudienceSnapshot, error)
	AllowSurfaceDataClass(context.Context, SurfaceRef, dlp.DataClass) error
}

func invalidSurface(surface SurfaceRef) bool {
	return strings.TrimSpace(surface.TenantID) == "" || strings.TrimSpace(surface.ResourceID) == "" ||
		surface.Revision == 0 || surface.Archived || !surface.Kind.Valid()
}

func surfaceSnapshotMatches(snapshot AudienceSnapshot, surface SurfaceRef) bool {
	return snapshot.Surface.TenantID == surface.TenantID && snapshot.Surface.Kind == surface.Kind &&
		snapshot.Surface.ResourceID == surface.ResourceID && snapshot.Revision != 0 &&
		snapshot.Complete && snapshot.GuestAndExternalComplete
}

func isZeroConversation(conversation chat.Conversation) bool {
	return conversation == (chat.Conversation{})
}

// evaluateSurfaceAudienceFloor routes one candidate persona delivery on a
// workflow case or hub document. Surfaces have no eligible-future-joiner
// population, so the audience is exactly the current readers; every other
// rule — disclosure reauthorization for every reader, data-class policy,
// fail-closed revision pinning — is the shared one.
func evaluateSurfaceAudienceFloor(ctx context.Context, request AudienceFloorRequest, authority AudienceFloorAuthority) AudienceFloorDecision {
	private := AudienceFloorDecision{Route: RoutePrivate, Reason: AudienceFloorInvalid}
	if request.Surface == nil || invalidSurface(*request.Surface) || !isZeroConversation(request.Conversation) {
		return private
	}
	if request.AlwaysPrivate {
		private.Reason = AudienceFloorAlwaysPrivate
		return private
	}
	source, ok := authority.(SurfaceAudienceSource)
	if !ok {
		private.Reason = AudienceFloorIncomplete
		return private
	}
	snapshot, err := source.CurrentSurfaceAudience(ctx, *request.Surface)
	if err != nil || !surfaceSnapshotMatches(snapshot, *request.Surface) {
		private.Reason = AudienceFloorIncomplete
		return private
	}
	audience, ok := audienceSet(snapshot.CurrentMembers, nil)
	if !ok || len(audience) == 0 || len(snapshot.CurrentMembers) == 0 {
		private.Reason = AudienceFloorIncomplete
		return private
	}
	if reason := authorizeFloorDisclosures(ctx, request.Disclosures, audience,
		func(ctx context.Context, class dlp.DataClass) error {
			return source.AllowSurfaceDataClass(ctx, *request.Surface, class)
		},
		func(ctx context.Context, recipient AudiencePrincipal, disclosure Disclosure) error {
			return authority.AuthorizeDisclosure(ctx, recipient, disclosure)
		}); reason != "" {
		private.Reason = reason
		return private
	}
	return AudienceFloorDecision{Route: RoutePublic, Reason: AudienceFloorAllowed, SnapshotRevision: snapshot.Revision, AudienceSize: len(audience)}
}

// authorizeFloorDisclosures checks every data class against the channel or
// surface policy and reauthorizes every disclosure for every audience
// member. It returns "" when the public route is allowed.
func authorizeFloorDisclosures(ctx context.Context, disclosures []Disclosure, audience []AudiencePrincipal, allowClass func(context.Context, dlp.DataClass) error, authorize func(context.Context, AudiencePrincipal, Disclosure) error) AudienceFloorReason {
	classes := make(map[dlp.DataClass]struct{}, len(disclosures))
	for _, disclosure := range disclosures {
		classes[disclosure.DataClass] = struct{}{}
	}
	for class := range classes {
		if err := allowClass(ctx, class); err != nil {
			return AudienceFloorUnauthorized
		}
	}
	for _, recipient := range audience {
		for _, disclosure := range disclosures {
			if err := authorize(ctx, recipient, disclosure); err != nil {
				return AudienceFloorUnauthorized
			}
		}
	}
	return ""
}
