package chatui

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const (
	keyWorkflowOfferHeading     = "issue3805.heading"
	keyWorkflowOfferReason      = "issue3805.reason"
	keyWorkflowOfferStart       = "issue3805.start"
	keyWorkflowOfferNotNow      = "issue3805.not_now"
	keyWorkflowOfferNever       = "issue3805.never"
	keyWorkflowOfferDismiss     = "issue3805.dismiss"
	keyWorkflowOfferOpened      = "issue3805.opened"
	keyWorkflowOfferOpenLink    = "issue3805.open_link"
	keyWorkflowOfferUnavailable = "issue3805.unavailable"
	keyWorkflowOfferWithdrawn   = "issue3805.withdrawn"
	keyWorkflowOfferChanged     = "issue3805.changed"
	keyWorkflowOfferError       = "issue3805.error"
	keyWorkflowOfferSaving      = "issue3805.saving"
	keyWorkflowOfferRegion      = "issue3805.region"
	keyWorkflowOfferAgent       = "issue3805.agent_name"
)

var issue3805WorkflowOfferCopy = map[string]map[string]string{
	"en-US": {
		keyWorkflowOfferHeading:     "Start a “{name}” request?",
		keyWorkflowOfferReason:      "Your message sounds like you want to do this. Nothing is started or sent until you review and submit the request yourself.",
		keyWorkflowOfferStart:       "Start request",
		keyWorkflowOfferNotNow:      "Not now",
		keyWorkflowOfferNever:       "Don’t offer this again",
		keyWorkflowOfferDismiss:     "Dismiss",
		keyWorkflowOfferOpened:      "Next step: review the request form and submit it yourself. Nothing has been submitted.",
		keyWorkflowOfferOpenLink:    "Open the request form",
		keyWorkflowOfferUnavailable: "This request is not available to you here any more. Nothing was started.",
		keyWorkflowOfferWithdrawn:   "The message this offer came from was deleted or changed, so the offer was withdrawn. Nothing was started.",
		keyWorkflowOfferChanged:     "The message changed after this offer was made. Open it and offer again if you still want this. Nothing was started.",
		keyWorkflowOfferError:       "That did not work. Nothing was started; try again.",
		keyWorkflowOfferSaving:      "Working; your messages remain available",
		keyWorkflowOfferRegion:      "Suggested request, visible only to you",
		keyWorkflowOfferAgent:       "Workflow Offer",
	},
	"de-DE": {
		keyWorkflowOfferHeading:     "Antrag „{name}“ starten?",
		keyWorkflowOfferReason:      "Ihre Nachricht klingt, als wollten Sie das tun. Es wird nichts gestartet oder gesendet, bevor Sie den Antrag selbst prüfen und absenden.",
		keyWorkflowOfferStart:       "Antrag starten",
		keyWorkflowOfferNotNow:      "Jetzt nicht",
		keyWorkflowOfferNever:       "Das nicht mehr anbieten",
		keyWorkflowOfferDismiss:     "Verwerfen",
		keyWorkflowOfferOpened:      "Nächster Schritt: Prüfen Sie das Antragsformular und senden Sie es selbst ab. Es wurde nichts abgesendet.",
		keyWorkflowOfferOpenLink:    "Antragsformular öffnen",
		keyWorkflowOfferUnavailable: "Dieser Antrag steht Ihnen hier nicht mehr zur Verfügung. Es wurde nichts gestartet.",
		keyWorkflowOfferWithdrawn:   "Die Nachricht, zu der dieses Angebot gehört, wurde gelöscht oder geändert, daher wurde das Angebot zurückgezogen. Es wurde nichts gestartet.",
		keyWorkflowOfferChanged:     "Die Nachricht wurde nach diesem Angebot geändert. Öffnen Sie sie und lassen Sie sich das Angebot erneut machen, wenn Sie es noch möchten. Es wurde nichts gestartet.",
		keyWorkflowOfferError:       "Das hat nicht geklappt. Es wurde nichts gestartet; versuchen Sie es erneut.",
		keyWorkflowOfferSaving:      "Wird bearbeitet; Ihre Nachrichten bleiben verfügbar",
		keyWorkflowOfferRegion:      "Vorgeschlagener Antrag, nur für Sie sichtbar",
		keyWorkflowOfferAgent:       "Workflow-Angebot",
	},
	"ar": {
		keyWorkflowOfferHeading:     "هل تريد بدء طلب «{name}»؟",
		keyWorkflowOfferReason:      "تبدو رسالتك كأنك تريد القيام بذلك. لن يبدأ أو يُرسل أي شيء حتى تراجع الطلب وتقدّمه بنفسك.",
		keyWorkflowOfferStart:       "ابدأ الطلب",
		keyWorkflowOfferNotNow:      "ليس الآن",
		keyWorkflowOfferNever:       "لا تعرض هذا مجدداً",
		keyWorkflowOfferDismiss:     "تجاهل",
		keyWorkflowOfferOpened:      "الخطوة التالية: راجع نموذج الطلب وقدّمه بنفسك. لم يُقدَّم أي شيء.",
		keyWorkflowOfferOpenLink:    "افتح نموذج الطلب",
		keyWorkflowOfferUnavailable: "لم يعد هذا الطلب متاحاً لك هنا. لم يبدأ أي شيء.",
		keyWorkflowOfferWithdrawn:   "حُذفت الرسالة التي جاء منها هذا العرض أو تغيّرت، لذلك سُحب العرض. لم يبدأ أي شيء.",
		keyWorkflowOfferChanged:     "تغيّرت الرسالة بعد هذا العرض. افتحها واطلب العرض مجدداً إن كنت لا تزال تريده. لم يبدأ أي شيء.",
		keyWorkflowOfferError:       "لم تنجح العملية. لم يبدأ أي شيء؛ حاول مجدداً.",
		keyWorkflowOfferSaving:      "جارٍ التنفيذ؛ تبقى رسائلك متاحة",
		keyWorkflowOfferRegion:      "طلب مقترح، مرئي لك فقط",
		keyWorkflowOfferAgent:       "عرض سير العمل",
	},
}

