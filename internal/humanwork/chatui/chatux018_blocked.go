package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-018. A message the filter blocked kept its text in the box and said so
// in one grey line, and the word was not marked anywhere. The line is now in the
// warning colour and the refused text is shown under it with each offending word
// underlined, since the box itself cannot underline part of its text.

// chatux018ExcerptLimit is how much of the refused text is shown.
const chatux018ExcerptLimit = 160

// chatux018Draft is the refused text, cut around the first offending word when
// it is long, with every offending word marked.
func chatux018Draft(entry AuthorBlocked) ui.Node {
	text := strings.TrimSpace(entry.Text)
	if text == "" || len(entry.Words) == 0 {
		return nil
	}
	words := [][]rune{}
	for _, word := range entry.Words {
		if r := []rune(strings.TrimSpace(word)); len(r) > 0 {
			words = append(words, r)
		}
	}
	if len(words) == 0 {
		return nil
	}
	query := []string{}
	for _, word := range words {
		query = append(query, string(word))
	}
	text = searchSnippet(text, strings.Join(query, " "), chatux018ExcerptLimit)
	runes := []rune(text)
	nodes := []ui.Node{}
	plain := 0
	for _, span := range highlightSpans(runes, words) {
		if plain < span[0] {
			nodes = append(nodes, ui.Text(string(runes[plain:span[0]])))
		}
		nodes = append(nodes, html.Mark(html.Props{Class: "chatmod002-word"}, ui.Text(string(runes[span[0]:span[1]]))))
		plain = span[1]
	}
	if plain < len(runes) {
		nodes = append(nodes, ui.Text(string(runes[plain:])))
	}
	// The field already holds this text, so a reader of the page hears it once.
	return html.P(html.Props{Class: "chatmod002-draft", Dir: "auto", Aria: map[string]string{"hidden": "true"}}, nodes...)
}
