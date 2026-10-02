package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// CHATUX-010: Chat's search has one name ("Search Chat"), shows the key that
// reaches it, and names its result groups in plain words.

// chatux010IsApple reports whether a platform string (navigator.platform or
// userAgentData.platform) names an Apple system, where the command key
// replaces control. It is decided once from that string; nothing about it
// depends on a focus or a keyboard event.
func chatux010IsApple(platform string) bool {
	p := strings.ToLower(platform)
	for _, name := range []string{"mac", "iphone", "ipad", "ipod"} {
		if strings.Contains(p, name) {
			return true
		}
	}
	return false
}

// chatux010ShortcutLabel is the key combination as written in the search box.
func chatux010ShortcutLabel(platform string) string {
	if chatux010IsApple(platform) {
		return "Cmd+K"
	}
	return "Ctrl+K"
}

// chatux010KeyShortcuts is the same combination in aria-keyshortcuts syntax.
func chatux010KeyShortcuts(platform string) string {
	if chatux010IsApple(platform) {
		return "Meta+K"
	}
	return "Control+K"
}

// chatux010ShortcutHint is the key label drawn inside the sidebar search box.
// It is decoration for sighted readers (the input carries aria-keyshortcuts),
// and it leaves once there are words in the box, where the browser's own clear
// control takes its place.
func chatux010ShortcutHint(m Model) ui.Node {
	if strings.TrimSpace(m.Search) != "" {
		return nil
	}
	return html.Kbd(html.Props{Class: "chat-search-shortcut", Dir: "ltr", Aria: map[string]string{"hidden": "true"}, Text: chatux010ShortcutLabel(chatPlatform())})
}

// chatux010Hint is the search control's tooltip: its name and its key.
func chatux010Hint(m Model) string {
	return chatux001Textf(m, "chat.ux001.search_hint", map[string]string{"shortcut": chatux010ShortcutLabel(chatPlatform())})
}

// chatux010GroupName is the heading of a group of results, in the words a new
// employee uses: Conversations, People, Messages, never the internal kind.
func chatux010GroupName(locale string, kind chatsearch.Kind) string {
	names := map[chatsearch.Kind][3]string{
		chatsearch.Message:      {"Messages", "Nachrichten", "الرسائل"},
		chatsearch.Thread:       {"Thread replies", "Thread-Antworten", "الردود في السلاسل"},
		chatsearch.Conversation: {"Conversations", "Unterhaltungen", "المحادثات"},
		chatsearch.Person:       {"People", "Personen", "الأشخاص"},
		chatsearch.File:         {"Files", "Dateien", "الملفات"},
		chatsearch.Pin:          {"Pinned messages", "Angeheftete Nachrichten", "الرسائل المثبتة"},
		chatsearch.Todo:         {"To-dos", "Aufgaben", "المهام"},
		chatsearch.Poll:         {"Polls", "Umfragen", "الاستطلاعات"},
		chatsearch.AgentAnswer:  {"Agent answers", "Agentenantworten", "إجابات الوكلاء"},
		chatsearch.SourceTitle:  {"Sources", "Quellen", "المصادر"},
		chatsearch.Voice:        {"Voice messages", "Sprachnachrichten", "الرسائل الصوتية"},
		chatsearch.Saved:        {"Saved items", "Gespeicherte Inhalte", "العناصر المحفوظة"},
		chatsearch.Location:     {"Locations", "Orte", "المواقع"},
		chatsearch.Announcement: {"Announcements", "Ankündigungen", "الإعلانات"},
		chatsearch.Reminder:     {"Reminders", "Erinnerungen", "التذكيرات"},
	}
	i := 0
	switch {
	case strings.HasPrefix(strings.ToLower(locale), "de"):
		i = 1
	case strings.HasPrefix(strings.ToLower(locale), "ar"):
		i = 2
	}
	if group, ok := names[kind]; ok {
		return chatbug039Text(string(kind), group[i], group[0])
	}
	return chatsearchKind(locale, kind)
}

// chatux010LegacyGroupName names the three groups the results panel builds from
// the model's own lists (conversations, people, messages) with the same words
// as the groups the search service returns, so one page never calls the same
// thing "Channels" in one place and "Conversations" in another.
func chatux010LegacyGroupName(m Model, key string) string {
	switch key {
	case KeySearchChannels:
		return chatbug039Text(key, chatux010GroupName(m.Locale, chatsearch.Conversation), chatux010GroupName("en-US", chatsearch.Conversation))
	case KeySearchPeople:
		return chatbug039Text(key, chatux010GroupName(m.Locale, chatsearch.Person), chatux010GroupName("en-US", chatsearch.Person))
	case KeySearchMessages:
		return chatbug039Text(key, chatux010GroupName(m.Locale, chatsearch.Message), chatux010GroupName("en-US", chatsearch.Message))
	}
	return ""
}

// chatux010ShortcutMatches reports whether a key press is the search shortcut:
// K with Control on other systems, K with Command on Apple ones, and no other
// modifier. Control+K stays free on Apple systems, where text fields use it to
// delete to the end of the line.
func chatux010ShortcutMatches(apple bool, key string, ctrl, meta, alt, shift bool) bool {
	if (key != "k" && key != "K") || alt || shift {
		return false
	}
	if apple {
		return meta && !ctrl
	}
	return ctrl && !meta
}
