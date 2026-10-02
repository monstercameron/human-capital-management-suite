package chatui

import (
	"net/url"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-037: how each Chat surface calls the shared loading component. The
// surface keeps its own frame; these helpers only choose the rows and the status.

// chatux037ModerationRetry is the Try again of the Moderation page: it reads the
// tab that was showing again, the way every other control on the page does.
func chatux037ModerationRetry(m ModerationPageModel) map[string]string {
	tab := m.Tab
	if tab == "" {
		tab = "open"
	}
	return map[string]string{"chatremove-open": ModerationPageHref + "?" + url.Values{"tab": {tab}, "locale": {m.Locale}}.Encode()}
}

// chatux037ModerationTabs is the segmented control of the page. While the page
// is read, Open and Resolved are drawn and disabled, with no counts: the frame is
// there at once and nothing in it claims a number it does not have.
func chatux037ModerationTabs(m ModerationPageModel) ui.Node {
	if m.State != StateLoading {
		return moderationTabs(m)
	}
	t := func(key string) string { return chatremoveText(m.Locale, key) }
	current := m.Tab
	if current != "resolved" {
		current = "open"
	}
	tab := func(name, label string) ui.Node {
		return html.Button(html.Props{Type: "button", Class: "chatsave-seg-button chatmod005-tab", Role: "tab", Disabled: true, Aria: map[string]string{"selected": boolString(name == current)}}, html.Span(html.Props{Text: label}))
	}
	return html.Div(html.Props{Class: "chatsave-seg chatmod005-tabs", Role: "tablist", Aria: map[string]string{"label": t("moderation")}},
		tab("open", t("tab_open")), tab("resolved", t("tab_resolved")))
}

// chatux037ModerationLoading is the queue's rows while the page is read: four
// rows shaped like the queue's own, the status announced once.
func chatux037ModerationLoading(m ModerationPageModel) ui.Node {
	return ChatLoadingFrame(LoadingFrame{Locale: m.Locale, Shape: LoadingShapeQueue, Status: chatremoveText(m.Locale, "loading"), RetryData: chatux037ModerationRetry(m)})
}

// ModerationLoadingMarkup is the whole Moderation page in its loading state, as
// markup: the heading with its close control, Open and Resolved disabled, and the
// placeholder rows. The browser client puts it in the page's layer at once, then
// replaces it with the server's answer.
func ModerationLoadingMarkup(locale, tab string) string {
	markup, err := ui.RenderToString(ModerationPage(ModerationPageModel{Locale: locale, State: StateLoading, Tab: tab}))
	if err != nil {
		return ""
	}
	return markup
}

// ModerationFailedMarkup is the page when the read failed: the heading, then what
// failed with Try again in the place the rows were.
func ModerationFailedMarkup(locale, tab, errorKey string) string {
	// The counts are not known when the read failed, so the tabs stay disabled.
	m := ModerationPageModel{Locale: locale, State: StateLoading, Tab: tab}
	what := chatremoveText(locale, errorKey)
	if what == "" {
		what = chatremoveText(locale, "error")
	}
	markup, err := ui.RenderToString(html.Main(html.Props{ID: "chatremove-moderation", Class: "chatremove chatmod005-page chatux037-page-failed", Lang: locale, Dir: direction(locale)},
		moderationHeading(locale), chatux037ModerationTabs(m), ChatLoadFailed(locale, what, chatux037ModerationRetry(m), nil)))
	if err != nil {
		return ""
	}
	return markup
}

// chatux037MenuLoading is the loading row of the mention and command menus: two
// menu-shaped rows under the group heading, the status announced once. Try again,
// after eight seconds, runs the action the menu's failed state already uses.
func chatux037MenuLoading(m Model, class, status, retryAction string) ui.Node {
	return html.Div(html.Props{Class: "mention-agent-state " + class},
		ChatLoadingFrame(LoadingFrame{Locale: m.Locale, Shape: LoadingShapeMenu, Rows: 2, Status: status, RetryData: map[string]string{"action": retryAction}}))
}
