package chatui

import (
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/emojiset"
)

// groupIcons are the category tabs' icons, by Unicode group id: one emoji each,
// so the strip reads as emoji rather than as words. The name of the category is
// the tab's tooltip and accessible name.
var emojiGroupIcons = map[string]string{
	"smileys-emotion": "😀", "people-body": "👋", "animals-nature": "🐻", "food-drink": "🍔",
	"travel-places": "✈️", "activities": "⚽", "objects": "💡", "symbols": "❤️", "flags": "🏳️",
}

const (
	emojiFrequentIcon = "🕘"
	emojiToneHand     = "✋"
)

// chatEmojiPicker is the one emoji picker: the composer's, the thread composer's
// and the reaction picker are all this node. kind is the chat layer kind
// ("emoji" for a composer, "reaction" for a message). The rows in the page are
// only those near the viewport; the spacers stand in for the rest.
func chatEmojiPicker(m Model, st emojiPickerState, kind string, ix *emojiset.Index, status emojiDataStatus, prefs emojiPrefs) ui.Node {
	return chatEmojiPickerNode(m, st, kind, ix, status, prefs, false)
}

// chatEmojiShell is a composer's picker while it is closed: the same element, drawn
// from what is compiled in (the frequently used row and the person's own most
// used), hidden. Opening shows this element at once from the click itself, before
// the render that follows and before the emoji data has arrived; that render then
// gives the same element its categories. It is not a layer (no data-chat-layer,
// no data-emoji-picker) until it is open.
func chatEmojiShell(m Model, target string, prefs emojiPrefs) ui.Node {
	return chatEmojiPickerNode(m, emojiOpenState(target, false, false, false), "emoji", nil, emojiDataIdle, prefs, true)
}

