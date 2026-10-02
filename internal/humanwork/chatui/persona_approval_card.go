package chatui

import (
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentapproval"
)

const personaApprovalTaskPath = "/workspace/app/agents"

// PersonaApprovalCard is the server-owned, typed projection of one
// AGENT2-006 approval for an ephemeral chat post. It has no chat approval
// callback: decisions remain on the product task view until that contract
// explicitly admits chat as a decision surface.
type PersonaApprovalCard = agentapproval.PersonaChatCard

// BuildPersonaApprovalCard binds one approval to its invocation and assigned
// user. The agentapproval package computes the exact digest and bounded expiry;
// caller-supplied display text cannot replace either value.
func BuildPersonaApprovalCard(approval agentapproval.AgentActionApproval, invocationID, invokerID string, issuedAt time.Time) (PersonaApprovalCard, error) {
	return agentapproval.BuildPersonaChatCard(approval, invocationID, invokerID, issuedAt)
}

// RenderPersonaApprovalCard renders a typed, invoker-only ephemeral card.
// Values are always text nodes, and the only URL accepted is the generated
// same-origin task-view route. T4, high-risk, step-up and batch cards remain
// task-view-only; this renderer never implies that chat approval succeeded.
func RenderPersonaApprovalCard(model Model, card PersonaApprovalCard, now time.Time) ui.Node {
	if !personaApprovalVisibleTo(model.CurrentUser, card) {
		return ui.Fragment()
	}
	values := card.Card.Values
	if strings.TrimSpace(values["item_digest"]) == "" || values["item_digest"] != card.Digest {
		return ui.Fragment()
	}
	label := personaApprovalText(model, "chat.persona_approval.label", "Persona approval")
	children := []ui.Node{
		html.Strong(html.Props{Text: label}),
		html.P(html.Props{Class: "persona-approval-summary", Text: values["review_reason"]}),
		html.Code(html.Props{Class: "persona-approval-digest", Text: card.Digest}),
		html.P(html.Props{Class: "persona-approval-expiry", Text: values["expires_at"]}),
	}
	for _, field := range []struct{ class, key string }{
		{class: "persona-approval-actions", key: "actions"},
		{class: "persona-approval-material-changes", key: "material_changes"},
		{class: "persona-approval-sources", key: "sources"},
		{class: "persona-approval-uncertainty", key: "uncertainty"},
	} {
		if value := strings.TrimSpace(values[field.key]); value != "" {
			children = append(children, html.P(html.Props{Class: field.class, Text: value}))
		}
	}
	active := !now.IsZero() && now.Before(card.ExpiresAt)
	if !active {
		children = append(children, html.P(html.Props{Class: "persona-approval-expired", Role: "status", Text: personaApprovalText(model, "chat.persona_approval.expired", "This approval is no longer available.")}))
	} else if href, ok := safePersonaApprovalTaskHref(card.TaskViewHref, values["task_view"]); ok {
		children = append(children, html.A(html.Props{Class: "persona-approval-task-link", Href: href, Aria: map[string]string{"label": personaApprovalText(model, "chat.persona_approval.open_task", "Open in task view")}}, ui.Text(personaApprovalText(model, "chat.persona_approval.open_task", "Open in task view"))))
	}
	return html.Article(html.Props{Class: "persona-chat-approval-card", Role: "region", Aria: map[string]string{"label": label}, Data: map[string]string{
		"agent-card-kind": "action-approval", "agent-delivery": "ephemeral", "agent-visibility": "invoker",
		"agent-approval-id": card.ApprovalID, "agent-invocation-id": card.InvocationID, "agent-invoker-id": card.InvokerID,
		"agent-item-digest": card.Digest, "agent-expires-at": card.ExpiresAt.UTC().Format(time.RFC3339),
	}}, children...)
}

func personaApprovalVisibleTo(viewerID string, card PersonaApprovalCard) bool {
	return strings.TrimSpace(viewerID) != "" && viewerID == card.InvokerID && card.ApprovalID != "" && card.InvocationID != "" && card.Digest != "" && !card.IssuedAt.IsZero() && card.ExpiresAt.After(card.IssuedAt)
}

func safePersonaApprovalTaskHref(generated, encoded string) (string, bool) {
	if generated == "" || generated != encoded {
		return "", false
	}
	u, err := url.Parse(generated)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil || u.Path != personaApprovalTaskPath {
		return "", false
	}
	if u.Query().Get("task") == "" || len(u.Query()) != 1 {
		return "", false
	}
	return generated, true
}

func personaApprovalText(model Model, key, fallback string) string {
	return chatbug039Text(key, agentReplyFallback(model.Locale, key, fallback), fallback)
}
