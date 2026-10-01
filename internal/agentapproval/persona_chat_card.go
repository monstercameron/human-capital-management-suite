package agentapproval

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
)

const PersonaChatCardLifetime = 15 * time.Minute

const (
	personaChatCardKind = "agent_action_approval"
	personaChatOpenTask = "open_task_view"
)

var (
	ErrChatCardInvalid     = errors.New("agentapproval: invalid persona chat card")
	ErrChatCardDenied      = errors.New("agentapproval: persona chat card denied")
	ErrChatCardStale       = errors.New("agentapproval: persona chat card is stale")
	ErrChatCardExpired     = errors.New("agentapproval: persona chat card expired")
	ErrChatApprovalSurface = errors.New("agentapproval: chat cannot decide an approval")
)

// PersonaChatCard is a recipient-bound, short-lived rendering of an
// AgentActionApproval. It carries no approval authority: AGENT2-006 currently
// accepts decisions only from the product task view.
type PersonaChatCard struct {
	ApprovalID   string
	InvocationID string
	InvokerID    string
	Digest       string
	IssuedAt     time.Time
	ExpiresAt    time.Time
	TaskViewHref string
	Card         chatapps.Card
}

// PersonaChatCardCallback is server-resolved callback context. ActorID,
// InvocationID and CurrentApproval must come from authenticated current state,
// never from a reaction, reply or persona output.
type PersonaChatCardCallback struct {
	Action       string
	ActorID      string
	InvocationID string
	Now          time.Time
	Current      AgentActionApproval
}

// BuildPersonaChatCard projects a pending AGENT2-006 approval into a typed
// CHAT-041 payload for one invocation and its assigned user. T4, high-risk,
// step-up and batch cards are link-only; all cards currently route through the
// product task view because AGENT2-006 does not authorize chat decisions.
func BuildPersonaChatCard(approval AgentActionApproval, invocationID, invokerID string, issuedAt time.Time) (PersonaChatCard, error) {
	if strings.TrimSpace(invocationID) == "" || strings.TrimSpace(invokerID) == "" || issuedAt.IsZero() ||
		approval.ID == "" || approval.AssignedUserID != invokerID || approval.State != StatePending ||
		approval.Digest == "" || Digest(approval) != approval.Digest || len(approval.Items) == 0 {
		return PersonaChatCard{}, ErrChatCardInvalid
	}
	issuedAt = issuedAt.UTC()
	expiresAt := issuedAt.Add(PersonaChatCardLifetime)
	if approval.ExpiresAt.Before(expiresAt) {
		expiresAt = approval.ExpiresAt
	}
	if approval.TaskExpiresAt.Before(expiresAt) {
		expiresAt = approval.TaskExpiresAt
	}
	if !expiresAt.After(issuedAt) {
		return PersonaChatCard{}, ErrChatCardExpired
	}
	query := url.Values{"task": []string{approval.TaskID}}
	href := "/workspace/app/agents?" + query.Encode()
	actions, changes, sources, uncertainties := personaCardDetails(approval.Items)
	card := PersonaChatCard{
		ApprovalID: approval.ID, InvocationID: invocationID, InvokerID: invokerID,
		Digest: approval.Digest, IssuedAt: issuedAt, ExpiresAt: expiresAt,
		TaskViewHref: href,
		Card: chatapps.Card{Kind: personaChatCardKind, Values: map[string]string{
			"approval_id":      approval.ID,
			"invocation_id":    invocationID,
			"invoker_id":       invokerID,
			"item_digest":      approval.Digest,
			"expires_at":       expiresAt.Format(time.RFC3339),
			"task_view":        href,
			"route":            personaChatOpenTask,
			"review_reason":    chatReviewReason(approval),
			"actions":          actions,
			"material_changes": changes,
			"sources":          sources,
			"uncertainty":      uncertainties,
		}},
	}
	return card, nil
}

// PersonaChatCardManifest returns the server-owned CHAT-041 schema for this
// projection. RenderCard validates the payload and escapes every value.
func PersonaChatCardManifest() chatapps.Manifest {
	return chatapps.Manifest{
		AppID: "hcm-agent-approval", Version: 1,
		Cards: []chatapps.CardType{{Kind: personaChatCardKind, Fields: []chatapps.Field{
			{Name: "approval_id", Type: "string", Required: true},
			{Name: "invocation_id", Type: "string", Required: true},
			{Name: "invoker_id", Type: "string", Required: true},
			{Name: "item_digest", Type: "string", Required: true},
			{Name: "expires_at", Type: "string", Required: true},
			{Name: "task_view", Type: "string", Required: true},
			{Name: "route", Type: "string", Required: true},
			{Name: "review_reason", Type: "string", Required: true},
			{Name: "actions", Type: "string", Required: true},
			{Name: "material_changes", Type: "string", Required: true},
			{Name: "sources", Type: "string", Required: true},
			{Name: "uncertainty", Type: "string", Required: true},
		}}},
	}
}

