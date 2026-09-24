package chatui

import (
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The composer's document autocomplete: typing "[[" or "doc:" lists
// documents the reader can open (Callbacks.SuggestDocuments asks the
// document service, which only ever returns readable titles). Picking one
// writes "doc:<id> ", the reference DocReferences unfurls.

const docSuggestLimit = 8

// DocSuggestion is one readable document offered by the list.
type DocSuggestion struct {
	ID, Title, Detail string
}

// docSuggestState is the open list. Start and End are UTF-16 offsets of
// the trigger and query in the field, the units a textarea reports.
type docSuggestState struct {
	Target, Query string
	Start, End    int
	Open, Loading bool
	Active        int
	Items         []DocSuggestion
	seq           uint64
}

// docTokenAt finds the "[[query" or "doc:query" the caret sits at the end
// of. The trigger must start a word; the query stops at a newline or "]".
func docTokenAt(value string, caret int) (query string, start int, ok bool) {
	units := utf16.Encode([]rune(value))
	if caret < 0 || caret > len(units) {
		return "", 0, false
	}
	before := string(utf16.Decode(units[:caret]))
	wordStart := func(i int) bool {
		if i == 0 {
			return true
		}
		prev := []rune(before[:i])
		r := prev[len(prev)-1]
		return unicode.IsSpace(r) || strings.ContainsRune("([{\"'", r)
	}
	pick := func(i, trigger int) (string, int, bool) {
		q := before[i+trigger:]
		if strings.ContainsAny(q, "]\n") || len([]rune(q)) > 60 || !wordStart(i) {
			return "", 0, false
		}
		return q, len(utf16.Encode([]rune(before[:i]))), true
	}
	query, start, ok = "", 0, false
	at := -1
	for _, trigger := range []string{"[[", "doc:"} {
		i := strings.LastIndex(before, trigger)
		if i <= at && at >= 0 || i < 0 {
			continue
		}
		q, s, found := pick(i, len(trigger))
		if found && trigger == "doc:" && strings.ContainsAny(q, " 	") {
			found = false
		}
		if found {
			query, start, ok, at = q, s, true, i
		}
	}
	return query, start, ok
}

// applyDocSuggestion writes "doc:<id> " over the trigger and query.
func applyDocSuggestion(value string, start, end int, id string) (string, int) {
	return insertEmojiAtUTF16(value, "doc:"+id+" ", start, end)
}

// docSuggestTrack follows the composer: it opens, re-queries or closes the
// list as the text before the caret changes.
func docSuggestTrack(m Model, local localStore, target string) {
	current := local.get().docSuggest
	value, caret, ok := composerSelection(target)
	query, start, found := docTokenAt(value, caret)
	if !ok || !found || m.Callbacks.SuggestDocuments == nil {
		if current.Open {
			local.update(func(u *localUI) { u.docSuggest = docSuggestState{} })
		}
		return
	}
	if current.Open && current.Target == target && current.Query == query && current.Start == start {
		return
	}
	next := docSuggestState{Target: target, Query: query, Start: start, End: caret, Open: true, Loading: true, seq: current.seq + 1}
	if current.Open && current.Target == target {
		next.Items = current.Items
	}
	local.update(func(u *localUI) { u.docSuggest = next })
	seq := next.seq
	m.Callbacks.SuggestDocuments(query, func(items []DocSuggestion) {
		live := local.get().docSuggest
		if !live.Open || live.seq != seq {
			return
		}
		if len(items) > docSuggestLimit {
			items = items[:docSuggestLimit]
		}
		local.update(func(u *localUI) {
			u.docSuggest.Items, u.docSuggest.Loading, u.docSuggest.Active = items, false, 0
		})
	})
}

// docSuggestKey routes the list's keys while it is open; it reports
// whether the key was the list's.
func docSuggestKey(local localStore, key, target string) bool {
	state := local.get().docSuggest
	if !state.Open || state.Target != target {
		return false
	}
	count := len(state.Items)
	switch key {
	case "ArrowDown", "ArrowUp":
		if count > 0 {
			delta := 1
			if key == "ArrowUp" {
				delta = -1
			}
			local.update(func(u *localUI) { u.docSuggest.Active = nextMention(state.Active, delta, count) })
		}
		return true
	case "Enter", "Tab":
		if count == 0 {
			local.update(func(u *localUI) { u.docSuggest = docSuggestState{} })
			return false
		}
		docSuggestPick(local, state.Active)
		return true
	case "Escape":
		local.update(func(u *localUI) { u.docSuggest = docSuggestState{} })
		return true
	}
	return false
}

// docSuggestPick writes the chosen document into the field it was typed
// in, re-reading the field so the replacement lands on the query it holds
// now.
func docSuggestPick(local localStore, index int) {
	state := local.get().docSuggest
	local.update(func(u *localUI) { u.docSuggest = docSuggestState{} })
	if !state.Open || index < 0 || index >= len(state.Items) {
		return
	}
	value, caret, ok := composerSelection(state.Target)
	if !ok {
		return
	}
	if _, start, found := docTokenAt(value, caret); found && start == state.Start {
		updated, next := applyDocSuggestion(value, start, caret, state.Items[index].ID)
		replaceComposerText(state.Target, updated, next)
	}
}

// docSuggestMenu is the list above the composer, in the mention list's
// place and style; the field keeps focus and owns the keys.
func docSuggestMenu(m Model, state docSuggestState, target string) ui.Node {
	if !state.Open || state.Target != target {
		return html.Div(html.Props{Class: "doc-suggest-slot"})
	}
	if len(state.Items) == 0 {
		text := m.t(KeyDocSuggestNone)
		if state.Loading {
			text = m.t(KeyEmbedLoading)
		}
		return html.Div(html.Props{Class: "mention-menu doc-suggest-menu empty", Role: "status"}, html.P(html.Props{Class: "mention-empty", Text: text}))
	}
	rows := []ui.Node{html.P(html.Props{Class: "mention-heading", Aria: map[string]string{"hidden": "true"}, Text: m.t(KeyDocSuggestTitle)})}
	for i, item := range state.Items {
		class := "mention-option doc-suggest-option"
		if i == state.Active {
			class += " active"
		}
		rows = append(rows, html.WithKey(html.Button(html.Props{ID: target + "-doc-" + itoa(i+1), Class: class, Type: "button", Role: "option", TabIndex: -1,
			Data: map[string]string{"action": "doc-suggest-pick", "id": target, "extra": itoa(i + 1)},
			Aria: map[string]string{"selected": boolString(i == state.Active)}},
			html.Span(html.Props{Class: "mention-name", Dir: "auto", Text: item.Title}),
			html.Span(html.Props{Class: "mention-detail", Dir: "auto", Text: item.Detail})), "doc:"+item.ID))
	}
	return html.Div(html.Props{ID: target + "-docs", Class: "mention-menu doc-suggest-menu", Role: "listbox", Aria: map[string]string{"label": m.t(KeyDocSuggestTitle)}}, rows...)
}

// docSuggestFieldAria points the composer at the open document list.
func docSuggestFieldAria(state docSuggestState, target string, aria map[string]string) map[string]string {
	if !state.Open || state.Target != target || len(state.Items) == 0 {
		return aria
	}
	out := map[string]string{}
	for k, v := range aria {
		out[k] = v
	}
	out["autocomplete"] = "list"
	out["controls"] = target + "-docs"
	out["activedescendant"] = target + "-doc-" + itoa(state.Active+1)
	return out
}
