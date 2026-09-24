package chatui

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// mentionLimit is how many people the suggestion list offers at once.
const mentionLimit = 8

// mentionState is the composer's open "@" suggestion list. Start and Caret are
// UTF-16 offsets into the field, the units a textarea's selection reports.
type mentionState struct {
	Target      string
	Query       string
	Start, End  int
	Active      int
	Open        bool
	LoadedFresh bool
}

// mentionCandidate is one person the viewer can mention.
type mentionCandidate struct {
	ID, Name string
	Member   bool
}

// mentionTokenAt finds the "@query" the caret sits at the end of. The "@" must
// start a word, and the query is the run of name characters typed after it,
// so an email address or a finished mention followed by a space is ignored.
func mentionTokenAt(value string, caret int) (query string, start int, ok bool) {
	units := utf16.Encode([]rune(value))
	if caret < 0 || caret > len(units) {
		return "", 0, false
	}
	i := caret
	for i > 0 {
		r := rune(units[i-1])
		if r == '@' {
			break
		}
		if !mentionRune(r) || caret-i >= 40 {
			return "", 0, false
		}
		i--
	}
	if i == 0 || rune(units[i-1]) != '@' {
		return "", 0, false
	}
	at := i - 1
	if at > 0 {
		prev := rune(units[at-1])
		if unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == '_' || prev == '@' {
			return "", 0, false
		}
	}
	return string(utf16.Decode(units[i:caret])), at, true
}

func mentionRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' || r == '\''
}

// mentionCandidates ranks the room's members ahead of the wider directory and
// names that start with the query ahead of names with a later word that does.
func mentionCandidates(m Model, query string) []mentionCandidate {
	q := strings.ToLower(strings.TrimSpace(query))
	type scored struct {
		c    mentionCandidate
		rank int
	}
	seen := map[string]bool{}
	var out []scored
	consider := func(id, name string, member bool) {
		name = strings.TrimSpace(name)
		// Members carry subject IDs and the directory carries worker refs, so
		// the same person can arrive under two IDs; the name key merges them.
		nameKey := "name:" + strings.ToLower(name)
		if id == "" || name == "" || seen[id] || seen[nameKey] || name == id {
			return
		}
		seen[nameKey] = true
		lower := strings.ToLower(name)
		rank := -1
		switch {
		case q == "" || strings.HasPrefix(lower, q):
			rank = 0
		default:
			for _, word := range strings.Fields(lower)[1:] {
				if strings.HasPrefix(word, q) {
					rank = 1
					break
				}
			}
		}
		if rank < 0 {
			return
		}
		if !member {
			rank += 2
		}
		seen[id] = true
		out = append(out, scored{mentionCandidate{ID: id, Name: name, Member: member}, rank})
	}
	// Authors already on screen carry resolved names even when the member
	// list has not finished resolving its own, and they are in the room.
	authors := map[string]string{}
	for _, list := range [][]Message{m.Messages, m.ThreadMessages} {
		for _, msg := range list {
			if msg.AuthorID != "" && strings.TrimSpace(msg.Author) != "" && msg.Author != msg.AuthorID {
				authors[msg.AuthorID] = msg.Author
			}
		}
	}
	for _, member := range m.Members {
		name := member.Name
		if (strings.TrimSpace(name) == "" || name == member.ID) && authors[member.ID] != "" {
			name = authors[member.ID]
		}
		consider(member.ID, name, true)
	}
	ids := make([]string, 0, len(authors))
	for id := range authors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		consider(id, authors[id], true)
	}
	for _, person := range m.SearchDirectory {
		consider(person.ID, person.Name, false)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		return strings.ToLower(out[i].c.Name) < strings.ToLower(out[j].c.Name)
	})
	if len(out) > mentionLimit {
		out = out[:mentionLimit]
	}
	result := make([]mentionCandidate, len(out))
	for i := range out {
		result[i] = out[i].c
	}
	return result
}

// applyMention replaces the "@query" between start and end with the person's
// full name and a trailing space, the form chatBodyHasMention matches.
func applyMention(value string, start, end int, name string) (string, int) {
	return insertEmojiAtUTF16(value, "@"+name+" ", start, end)
}

