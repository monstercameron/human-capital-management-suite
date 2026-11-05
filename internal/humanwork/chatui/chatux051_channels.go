package chatui

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-051: typing "#" suggests the channels the person can read and inserts
// a link. The list is the mention list's own shape (mention-menu, one option
// per row, best match first and already highlighted); choosing replaces
// "#query" with the "#label URL" pair the body renderer reads as one link
// (links_channel.go). The client's input and key handlers drive it the way
// they drive the colon list (render.go).

// channelSuggestShown is the length of the list typing "#" opens.
const channelSuggestShown = 5

// channelSuggestState is the list's state, kept in localUI like the colon
// list's: what was typed, where, and which row is highlighted.
type channelSuggestState struct {
	Target     string
	Query      string
	Start, End int
	Active     int
	Open       bool
}

// channelQueryRune reports whether r can be part of a channel query: the
// letters, digits, hyphens and underscores of a channel name.
func channelQueryRune(r rune) bool {
	return r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// channelSuggestToken is the channel query at the caret: "#" and the runes
// after it, with the "#" at the start of the draft or after a space. A digit
// first ("#1"), a second "#" ("##"), a "#" glued to a word ("a#b") and a caret
// in the middle of a word are not a trigger.
func channelSuggestToken(value string, caret int) (query string, start, end int, ok bool) {
	units := utf16.Encode([]rune(value))
	if caret < 0 || caret > len(units) {
		return "", 0, 0, false
	}
	if after := utf16.Decode(units[caret:]); len(after) > 0 && channelQueryRune(after[0]) {
		return "", 0, 0, false
	}
	runes := utf16.Decode(units[:caret])
	start = len(runes)
	for start > 0 && channelQueryRune(runes[start-1]) {
		start--
	}
	if len(runes)-start < 1 || start == 0 || runes[start-1] != '#' || !unicode.IsLetter(runes[start]) {
		return "", 0, 0, false
	}
	hash := start - 1
	if hash > 0 && !unicode.IsSpace(runes[hash-1]) {
		return "", 0, 0, false
	}
	return strings.ToLower(string(runes[start:])), len(utf16.Encode(runes[:hash])), caret, true
}

// channelSuggestCandidates are the conversations "#" may offer: the channels
// the person has joined, in list order. Direct and group conversations are
// not #-channels, and a room the person has not joined is not named.
func channelSuggestCandidates(m Model) []Conversation {
	var out []Conversation
	for _, c := range m.Conversations {
		if c.ID == "" || !c.Joined || (c.Kind != PublicChannel && c.Kind != PrivateChannel) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// channelSuggest is the limit best matches for a query: a channel whose name
// holds the query, in any casing, in list order.
func channelSuggest(m Model, query string) []Conversation {
	return channelSuggestFilter(channelSuggestCandidates(m), query, channelSuggestShown)
}

func channelSuggestFilter(candidates []Conversation, query string, limit int) []Conversation {
	if limit <= 0 {
		return nil
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var out []Conversation
	for _, c := range candidates {
		if query != "" && !strings.Contains(strings.ToLower(c.Name), query) {
			continue
		}
		out = append(out, c)
		if len(out) == limit {
			break
		}
	}
	return out
}

var channelSuggestCopy = map[string][3]string{
	"label": {"Channels", "Kanäle", "القنوات"},
}

func channelSuggestText(m Model, key string) string {
	values, ok := channelSuggestCopy[key]
	if !ok {
		return ""
	}
	return chatbug039Text("chatux051."+key, values[chatbug039LocaleIndex(m.Locale)], values[0])
}

// channelSuggestDetail is what an option says under its name: how many people
// read the channel when that is known, else what kind of room it is.
func channelSuggestDetail(m Model, c Conversation) string {
	if c.MemberCount > 0 {
		return memberCountLabel(m, c.MemberCount)
	}
	return m.t(kindKey(c.Kind))
}

// channelSuggestMenu is the open list, or the slot the closed list keeps among
// the composer's children so closing moves nothing (CHATBUG-086).
func channelSuggestMenu(m Model, state channelSuggestState, target string) ui.Node {
	if !state.Open || state.Target != target {
		return html.Div(html.Props{Class: "channel-suggest-slot"})
	}
	var rows []ui.Node
	for i, c := range channelSuggest(m, state.Query) {
		class := "mention-option channel-suggest-option"
		if i == state.Active {
			class += " active"
		}
		name := displayName(m, c)
		glyph := "hash"
		if c.Kind == PrivateChannel {
			glyph = "lock"
		}
		rows = append(rows, html.Button(html.Props{ID: target + "-channel-option-" + strconv.Itoa(i), Class: class, Type: "button", Role: "option", TabIndex: -1, Title: name,
			Data: map[string]string{"action": "channel-suggest-pick", "extra": strconv.Itoa(i)},
			Aria: map[string]string{"selected": boolString(i == state.Active), "label": name}},
			icon(glyph),
			html.Span(html.Props{Class: "mention-name", Dir: "auto", Text: name}),
			html.Span(html.Props{Class: "mention-detail", Dir: "ltr", Text: channelSuggestDetail(m, c)})))
	}
	return html.Div(html.Props{ID: target + "-channel-suggest", Class: "mention-menu channel-suggest-menu", Role: "listbox", Aria: map[string]string{"label": channelSuggestText(m, "label")}}, rows...)
}

// channelSuggestFieldAria points a composer at its open channel list so a
// screen reader announces the highlighted channel while focus stays in the
// field.
func channelSuggestFieldAria(m Model, state channelSuggestState, target string, base map[string]string) map[string]string {
	if !state.Open || state.Target != target {
		return base
	}
	result := copyMap(base)
	if result == nil {
		result = make(map[string]string)
	}
	result["expanded"], result["controls"], result["autocomplete"] = "true", target+"-channel-suggest", "list"
	if state.Active >= 0 && state.Active < len(channelSuggest(m, state.Query)) {
		result["activedescendant"] = target + "-channel-option-" + strconv.Itoa(state.Active)
	} else {
		delete(result, "activedescendant")
	}
	return result
}

// channelSuggestInsert is the draft after a channel is chosen: "#query" becomes
// the "#label URL" pair, with a space to keep typing in unless one already
// follows the caret, so the sentence around it is never changed.
func channelSuggestInsert(m Model, value string, caret, start int, c Conversation) (string, int) {
	insert := conversationReferenceLabel(m, c) + " " + ChannelReferenceURL(c.ID)
	units := utf16.Encode([]rune(value))
	after := utf16.Decode(units[min(max(caret, 0), len(units)):])
	if len(after) == 0 || !unicode.IsSpace(after[0]) {
		insert += " "
	}
	return insertEmojiAtUTF16(value, insert, start, caret)
}

// channelSuggestTrack runs the "#" list for one input event in a composer.
func channelSuggestTrack(m Model, local localStore, target string) {
	value, caret, ok := composerSelection(target)
	query, start, end, found := channelSuggestToken(value, caret)
	current := local.get().channelSuggest
	next := channelSuggestState{Active: -1}
	if ok && found && len(channelSuggest(m, query)) > 0 {
		// The best match is highlighted, so Enter or Tab takes it; Escape
		// leaves the text.
		next = channelSuggestState{Target: target, Query: query, Start: start, End: end, Open: true, Active: 0}
		if current.Open && current.Query == query && current.Target == target {
			next.Active = current.Active
		}
	}
	if current != next {
		local.update(func(u *localUI) { u.channelSuggest = next })
	}
}

// channelSuggestTyped runs the list for one input event: only the person's own
// typing opens it, in the box that holds the caret (CHATBUG-086).
func channelSuggestTyped(e ui.Event, m Model, local localStore, target string) {
	if !chatbug086Typed(e, target) {
		if local.get().channelSuggest.Open {
			local.update(func(u *localUI) { u.channelSuggest = channelSuggestState{Active: -1} })
		}
		return
	}
	channelSuggestTrack(m, local, target)
}

// channelSuggestKey moves through the open list and picks from it. It reports
// whether it consumed the key.
func channelSuggestKey(m Model, local localStore, key, target string) bool {
	state := local.get().channelSuggest
	if !state.Open || state.Target != target {
		return false
	}
	items := channelSuggest(m, state.Query)
	switch key {
	case "Escape":
		local.update(func(u *localUI) { u.channelSuggest = channelSuggestState{Active: -1} })
		return true
	case "ArrowDown", "ArrowUp":
		if len(items) == 0 {
			return false
		}
		if key == "ArrowUp" {
			state.Active = nextMention(state.Active, -1, len(items))
		} else {
			state.Active = nextMention(state.Active, 1, len(items))
		}
		local.update(func(u *localUI) { u.channelSuggest = state })
		return true
	case "Enter":
		if state.Active < 0 || state.Active >= len(items) {
			local.update(func(u *localUI) { u.channelSuggest = channelSuggestState{Active: -1} })
			return false
		}
		channelSuggestPick(m, local, state.Active)
		return true
	}
	return false
}

// channelSuggestPick replaces the "#query" with the indexed channel's link.
func channelSuggestPick(m Model, local localStore, index int) {
	state := local.get().channelSuggest
	local.update(func(u *localUI) { u.channelSuggest = channelSuggestState{Active: -1} })
	items := channelSuggest(m, state.Query)
	if !state.Open || index < 0 || index >= len(items) {
		return
	}
	value, caret, ok := composerSelection(state.Target)
	query, start, end, found := channelSuggestToken(value, caret)
	if !ok || !found || query != state.Query || start != state.Start || end != state.End {
		return
	}
	updated, next := channelSuggestInsert(m, value, caret, start, items[index])
	replaceComposerText(state.Target, updated, next)
	// The caret stays after the link through the renders that follow.
	chatbug086HoldCaret(state.Target, next)
}
