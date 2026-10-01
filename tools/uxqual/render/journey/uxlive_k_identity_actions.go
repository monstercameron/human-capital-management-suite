package journey

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// positionIdentityNode renders the chosen position without exposing a raw
// revision token. A missing title is an explicit safe state; a governed code
// may still help the reader distinguish two unresolved choices.
func positionIdentityNode(locale string, identity PositionIdentity) ui.Node {
	if strings.TrimSpace(identity.Title) == "" && strings.TrimSpace(identity.Code) == "" && strings.TrimSpace(identity.State) == "" {
		return nil
	}
	copy := productui.ResolveProductLocale(locale)
	value := strings.TrimSpace(identity.Title)
	switch strings.TrimSpace(identity.State) {
	case PositionIdentityWithheld:
		value = copy.Text("journey.position_withheld")
	case PositionIdentityUnresolved:
		value = copy.Text("journey.position_unavailable")
	case PositionIdentityResolved:
		if value == "" {
			value = copy.Text("journey.position_unavailable")
		}
	default:
		if value == "" {
			value = copy.Text("journey.position_unavailable")
		}
	}
	code := safePositionCode(identity.Code)
	children := []ui.Node{
		html.Span(html.Props{Class: "jn-meta-key"}, html.Text(copy.Text("journey.position_identity")+" ")),
		html.Span(html.Props{Class: "jn-position-value"}, html.Text(value)),
	}
	if code != "" {
		children = append(children,
			html.Span(html.Props{Class: "jn-position-code", Raw: map[string]any{"dir": "ltr", "translate": "no"}}, html.Text(" · "+code)),
		)
	}
	return html.P(html.Props{Class: "jn-position-identity"}, children...)
}

// safePositionCode accepts the stable role code supplied by the authorized
// placement projection but refuses UUID-shaped values and revision-like
// tokens, which are implementation diagnostics rather than user identity.
func safePositionCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || looksLikeUUID(value) || strings.Contains(strings.ToLower(value), "revision") || strings.HasPrefix(strings.ToLower(value), "rev-") {
		return ""
	}
	return value
}

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, r := range value {
		switch index {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}
	return true
}

// journeyCardActionKey is the shared lifecycle-to-task vocabulary for card
// links. The detail page owns the actual transition; the card only tells the
// reader why opening it is useful.
func journeyCardActionKey(stage string) string {
	switch strings.ToUpper(strings.TrimSpace(stage)) {
	case "PROPOSED":
		return "journey.card_action_start"
	case "BLOCKED", "REPAIR_REQUIRED":
		return "journey.card_action_correct"
	case "AWAITING_APPROVAL", "FINANCE_APPROVAL", "MANAGER_APPROVAL", "REAPPROVAL":
		return "journey.card_action_complete"
	case "COMPLETED", "REJECTED", "FAILED", "RECORDED":
		return "journey.card_action_review"
	case "WAITING_EFFECTIVE_DATE", "REVALIDATION", "EXECUTED", "OBSERVING_EFFECTS":
		return "journey.card_action_track"
	default:
		return "journey.card_action_review_request"
	}
}

func journeyCardActionLabel(locale string, card JourneyCard) string {
	return productui.ResolveProductLocale(locale).Text(journeyCardActionKey(card.Stage))
}