// renderAgentUXAmbientWorkflowCard draws the offer to start a governed
// workflow. It names the workflow from the catalog and never repeats the
// message; it is private to its author (checked again here); and the only thing
// "Start request" does is take the person to the workflow's own start page.
// Nothing on this card submits, drafts or implies approval.
func renderAgentUXAmbientWorkflowCard(m Model, c AgentUXAmbientCard) ui.Node {
	if c.Person != m.CurrentUser {
		return nil
	}
	name := strings.TrimSpace(c.Title)
	agentName := c.AgentName
	if agentName == "" || agentName == "Workflow Offer" {
		agentName = laneText(m, issue3805WorkflowOfferCopy, keyWorkflowOfferAgent)
	}
	headingID := c.ID + "-heading"
	children := []ui.Node{renderAgentReplyIdentity(m, agentName, storedAgentIcon(m, []string{c.AgentID}, agentName))}
	button := func(action, key string, primary bool, label string) ui.Node {
		class := "button secondary small"
		if primary {
			class = "button primary small"
		}
		props := html.Props{Type: "button", Class: class, Text: laneText(m, issue3805WorkflowOfferCopy, key), Disabled: c.Busy || !c.CanManage, Raw: map[string]any{"data-ambient-action": action, "data-ambient-id": c.ID, "data-ambient-revision": strconv.FormatUint(c.Revision, 10), "data-ambient-conversation": c.Conversation}}
		if label != "" {
			props.Aria = map[string]string{"label": label}
		}
		return html.Button(props)
	}
	status := func(key string) ui.Node {
		return html.P(html.Props{Role: "status", Dir: "auto", Text: laneText(m, issue3805WorkflowOfferCopy, key)})
	}
	actions := []ui.Node{}
	switch {
	case c.State == "CANCELLED" || c.State == "SOURCE_UNAVAILABLE":
		children = append(children, status(keyWorkflowOfferWithdrawn))
		actions = append(actions, button("DISMISS", keyWorkflowOfferDismiss, true, ""))
	case c.State == "UNAVAILABLE":
		children = append(children, html.H3(html.Props{ID: headingID, Dir: "auto", Text: laneTextf(m, issue3805WorkflowOfferCopy, keyWorkflowOfferHeading, map[string]string{"name": name})}), status(keyWorkflowOfferUnavailable))
		actions = append(actions, button("DISMISS", keyWorkflowOfferDismiss, true, ""))
	case c.State == "ACCEPTED":
		children = append(children, html.H3(html.Props{ID: headingID, Dir: "auto", Text: laneTextf(m, issue3805WorkflowOfferCopy, keyWorkflowOfferHeading, map[string]string{"name": name})}), status(keyWorkflowOfferOpened))
		if parsed, err := url.Parse(c.StartHref); err == nil && !parsed.IsAbs() && parsed.Host == "" && strings.HasPrefix(parsed.Path, "/") && !strings.HasPrefix(c.StartHref, "//") && !strings.ContainsAny(c.StartHref, "\\\r\n") {
			actions = append(actions, html.A(html.Props{Href: c.StartHref, Class: "button primary small", Text: laneText(m, issue3805WorkflowOfferCopy, keyWorkflowOfferOpenLink), Aria: map[string]string{"label": laneText(m, issue3805WorkflowOfferCopy, keyWorkflowOfferOpenLink) + ": " + name}}))
		}
		actions = append(actions, button("DISMISS", keyWorkflowOfferDismiss, false, ""))
	default:
		children = append(children, html.H3(html.Props{ID: headingID, Dir: "auto", Text: laneTextf(m, issue3805WorkflowOfferCopy, keyWorkflowOfferHeading, map[string]string{"name": name})}),
			html.P(html.Props{Class: "muted", Text: agentUXAmbientText(m.Locale, "private")}),
			html.P(html.Props{Class: "muted", Dir: "auto", Text: laneText(m, issue3805WorkflowOfferCopy, keyWorkflowOfferReason)}))
		actions = append(actions, button("OPEN", keyWorkflowOfferStart, true, laneText(m, issue3805WorkflowOfferCopy, keyWorkflowOfferStart)+": "+name), button("DISMISS", keyWorkflowOfferNotNow, false, ""), button("NEVER", keyWorkflowOfferNever, false, laneText(m, issue3805WorkflowOfferCopy, keyWorkflowOfferNever)+": "+name))
	}
	if c.Error {
		children = append(children, html.P(html.Props{ID: c.ID + "-error", Role: "alert", Dir: "auto", Text: laneText(m, issue3805WorkflowOfferCopy, keyWorkflowOfferError)}))
	}
	if c.Busy {
		children = append(children, html.P(html.Props{Role: "status", Dir: "auto", Text: laneText(m, issue3805WorkflowOfferCopy, keyWorkflowOfferSaving)}))
	}
	children = append(children, html.Div(html.Props{Class: "agentux-ambient-actions"}, actions...))
	aria := map[string]string{"label": laneText(m, issue3805WorkflowOfferCopy, keyWorkflowOfferRegion)}
	if headingID != "" && (c.State == "" || c.State == "OFFERED" || c.State == "ACCEPTED" || c.State == "UNAVAILABLE") {
		aria = map[string]string{"labelledby": headingID}
	}
	return html.Article(html.Props{Class: "agentux-ambient-card agentux-workflow-offer agent-reply-answer", Dir: direction(m.Locale), Aria: aria, Raw: map[string]any{"data-ambient-card": c.ID, "data-ambient-scope": c.Scope, "data-ambient-kind": c.Kind, "data-source-post": c.Source, "style": "min-width:0;max-width:100%;overflow-wrap:anywhere;padding:12px;border-radius:var(--hcm-radius-control)"}}, children...)
}
