package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// RouteStateText is the reviewed copy for the two states a route can land in
// without a page: an address that names nothing, and a read that failed. The
// words never include the error that caused either one.
func RouteStateText(locale LocaleContext, key string) string {
	copy := map[string][3]string{
		"title":          {"This page does not exist", "Diese Seite existiert nicht", "هذه الصفحة غير موجودة"},
		"message":        {"Check the address or use one of the links below.", "Prüfen Sie die Adresse oder verwenden Sie einen der Links unten.", "تحقق من العنوان أو استخدم أحد الروابط أدناه."},
		"home":           {"Home", "Startseite", "الرئيسية"},
		"failed_title":   {"This page could not be loaded", "Diese Seite konnte nicht geladen werden", "تعذر تحميل هذه الصفحة"},
		"failed_message": {"Try again, or go back to Home.", "Versuchen Sie es erneut oder kehren Sie zur Startseite zurück.", "حاول مرة أخرى أو عد إلى الرئيسية."},
		"retry":          {"Try again", "Erneut versuchen", "حاول مرة أخرى"},
	}
	value := copy[key]
	switch locale.Resolved {
	case "de-DE":
		return value[1]
	case "ar":
		return value[2]
	default:
		return value[0]
	}
}

// RouteNotFound is the ordinary not-found state for an unknown address under
// the product, the same words the server prints for a direct request.
func RouteNotFound(locale LocaleContext) ui.Node {
	return ui.CreateElement(routeProblem, routeProblemProps{Locale: locale, TitleKey: "title", MessageKey: "message"})
}

// RouteLoadFailed is the state for a page whose read failed. It offers the
// retry when there is one and never prints the failure itself. A detail is
// reviewed product copy that replaces the generic message, never an error.
func RouteLoadFailed(locale LocaleContext, retry func(), detail string) ui.Node {
	return ui.CreateElement(routeProblem, routeProblemProps{Locale: locale, TitleKey: "failed_title", MessageKey: "failed_message", Retry: retry, Detail: detail})
}

type routeProblemProps struct {
	Locale     LocaleContext
	TitleKey   string
	MessageKey string
	Retry      func()
	Detail     string
}

func routeProblem(props routeProblemProps) ui.Node {
	locale := props.Locale
	run := ui.UseEvent(func(ui.MouseEvent) {
		if props.Retry != nil {
			props.Retry()
		}
	})
	actions := []ui.Node{}
	if props.Retry != nil {
		actions = append(actions, html.Button(html.Props{Class: "button primary", Type: "button", OnClick: run, Text: RouteStateText(locale, "retry")}))
	}
	actions = append(actions, html.A(html.Props{Class: "button secondary", Href: Path(PageHome), Text: RouteStateText(locale, "home")}))
	message := RouteStateText(locale, props.MessageKey)
	if props.Detail != "" {
		message = props.Detail
	}
	return html.Section(html.Props{Class: "surface empty-state workspace-page-problem", Role: "alert", Dir: string(locale.Direction), Aria: map[string]string{"labelledby": "workspace-problem-title"}},
		html.H1(html.Props{ID: "workspace-problem-title", Text: RouteStateText(locale, props.TitleKey)}),
		html.P(html.Props{Class: "workspace-problem-message", Text: message}),
		html.Div(html.Props{Class: "action-row"}, actions...))
}
