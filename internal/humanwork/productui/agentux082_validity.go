package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// agentUX082Text is the validity vocabulary: how long the agent keeps
// answering, and what to do once its evidence lapsed.
func agentUX082Text(locale LocaleContext, key string, replacements ...string) string {
	copy := map[string][3]string{
		"card_until":       {"Answers until {date}.", "Antwortet bis {date}.", "يجيب حتى {date}."},
		"activity_until":   {"Answers until {date}", "Antwortet bis {date}", "يجيب حتى {date}"},
		"card_expired":     {"Stopped: evidence expired on {date}.", "Angehalten: die Nachweise sind am {date} abgelaufen.", "متوقف: انتهت صلاحية الأدلة في {date}."},
		"activity_expired": {"Evidence expired on {date}", "Nachweise abgelaufen am {date}", "انتهت صلاحية الأدلة في {date}"},
		"renew":            {"Renew", "Erneuern", "تجديد"},
		"ask_owner_renew":  {"Ask {owner} to renew it.", "Bitten Sie {owner}, ihn zu erneuern.", "اطلب من {owner} تجديده."},
	}
	text := copy[key][agentRPLocaleIndex(locale)]
	if len(replacements) > 0 {
		text = strings.NewReplacer(replacements...).Replace(text)
	}
	return text
}

// agentUX082ValidityLine is the Agent setup card's validity banner: while the
// retained evidence is current the card says how long the agent keeps
// answering; past it the card says the agent stopped, when, and how to renew
// it. A version without retained evidence shows nothing.
func agentUX082ValidityLine(locale LocaleContext, persona PersonaAdminPersona, snapshot PersonaAdminSnapshot) ui.Node {
	date, ok := personaAdminFormattedDate(locale, persona.EvidenceExpiresAt)
	if !ok {
		return nil
	}
	if !persona.EvidenceExpired {
		return html.P(html.Props{Class: "persona-admin-validity", Role: "status", Raw: map[string]any{"data-validity": "current"}},
			ui.Text(agentUX082Text(locale, "card_until", "{date}", date)))
	}
	children := []ui.Node{html.P(html.Props{Class: "persona-admin-validity-expired", Role: "status", Raw: map[string]any{"data-validity": "expired"}},
		ui.Text(agentUX082Text(locale, "card_expired", "{date}", date)))}
	if personaAdminCommandAllowed(snapshot, "CREATE_VERSION") && personaAdminVersionEditorAvailable(persona) {
		// Renewal is a new version through the same editor as Edit: it goes
		// through review, evaluation and publication like any new version,
		// and the old one stays as history.
		editorID := personaAdminVersionEditorID(persona)
		children = append(children, html.P(html.Props{Class: "persona-admin-validity-renew"},
			html.Button(html.Props{Class: "button secondary", Type: "button", Aria: map[string]string{"controls": editorID, "expanded": "false"}, Raw: map[string]any{"data-persona-editor-toggle": editorID}}, ui.Text(agentUX082Text(locale, "renew")))))
	} else {
		owner := PersonaWorkerLabel(persona.Owner, persona.OwnerName)
		children = append(children, html.P(html.Props{Class: "muted persona-admin-validity-renew"},
			ui.Text(agentUX082Text(locale, "ask_owner_renew", "{owner}", owner))))
	}
	return html.Div(html.Props{Class: "persona-admin-validity-stopped"}, children...)
}

// agentUX082ActivityValidity is the Activity row's validity subline under the
// agent's name: "Answers until" while current, the expiry with a Renew link to
// the agent's setup card past it. A version without retained evidence adds no
// line, so existing rows render as before.
func agentUX082ActivityValidity(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	date, ok := personaAdminFormattedDate(locale, persona.EvidenceExpiresAt)
	if !ok {
		return nil
	}
	if !persona.EvidenceExpired {
		return html.Small(html.Props{Class: "muted agent-activity-validity", Raw: map[string]any{"data-validity": "current"}},
			ui.Text(agentUX082Text(locale, "activity_until", "{date}", date)))
	}
	href := Path(PagePersonaAdmin) + "#persona-admin-" + safeAgentDOMToken(persona.ID)
	return html.Small(html.Props{Class: "muted agent-activity-validity", Raw: map[string]any{"data-validity": "expired"}},
		ui.Text(agentUX082Text(locale, "activity_expired", "{date}", date)+personaAdminInlineSeparator),
		html.A(html.Props{Href: href}, ui.Text(agentUX082Text(locale, "renew"))))
}
