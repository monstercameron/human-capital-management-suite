package productui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PersonaAdminRemovedPlacement is a conversation an agent was removed from by a
// person, newest first on the card. Placements the server stopped or replaced
// are not listed here: they have no one to name (AGENTUX-037).
type PersonaAdminRemovedPlacement struct {
	ConversationID string
	Conversation   string
	Kind           string
	// RemovedBy is the name people see for the person who removed it.
	RemovedBy string
	RemovedAt time.Time
	// Version is the version the placement ran.
	Version string
}

// personaAdminRemovedPlacementsMax keeps the history to the most recent
// removals, so a long-lived agent's card does not grow without bound.
const personaAdminRemovedPlacementsMax = 5

// personaAdminRemovedPlacements is the placement history: one line per removal,
// "Removed by <name> on <date>", under the live placements. It draws nothing
// when no one has removed the agent from a conversation.
func personaAdminRemovedPlacements(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	if len(persona.RemovedPlacements) == 0 {
		return nil
	}
	items := make([]ui.Node, 0, len(persona.RemovedPlacements))
	for index, removed := range persona.RemovedPlacements {
		if index == personaAdminRemovedPlacementsMax {
			break
		}
		name := personaAdminPlacementName(locale, PersonaAdminInstallation{Conversation: removed.Conversation, Kind: removed.Kind})
		sentence := strings.NewReplacer("{name}", removed.RemovedBy, "{date}", locale.FormatDate(removed.RemovedAt)).Replace(agentUX037Text(locale, "removed_by"))
		if strings.TrimSpace(removed.RemovedBy) == "" {
			sentence = strings.ReplaceAll(agentUX037Text(locale, "removed_on"), "{date}", locale.FormatDate(removed.RemovedAt))
		}
		items = append(items, html.Li(html.Props{Class: "persona-admin-removed-placement", Raw: map[string]any{"data-conversation-id": removed.ConversationID}},
			html.Span(html.Props{Class: "persona-admin-removed-name", Dir: "auto", Raw: map[string]any{"lang": "und"}}, html.Tag("bdi", html.Props{Dir: "ltr"}, ui.Text(name))),
			html.Span(html.Props{Class: "persona-admin-removed-detail muted"}, ui.Text(personaAdminCompactSeparator+sentence)),
		))
	}
	return html.Div(html.Props{Class: "persona-admin-removed-placements"},
		html.H5(html.Props{Class: "muted persona-admin-removed-heading"}, ui.Text(agentUX037Text(locale, "removed_heading"))),
		html.Ul(html.Props{Class: "persona-admin-removed-list"}, items...),
	)
}

func agentUX037Text(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"removed_heading": {"Removed from", "Entfernt aus", "أُزيل من"},
		"removed_by":      {"Removed by {name} on {date}", "Entfernt von {name} am {date}", "أزاله {name} في {date}"},
		"removed_on":      {"Removed on {date}", "Entfernt am {date}", "أُزيل في {date}"},
	}
	values, ok := copy[key]
	if !ok {
		return key
	}
	index := 0
	switch locale.Resolved {
	case "de-DE":
		index = 1
	case "ar":
		index = 2
	}
	return values[index]
}