func chatEmojiPickerNode(m Model, st emojiPickerState, kind string, ix *emojiset.Index, status emojiDataStatus, prefs emojiPrefs, shell bool) ui.Node {
	v := buildEmojiView(st, ix, prefs)
	base := "emoji-" + kind + "-" + st.Target
	// A hidden shell is not yet anybody's picker: its controls name no target (they
	// act on the open picker, which is found from the page's state), so no
	// composer id appears in the page twice.
	inner := st
	if shell {
		inner.Target = ""
	}
	total := v.seq.len()
	active := -1
	if total > 0 {
		active = max(0, min(st.Active, total-1))
	}
	label := chatEmojiText(m, emojiKeyTitle)
	if st.Reaction {
		label = chatEmojiText(m, emojiKeyReactTitle)
	}
	search := chatEmojiText(m, emojiKeySearch)
	inputAria := map[string]string{"label": search, "controls": base + "-grid", "expanded": "true", "autocomplete": "list"}
	if active >= 0 {
		inputAria["activedescendant"] = base + "-c" + strconv.Itoa(active)
	}
	viewH := st.ViewH
	if viewH <= 0 {
		viewH = emojiDefaultViewH
	}

	var gridRows []ui.Node
	first, last := emojiWindow(v.rows, st.Scroll, viewH, v.metrics)
	topSpace, bottomSpace := 0.0, 0.0
	if last >= first && last >= 0 {
		topSpace = v.rows[first].Y
		bottomSpace = v.total - (v.rows[last].Y + v.rows[last].H)
		for i := first; i <= last; i++ {
			gridRows = append(gridRows, chatEmojiRow(m, inner, v, base, i, active))
		}
	}

	scroll := []ui.Node{}
	if label := chatEmojiStickyLabel(m, st, v); label != "" {
		scroll = append(scroll, html.Div(html.Props{Class: "emoji-pop-sticky", Aria: map[string]string{"hidden": "true"}}, html.Span(html.Props{Text: label})))
	}
	scroll = append(scroll,
		html.Div(html.Props{Class: "emoji-pop-spacer", Data: map[string]string{"h": strconv.Itoa(int(topSpace))}}),
		html.Div(html.Props{ID: base + "-grid", Class: "emoji-pop-grid", Role: "grid", Aria: map[string]string{"label": chatEmojiText(m, emojiKeyGrid), "rowcount": strconv.Itoa(len(v.rows)), "colcount": strconv.Itoa(emojiCols)}}, gridRows...),
		html.Div(html.Props{Class: "emoji-pop-spacer", Data: map[string]string{"h": strconv.Itoa(int(bottomSpace))}}),
	)
	if note := chatEmojiNote(m, st, v, status); note != nil && !shell {
		scroll = append(scroll, note)
	}

	// The picker places itself (chatEmojiPlaceLayer); the shared layer code leaves it alone.
	data := map[string]string{"emoji-picker": kind, "target": st.Target, "touch": boolString(st.Touch || st.Sheet), "sheet": boolString(st.Sheet), chatLayerSelfPlacedAttr: "true"}
	if st.ScrollSet {
		data["scroll-to"] = strconv.Itoa(int(st.Scroll))
	}
	props := html.Props{ID: base, Class: "emoji-pop", Role: "dialog", Data: data, Aria: map[string]string{"label": label}}
	if shell {
		delete(data, "emoji-picker")
		delete(data, chatLayerSelfPlacedAttr)
		data["emoji-shell"] = kind
		props.Raw = map[string]any{"popover": "manual"}
	}
	children := []ui.Node{
		html.Div(html.Props{Class: "emoji-pop-search"},
			html.Input(html.Props{ID: base + "-search", Type: "search", Class: "emoji-pop-input", Role: "combobox", Placeholder: search, AutoComplete: "off", AutoFocus: true, Dir: "auto",
				Data: map[string]string{"emoji-search": "true"}, Aria: inputAria, Raw: map[string]any{"spellcheck": "false"}})),
		chatEmojiTabs(m, inner, v, base),
		html.Div(html.Props{ID: base + "-scroll", Class: "emoji-pop-scroll", Data: map[string]string{"emoji-scroll": "true"}}, scroll...),
		chatEmojiFooter(m, inner, v, base, active),
	}
	if shell {
		return html.Div(props, children...)
	}
	return anchoredChatLayer(props, kind, children...)
}

// chatEmojiRow is one row of the grid: a section heading, or up to eight emoji.
func chatEmojiRow(m Model, st emojiPickerState, v emojiView, base string, index, active int) ui.Node {
	row := v.rows[index]
	aria := map[string]string{"rowindex": strconv.Itoa(index + 1)}
	if row.Head {
		return html.Div(html.Props{Class: "emoji-pop-headrow", Role: "row", Aria: aria},
			html.Span(html.Props{Class: "emoji-pop-head", Role: "rowheader", Aria: map[string]string{"colspan": strconv.Itoa(emojiCols)}, Text: chatEmojiGroupName(m, v, row.Group)}))
	}
	action := "emoji-insert"
	if st.Reaction {
		action = "react-with"
	}
	cells := make([]ui.Node, 0, row.Count)
	for pos := row.First; pos < row.First+row.Count; pos++ {
		cell, ok := v.cell(pos)
		if !ok {
			continue
		}
		name := cell.Name
		if name == "" {
			name = chatEmojiFormat(m, emojiKeyItem, map[string]string{"emoji": cell.Glyph})
		}
		class := "emoji-pop-cell"
		cellAria := map[string]string{}
		if pos == active {
			class += " active"
			cellAria["selected"] = "true"
		}
		cells = append(cells, html.Div(html.Props{ID: base + "-c" + strconv.Itoa(pos), Class: class, Role: "gridcell", Aria: cellAria},
			html.Button(html.Props{Class: "emoji-pop-btn", Type: "button", TabIndex: -1, Title: name,
				Data: map[string]string{"action": action, "id": st.Target, "emoji": cell.Glyph, "pos": strconv.Itoa(pos)},
				Aria: map[string]string{"label": name}, Text: cell.Glyph})))
	}
	return html.Div(html.Props{Class: "emoji-pop-row", Role: "row", Aria: aria}, cells...)
}

