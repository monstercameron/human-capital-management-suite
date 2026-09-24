package productui

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The split editor's reference autocomplete: "@" suggests people, "#"
// channels and "[[" documents the reader can open. Picking one writes the
// syntax docs_chat_chips.go reads back:
//
//	@handle                  (or @<subject-id> when the handle is shared)
//	[#name](channel:<id>)
//	[Title](doc:<id>)
//
// The browser half (docs_editor_suggest_wasm.go) watches both panes, keeps
// the list beside the caret through the CSSOM and routes the keys.

// Suggestion kinds.
const (
	DocsSuggestPeople   = "people"
	DocsSuggestChannels = "channels"
	DocsSuggestDocs     = "docs"
)

const docsSuggestLimit = 8

// DocsReferenceSuggestion is one row of the list. Insert is the exact
// Markdown written in place of the typed trigger and query.
type DocsReferenceSuggestion struct {
	ID, Label, Detail, Insert string
}

// DocsReferenceSuggester answers one query of one kind. done may be called
// once, on the UI loop, with at most a handful of rows the reader may see.
type DocsReferenceSuggester func(kind, query string, done func([]DocsReferenceSuggestion))

// docsSuggestTrigger reads the reference being typed at the end of before
// (the text up to the caret): its kind, the query typed after the trigger
// and the trigger's byte offset in before.
func docsSuggestTrigger(before string) (kind, query string, start int, ok bool) {
	if i := strings.LastIndex(before, "[["); i >= 0 {
		q := before[i+2:]
		if !strings.ContainsAny(q, "]\n") && utf8.RuneCountInString(q) <= 60 && (i == 0 || before[i-1] != '[') {
			return DocsSuggestDocs, q, i, true
		}
	}
	i := len(before)
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(before[:i])
		if r == '@' || r == '#' {
			break
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_') || len(before)-i >= 40 {
			return "", "", 0, false
		}
		i -= size
	}
	if i == 0 {
		return "", "", 0, false
	}
	at := i - 1
	if at > 0 {
		prev, _ := utf8.DecodeLastRuneInString(before[:at])
		if !(unicode.IsSpace(prev) || strings.ContainsRune("([{\"'", prev)) {
			return "", "", 0, false
		}
	}
	if before[at] == '@' {
		return DocsSuggestPeople, before[i:], at, true
	}
	return DocsSuggestChannels, before[i:], at, true
}

// docsSuggestApply replaces the trigger and query that end at caret in
// markdown with insert and a following space. The formatted pane's Markdown
// may carry the trigger escaped ("\#", "\[\["), so both spellings count.
func docsSuggestApply(markdown string, caret int, kind, insert string) (string, int, bool) {
	if caret < 0 || caret > len(markdown) || insert == "" {
		return markdown, caret, false
	}
	prefix := markdown[:caret]
	plain := strings.NewReplacer(`\[`, "[", `\_`, "_", `\#`, "#").Replace(prefix)
	gotKind, _, start, ok := docsSuggestTrigger(plain)
	if !ok || gotKind != kind {
		return markdown, caret, false
	}
	realStart := docsSuggestEscapedStart(prefix, start)
	next := markdown[:realStart] + insert + " " + strings.TrimPrefix(markdown[caret:], " ")
	return next, realStart + len(insert) + 1, true
}

// docsSuggestEscapedStart maps a byte offset in text with its "[", "_"
// and "#" escapes removed back onto the escaped text.
func docsSuggestEscapedStart(escaped string, plainOffset int) int {
	i, j := 0, 0
	for i < len(escaped) && j < plainOffset {
		if escaped[i] == 0x5c && i+1 < len(escaped) && strings.IndexByte("[_#", escaped[i+1]) >= 0 {
			i += 2
		} else {
			i++
		}
		j++
	}
	return i
}

// docsSuggestLinkText escapes a title for a Markdown link label.
func docsSuggestLinkText(title string) string {
	return strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(strings.Join(strings.Fields(title), " "))
}

// DocsSuggestDocInsert, DocsSuggestChannelInsert and DocsSuggestPersonInsert
// are the Markdown a picked row writes.
func DocsSuggestDocInsert(id, title string) string {
	return "[" + docsSuggestLinkText(title) + "](doc:" + id + ")"
}