// RenderPersonaChatCardForViewer produces no markup for anyone except the
// bound invoker. Transport must still deliver this only as an AGENTP-011
// ephemeral post; this check is an additional rendering boundary.
func RenderPersonaChatCardForViewer(card PersonaChatCard, viewerID string) (string, error) {
	if viewerID == "" || viewerID != card.InvokerID {
		return "", nil
	}
	return chatapps.RenderCard(PersonaChatCardManifest(), card.Card)
}

// ResolvePersonaChatCardCallback validates the card binding and returns only
// its task-view link. An "approve" callback is always refused until the
// AGENT2-006 contract explicitly admits a chat decision surface.
func ResolvePersonaChatCardCallback(card PersonaChatCard, callback PersonaChatCardCallback) (string, error) {
	if callback.Action != personaChatOpenTask {
		if callback.Action == "approve" {
			return "", ErrChatApprovalSurface
		}
		return "", ErrChatCardDenied
	}
	if callback.ActorID == "" || callback.ActorID != card.InvokerID || callback.InvocationID != card.InvocationID {
		return "", ErrChatCardDenied
	}
	if callback.Now.IsZero() || callback.Now.Before(card.IssuedAt) || !callback.Now.Before(card.ExpiresAt) {
		return "", ErrChatCardExpired
	}
	current := callback.Current
	if current.ID != card.ApprovalID || current.AssignedUserID != card.InvokerID || current.State != StatePending ||
		current.Digest != card.Digest || Digest(current) != card.Digest {
		return "", ErrChatCardStale
	}
	expected, err := BuildPersonaChatCard(current, card.InvocationID, card.InvokerID, card.IssuedAt)
	if err != nil || !samePersonaChatCard(card, expected) {
		return "", ErrChatCardInvalid
	}
	return card.TaskViewHref, nil
}

func chatReviewReason(approval AgentActionApproval) string {
	if len(approval.Items) > 1 {
		return "batch_requires_task_view"
	}
	if approvalNeedsStepUp(approval) {
		return "step_up_requires_task_view"
	}
	for _, item := range approval.Items {
		if item.Tier == TierExternalWrite {
			return "external_write_requires_task_view"
		}
	}
	return "product_approval_surface_required"
}

func personaCardDetails(items []ActionItem) (string, string, string, string) {
	actions := make([]string, 0, len(items))
	changes := make([]string, 0)
	sources := make([]string, 0)
	uncertainties := make([]string, 0, len(items))
	for _, item := range items {
		action := item.ID + ": "
		if item.Kind == ItemGovernedIntent {
			action += item.IntentDefinitionID + " " + item.IntentDefinitionVersion
		} else {
			action += item.ConnectionID + " / " + item.ConnectionOperation
		}
		actions = append(actions, action)
		uncertainties = append(uncertainties, item.ID+": "+item.Uncertainty)
		for _, field := range item.MaterialFields {
			changes = append(changes, item.ID+": "+field.Path+" | "+field.Before+" → "+field.After)
		}
		for _, source := range item.Sources {
			sources = append(sources, item.ID+": "+source.Ref+" ("+source.Taint+")")
		}
	}
	return strings.Join(actions, "; "), strings.Join(changes, "; "), strings.Join(sources, "; "), strings.Join(uncertainties, "; ")
}

func samePersonaChatCard(left, right PersonaChatCard) bool {
	if left.ApprovalID != right.ApprovalID || left.InvocationID != right.InvocationID || left.InvokerID != right.InvokerID ||
		left.Digest != right.Digest || !left.IssuedAt.Equal(right.IssuedAt) || !left.ExpiresAt.Equal(right.ExpiresAt) || left.TaskViewHref != right.TaskViewHref || left.Card.Kind != right.Card.Kind || len(left.Card.Values) != len(right.Card.Values) {
		return false
	}
	for key, value := range right.Card.Values {
		if left.Card.Values[key] != value {
			return false
		}
	}
	return true
}