func chatEmojiGroupName(m Model, v emojiView, group int) string {
	if group == emojiGroupFrequent {
		return chatEmojiText(m, emojiKeyFrequent)
	}
	if v.set == nil || group < 0 || group >= len(v.set.Groups) {
		return ""
	}
	if text := chatEmojiText(m, emojiKeyGroupPrefix+v.set.Groups[group].ID); text != "" {
		return text
	}
	return v.set.Groups[group].Name
}

// chatEmojiCurrentGroup is the category at the top of the grid, which the marked
// tab and the sticky heading follow.
func chatEmojiCurrentGroup(st emojiPickerState, v emojiView) (int, bool) {
	if v.seq.query || len(v.rows) == 0 {
		return 0, false
	}
	return v.rows[emojiRowAtScroll(v.rows, st.Scroll)].Group, true
}

func chatEmojiStickyLabel(m Model, st emojiPickerState, v emojiView) string {
	if group, ok := chatEmojiCurrentGroup(st, v); ok {
		return chatEmojiGroupName(m, v, group)
	}
	return ""
}

// chatEmojiTabs is one row of category tabs, the current one marked. While a
// query is typed the grid is a flat result list, so the tabs rest.
func chatEmojiTabs(m Model, st emojiPickerState, v emojiView, base string) ui.Node {
	current, hasCurrent := chatEmojiCurrentGroup(st, v)
	type tab struct {
		group int
		icon  string
	}
	tabs := []tab{{emojiGroupFrequent, emojiFrequentIcon}}
	if v.set != nil {
		for gi, group := range v.set.Groups {
			if v.seq.skipLast && gi == len(v.set.Groups)-1 {
				break
			}
			icon := emojiGroupIcons[group.ID]
			if icon == "" {
				icon = group.Name
			}
			tabs = append(tabs, tab{gi, icon})
		}
	}
	nodes := make([]ui.Node, 0, len(tabs))
	for _, t := range tabs {
		name := chatEmojiGroupName(m, v, t.group)
		aria := map[string]string{"label": name}
		if hasCurrent && t.group == current {
			aria["current"] = "true"
		}
		class := "emoji-pop-tab"
		if hasCurrent && t.group == current {
			class += " current"
		}
		nodes = append(nodes, html.Button(html.Props{Class: class, Type: "button", Disabled: v.seq.query, Title: name,
			Data: map[string]string{"action": "emoji-tab", "id": st.Target, "extra": strconv.Itoa(t.group)}, Aria: aria, Text: t.icon}))
	}
	return html.Div(html.Props{Class: "emoji-pop-tabs", Role: "toolbar", Aria: map[string]string{"label": chatEmojiText(m, emojiKeyTabs)}}, nodes...)
}

// chatEmojiNote is the one plain line under the grid when there is something to
// say: the data is still loading, could not be loaded, or nothing matched.
func chatEmojiNote(m Model, st emojiPickerState, v emojiView, status emojiDataStatus) ui.Node {
	var text string
	switch {
	case v.ix == nil && status == emojiDataFailed:
		text = chatEmojiText(m, emojiKeyLoadFailed)
	case v.ix == nil:
		text = chatEmojiText(m, emojiKeyLoading)
	case v.seq.query && len(v.seq.results) == 0:
		text = chatEmojiFormat(m, emojiKeyNoMatch, map[string]string{"query": trimEmojiQuery(st.Query)})
	}
	if text == "" {
		return nil
	}
	return html.P(html.Props{Class: "emoji-pop-note", Role: "status", Text: text})
}