// nextMention moves the highlighted suggestion, wrapping at either end.
func nextMention(active, delta, count int) int {
	if count <= 0 {
		return 0
	}
	return ((active+delta)%count + count) % count
}

// mentionMenu is the suggestion list above a composer. The field keeps focus
// and owns the keys; aria-activedescendant names the highlighted person.
func mentionMenu(m Model, state mentionState, target string) ui.Node {
	if !state.Open || state.Target != target {
		return html.Div(html.Props{Class: "mention-slot"})
	}
	people := mentionCandidates(m, state.Query)
	if len(people) == 0 {
		return html.Div(html.Props{Class: "mention-menu empty", Role: "status"}, html.P(html.Props{Class: "mention-empty", Text: m.tf(KeyMentionNone, map[string]string{"query": state.Query})}))
	}
	rows := make([]ui.Node, 0, len(people)+1)
	rows = append(rows, html.P(html.Props{Class: "mention-heading", Aria: map[string]string{"hidden": "true"}, Text: m.t(KeyMentionTitle)}))
	outsiders := false
	for i, person := range people {
		class := "mention-option"
		if i == state.Active {
			class += " active"
		}
		// People outside the room are listed once under their own heading
		// rather than carrying the same note on every row.
		if !person.Member && !outsiders {
			outsiders = true
			rows = append(rows, html.WithKey(html.P(html.Props{Class: "mention-heading outside", Aria: map[string]string{"hidden": "true"}, Text: m.t(KeyMentionNotMember)}), "outside"))
		}
		detail := ""
		rows = append(rows, html.WithKey(html.Button(html.Props{ID: target + "-mention-" + itoa(i+1), Class: class, Type: "button", Role: "option", TabIndex: -1,
			Data: map[string]string{"action": "mention-pick", "id": target, "extra": itoa(i + 1)},
			Aria: map[string]string{"selected": boolString(i == state.Active)}},
			personAvatar(m, person.ID, person.Name, "avatar small"),
			html.Span(html.Props{Class: "mention-name", Text: person.Name}),
			html.Span(html.Props{Class: "mention-detail", Text: detail})), "mention:"+person.ID))
	}
	return html.Div(html.Props{ID: target + "-mentions", Class: "mention-menu", Role: "listbox", Aria: map[string]string{"label": m.t(KeyMentionTitle)}},
		append(rows, html.P(html.Props{Class: "mention-hint", Aria: map[string]string{"hidden": "true"}, Text: m.t(KeyMentionHint)}))...)
}

// mentionFieldAria points a composer at its open suggestion list so a screen
// reader announces the highlighted person while focus stays in the field.
func mentionFieldAria(state mentionState, target string, aria map[string]string) map[string]string {
	out := map[string]string{"autocomplete": "list"}
	for k, v := range aria {
		out[k] = v
	}
	if state.Open && state.Target == target {
		out["controls"] = target + "-mentions"
		out["activedescendant"] = target + "-mention-" + itoa(state.Active+1)
	}
	return out
}

// mentionBox holds the live suggestion state. Keystrokes can arrive faster than
// renders, and a state hook read inside a handler returns the value of the
// last render: typing " in @a" then read a list that had already closed and
// the new "@a" never opened it. Handlers read and write the box; the tick
// only asks for a render.
type mentionBox struct {
	state mentionState
	seq   uint64
}

type mentionStore struct {
	box  *mentionBox
	tick ui.State[uint64]
}

func (s mentionStore) Get() mentionState { return s.box.state }

func (s mentionStore) Set(v mentionState) {
	if s.box.state == v {
		return
	}
	s.box.state = v
	s.box.seq++
	s.tick.Set(s.box.seq)
}

// composerNotice is a one-line status above the composer for a command that
// could not run; an empty slot keeps the textarea's position stable.
func composerNotice(text string) ui.Node {
	if text == "" {
		return html.Div(html.Props{Class: "composer-notice-slot"})
	}
	return html.P(html.Props{Class: "composer-notice", Role: "status", Aria: map[string]string{"live": "polite"}, Text: text})
}
