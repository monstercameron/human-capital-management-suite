package productui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// ChatPageComponent, when set, renders the chat page instead of the static
// build below. The browser client sets it to a component that reads the live
// chat model and re-renders on its own state, so a reaction, a pane drag or
// a stream event repaints the workspace without re-running the route's
// loaders. The server-rendered document and tests leave it nil.
var ChatPageComponent func(view View) ui.Node

func chatPage(view View) ui.Node {
	if ChatPageComponent != nil {
		return ChatPageComponent(view)
	}
	return BuildChatPage(view, view.Chat)
}

// ChatDocDateLabel is the date a chat document card shows for the
// document's last update (C-9, r3). It uses the docs library's own
// vocabulary -- "Sep 3" this year, "Dec 14, 2025" for another year, with
// each locale's order and digits -- so the card and the document it opens
// never disagree on how a date reads.
func ChatDocDateLabel(locale LocaleContext, at, now time.Time) string {
	zone, err := time.LoadLocation(locale.normalized().TimeZone)
	if err != nil {
		zone = time.UTC
	}
	local := at.In(zone)
	if local.Year() == now.In(zone).Year() {
		return docsShortDateLabel(locale.Resolved, local)
	}
	return docsCompareDateLabel(locale, at, false)
}

// BuildChatPage completes a chat model with what the view knows (locale,
// direction, viewer, catalog) and builds the workspace.
func BuildChatPage(view View, model chatui.Model) ui.Node {
	if model.State == "" {
		model.State = chatui.StateLoading
	}
	if model.Locale == "" {
		model.Locale = view.Locale.Resolved
	}
	if model.Direction == "" {
		model.Direction = string(view.Locale.Direction)
	}
	if model.CurrentUser == "" {
		model.CurrentUser = view.Principal
	}
	if model.CurrentUserName == "" {
		model.CurrentUserName = strings.TrimSpace(view.Viewer.Name)
	}
	if model.Callbacks.Navigate == nil {
		model.Callbacks.Navigate = view.Navigate
	}
	if model.Text == nil {
		locale := view.Locale
		model.Text = func(key string) string { return locale.Text(key) }
	}
	if view.LoadError != "" {
		// The shell's failure notice is not drawn over a full-bleed page, so
		// the page carries the recovery step itself: as a slim bar when the
		// chat loaded and only another domain failed, and as the timeline's
		// own error state when chat has nothing to show.
		recovery := view.Locale.Text("shell.load_recovery")
		if model.State == chatui.StateReady && len(model.Conversations) > 0 {
			if model.Notice == "" {
				model.Notice, model.NoticeRetry = recovery, true
			}
		} else if model.State != chatui.StateError {
			model.State, model.Error = chatui.StateError, recovery
		}
	}
	return chatui.Build(model)
}

// chatShellStylesheet is the only chat rule that lives outside the chat
// scope: it tells the shell frame to hand a full-bleed page the whole main
// region and to stop scrolling it, so the chat workspace can pin its rail,
// header and composer and scroll only its timeline.
func chatShellStylesheet() string {
	return ".main.page-full-bleed{max-width:none;padding:0;height:100%;min-height:0;display:flex;flex-direction:column}" +
		".main.page-full-bleed>.loading-progress{flex:none}" +
		".main.page-full-bleed>.page-stack{display:flex;flex-direction:column;flex:1;min-height:0;gap:0}.main.page-full-bleed>.page-stack>.chat-workspace{flex:1;min-height:0}" +
		".main-scroll.main-scroll-full-bleed{overflow:hidden;scrollbar-gutter:auto}" +
		// Under 760px the shell grid keeps an `auto` track for the (fixed,
		// out-of-flow) navigation drawer and gives the main region the
		// leftover, which leaves a full-bleed page half a screen tall. The
		// chat page is the only row, so it takes the whole height.
		"@media(max-width:760px){.main.page-full-bleed{padding:0}.main-scroll.main-scroll-full-bleed{scrollbar-gutter:auto}.app-shell .shell-grid:has(>.main-scroll-full-bleed){grid-template-rows:minmax(0,1fr)!important}}"
}