func DocsSuggestChannelInsert(id, name string) string {
	return "[#" + docsSuggestLinkText(name) + "](channel:" + id + ")"
}

// DocsSuggestPersonInsert writes "@handle" unless another person in the
// directory shares the handle, then "@<subject-id>".
func DocsSuggestPersonInsert(id, name string, handleShared bool) string {
	handle := DocsPersonHandle(name)
	if handle == "" || handleShared {
		return "@" + id
	}
	return "@" + handle
}

// docsSuggestState is what the list shows.
type docsSuggestState struct {
	Open, Loading bool
	Kind          string
	Items         []DocsReferenceSuggestion
	Active        int
}

// docsSuggestController is the live list: the browser half and the list
// component share it through a ref, so key handling never reads a stale
// render.
type docsSuggestController struct {
	state      docsSuggestState
	query      string
	seq        int
	fetch      DocsReferenceSuggester
	publish    func(docsSuggestState)
	onPick     func(DocsReferenceSuggestion, string)
	onPosition func()
}

func (s *docsSuggestController) emit() {
	if s.publish != nil {
		s.publish(s.state)
	}
}

// update follows the caret: a new kind or query asks again; no trigger
// closes the list.
func (s *docsSuggestController) update(kind, query string, ok bool) {
	if !ok || s.fetch == nil {
		s.close()
		return
	}
	if s.state.Open && s.state.Kind == kind && s.query == query {
		if s.onPosition != nil {
			s.onPosition()
		}
		return
	}
	s.seq++
	seq := s.seq
	s.query = query
	items := s.state.Items
	if s.state.Kind != kind {
		items = nil
	}
	s.state = docsSuggestState{Open: true, Loading: true, Kind: kind, Items: items}
	s.emit()
	if s.onPosition != nil {
		s.onPosition()
	}
	s.fetch(kind, query, func(items []DocsReferenceSuggestion) {
		if seq != s.seq || !s.state.Open {
			return
		}
		if len(items) > docsSuggestLimit {
			items = items[:docsSuggestLimit]
		}
		s.state = docsSuggestState{Open: true, Kind: kind, Items: items}
		s.emit()
	})
}

func (s *docsSuggestController) close() {
	s.seq++
	s.query = ""
	if !s.state.Open {
		return
	}
	s.state = docsSuggestState{}
	s.emit()
}

// key handles one key while the list is open: it reports whether the key
// was the list's (the caller then prevents the default).
func (s *docsSuggestController) key(key string) bool {
	if !s.state.Open {
		return false
	}
	count := len(s.state.Items)
	switch key {
	case "ArrowDown", "ArrowUp":
		if count == 0 {
			return true
		}
		delta := 1
		if key == "ArrowUp" {
			delta = -1
		}
		s.state.Active = (s.state.Active + delta + count) % count
		s.emit()
		return true
	case "Enter", "Tab":
		if count == 0 {
			if key == "Tab" {
				return false
			}
			s.close()
			return false
		}
		s.pick(s.state.Active)
		return true
	case "Escape":
		s.close()
		return true
	}
	return false
}

func (s *docsSuggestController) pick(index int) {
	if index < 0 || index >= len(s.state.Items) {
		return
	}
	item, kind := s.state.Items[index], s.state.Kind
	s.close()
	if s.onPick != nil {
		s.onPick(item, kind)
	}
}

// docsSuggestOptionID is the id of one option, for aria-activedescendant.
func docsSuggestOptionID(index int) string { return "docs-editor-suggest-" + strconv.Itoa(index) }

type docsSuggestListProps struct {
	Locale     string
	Controller *docsSuggestController
	// Initial seeds the list's first render (tools/uxqual/cmd/genfixtures'
	// static fixtures, which never run the browser-side controller that
	// would otherwise populate it); the live editor always leaves this at
	// its zero value and drives the list through Controller instead.
	Initial docsSuggestState
}

