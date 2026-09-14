package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// HistoryNavigationProps is the browser-independent contract for the two
// resource-history controls beside global search. The WASM adapter resolves
// availability from the browser's real navigation entries; SSR leaves both
// directions disabled because it cannot truthfully observe browser history.
type HistoryNavigationProps struct {
	I18nProps
	CanGoBack    bool
	CanGoForward bool
	GoBack       func()
	GoForward    func()
}

// HistoryNavigation renders native disabled buttons so unavailable history
// is conveyed consistently to pointer, keyboard, and assistive-tech users.
func HistoryNavigation(props HistoryNavigationProps) ui.Node {
	return html.Nav(html.Props{Class: "history-navigation", Aria: map[string]string{"label": props.Text("shell.resource_history")}},
		historyNavigationButton("back", props.Text("shell.history_back"), props.CanGoBack, props.GoBack),
		historyNavigationButton("forward", props.Text("shell.history_forward"), props.CanGoForward, props.GoForward),
	)
}

func historyNavigationButton(direction, label string, enabled bool, action func()) ui.Node {
	button := html.Props{
		Class: "history-navigation-button history-navigation-" + direction,
		Type:  "button", Title: label, Disabled: !enabled,
		Aria: map[string]string{"label": label},
	}
	if enabled && action != nil {
		button.OnClick = ui.UseEvent(func(ui.MouseEvent) { action() })
	}
	icon := "history-back"
	if direction == "forward" {
		icon = "history-forward"
	}
	return html.Button(button, productIcon(icon, "history-navigation-glyph"))
}
