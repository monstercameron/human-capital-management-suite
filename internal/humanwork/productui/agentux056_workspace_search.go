package productui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// agentux056AssistantPersonaID is the general-purpose agent whose row carries
// the workspace search status; every other agent's row has none to show.
const agentux056AssistantPersonaID = "hcmnext.local.persona.assistant"

func agentux056Text(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"label":   {"Workspace search", "Arbeitsbereichssuche", "البحث في مساحة العمل"},
		"indexed": {"Searches {count} workspace documents (indexed {time})", "Durchsucht {count} Dokumente im Arbeitsbereich (indexiert {time})", "يبحث في {count} من مستندات مساحة العمل (فُهرست {time})"},
		"behind":  {"{pending} of {count} workspace documents are not indexed yet, so their meaning is not searched; they are matched by their words.", "{pending} von {count} Dokumenten im Arbeitsbereich sind noch nicht indexiert; sie werden nur nach ihren Wörtern gefunden.", "لم تُفهرس {pending} من {count} من مستندات مساحة العمل بعد، لذا تُطابَق بكلماتها لا بمعناها."},
		"absent":  {"Searches {count} workspace documents by their words only. None is indexed for meaning yet.", "Durchsucht {count} Dokumente im Arbeitsbereich nur nach Wörtern. Noch keines ist für die Bedeutungssuche indexiert.", "يبحث في {count} من مستندات مساحة العمل بكلماتها فقط. لم يُفهرس أي منها للبحث بالمعنى بعد."},
		"none":    {"No workspace documents are open to every member yet.", "Noch ist kein Dokument im Arbeitsbereich für alle Mitglieder offen.", "لا توجد مستندات في مساحة العمل مفتوحة لكل الأعضاء بعد."},
	}
	index := 0
	switch {
	case strings.HasPrefix(locale.Resolved, "de"):
		index = 1
	case strings.HasPrefix(locale.Resolved, "ar"):
		index = 2
	}
	return copy[key][index]
}

// personaAdminWorkspaceSearchDefinition says how many workspace documents the
// general-purpose agent searches and when they were indexed, and says plainly
// when the index is behind or absent. It is empty for every other agent.
func personaAdminWorkspaceSearchDefinition(locale LocaleContext, persona PersonaAdminPersona) ui.Node {
	if persona.ID != agentux056AssistantPersonaID {
		return nil
	}
	count := locale.FormatNumber(strconv.Itoa(persona.WorkspaceDocuments), 0)
	var value ui.Node
	indexedAt, indexed := personaAdminFormattedDate(locale, persona.WorkspaceIndexedAt)
	switch {
	case persona.WorkspaceDocuments == 0 && persona.WorkspacePending == 0:
		value = ui.Text(agentux056Text(locale, "none"))
	case !indexed:
		value = ui.Text(strings.ReplaceAll(agentux056Text(locale, "absent"), "{count}", count))
	case persona.WorkspacePending > 0:
		value = ui.Text(strings.NewReplacer("{pending}", locale.FormatNumber(strconv.Itoa(persona.WorkspacePending), 0), "{count}", count).Replace(agentux056Text(locale, "behind")))
	default:
		sentence := strings.ReplaceAll(agentux056Text(locale, "indexed"), "{count}", count)
		before, after, _ := strings.Cut(sentence, "{time}")
		value = html.Span(html.Props{}, ui.Text(before), html.Time(html.Props{Raw: map[string]any{"datetime": persona.WorkspaceIndexedAt}}, ui.Text(indexedAt)), ui.Text(after))
	}
	return html.Div(html.Props{Class: "persona-admin-fact", Raw: map[string]any{"data-workspace-search": "true"}}, html.Tag("dt", html.Props{Class: "muted"}, ui.Text(agentux056Text(locale, "label"))), html.Tag("dd", html.Props{Dir: "auto"}, value))
}