// docsSuggestList is the listbox. It re-renders only from its own state,
// which the controller publishes.
func docsSuggestList(props docsSuggestListProps) ui.Node {
	state := ui.UseState(props.Initial)
	if props.Controller != nil {
		props.Controller.publish = func(next docsSuggestState) { state.Set(next) }
	}
	pick := ui.UseEvent(func(event ui.MouseEvent) {
		index, err := strconv.Atoi(docsSuggestEventIndex(event))
		if err == nil && props.Controller != nil {
			props.Controller.pick(index)
		}
	})
	keep := ui.UseEvent(func(event ui.MouseEvent) { event.PreventDefault() })
	current := state.Get()
	locale := props.Locale
	heading := map[string]string{DocsSuggestPeople: "suggest_people", DocsSuggestChannels: "suggest_channels", DocsSuggestDocs: "suggest_docs"}[current.Kind]
	if heading == "" {
		heading = "suggest_docs"
	}
	options := make([]ui.Node, 0, len(current.Items)+1)
	for index, item := range current.Items {
		active := index == current.Active
		class := "docs-suggest-option"
		if active {
			class += " is-active"
		}
		children := []ui.Node{html.Span(html.Props{Class: "docs-suggest-label", Dir: "auto"}, ui.Text(item.Label))}
		if item.Detail != "" {
			children = append(children, html.Span(html.Props{Class: "docs-suggest-detail", Dir: "auto"}, ui.Text(item.Detail)))
		}
		options = append(options, html.Div(html.Props{Key: current.Kind + ":" + item.ID, ID: docsSuggestOptionID(index), Class: class, Role: "option",
			Aria: map[string]string{"selected": strconv.FormatBool(active)}, Data: map[string]string{"suggest-index": strconv.Itoa(index)}}, children...))
	}
	status := ""
	switch {
	case current.Open && current.Loading && len(current.Items) == 0:
		status = docsChatText(locale, "suggest_loading")
	case current.Open && !current.Loading && len(current.Items) == 0:
		status = docsChatText(locale, "suggest_empty")
	}
	return html.Div(html.Props{ID: "docs-editor-suggest-box", Class: "docs-suggest", Hidden: !current.Open, OnMouseDown: keep, OnClick: pick},
		html.Div(html.Props{Class: "docs-suggest-head", ID: "docs-editor-suggest-head"}, ui.Text(docsChatText(locale, heading))),
		html.Div(html.Props{ID: "docs-editor-suggest", Role: "listbox", Aria: map[string]string{"labelledby": "docs-editor-suggest-head"}}, options...),
		html.P(html.Props{Class: "docs-suggest-status", Raw: map[string]any{"role": "status", "aria-live": "polite"}, Hidden: status == ""}, ui.Text(status)),
	)
}

// docsSuggestStylesheet places the list: fixed, with its inset set from
// the caret through the CSSOM custom properties below.
func docsSuggestStylesheet() string {
	return `
.docs-suggest{--docs-suggest-top:0px;--docs-suggest-start:0px;position:fixed;inset-block-start:var(--docs-suggest-top);inset-inline-start:var(--docs-suggest-start);z-index:40;min-width:14rem;max-width:min(24rem,calc(100vw - 2rem));max-height:18rem;overflow:auto;padding:var(--hcm-space-1);background:var(--surface);color:var(--ink);border:1px solid var(--line);border-radius:var(--hcm-radius-control);box-shadow:var(--hcm-shadow-raised,0 8px 24px rgb(0 0 0 / .18))}
.docs-suggest[hidden]{display:none}
.docs-suggest-head{padding:0 var(--hcm-space-1) var(--hcm-space-1);font-size:var(--hcm-font-size-small);color:var(--muted);font-weight:600;text-align:start}
.docs-suggest-option{display:grid;gap:2px;padding:var(--hcm-space-1);border-radius:var(--hcm-radius-control);cursor:pointer;text-align:start}
.docs-suggest-option.is-active{background:var(--soft);outline:var(--hcm-focus-ring-width) solid var(--hcm-color-focus);outline-offset:-2px}
.docs-suggest-label{font-weight:600;overflow-wrap:anywhere}
.docs-suggest-detail{font-size:var(--hcm-font-size-small);color:var(--muted);overflow-wrap:anywhere}
.docs-suggest-status{margin:0;padding:var(--hcm-space-1);color:var(--muted);font-size:var(--hcm-font-size-small)}
.docs-suggest-status[hidden]{display:none}
`
}
