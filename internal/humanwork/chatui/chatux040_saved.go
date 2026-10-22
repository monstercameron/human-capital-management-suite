package chatui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// CHATUX-040: the Saved panel's tab counts add up and say what they are, and a
// day word inside a sentence is not capitalised.

var chatux040SavedTable = map[string][3]string{
	"all_note_one":  {"All also holds {n} saved message whose original was deleted.", "Alle enthält außerdem {n} gespeicherte Nachricht, deren Original gelöscht wurde.", "يضم «الكل» أيضًا رسالة محفوظة حُذف أصلها ({n})."},
	"all_note_many": {"All also holds {n} saved messages whose originals were deleted.", "Alle enthält außerdem {n} gespeicherte Nachrichten, deren Originale gelöscht wurden.", "يضم «الكل» أيضًا رسائل محفوظة حُذفت أصولها ({n})."},
}

// ChatUX040SavedStyles clamps a saved message's text on whole lines (four, with
// the ellipsis the browser draws, never a fade across a half line), puts the
// conversation each item is from on a line of its own so a long name is not
// cut, and sets the note under the tabs.
const ChatUX040SavedStyles = `
.chat-workspace .chatsave-text{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:4;max-height:none;overflow:hidden}
.chat-workspace .chatsave-text.is-expanded{display:block;-webkit-line-clamp:unset;overflow:visible}
.chat-workspace .chatsave-text[data-clamped="true"]{-webkit-mask-image:none;mask-image:none}
.chat-workspace .chatsave-text .message-body>:last-child{margin-block-end:0}
.chat-workspace .chatsave-meta{flex-wrap:wrap}
.chat-workspace .chatsave-meta .chatsave-time{margin-inline-start:auto}
.chat-workspace .chatsave-meta .chatsave-where{order:3;flex:1 0 100%;max-inline-size:100%}
.chat-workspace .chatsave-where+.chatsave-time::before{content:none}
.chat-workspace .chatsave-all-note{margin:0;padding:6px 16px 0;color:var(--muted);font-size:.8125rem;line-height:1.4}
`

// chatux040AllNote explains a count of All that is larger than To do plus Done:
// the saved messages whose original was deleted are listed under All only. It
// is "" when the three counts add up.
func chatux040AllNote(locale string, todo, done, all int) string {
	extra := all - todo - done
	if extra <= 0 {
		return ""
	}
	key := "all_note_many"
	if extra == 1 {
		key = "all_note_one"
	}
	values := chatux040SavedTable[key]
	text := chatbug039Text(key, values[chatbug039LocaleIndex(locale)], values[0])
	return strings.ReplaceAll(text, "{n}", chatsave002Num(locale, extra))
}

// chatux040DoneWhen is the day a message was ticked off, ready to follow
// "Done": a day word (today, yesterday) starts with a small letter in English
// and German ("Done yesterday"), and a date is left as it is.
func chatux040DoneWhen(locale, when string) string {
	switch dateLocale(locale) {
	case "ar":
		return when
	}
	for _, key := range []string{"today", "yesterday", "tomorrow"} {
		if when == chatsave002Text(locale, key) {
			first, size := utf8.DecodeRuneInString(when)
			return string(unicode.ToLower(first)) + when[size:]
		}
	}
	return when
}
