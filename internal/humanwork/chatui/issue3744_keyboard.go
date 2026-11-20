package chatui

import "strings"

// Issue #3744: Chat can be worked from the keyboard alone. Alt+Up and Alt+Down
// move between conversations, Alt+Shift+Up and Alt+Shift+Down between the ones
// with unread messages, Alt+Shift+R marks the open conversation read,
// Alt+Shift+T opens the thread of the message in focus, ? lists all of it, and
// Esc closes the topmost layer. Nothing here fires while a person types.

type issue3744Action string

const (
	issue3744Prev       issue3744Action = "prev"
	issue3744Next       issue3744Action = "next"
	issue3744PrevUnread issue3744Action = "prev-unread"
	issue3744NextUnread issue3744Action = "next-unread"
	issue3744MarkRead   issue3744Action = "mark-read"
	issue3744Thread     issue3744Action = "thread"
	issue3744Sheet      issue3744Action = "sheet"
)

// issue3744NavKeyShortcuts is the sidebar's aria-keyshortcuts value.
const issue3744NavKeyShortcuts = "Alt+ArrowUp Alt+ArrowDown Alt+Shift+ArrowUp Alt+Shift+ArrowDown Alt+Shift+R Shift+?"

// issue3744Match names the action a key press asks for, or "". Letters are read
// from the physical key (code) because Option on Apple keyboards types symbols.
// Control and Command never take part: they belong to the browser and system.
func issue3744Match(key, code string, alt, shift, ctrl, meta bool) issue3744Action {
	if ctrl || meta {
		return ""
	}
	if !alt {
		if key == "?" {
			return issue3744Sheet
		}
		return ""
	}
	if key == "?" {
		return ""
	}
	switch {
	case key == "ArrowUp" && !shift:
		return issue3744Prev
	case key == "ArrowDown" && !shift:
		return issue3744Next
	case key == "ArrowUp":
		return issue3744PrevUnread
	case key == "ArrowDown":
		return issue3744NextUnread
	case shift && code == "KeyR":
		return issue3744MarkRead
	case shift && code == "KeyT":
		return issue3744Thread
	}
	return ""
}

// issue3744Step returns the id of the conversation an action moves to, or ""
// when it stays. Plain steps stop at the ends of the list; unread steps wrap.
func issue3744Step(ids []string, unread []bool, current string, action issue3744Action) string {
	n := len(ids)
	if n == 0 {
		return ""
	}
	at := -1
	for i, id := range ids {
		if id == current {
			at = i
			break
		}
	}
	switch action {
	case issue3744Next:
		if at < 0 {
			return ids[0]
		}
		return ids[min(at+1, n-1)]
	case issue3744Prev:
		if at < 0 {
			return ids[n-1]
		}
		return ids[max(at-1, 0)]
	case issue3744NextUnread, issue3744PrevUnread:
		dir := 1
		if action == issue3744PrevUnread {
			dir = -1
		}
		start := at
		if start < 0 && dir < 0 {
			start = n
		}
		for step := 1; step <= n; step++ {
			i := ((start+dir*step)%n + n) % n
			if i == at {
				break
			}
			if i < len(unread) && unread[i] {
				return ids[i]
			}
		}
	}
	return ""
}

// Issue3744Styles draws the shortcut sheet. It lives in the shared stylesheet,
// not in a style element the page adds: the server pins the stylesheet's hash
// in its content security policy and refuses any other inline style.
const Issue3744Styles = `dialog.chat-shortcuts{box-sizing:border-box;inline-size:min(34rem,calc(100vw - 32px));max-block-size:calc(100dvh - 32px);margin:auto;padding:0;border:1px solid var(--line,#c9ced6);border-radius:12px;background:var(--canvas,#fff);color:var(--ink,#111827);overflow:hidden}` +
	`dialog.chat-shortcuts[open]{display:flex;flex-direction:column}` +
	`dialog.chat-shortcuts::backdrop{background:color-mix(in srgb,var(--ink) 45%,transparent)}` +
	`.chat-shortcuts-head{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:8px 8px 8px 16px;padding-inline:16px 8px;border-block-end:1px solid var(--line,#c9ced6)}` +
	`.chat-shortcuts-head h2{margin:0;font-size:1.125rem}` +
	`.chat-shortcuts-close{min-inline-size:44px;min-block-size:44px;border:0;border-radius:8px;background:none;color:inherit;font:inherit;font-size:1.25rem;cursor:pointer}` +
	`.chat-shortcuts-close:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.chat-shortcuts-list{margin:0;padding:0;list-style:none;overflow:auto}` +
	`.chat-shortcuts-list li{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:10px 16px;border-block-start:1px solid var(--line,#e5e7eb)}` +
	`.chat-shortcuts-list li:first-child{border-block-start:0}` +
	`.chat-shortcuts-keys{display:inline-flex;flex:none;gap:4px;direction:ltr}` +
	`.chat-shortcuts-keys kbd{min-inline-size:1.75rem;padding:2px 6px;border:1px solid var(--line,#c9ced6);border-radius:6px;background:var(--surface,#f3f4f6);font:inherit;font-size:.8125rem;text-align:center}` +
	`.chat-shortcuts-status{position:absolute;inline-size:1px;block-size:1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap}`

