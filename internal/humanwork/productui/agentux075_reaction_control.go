package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AGENTUX-075: Agent setup shows one checkbox per agent, "React to questions with
// an emoji". Checked is the default: an agent whose owner has not chosen reacts.

func agentUX075ReactionText(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"label": {"React to questions with an emoji", "Auf Fragen mit einem Emoji reagieren", "التفاعل مع الأسئلة برمز تعبيري"},
		"hint": {"When someone asks this agent a question, it adds an emoji to the question before it answers.",
			"Wenn jemand diesem Agenten eine Frage stellt, fügt er der Frage ein Emoji hinzu, bevor er antwortet.",
			"عندما يطرح شخص سؤالاً على هذا الوكيل، يضيف رمزاً تعبيرياً إلى السؤال قبل أن يجيب."},
	}
	index := 0
	switch language := strings.ToLower(locale.Resolved); {
	case strings.HasPrefix(language, "de"):
		index = 1
	case strings.HasPrefix(language, "ar"):
		index = 2
	}
	return copy[key][index]
}

// personaAdminReactionsControl is the checkbox. It is left out for a retired
// agent, and disabled for a person who may not change the agent's setup.
func personaAdminReactionsControl(locale LocaleContext, persona PersonaAdminPersona, snapshot PersonaAdminSnapshot) ui.Node {
	if persona.Lifecycle == PersonaRetired || strings.TrimSpace(persona.ID) == "" {
		return nil
	}
	id := "persona-admin-reactions-" + safeAgentDOMToken(persona.ID)
	allowed := personaAdminCommandAllowed(snapshot, "SET_REACTIONS")
	return html.Div(html.Props{Class: "persona-admin-reactions"},
		html.Label(html.Props{For: id, Class: "persona-admin-reactions-label"},
			html.Input(html.Props{ID: id, Type: "checkbox", Checked: !persona.ReactionsOff, Disabled: !allowed, Raw: map[string]any{"data-persona-reactions": persona.ID, "aria-describedby": id + "-hint"}}),
			ui.Text(" "+agentUX075ReactionText(locale, "label"))),
		html.Small(html.Props{ID: id + "-hint", Class: "muted"}, ui.Text(agentUX075ReactionText(locale, "hint"))))
}
