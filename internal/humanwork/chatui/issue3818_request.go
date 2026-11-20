package chatui

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3818: a person who asked to join a gated channel is told where the
// request stands. Browse says "Request sent, waiting for <who>" (or "Not
// admitted: <reason>" once refused) in place of the invitation to answer, and
// its button opens the join form, where the request is read and withdrawn. When
// a person decides, the applicant gets a private notice on the Moderation page.

const (
	// gateRequestWaiting is GateJoin.Request while a person has yet to decide.
	gateRequestWaiting = "review"
	// gateRequestDeclined is GateJoin.Request after the request was refused.
	gateRequestDeclined = "declined"
)

func init() {
	for locale, words := range map[string]map[string]string{
		"en-US": {
			"requestSent": "Request sent, waiting for %s", "requestSentManagers": "Request sent, waiting for the channel’s managers",
			"requestDeclined": "Not admitted: %s", "requestDeclinedPlain": "Not admitted",
			"requestView": "View or withdraw request", "requestAgain": "Answer again",
		},
		"de-DE": {
			"requestSent": "Anfrage gesendet, wartet auf %s", "requestSentManagers": "Anfrage gesendet, wartet auf die Verwalter des Kanals",
			"requestDeclined": "Nicht zugelassen: %s", "requestDeclinedPlain": "Nicht zugelassen",
			"requestView": "Anfrage ansehen oder zurückziehen", "requestAgain": "Erneut antworten",
		},
		"ar": {
			"requestSent": "تم إرسال الطلب، بانتظار %s", "requestSentManagers": "تم إرسال الطلب، بانتظار مديري القناة",
			"requestDeclined": "لم يتم قبولك: %s", "requestDeclinedPlain": "لم يتم قبولك",
			"requestView": "عرض الطلب أو سحبه", "requestAgain": "أجب مرة أخرى",
		},
	} {
		for key, value := range words {
			chatgateExtraCopy[locale][key] = value
		}
	}
}

// chatgateRequest is the person's own request on this channel's gate, "" when
// they have none that matters.
func chatgateRequest(m Model, c Conversation) string {
	gate, ok := chatgateJoin(m, c)
	if !ok || c.Joined {
		return ""
	}
	return gate.Request
}

// chatgateRequestLabel is the Browse button's text for a channel the person
// has a request in; "" for any other channel.
func chatgateRequestLabel(m Model, c Conversation) string {
	locale := chatgateLocale(m.Locale)
	switch chatgateRequest(m, c) {
	case gateRequestWaiting:
		return GateText(locale, "requestView")
	case gateRequestDeclined:
		return GateText(locale, "requestAgain")
	}
	return ""
}

// chatgateRequestNote is the line under the channel's name that says where the
// request stands. It is a status line, so a screen reader hears it when Browse
// opens and when it changes. Nil when there is no request.
func chatgateRequestNote(m Model, c Conversation) ui.Node {
	state := chatgateRequest(m, c)
	if state == "" {
		return nil
	}
	gate, _ := chatgateJoin(m, c)
	locale := chatgateLocale(m.Locale)
	var text string
	switch state {
	case gateRequestWaiting:
		names := []string{}
		for _, name := range gate.Reviewers {
			if name = strings.TrimSpace(name); name != "" {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			text = GateText(locale, "requestSentManagers")
		} else {
			text = fmt.Sprintf(GateText(locale, "requestSent"), strings.Join(names, GateText(locale, "comma")))
		}
	case gateRequestDeclined:
		if reason := strings.TrimSpace(gate.RequestReason); reason != "" {
			text = fmt.Sprintf(GateText(locale, "requestDeclined"), reason)
		} else {
			text = GateText(locale, "requestDeclinedPlain")
		}
	default:
		return nil
	}
	return html.P(html.Props{Class: "chatgate-browse-note chatgate-request", Role: "status", Dir: "auto", Text: text})
}

// moderationGateDecisionNotice is the applicant's notice of a decision: they
// were let in (with a link to the channel) or not admitted (with the reason
// they were given and a link to their request).
func moderationGateDecisionNotice(locale string, n chat.ModerationNotice) ui.Node {
	children := []ui.Node{html.H3(html.Props{Text: gateNoticeText(locale, n.Outcome)})}
	if !n.At.IsZero() {
		children = append(children, html.P(html.Props{Class: "chatremove-date", Text: formatDay(locale, n.At, true)}))
	}
	if n.Outcome == chat.ModerationOutcomeGateDeclined {
		if reason := strings.TrimSpace(n.Reason); reason != "" {
			children = append(children, html.P(html.Props{Dir: "auto", Text: gateNoticeText(locale, "gate_reason") + ": " + reason}))
		}
		if n.ConversationID != "" {
			children = append(children, html.A(html.Props{Class: "chatmod005-channel", Href: "#gate=" + url.QueryEscape(n.ConversationID), Data: map[string]string{"gate-open": n.ConversationID}, Text: gateNoticeText(locale, "gate_declined_open")}))
		}
	} else if n.ConversationID != "" {
		children = append(children, html.A(html.Props{Class: "chatmod005-channel", Href: ChannelReferenceURL(n.ConversationID), Text: gateNoticeText(locale, "gate_admitted_open")}))
	}
	return html.Aside(html.Props{Class: "chatremove-notice", Role: "status", Aria: map[string]string{"live": "polite"}}, children...)
}