type issue3744Shortcut struct {
	Keys   []string
	Action string
}

// issue3744Shortcuts is the list the shortcut sheet shows, in the words on the
// reader's own keyboard.
func issue3744Shortcuts(locale, platform string) []issue3744Shortcut {
	alt := "Alt"
	if chatux010IsApple(platform) {
		alt = "Option"
	}
	search := strings.Split(chatux010ShortcutLabelFor(locale, platform), "+")
	text := func(key string) string { return issue3744Text(locale, key) }
	return []issue3744Shortcut{
		{[]string{alt, "↑"}, text("prev")},
		{[]string{alt, "↓"}, text("next")},
		{[]string{alt, "Shift", "↑"}, text("prev_unread")},
		{[]string{alt, "Shift", "↓"}, text("next_unread")},
		{[]string{alt, "Shift", "R"}, text("mark_read")},
		{[]string{alt, "Shift", "T"}, text("thread")},
		{search, text("search")},
		{[]string{alt, "Shift", "S"}, text("save")},
		{[]string{"?"}, text("sheet")},
		{[]string{"Esc"}, text("escape")},
	}
}

// issue3744SheetCopy is the sheet's title, its close button, and the words
// announced when a conversation is marked read from the keyboard.
func issue3744SheetCopy(locale string) (title, closeLabel, markedRead string) {
	return issue3744Text(locale, "title"), issue3744Text(locale, "close"), issue3744Text(locale, "marked_read")
}

func issue3744Text(locale, key string) string {
	tables := [3]map[string]string{issue3744En, issue3744De, issue3744Ar}
	return chatbug039Text(key, tables[chatbug039LocaleIndex(locale)][key], issue3744En[key])
}

var issue3744En = map[string]string{
	"title":       "Keyboard shortcuts",
	"close":       "Close keyboard shortcuts",
	"marked_read": "Conversation marked as read",
	"prev":        "Previous conversation",
	"next":        "Next conversation",
	"prev_unread": "Previous conversation with unread messages",
	"next_unread": "Next conversation with unread messages",
	"mark_read":   "Mark this conversation as read",
	"thread":      "Reply in thread to the message in focus",
	"search":      "Search messages",
	"save":        "Save the message in focus for later",
	"sheet":       "Show this list",
	"escape":      "Close the open panel, menu or dialog",
	"open":        "Show keyboard shortcuts",
	"open_note":   "Or press ? when you are not typing in a field.",
}

var issue3744De = map[string]string{
	"title":       "Tastenkürzel",
	"close":       "Tastenkürzel schließen",
	"marked_read": "Unterhaltung als gelesen markiert",
	"prev":        "Vorherige Unterhaltung",
	"next":        "Nächste Unterhaltung",
	"prev_unread": "Vorherige Unterhaltung mit ungelesenen Nachrichten",
	"next_unread": "Nächste Unterhaltung mit ungelesenen Nachrichten",
	"mark_read":   "Diese Unterhaltung als gelesen markieren",
	"thread":      "Im Thread auf die fokussierte Nachricht antworten",
	"search":      "Nachrichten durchsuchen",
	"save":        "Fokussierte Nachricht für später speichern",
	"sheet":       "Diese Liste anzeigen",
	"escape":      "Geöffnetes Fenster, Menü oder Dialog schließen",
	"open":        "Tastenkürzel anzeigen",
	"open_note":   "Oder drücke ?, wenn du gerade nicht in einem Feld tippst.",
}

var issue3744Ar = map[string]string{
	"title":       "اختصارات لوحة المفاتيح",
	"close":       "إغلاق اختصارات لوحة المفاتيح",
	"marked_read": "تم تحديد المحادثة كمقروءة",
	"prev":        "المحادثة السابقة",
	"next":        "المحادثة التالية",
	"prev_unread": "المحادثة السابقة التي بها رسائل غير مقروءة",
	"next_unread": "المحادثة التالية التي بها رسائل غير مقروءة",
	"mark_read":   "تحديد هذه المحادثة كمقروءة",
	"thread":      "الرد في سلسلة على الرسالة المحددة",
	"search":      "البحث في الرسائل",
	"save":        "حفظ الرسالة المحددة لوقت لاحق",
	"sheet":       "عرض هذه القائمة",
	"escape":      "إغلاق اللوحة أو القائمة أو مربع الحوار المفتوح",
	"open":        "عرض اختصارات لوحة المفاتيح",
	"open_note":   "أو اضغط ? عندما لا تكتب في حقل.",
}
