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
	// Insert is the text written for the pick; empty means "doc:<ID>".
	Insert string
}

// DocSuggestStatus is how a document search ended: DocSuggestOK answered
// (an empty answer means nothing matched), DocSuggestFailed could not be
// completed and may work on a retry, DocSuggestUnavailable means document
// search is not set up for this reader.
type DocSuggestStatus int

const (
	DocSuggestOK DocSuggestStatus = iota
	DocSuggestFailed
	DocSuggestUnavailable
)

// docSuggestState is the open list. Start and End are UTF-16 offsets of
// the trigger and query in the field, the units a textarea reports.
type docSuggestState struct {
	Target, Query string
	// Kind is which picker is open: empty for documents, SuggestKindTicket or
	// SuggestKindBoard (issue 3747).
	Kind          string
	Start, End    int
	Open, Loading bool
	Active        int
	Items         []DocSuggestion
	Status        DocSuggestStatus
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
	kind, query, start, found := suggestTokenAt(value, caret)
	if !ok || !found || workSuggestSource(m, kind) == nil {
		if current.Open {
			local.update(func(u *localUI) { u.docSuggest = docSuggestState{} })
		}
		return
	}
	if current.Open && current.Target == target && current.Kind == kind && current.Query == query && current.Start == start {
		return
	}
	next := docSuggestState{Target: target, Kind: kind, Query: query, Start: start, End: caret, Open: true, Loading: true, seq: current.seq + 1}
	if current.Open && current.Target == target && current.Kind == kind {
		next.Items = current.Items
	}
	docSuggestRun(m, local, next)
}

// docSuggestRun shows next as the open list and asks for its documents; an
// answer for a list that has since moved on is dropped.
func docSuggestRun(m Model, local localStore, next docSuggestState) {
	local.update(func(u *localUI) { u.docSuggest = next })
	seq := next.seq
	source := workSuggestSource(m, next.Kind)
	if source == nil {
		local.update(func(u *localUI) { u.docSuggest = docSuggestState{} })
		return
	}
	source(next.Query, func(items []DocSuggestion, status DocSuggestStatus) {
		live := local.get().docSuggest
		if !live.Open || live.seq != seq {
			return
		}
		if next.Kind != "" {
			// A ticket or board whose ids cannot form a canonical address has
			// no link to write; picking it must not fall back to a "doc:" token.
			kept := make([]DocSuggestion, 0, len(items))
			for _, item := range items {
				if item.Insert != "" {
					kept = append(kept, item)
				}
			}
			items = kept
		}
		if len(items) > docSuggestLimit {
			items = items[:docSuggestLimit]
		}
		if status != DocSuggestOK {
			items = nil
		}
		local.update(func(u *localUI) {
			u.docSuggest.Items, u.docSuggest.Loading, u.docSuggest.Active, u.docSuggest.Status = items, false, 0, status
		})
	})
}

// docSuggestRetry asks again for the open list's query after a failure.
func docSuggestRetry(m Model, local localStore) {
	current := local.get().docSuggest
	if !current.Open || current.Status != DocSuggestFailed || workSuggestSource(m, current.Kind) == nil {
		return
	}
	next := current
	next.Loading, next.Status, next.Items, next.seq = true, DocSuggestOK, nil, current.seq+1
	docSuggestRun(m, local, next)
	// The button that was pressed is replaced by the searching frame; the
	// caret goes back to the field the list belongs to.
	focusChatLayerField(current.Target)
}

// docSuggestRetryKey lets a keyboard reader retry a failed search from the
// field: Tab, which accepts a suggestion when there is one, asks again when
// the list is showing the failure. It reports whether the key was used.
func docSuggestRetryKey(m Model, local localStore, key, target string) bool {
	state := local.get().docSuggest
	if key != "Tab" || !state.Open || state.Target != target || state.Loading || state.Status != DocSuggestFailed {
		return false
	}
	docSuggestRetry(m, local)
	return true
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
	if kind, _, start, found := suggestTokenAt(value, caret); found && start == state.Start && kind == state.Kind {
		item := state.Items[index]
		updated, next := applyDocSuggestion(value, start, caret, item.ID)
		if item.Insert != "" {
			updated, next = insertEmojiAtUTF16(value, item.Insert+" ", start, caret)
		}
		replaceComposerText(state.Target, updated, next)
	}
}

// docSuggestMenu is the list above the composer, in the mention list's
// place and style; the field keeps focus and owns the keys.
func docSuggestMenu(m Model, state docSuggestState, target string) ui.Node {
	if !state.Open || state.Target != target {
		return html.Div(html.Props{Class: "doc-suggest-slot"})
	}
	titleKey, noneKey, failedKey, unavailableKey := workSuggestCopy(state.Kind)
	if len(state.Items) == 0 {
		if state.Loading {
			return html.Div(html.Props{Class: "mention-menu doc-suggest-menu empty"}, ChatLoadingFrame(LoadingFrame{Locale: m.Locale, Shape: LoadingShapeMenu, Rows: 2, Status: m.t(KeyEmbedLoading)}))
		}
		switch state.Status {
		case DocSuggestFailed:
			return html.Div(html.Props{Class: "mention-menu doc-suggest-menu empty error", Role: "status", Aria: map[string]string{"live": "polite"}},
				html.P(html.Props{Class: "mention-empty", Text: m.t(failedKey)}),
				html.Button(html.Props{Class: "button secondary small", Type: "button", Data: map[string]string{"action": "doc-suggest-retry"}, Text: m.t(KeyRetry)}))
		case DocSuggestUnavailable:
			return html.Div(html.Props{Class: "mention-menu doc-suggest-menu empty", Role: "status"}, html.P(html.Props{Class: "mention-empty", Text: m.t(unavailableKey)}))
		}
		return html.Div(html.Props{Class: "mention-menu doc-suggest-menu empty", Role: "status"}, html.P(html.Props{Class: "mention-empty", Text: m.t(noneKey)}))
	}
	rows := []ui.Node{html.P(html.Props{Class: "mention-heading", Aria: map[string]string{"hidden": "true"}, Text: m.t(titleKey)})}
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
	return html.Div(html.Props{ID: target + "-docs", Class: "mention-menu doc-suggest-menu", Role: "listbox", Aria: map[string]string{"label": m.t(titleKey)}}, rows...)
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
