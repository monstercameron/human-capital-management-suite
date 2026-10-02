package productui

import (
	"errors"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// agentUX022SnapshotFailure names why the whole of Agent setup could not be
// drawn. Catalog, targets, preview, commands, starters and documents fail on
// their own inside the page; only two things take the page down, and each has
// its own sentence: the reader may not administer agents ("denied"), or the
// agent store is not part of this workspace ("store"). Anything else is a read
// that failed just now ("load"), which the reader can try again. It used to be
// reported as a disconnected service whatever the cause.
func agentUX022SnapshotFailure(err error) string {
	var staged interface{ PersonaCatalogFailureStage() string }
	if errors.As(err, &staged) {
		switch staged.PersonaCatalogFailureStage() {
		case "authorization":
			return "denied"
		case "persona_store":
			return "store"
		}
	}
	return "load"
}

func agentUX022Text(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"load_failed": {"Agent setup could not be loaded just now. Nothing was changed.", "Die Agenteneinrichtung konnte gerade nicht geladen werden. Es wurde nichts geändert.", "تعذر تحميل إعداد الوكلاء الآن. لم يتغير شيء."},
		"retry":       {"Try again", "Erneut versuchen", "حاول مرة أخرى"},
	}
	return copy[key][agentRPLocaleIndex(locale)]
}

// agentUX022LoadFailed is the page for a read that failed: the frame and page
// navigation stay, the sentence says what happened, and Try again reloads the
// catalog without leaving the page.
func agentUX022LoadFailed(view View, locale LocaleContext) ui.Node {
	view.Page = PagePersonaAdmin
	view.Locale = locale
	return ProductPageFrame(ProductPageFrameProps{
		Class: "agent-page-frame", Dir: string(locale.Direction), Aria: map[string]string{"labelledby": "persona-admin-title"}, Raw: map[string]any{"data-persona-admin-state": string(PersonaAdminUnavailable), "data-persona-admin-failure": "load"},
		Breadcrumbs: personaAdminBreadcrumb(locale), Title: personaAdminText(locale, "title"), TitleID: "persona-admin-title",
		Actions: []ui.Node{AgentPageNavigation(view, AgentPageSetup)},
		Body: []ui.Node{html.Div(html.Props{Class: "persona-admin-page"},
			ui.CreateElement(EmptyState, EmptyStateProps{Title: personaAdminText(locale, "unavailable_title"), Description: agentUX022Text(locale, "load_failed"), Role: "status", Class: "persona-admin-unavailable"}),
			html.Button(html.Props{Class: "button secondary", Type: "button", Raw: map[string]any{"data-persona-admin-retry": "page"}}, ui.Text(agentUX022Text(locale, "retry"))),
		)},
	})
}
