package chatui

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type emojiCompletion struct {
	Target, Query string
	Start, End    int
	Active        int
	Open          bool
}

// Colon completion belongs to a word such as :smile. A sentence's colon and
// its following space never become a query or take over the Send key.
func emojiCompletionToken(value string, caret int) (string, int, bool) {
	units := utf16.Encode([]rune(value))
	if caret < 0 || caret > len(units) {
		return "", 0, false
	}
	runes := utf16.Decode(units[:caret])
	start := len(runes)
	for start > 0 && emojiQueryRune(runes[start-1]) {
		start--
	}
	if len(runes)-start < 2 || start == 0 || runes[start-1] != ':' || !unicode.IsLetter(runes[start]) {
		return "", 0, false
	}
	colon := start - 1
	if colon > 0 && !unicode.IsSpace(runes[colon-1]) {
		return "", 0, false
	}
	return strings.ToLower(string(runes[start:])), len(utf16.Encode(runes[:colon])), true
}

// emojiChoice is one offered emoji: its glyph (in the person's skin tone), its
// name in the reader's language and its ":shortcode:". name is what a query is
// matched against while the emoji data has not loaded.
type emojiChoice struct{ name, glyph, label, code string }

// emojiCompletionItems are the five best matches for a colon query. They come
// from the full emoji data once it has loaded (the first colon typed starts the
// load); until then the short starter list answers.
func emojiCompletionItems(query string) []emojiChoice {
	if ix := chatEmojiData.index; ix != nil {
		return emojiSuggest(ix, query, chatEmojiHost.prefs, emojiSuggestionsShown)
	}
	var items []emojiChoice
	for _, item := range []emojiChoice{{name: "smile", glyph: "😀"}, {name: "laugh", glyph: "😂"}, {name: "heart", glyph: "❤️"}, {name: "thumbsup", glyph: "👍"}, {name: "celebrate", glyph: "🎉"}, {name: "thanks", glyph: "🙏"}, {name: "fire", glyph: "🔥"}, {name: "eyes", glyph: "👀"}} {
		if strings.HasPrefix(item.name, query) {
			item.code = ":" + item.name + ":"
			items = append(items, item)
		}
	}
	return items
}

func emojiCompletionTrack(local localStore, target string) {
	value, caret, ok := composerSelection(target)
	query, start, found := emojiCompletionToken(value, caret)
	current := local.get().emojiCompletion
	// A colon in the draft is a reason to have the emoji data: the list and the
	// ":shortcode:" conversion at Send both need it.
	if strings.Contains(value, ":") {
		chatEmojiEnsureData(false)
	}
	next := emojiCompletion{Active: -1}
	if ok && found && len(emojiCompletionItems(query)) > 0 {
		// The best match is highlighted, so Enter or Tab takes it; Escape leaves the text.
		next = emojiCompletion{Target: target, Query: query, Start: start, End: caret, Open: true, Active: 0}
		if current.Open && current.Query == query && current.Target == target {
			next.Active = current.Active
		}
	}
	if current != next {
		local.update(func(u *localUI) { u.emojiCompletion = next })
	}
}

func emojiCompletionKey(local localStore, key, target string) bool {
	state := local.get().emojiCompletion
	if !state.Open || state.Target != target {
		return false
	}
	items := emojiCompletionItems(state.Query)
	switch key {
	case "Escape":
		local.update(func(u *localUI) { u.emojiCompletion = emojiCompletion{Active: -1} })
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
		local.update(func(u *localUI) { u.emojiCompletion = state })
		return true
	case "Enter":
		if state.Active < 0 || state.Active >= len(items) {
			local.update(func(u *localUI) { u.emojiCompletion = emojiCompletion{Active: -1} })
			return false
		}
		emojiCompletionPick(local, state.Active)
		return true
	}
	return false
}

func emojiCompletionPick(local localStore, index int) {
	state := local.get().emojiCompletion
	local.update(func(u *localUI) { u.emojiCompletion = emojiCompletion{Active: -1} })
	items := emojiCompletionItems(state.Query)
	if !state.Open || index < 0 || index >= len(items) {
		return
	}
	value, caret, ok := composerSelection(state.Target)
	query, start, found := emojiCompletionToken(value, caret)
	if !ok || !found || query != state.Query || start != state.Start {
		return
	}
	updated, next := insertEmojiAtUTF16(value, items[index].glyph+" ", start, caret)
	replaceComposerText(state.Target, updated, next)
	// Choosing here counts as a use, like choosing in the picker.
	chatEmojiHost.record(items[index].glyph)
}

func emojiCompletionMenu(model Model, state emojiCompletion, target string) ui.Node {
	if !state.Open || state.Target != target {
		return nil
	}
	var rows []ui.Node
	for i, item := range emojiCompletionItems(state.Query) {
		class := "mention-option emoji-completion-option"
		if i == state.Active {
			class += " active"
		}
		// Visible: the emoji's name. Spoken: the same, or "Emoji 😀" while only the
		// starter list (no names) is available.
		name, shown := item.label, item.label
		if name == "" {
			name, shown = chatEmojiFormat(model, emojiKeyItem, map[string]string{"emoji": item.glyph}), item.name
		}
		rows = append(rows, html.Button(html.Props{ID: target + "-emoji-option-" + strconv.Itoa(i), Class: class, Type: "button", Role: "option", TabIndex: -1, Title: name,
			Data: map[string]string{"action": "emoji-completion-pick", "extra": strconv.Itoa(i)},
			Aria: map[string]string{"selected": boolString(i == state.Active), "label": name}},
			html.Span(html.Props{Class: "mention-emoji", Aria: map[string]string{"hidden": "true"}, Text: item.glyph}),
			html.Span(html.Props{Class: "mention-name", Dir: "auto", Text: shown}),
			html.Span(html.Props{Class: "mention-detail", Dir: "ltr", Text: item.code})))
	}
	return html.Div(html.Props{ID: target + "-emoji-completion", Class: "mention-menu emoji-completion-menu", Role: "listbox", Aria: map[string]string{"label": chatEmojiText(model, emojiKeyTitle)}}, rows...)
}

func emojiCompletionFieldAria(state emojiCompletion, target string, base map[string]string) map[string]string {
	if !state.Open || state.Target != target {
		return base
	}
	result := copyMap(base)
	if result == nil {
		result = make(map[string]string)
	}
	result["expanded"], result["controls"], result["autocomplete"] = "true", target+"-emoji-completion", "list"
	if state.Active >= 0 && state.Active < len(emojiCompletionItems(state.Query)) {
		result["activedescendant"] = target + "-emoji-option-" + strconv.Itoa(state.Active)
	} else {
		delete(result, "activedescendant")
	}
	return result
}
