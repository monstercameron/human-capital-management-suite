package chatui

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// channelNamed reports whether the kind is a channel, whose name follows the
// naming rule. A group is named for its people and a direct message has none.
func channelNamed(kind ConversationKind) bool {
	return kind == PublicChannel || kind == PrivateChannel
}

// createNameInput applies the naming rule to the create dialog's name box as
// the person types: letters go lowercase, spaces become dashes, and other
// characters are refused with the reason under the field. The rule is the
// service's (chat.NormalizeChannelName), so a name the box lets through is a
// name the service accepts.
func createNameInput(m Model, local localStore, typed string) {
	if !channelNamed(createKindOf(m, local.get())) {
		return
	}
	name, refused := chat.NormalizeChannelName(typed)
	if name != typed {
		setDOMValue("new-chat-name", name)
	}
	if st := local.get(); st.nameRefused != refused || st.nameTyped != name {
		local.update(func(u *localUI) { u.nameRefused, u.nameTyped = refused, name })
	}
}

// createNameForSubmit is the name a submit sends: the box's text under the
// rule for a channel, and false when nothing usable is left, in which case the
// reason is shown instead of calling the service.
func createNameForSubmit(local localStore, kind ConversationKind, typed string) (string, bool) {
	typed = strings.TrimSpace(typed)
	if !channelNamed(kind) {
		return typed, typed != ""
	}
	name, _ := chat.NormalizeChannelName(typed)
	if !chat.ValidChannelName(name) {
		local.update(func(u *localUI) { u.nameRefused, u.nameTyped = true, name })
		return "", false
	}
	local.update(func(u *localUI) { u.nameTyped = name })
	return name, true
}

// createNameNote is the reason printed under the name box: the rule when a
// character was refused, or when the service refused the very name that is in
// the box now. Typing a different name takes the service's answer away.
func createNameNote(m Model, local localUI, kind ConversationKind) string {
	if !channelNamed(kind) {
		return ""
	}
	if local.nameRefused || (m.NewNameRefused != "" && m.NewNameRefused == local.nameTyped) {
		return laneText(m, chatbug072Copy, keyChatbug072NameRule)
	}
	return ""
}

// searchInToken is how the "in:" filter names a channel: bare after "#" when
// the name is one word, quoted when it holds spaces, so the filter reads back
// to the same channel. A name with a quote in it cannot be quoted; the filter
// reader still finds it by matching the channels' own names.
func searchInToken(name string) string {
	if strings.ContainsAny(name, " \t") && !strings.Contains(name, `"`) {
		return `"` + name + `"`
	}
	return "#" + name
}
