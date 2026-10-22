package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-038. Phone, round two.
//
// The conversation list is the one screen that holds the workspace controls on
// a phone, so it is one bar: the workspace menu button, the title, the Chat
// preferences and the new-conversation button. The workspace bar above the page
// is hidden by the shell rule in productui (chatPhoneShellStylesheet) and the
// list's own close button is hidden here, because a list that is the root view
// has nothing to close; the conversation under it is chosen from the list.

const keyChatux038WorkspaceMenu = "chat.ux038.workspace_menu"

var chatux038Copy = map[string]map[string]string{
	"en-US": {keyChatux038WorkspaceMenu: "Open workspace menu"},
	"de-DE": {keyChatux038WorkspaceMenu: "Arbeitsbereich-Menü öffnen"},
	"ar":    {keyChatux038WorkspaceMenu: "فتح قائمة مساحة العمل"},
}

// chatux038WorkspaceMenu is the button at the start of the list's bar that
// opens the workspace's own navigation, the control the hidden workspace bar
// carries. It is drawn only at phone width (chatux038Styles).
func chatux038WorkspaceMenu(m Model) ui.Node {
	return actionButton("icon-button rail-workspace-menu", "open-workspace-menu", "", laneText(m, chatux038Copy, keyChatux038WorkspaceMenu), false, icon("menu"))
}

// chatux038Person reports whether a direct conversation is with a person while
// the agent list is still being read. An agent never has a photograph, so a
// peer whose photograph is already held is a person, and the header and the row
// draw their face from the first paint instead of a blank tile.
func chatux038Person(m Model, c Conversation) bool {
	if c.Agent || c.Kind != DirectMessage {
		return false
	}
	return strings.TrimSpace(m.PhotoURLs[m.PeerIDs[c.ID]]) != ""
}

// ChatUX038Styles is the phone's list bar, the centred row avatars and the
// press highlight that stands in for hover on a device without it.
const ChatUX038Styles = `.chat-workspace .chat-rail-row .chat-row{align-items:center}` +
	`.chat-workspace .rail-workspace-menu{display:none}` +
	`@media(max-width:760px){` +
	`.chat-workspace .rail-workspace-menu{display:inline-flex;flex:none}` +
	`.chat-workspace .rail-head{gap:4px;padding-inline-start:4px}` +
	`.chat-workspace .rail-head .rail-title-group{flex:1;min-width:0;justify-content:flex-start}` +
	`.chat-workspace .rail-head .mobile-chat-close{display:none}` +
	`.chat-workspace .rail-head .icon-button{width:44px;height:44px}` +
	// The resting composer is one row; a line the composer adds above its field
	// (who will read the message, in which language) takes a row of its own
	// instead of squeezing the field to a few letters wide.
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown){flex-wrap:wrap}` +
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown)>.chatlang-audience{flex:0 0 100%;order:-2;margin:0 4px 2px}` +
	`}` +
	`@media(hover:none){` +
	`.chat-workspace :is(.chat-row,.menu-item,.rail-link,.search-result,.browse-row,.chatsave-item):active{background:var(--soft)}` +
	`.chat-workspace .chat-row.selected:active{background:var(--accent);color:var(--on-brand,#fff)}` +
	`.chat-workspace :is(.icon-button,.tool-button,.message-action,.reaction,.agent-reply-action,.agent-feedback-button):active{background:color-mix(in srgb,var(--ink) 8%,transparent)}` +
	`}`
