package chatui

import (
	"strconv"
	"strings"
)

// Issue 3797. A tab that is hidden or without focus is not being read: a
// message that arrives there must not move the read position, and the tab
// title carries the unread and mention counts so the person is told something
// needs them.

// ChatReadsAllowed is whether messages on screen count as seen: only while the
// page is visible and has the focus.
func ChatReadsAllowed(visible, focused bool) bool { return visible && focused }

// ChatAttentionCounts is the unread and mention totals the tab title shows. It
// follows each conversation's notification choice: a muted conversation adds
// nothing, and one set to mentions only adds its mentions.
func ChatAttentionCounts(m Model) (unread, mentions int) {
	for _, c := range m.Conversations {
		mode, ok := m.Preferences.Notifications[c.ID]
		if !ok || mode == "" {
			mode = NotifyAll
		}
		switch mode {
		case NotifyMute:
			continue
		case NotifyMention:
			unread += c.Mentions
		default:
			unread += max(c.Unread, c.Mentions)
		}
		mentions += c.Mentions
	}
	return unread, mentions
}

var chatAttentionTitleCopy = map[string][2]string{
	"en": {"({n}) ", "({n} · @{m}) "},
	"de": {"({n}) ", "({n} · @{m}) "},
	"ar": {"({n}) ", "({n} · @{m}) "},
}

// ChatAttentionTitlePrefix is the text put in front of the tab title, "" when
// nothing needs the person.
func ChatAttentionTitlePrefix(locale string, unread, mentions int) string {
	unread = max(unread, mentions)
	if unread <= 0 {
		return ""
	}
	lang := "en"
	switch l := strings.ToLower(locale); {
	case strings.HasPrefix(l, "de"):
		lang = "de"
	case strings.HasPrefix(l, "ar"):
		lang = "ar"
	}
	forms := chatAttentionTitleCopy[lang]
	count := func(n int) string {
		text := strconv.Itoa(n)
		if n > 99 {
			text = "99+"
		}
		if lang == "ar" {
			text = arabicDigits(text)
		}
		return text
	}
	form := forms[0]
	if mentions > 0 {
		form = forms[1]
	}
	return strings.NewReplacer("{n}", count(unread), "{m}", count(mentions)).Replace(form)
}
