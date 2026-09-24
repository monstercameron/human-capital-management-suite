package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// highlightText marks every case-insensitive occurrence of each query word in
// text. Matching is rune by rune so a case fold that changes byte length (for
// example Turkish dotted I) cannot shift a mark into the wrong characters.
func highlightText(text, query string) []ui.Node {
	words := []([]rune){}
	for _, w := range strings.Fields(query) {
		if r := []rune(w); len(r) > 0 {
			words = append(words, r)
		}
	}
	runes := []rune(text)
	if len(words) == 0 || len(runes) == 0 {
		return []ui.Node{ui.Text(text)}
	}
	var out []ui.Node
	plain := 0
	for i := 0; i < len(runes); {
		matched := 0
		for _, w := range words {
			if len(w) > matched && i+len(w) <= len(runes) && strings.EqualFold(string(runes[i:i+len(w)]), string(w)) {
				matched = len(w)
			}
		}
		if matched == 0 {
			i++
			continue
		}
		if plain < i {
			out = append(out, ui.Text(string(runes[plain:i])))
		}
		out = append(out, html.Mark(html.Props{Class: "search-hit"}, ui.Text(string(runes[i:i+matched]))))
		i += matched
		plain = i
	}
	if plain < len(runes) {
		out = append(out, ui.Text(string(runes[plain:])))
	}
	return out
}

// searchWhen says when a hit was sent the way the timeline's dividers do:
// "Today", "Yesterday" or the date, then the message's own time label.
func searchWhen(m Model, msg Message) string {
	if msg.SentAt.IsZero() {
		return msg.TimeLabel
	}
	day := dayLabel(m, msg.SentAt)
	clock := msg.TimeLabel
	if clock == "" {
		clock = msg.SentAt.Local().Format(time.Kitchen)
	}
	return day + " · " + clock
}

// searchSnippet flattens a message to one line and, when the first match sits
// past the opening words, starts the excerpt shortly before it, so a hit deep
// in a long post still shows the matched words instead of the post's opening.
func searchSnippet(body, query string, limit int) string {
	// List items read as bullets once the lines are joined.
	for _, marker := range []string{"- ", "* "} {
		body = strings.ReplaceAll(body, ":\n"+marker, ": ")
		body = strings.ReplaceAll(body, "\n"+marker, " · ")
	}
	flat := []rune(strings.Join(strings.Fields(body), " "))
	if limit <= 0 || len(flat) <= limit {
		return string(flat)
	}
	first := -1
	for _, w := range strings.Fields(query) {
		word := []rune(w)
		for i := 0; i+len(word) <= len(flat); i++ {
			if strings.EqualFold(string(flat[i:i+len(word)]), w) {
				if first < 0 || i < first {
					first = i
				}
				break
			}
		}
	}
	start := 0
	if first > limit/3 {
		start = first - limit/4
	}
	end := start + limit
	if end > len(flat) {
		end, start = len(flat), max(0, len(flat)-limit)
	}
	// Cut at word boundaries so an excerpt never opens or closes mid-word.
	if start > 0 {
		for start < end && flat[start-1] != ' ' {
			start++
		}
	}
	if end < len(flat) {
		for end > start && flat[end] != ' ' {
			end--
		}
	}
	out := strings.TrimSpace(string(flat[start:end]))
	if start > 0 {
		out = "…" + out
	}
	if end < len(flat) {
		out += "…"
	}
	return out
}

// searchGroupHeading names a result group and how many of its results are on
// screen, so the page needs no separate total above the first group.
func searchGroupHeading(m Model, key string, count int) ui.Node {
	return html.H3(html.Props{Class: "search-group-heading"}, ui.Text(m.t(key)), html.Span(html.Props{Class: "search-group-count", Text: m.n(count)}))
}

// searchContext renders "in {channel}" with the conversation's rail glyph
// placed before the name, where the template puts the name, so a group
// message reads "in [icon] comp cycle working group" like "in #engineering".
func searchContext(m Model, glyph ui.Node, channel string) []ui.Node {
	template := m.t(KeySearchMessageIn)
	before, after, found := strings.Cut(template, "{channel}")
	if !found {
		return []ui.Node{glyph, ui.Text(m.tf(KeySearchMessageIn, map[string]string{"channel": channel}))}
	}
	return []ui.Node{ui.Text(before), glyph, ui.Text(channel + after)}
}

// unreadDot marks the conversations toggle when some other conversation has
// unread messages, so a folded-away rail still says there is news in it.
func unreadDot(m Model) ui.Node {
	for _, c := range m.Conversations {
		if c.ID != m.SelectedID && c.Unread > 0 && !c.Muted {
			return html.Span(html.Props{Class: "rail-pill-dot", Aria: map[string]string{"hidden": "true"}})
		}
	}
	return nil
}
