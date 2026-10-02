package chatui

import (
	"strings"
	"unicode"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-089: a document card shows the opening of the document's text. It is
// cut where a sentence ends, or failing that between two words, with an
// ellipsis, and "Show more" opens the rest in place. The service that gave the
// snippet may already have cut it mid-sentence with three dots; those are
// replaced by the one ellipsis so a cut is never printed twice.

// docSnippetLimit is how many characters of the snippet a card shows closed.
const docSnippetLimit = 140

// docSnippetParts is the card's closed text and its whole text. clamped says
// the two differ, which is when the card offers Show more.
func docSnippetParts(snippet string) (short, full string, clamped bool) {
	full = strings.TrimSpace(snippet)
	cut := false
	for _, mark := range []string{"...", "…"} {
		if strings.HasSuffix(full, mark) {
			full, cut = strings.TrimSpace(strings.TrimSuffix(full, mark)), true
			break
		}
	}
	if cut {
		full += "…"
	}
	runes := []rune(strings.TrimSuffix(full, "…"))
	if len(runes) <= docSnippetLimit {
		return full, full, false
	}
	window := runes[:docSnippetLimit]
	end := -1
	for i, r := range window {
		if (r == '.' || r == '!' || r == '?') && (i+1 >= len(runes) || unicode.IsSpace(runes[i+1])) {
			end = i + 1
		}
	}
	if end < docSnippetLimit/3 {
		end = docSnippetLimit
		for end > 0 && !unicode.IsSpace(runes[end]) {
			end--
		}
		if end == 0 {
			end = docSnippetLimit
		}
	}
	// A full stop before the ellipsis would read as ".…".
	short = strings.TrimRight(strings.TrimSpace(string(runes[:end])), ".,;:-–—")
	return short + "…", full, true
}

// docSnippetNodes is the snippet's paragraph for the card and, when the snippet
// was cut, the disclosure that opens the whole text under the card.
func docSnippetNodes(m Model, snippet string) (inCard, more ui.Node) {
	short, full, clamped := docSnippetParts(snippet)
	inCard = html.P(html.Props{Class: "chat-embed-body", Dir: "auto", Text: short})
	if !clamped {
		return inCard, nil
	}
	showMore, showLess := chatsave002Text(m.Locale, "show_more"), chatsave002Text(m.Locale, "show_less")
	return inCard, html.Details(html.Props{Class: "chat-embed-more"},
		html.Summary(html.Props{}, html.Span(html.Props{Class: "chat-embed-more-open", Text: showMore}), html.Span(html.Props{Class: "chat-embed-more-close", Text: showLess})),
		html.P(html.Props{Class: "chat-embed-body chat-embed-full", Dir: "auto", Text: full}))
}
