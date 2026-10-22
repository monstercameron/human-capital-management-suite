package chatui

import (
	"regexp"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-043. Mixed-direction runs.
//
// An English sentence inside an Arabic line (a skill description, a title, a
// name, the key names in a hint, a version) is ordered by the page's direction
// unless it is isolated, which puts its full stop, its colon or its comma at the
// wrong end: ".this installation may read". Every such run is wrapped as its
// own isolate, so it lays out by its own language and the neighbouring text
// treats it as one unit. Left-to-right pages are untouched.

const (
	chatux043FSI = rune(0x2068) // first strong isolate
	chatux043PDI = rune(0x2069) // pop directional isolate
)

// chatux043RTL reports whether the locale is written right to left.
func chatux043RTL(locale string) bool { return agentReplyDirection(locale) == "rtl" }

// chatux043Isolate wraps a run of text in a first-strong isolate for a
// right-to-left locale and returns it unchanged otherwise. It is for text that
// is joined into a string; markup uses chatux043Fact.
func chatux043Isolate(locale, text string) string {
	if !chatux043RTL(locale) || strings.TrimSpace(text) == "" {
		return text
	}
	return string(chatux043FSI) + text + string(chatux043PDI)
}

// chatux043Fact is a value that is written by someone else, in their own
// language (a purpose, an owner, a version), as one node.
func chatux043Fact(locale, text string) ui.Node {
	if !chatux043RTL(locale) {
		return ui.Text(text)
	}
	return html.Tag("bdi", html.Props{Dir: "auto"}, ui.Text(text))
}

var chatux043Latin = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+]*`)

// chatux043Hint is a hint that names keys ("Enter", "Shift+Enter"): in a
// right-to-left locale each key name is its own left-to-right isolate, so the
// comma after it lands where the sentence puts it.
func chatux043Hint(locale, text string) []ui.Node {
	if !chatux043RTL(locale) {
		return []ui.Node{ui.Text(text)}
	}
	var nodes []ui.Node
	last := 0
	for _, at := range chatux043Latin.FindAllStringIndex(text, -1) {
		if at[0] > last {
			nodes = append(nodes, ui.Text(text[last:at[0]]))
		}
		nodes = append(nodes, html.Tag("bdi", html.Props{Dir: "ltr"}, ui.Text(text[at[0]:at[1]])))
		last = at[1]
	}
	if last < len(text) {
		nodes = append(nodes, ui.Text(text[last:]))
	}
	// The hint's box lays its children out as flex items, which drop the space
	// at the edge of each text; one inner span keeps them in one line of text.
	return []ui.Node{html.Span(html.Props{}, nodes...)}
}

// ChatUX043Styles puts a paragraph, its heading and the list under it on one
// edge. A paragraph's own direction is detected from its words (dir="auto"), so
// "start" would put an English paragraph at the left of an Arabic page while its
// bullets sit at the right; the edge is the page's, so match-parent takes the
// container's start edge (the page's own direction) instead of the detected
// one, and no physical right is written. A
// citation marker and a version stay left to right as one unit. A bottom sheet
// keeps the safe area under its last line.
const ChatUX043Styles = `.chat-workspace[dir="rtl"] :is(.message-body,.agent-reply-answer) :is(p,h3,h4,h5,h6),` +
	`.chat-workspace[dir="rtl"] :is(.agent-summary,.mention-profile-card,.agent-profile-dialog) :is(p,li,h4,h5,a){text-align:match-parent}` +
	`.chat-workspace[dir="rtl"] .thread-pane .message-body{text-align:start}` +
	`.chat-workspace .agent-cite{unicode-bidi:isolate;direction:ltr}` +
	`.chat-workspace .composer-help bdi{unicode-bidi:isolate}` +
	`@media(max-width:760px){.chat-workspace .chat-dialog{padding-block-end:calc(env(safe-area-inset-bottom,0px) + 12px)}}`
