package chatrecipient

import (
	"context"
	"reflect"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// AudiencePrincipal is a resolved conversation reader. Guest and External are
// descriptive inputs from the audience owner; neither flag grants authority.
type AudiencePrincipal struct {
	TenantID  string
	SubjectID string
	Guest     bool
	External  bool
}

// AudienceSnapshot is an authoritative, versioned view taken for one delivery
// decision. Complete attests that current membership was fully enumerated;
// EligibilityComplete additionally attests that public-channel joiners were
// fully enumerated, including guests and external members.
type AudienceSnapshot struct {
	TenantID                 string
	ConversationID           string
	Revision                 uint64
	CurrentMembers           []AudiencePrincipal
	EligibilityPopulation    []AudiencePrincipal
	Complete                 bool
	EligibilityComplete      bool
	GuestAndExternalComplete bool
}

// Disclosure identifies one independently reauthorized part of a persona
// result. CitationTitle is checked as part of the same disclosure so titles
// cannot bypass field authorization.
type Disclosure struct {
	SourceID      string
	RecordID      string
	Field         string
	CitationTitle string
	DataClass     dlp.DataClass
}

// AudienceFloorAuthority owns current membership, eligibility, recipient
// authorization, and channel data-class policy. Implementations must resolve
// current state on each call; cached invocation-time audiences are not valid.
type AudienceFloorAuthority interface {
	CurrentAudience(context.Context, chat.Conversation) (AudienceSnapshot, error)
	AuthorizeDisclosure(context.Context, AudiencePrincipal, Disclosure) error
	AllowDataClass(context.Context, chat.Conversation, dlp.DataClass) error
}

// AudienceFloorRequest describes one candidate persona delivery. It carries
// no caller-selected recipient list: recipients come only from Authority.
type AudienceFloorRequest struct {
	Conversation  chat.Conversation
	AlwaysPrivate bool
	Disclosures   []Disclosure
}

// DeliveryRoute is the only delivery choice returned by the audience floor.
type DeliveryRoute string

const (
	// RoutePrivate means callers must use the invoker-only delivery path.
	RoutePrivate DeliveryRoute = "PRIVATE"
	// RoutePublic means the complete result may be considered for public commit.
	RoutePublic DeliveryRoute = "PUBLIC"
)

// AudienceFloorReason is deliberately coarse so a private receipt cannot leak
// which member, field, source, or policy caused the public route to refuse.
type AudienceFloorReason string

const (
	AudienceFloorAllowed        AudienceFloorReason = "ALLOWED"
	AudienceFloorPrivateChannel AudienceFloorReason = "PRIVATE_CHANNEL"
	AudienceFloorAlwaysPrivate  AudienceFloorReason = "ALWAYS_PRIVATE"
	AudienceFloorIncomplete     AudienceFloorReason = "INCOMPLETE_AUTHORITY"
	AudienceFloorUnauthorized   AudienceFloorReason = "NOT_AUTHORIZED_FOR_AUDIENCE"
	AudienceFloorInvalid        AudienceFloorReason = "INVALID_INPUT"
)

// AudienceFloorDecision is a fail-closed routing result. SnapshotRevision is
// the revision a commit owner must compare atomically before writing publicly.
type AudienceFloorDecision struct {
	Route            DeliveryRoute
	Reason           AudienceFloorReason
	SnapshotRevision uint64
	AudienceSize     int
}

// EvaluateAudienceFloor re-resolves the audience and checks each disclosure
// for every current reader and, on public channels, every eligible future
// joiner. Call it from the public commit boundary and require the committer to
// reject if the returned snapshot revision is no longer current. Any missing,
// malformed, stale, or failed authority routes privately.
func EvaluateAudienceFloor(ctx context.Context, request AudienceFloorRequest, authority AudienceFloorAuthority) AudienceFloorDecision {
	private := AudienceFloorDecision{Route: RoutePrivate, Reason: AudienceFloorInvalid}
	if ctx == nil || isNilAuthority(authority) || invalidConversation(request.Conversation) || len(request.Disclosures) == 0 {
		return private
	}
	for _, disclosure := range request.Disclosures {
		if !validDisclosure(disclosure) {
			return private
		}
	}
	if request.AlwaysPrivate {
		private.Reason = AudienceFloorAlwaysPrivate
		return private
	}
	if request.Conversation.Kind != chat.PublicChannel {
		private.Reason = AudienceFloorPrivateChannel
		return private
	}

	snapshot, err := authority.CurrentAudience(ctx, request.Conversation)
	if err != nil || !snapshotMatches(snapshot, request.Conversation, true) {
		private.Reason = AudienceFloorIncomplete
		return private
	}
	audience, ok := audienceSet(snapshot.CurrentMembers, snapshot.EligibilityPopulation)
	if !ok || len(audience) == 0 || len(snapshot.CurrentMembers) == 0 || len(snapshot.EligibilityPopulation) == 0 {
		private.Reason = AudienceFloorIncomplete
		return private
	}
	classes := make(map[dlp.DataClass]struct{}, len(request.Disclosures))
	for _, disclosure := range request.Disclosures {
		classes[disclosure.DataClass] = struct{}{}
	}
	for class := range classes {
		if err := authority.AllowDataClass(ctx, request.Conversation, class); err != nil {
			private.Reason = AudienceFloorUnauthorized
			return private
		}
	}
	for _, recipient := range audience {
		for _, disclosure := range request.Disclosures {
			if err := authority.AuthorizeDisclosure(ctx, recipient, disclosure); err != nil {
				private.Reason = AudienceFloorUnauthorized
				return private
			}
		}
	}
	return AudienceFloorDecision{Route: RoutePublic, Reason: AudienceFloorAllowed, SnapshotRevision: snapshot.Revision, AudienceSize: len(audience)}
}

func invalidConversation(conversation chat.Conversation) bool {
	return strings.TrimSpace(conversation.ID) == "" || strings.TrimSpace(conversation.TenantID) == "" || conversation.Revision == 0 || conversation.Archived
}

func validDisclosure(disclosure Disclosure) bool {
	return strings.TrimSpace(disclosure.SourceID) != "" && strings.TrimSpace(disclosure.RecordID) != "" && strings.TrimSpace(disclosure.Field) != "" && disclosure.DataClass.Valid()
}

func snapshotMatches(snapshot AudienceSnapshot, conversation chat.Conversation, public bool) bool {
	if snapshot.TenantID != conversation.TenantID || snapshot.ConversationID != conversation.ID || snapshot.Revision == 0 || !snapshot.Complete || !snapshot.GuestAndExternalComplete {
		return false
	}
	return !public || snapshot.EligibilityComplete
}

func audienceSet(current, eligible []AudiencePrincipal) ([]AudiencePrincipal, bool) {
	byID := make(map[string]AudiencePrincipal, len(current)+len(eligible))
	for _, principal := range append(append([]AudiencePrincipal(nil), current...), eligible...) {
		if strings.TrimSpace(principal.TenantID) == "" || strings.TrimSpace(principal.SubjectID) == "" {
			return nil, false
		}
		key := principal.TenantID + "\x00" + principal.SubjectID
		if previous, exists := byID[key]; exists {
			if previous.Guest != principal.Guest || previous.External != principal.External {
				return nil, false
			}
			continue
		}
		byID[key] = principal
	}
	out := make([]AudiencePrincipal, 0, len(byID))
	for _, principal := range byID {
		out = append(out, principal)
	}
	return out, true
}

func isNilAuthority(authority AudienceFloorAuthority) bool {
	if authority == nil {
		return true
	}
	v := reflect.ValueOf(authority)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
