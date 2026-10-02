package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"strconv"
	"strings"
)

func integrate1IconValue(values []agenticon.Value) agenticon.Value {
	if len(values) > 0 {
		return values[0]
	}
	return agenticon.Value{}
}

// personaAdminIDs lists the ids of every agent on the Agent setup page, so an
// agent with no stored icon is given a fallback no other agent there has.
func personaAdminIDs(snapshot PersonaAdminSnapshot) []string {
	ids := make([]string, 0, len(snapshot.Personas))
	for _, persona := range snapshot.Personas {
		ids = append(ids, persona.ID)
	}
	return ids
}

// choiceSeed is the id an agent choice derives its fallback icon from: the agent's
// own id, or the choice's id for the general agent that has none.
func choiceSeed(id, value string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return id
}

func Integrate1IconText(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"regenerate": {"Regenerate icon", "Symbol neu erzeugen", "إعادة إنشاء الأيقونة"},
		"shuffle":    {"Shuffle icon", "Symbol variieren", "تغيير الأيقونة"},
		"reset":      {"Reset icon", "Symbol zurücksetzen", "إعادة ضبط الأيقونة"},
		"apply":      {"Apply icon", "Symbol übernehmen", "تطبيق الأيقونة"},
		"undo":       {"Undo icon change", "Symboländerung rückgängig machen", "التراجع عن تغيير الأيقونة"},
		"preview":    {"Preview icon", "Symbolvorschau", "معاينة الأيقونة"},
		"failed":     {"Could not change the icon. Try again.", "Das Symbol konnte nicht geändert werden. Versuchen Sie es erneut.", "تعذر تغيير الأيقونة. حاول مرة أخرى."},
		"changed":    {"Icon changed.", "Symbol geändert.", "تم تغيير الأيقونة."},
		"cancel":     {"Cancel preview", "Vorschau abbrechen", "إلغاء المعاينة"},
	}
	index := 0
	if locale.Resolved == "de-DE" {
		index = 1
	}
	if locale.Resolved == "ar" {
		index = 2
	}
	return copy[key][index]
}
func integrate1AgentIconControls(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	nodes := []ui.Node{}
	for _, action := range []string{"regenerate", "shuffle", "reset"} {
		nodes = append(nodes, html.Button(html.Props{Type: "button", Class: "button secondary small", Text: Integrate1IconText(locale, action), Disabled: persona.IconRevision < 1, Data: map[string]string{"agent-icon-action": action}}))
	}
	nodes = append(nodes, html.Div(html.Props{Hidden: true, Data: map[string]string{"agent-icon-preview": "true"}, Role: "group", Aria: map[string]string{"label": Integrate1IconText(locale, "preview")}}, html.Span(html.Props{Data: map[string]string{"agent-icon-preview-image": "true"}}), html.Button(html.Props{Type: "button", Class: "button small", Text: Integrate1IconText(locale, "apply"), Data: map[string]string{"agent-icon-action": "apply"}}), html.Button(html.Props{Type: "button", Class: "button secondary small", Text: Integrate1IconText(locale, "cancel"), Data: map[string]string{"agent-icon-action": "cancel"}})))
	nodes = append(nodes, html.Button(html.Props{Type: "button", Class: "button secondary small", Text: Integrate1IconText(locale, "undo"), Hidden: true, Data: map[string]string{"agent-icon-action": "undo"}}), html.P(html.Props{Role: "status", Aria: map[string]string{"live": "polite"}, Data: map[string]string{"agent-icon-status": "true"}}))
	return html.Div(html.Props{Class: "persona-admin-actions", Data: map[string]string{"agent-icon-controls": persona.ID, "agent-icon-revision": strconv.FormatInt(persona.IconRevision, 10), "agent-icon-locale": locale.Resolved}}, nodes...)
}
