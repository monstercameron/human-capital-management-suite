package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Agent setup shows one checkbox per agent, "React to questions with an emoji",
// checked unless the owner turned it off, in each language, and disabled for a
// person who may not change the agent's setup (AGENTUX-075).
func TestTodo_AGENTUX_075_ReactionControl(t *testing.T) {
	render := func(locale string, persona PersonaAdminPersona, snapshot PersonaAdminSnapshot) string {
		t.Helper()
		node := personaAdminReactionsControl(ResolveProductLocale(locale), persona, snapshot)
		if node == nil {
			return ""
		}
		markup, err := ui.RenderToString(node)
		if err != nil {
			t.Fatal(err)
		}
		return markup
	}
	persona := PersonaAdminPersona{ID: "policy-helper", Lifecycle: PersonaPublished}
	allowed := PersonaAdminSnapshot{CommandPermissionsAvailable: true, AllowedCommands: []string{"SET_REACTIONS"}}
	for locale, label := range map[string]string{"en-US": "React to questions with an emoji", "de-DE": "Auf Fragen mit einem Emoji reagieren", "ar": "التفاعل مع الأسئلة برمز تعبيري"} {
		markup := render(locale, persona, allowed)
		if !strings.Contains(markup, label) || !strings.Contains(markup, `type="checkbox"`) || !strings.Contains(markup, `checked`) || strings.Contains(markup, `disabled`) || !strings.Contains(markup, `data-persona-reactions="policy-helper"`) {
			t.Errorf("%s: control is %s", locale, markup)
		}
	}
	// Turned off by the owner: unchecked.
	persona.ReactionsOff = true
	if markup := render("en-US", persona, allowed); strings.Contains(markup, `checked`) {
		t.Errorf("an agent switched off is drawn checked: %s", markup)
	}
	// A person who may not change setup sees it, disabled; a retired agent has none.
	persona.ReactionsOff = false
	if markup := render("en-US", persona, PersonaAdminSnapshot{CommandPermissionsAvailable: true}); !strings.Contains(markup, `disabled`) {
		t.Errorf("the control is enabled for a person who may not use it: %s", markup)
	}
	persona.Lifecycle = PersonaRetired
	if markup := render("en-US", persona, allowed); markup != "" {
		t.Errorf("a retired agent has the control: %s", markup)
	}
}