func trimEmojiQuery(q string) string {
	runes := []rune(q)
	for len(runes) > 0 && (runes[0] == ' ' || runes[0] == '\t') {
		runes = runes[1:]
	}
	for len(runes) > 0 && (runes[len(runes)-1] == ' ' || runes[len(runes)-1] == '\t') {
		runes = runes[:len(runes)-1]
	}
	if len(runes) > 48 {
		runes = append(runes[:48], '…')
	}
	return string(runes)
}

// chatEmojiToneName is the name of skin tone 0 (default) to 5.
func chatEmojiToneName(m Model, v emojiView, tone int) string {
	if tone == 0 {
		return chatEmojiText(m, emojiKeyToneDefault)
	}
	if v.ix != nil {
		if name := v.ix.ToneName(tone); name != "" {
			return name
		}
	}
	return chatEmojiText(m, emojiKeyTonePrefix+strconv.Itoa(tone))
}

func chatEmojiToneGlyph(tone int) string {
	if tone <= 0 {
		return emojiToneHand
	}
	return emojiToneHand + string(emojiset.ToneModifier(tone))
}

// chatEmojiFooter names the emoji under the pointer or the highlight, large, and
// holds the one skin-tone control.
func chatEmojiFooter(m Model, st emojiPickerState, v emojiView, base string, active int) ui.Node {
	tone := v.prefs.Tone
	toneButton := html.Button(html.Props{Class: "emoji-pop-tone", Type: "button", Title: chatEmojiText(m, emojiKeyToneChoose),
		Data: map[string]string{"action": "emoji-tone-toggle", "id": st.Target},
		Aria: map[string]string{"label": chatEmojiFormat(m, emojiKeyToneNow, map[string]string{"tone": chatEmojiToneName(m, v, tone)}), "haspopup": "true", "expanded": boolString(st.ToneOpen)},
		Text: chatEmojiToneGlyph(tone)})
	if st.ToneOpen {
		choices := make([]ui.Node, 0, emojiset.Tones+1)
		for t := 0; t <= emojiset.Tones; t++ {
			class := "emoji-pop-swatch"
			if t == tone {
				class += " current"
			}
			name := chatEmojiToneName(m, v, t)
			choices = append(choices, html.Button(html.Props{Class: class, Type: "button", Title: name,
				Data: map[string]string{"action": "emoji-tone", "id": st.Target, "extra": strconv.Itoa(t)},
				Aria: map[string]string{"label": name, "pressed": boolString(t == tone)}, Text: chatEmojiToneGlyph(t)}))
		}
		return html.Div(html.Props{Class: "emoji-pop-foot", Role: "group", Aria: map[string]string{"label": chatEmojiText(m, emojiKeyTone)}},
			html.Div(html.Props{Class: "emoji-pop-swatches"}, choices...), toneButton)
	}
	pos := active
	if st.Hover > 0 && st.Hover-1 < v.seq.len() {
		pos = st.Hover - 1
	}
	var preview ui.Node
	if cell, ok := v.cell(pos); ok {
		name := cell.Name
		if name == "" {
			name = chatEmojiFormat(m, emojiKeyItem, map[string]string{"emoji": cell.Glyph})
		}
		preview = html.Div(html.Props{Class: "emoji-pop-preview"},
			html.Span(html.Props{Class: "emoji-pop-big", Aria: map[string]string{"hidden": "true"}, Text: cell.Glyph}),
			html.Span(html.Props{Class: "emoji-pop-names"},
				html.Span(html.Props{Class: "emoji-pop-name", Dir: "auto", Text: name}),
				html.Span(html.Props{Class: "emoji-pop-code", Dir: "ltr", Text: cell.Code})))
	} else {
		hint := chatEmojiText(m, emojiKeyHintInsert)
		if st.Reaction {
			hint = chatEmojiText(m, emojiKeyHintReact)
		}
		preview = html.Div(html.Props{Class: "emoji-pop-preview hint"}, html.Span(html.Props{Class: "emoji-pop-name", Text: hint}))
	}
	return html.Div(html.Props{Class: "emoji-pop-foot"}, preview, toneButton)
}
