package chatui

import (
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// createKinds are the conversation types the create dialog offers, as cards:
// what each one is, not just its name, is what people choose between.
var createKinds = []struct {
	kind            ConversationKind
	glyph, key, sub string
}{
	{PublicChannel, "#", KeyKindPublic, KeyKindPublicDesc},
	{PrivateChannel, "lock", KeyKindPrivate, KeyKindPrivateDesc},
	{GroupChat, "people", KeyKindGroup, KeyKindGroupDesc},
	{DirectMessage, "chat", KeyKindDirect, KeyKindDirectDesc},
}

func createKindOf(m Model, local localUI) ConversationKind {
	switch {
	case local.createKind != "":
		return local.createKind
	case m.NewKind != "":
		return m.NewKind
	}
	return PublicChannel
}

func createDialog(m Model, h handlers) ui.Node {
	local := h.local
	kind := createKindOf(m, local)
	cards := make([]ui.Node, 0, len(createKinds))
	for _, k := range createKinds {
		glyph := ui.Node(html.Span(html.Props{Class: "kind-card-glyph hash", Aria: map[string]string{"hidden": "true"}, Text: "#"}))
		if k.glyph != "#" {
			glyph = html.Span(html.Props{Class: "kind-card-glyph", Aria: map[string]string{"hidden": "true"}}, icon(k.glyph))
		}
		cards = append(cards, html.Button(html.Props{Class: "kind-card", Type: "button", Role: "radio", Data: map[string]string{"action": "create-kind", "id": string(k.kind)},
			Aria: map[string]string{"checked": boolString(kind == k.kind)}},
			glyph, html.Span(html.Props{Class: "kind-card-text"}, html.Strong(html.Props{Text: m.t(k.key)}), html.Span(html.Props{Text: m.t(k.sub)}))))
	}
	channel := kind == PublicChannel || kind == PrivateChannel
	fields := []ui.Node{
		html.Div(html.Props{Class: "side-heading"}, html.H2(html.Props{Text: m.t(KeyCreateTitle)}), actionButton("icon-button", "close-create", "", m.t(KeyClose), m.Callbacks.CloseCreate == nil, icon("close"))),
		html.Div(html.Props{Class: "kind-cards", Role: "radiogroup", Aria: map[string]string{"label": m.t(KeyKind)}}, cards...),
	}
	if kind != DirectMessage {
		prefix := ui.Node(nil)
		if channel {
			prefix = html.Span(html.Props{Class: "name-prefix", Aria: map[string]string{"hidden": "true"}, Text: "#"})
		}
		hint := ui.Node(nil)
		describedBy := map[string]string{}
		if channel {
			hint = html.P(html.Props{ID: "new-chat-name-hint", Class: "field-hint", Text: m.t(KeyChannelNameHint)})
			describedBy["describedby"] = "new-chat-name-hint"
		}
		fields = append(fields, html.Div(html.Props{Class: "prefs-field create-name"},
			html.Label(html.Props{For: "new-chat-name", Text: m.t(KeyName)}),
			html.Div(html.Props{Class: "name-input"}, prefix,
				html.Input(html.Props{ID: "new-chat-name", Class: "chat-input", Data: map[string]string{"chat-value": m.NewName}, Required: true, AutoComplete: "off", AutoFocus: true, Placeholder: m.t(KeyNamePlaceholder), Aria: describedBy})),
			hint))
	}
	fields = append(fields, memberPicker(m, h, kind),
		// The submitted shape stays one kind and one comma-separated member
		// list of subject IDs, whatever the controls above look like.
		html.Input(html.Props{ID: "new-chat-kind", Type: "hidden", Name: "kind", Value: string(kind)}),
		html.Input(html.Props{ID: "new-chat-members", Type: "hidden", Name: "members", Value: pickedIDs(local.picked)}))
	submit := m.t(KeyCreateChannel)
	if !channel {
		submit = m.t(KeyStartConversation)
	}
	disabled := m.Callbacks.CreateConversation == nil || (kind == DirectMessage && len(local.picked) != 1) || (kind == GroupChat && len(local.picked) == 0)
	fields = append(fields, html.Div(html.Props{Class: "dialog-actions"},
		html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: m.Callbacks.CloseCreate == nil, Data: map[string]string{"action": "close-create"}, Text: m.t(KeyCancel)}),
		html.Button(html.Props{Class: "button", Type: "submit", Disabled: disabled, Text: submit})))
	return html.Div(html.Props{Class: "chat-dialog-backdrop", Role: "presentation"},
		html.Dialog(html.Props{Class: "chat-dialog create-dialog", Open: true, Role: "dialog", Aria: map[string]string{"label": m.t(KeyCreateTitle), "modal": "true"}},
			html.Form(html.Props{Class: "dialog-form", OnSubmit: h.createSubmit}, fields...)))
}

// memberPicker adds people as removable chips from a name search. The chips
// carry subject IDs; nobody types an ID or a comma-separated list.
func memberPicker(m Model, h handlers, kind ConversationKind) ui.Node {
	local := h.local
	chips := make([]ui.Node, 0, len(local.picked))
	for _, p := range local.picked {
		// Keyed so adding a chip never shifts the search box into a new slot:
		// an unkeyed shift rebuilds the input and drops the typing focus.
		chips = append(chips, html.WithKey(html.Span(html.Props{Class: "person-chip"},
			personAvatar(m, p.ID, p.Name, "avatar tiny"), html.Span(html.Props{Text: p.Name}),
			actionButton("chip-remove", "create-unpick", p.ID, m.tf(KeyRemovePerson, map[string]string{"name": p.Name}), false, icon("close"))), "chip:"+p.ID))
	}
	full := kind == DirectMessage && len(local.picked) >= 1
	if !full {
		chips = append(chips, html.WithKey(html.Input(html.Props{ID: "new-chat-member-search", Class: "chip-input", Type: "text", AutoComplete: "off", Placeholder: m.t(KeyAddPeoplePlaceholder),
			Data: map[string]string{"chat-value": local.pickQuery}, OnInput: h.pickInput, OnKeyDown: h.pickKey,
			Aria: map[string]string{"controls": "new-chat-member-options", "autocomplete": "list"}}), "chip-input"))
	}
	suggestions := ui.Node(html.Div(html.Props{Class: "pick-slot"}))
	if !full && strings.TrimSpace(local.pickQuery) != "" {
		people := pickCandidates(m, local.pickQuery, local.picked)
		rows := make([]ui.Node, 0, len(people))
		for i, p := range people {
			class := "mention-option"
			if i == local.pickActive {
				class += " active"
			}
			rows = append(rows, html.WithKey(html.Button(html.Props{Class: class, Type: "button", Role: "option", TabIndex: -1, Data: map[string]string{"action": "create-pick", "id": p.ID}, Aria: map[string]string{"selected": boolString(i == local.pickActive)}},
				personAvatar(m, p.ID, p.Name, "avatar small"), html.Span(html.Props{Class: "mention-name", Text: p.Name})), "pick:"+p.ID))
		}
		if len(rows) == 0 {
			rows = append(rows, html.P(html.Props{Class: "mention-empty", Text: m.tf(KeyMentionNone, map[string]string{"query": local.pickQuery})}))
		}
		suggestions = html.Div(html.Props{ID: "new-chat-member-options", Class: "pick-options", Role: "listbox", Aria: map[string]string{"label": m.t(KeyAddPeople)}}, rows...)
	}
	label := m.t(KeyAddPeople)
	if kind == PublicChannel || kind == PrivateChannel {
		label = m.t(KeyMembersOptional)
	}
	return html.Div(html.Props{Class: "prefs-field member-picker"},
		html.Label(html.Props{For: "new-chat-member-search", Text: label}),
		html.Div(html.Props{Class: "chip-field"}, chips...),
		suggestions)
}

// browseEntries is every channel the viewer can see: the discoverable ones
// they have not joined and the ones already in their rail, so browsing never
// claims there is nothing when the viewer is simply in everything.
func browseEntries(m Model) []Conversation {
	query := strings.ToLower(strings.TrimSpace(m.BrowseQuery))
	seen := map[string]bool{}
	var out []Conversation
	for _, c := range m.Browse {
		if !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, c)
		}
	}
	for _, c := range m.Conversations {
		if seen[c.ID] || (c.Kind != PublicChannel && c.Kind != PrivateChannel) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(c.Name+" "+c.Topic), query) {
			continue
		}
		seen[c.ID] = true
		c.Joined = true
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func browseDialog(m Model, h handlers) ui.Node {
	entries := browseEntries(m)
	rows := make([]ui.Node, 0, len(entries))
	for _, c := range entries {
		var action ui.Node
		if c.Joined {
			action = html.Button(html.Props{Class: "button secondary small", Type: "button", Disabled: m.Callbacks.SelectConversation == nil, Data: map[string]string{"action": "browse-open", "id": c.ID}, Text: m.t(KeyBrowseOpen)})
		} else {
			action = html.Button(html.Props{Class: "button small", Type: "button", Disabled: m.Callbacks.JoinConversation == nil, Data: map[string]string{"action": "join", "id": c.ID}, Text: m.t(KeyJoin)})
		}
		meta := []string{m.t(kindKey(c.Kind))}
		if c.Joined {
			meta = append([]string{"✓ " + m.t(KeyJoined)}, meta...)
		}
		if c.MemberCount > 0 {
			meta = append(meta, memberCountLabel(m, c.MemberCount))
		}
		if when := activityLabel(m, c.LastActivity); when != "" {
			meta = append(meta, when)
		}
		text := []ui.Node{html.Strong(html.Props{Text: c.Name}), html.Span(html.Props{Class: "browse-meta", Text: strings.Join(meta, " · ")})}
		if topic := strings.TrimSpace(c.Topic); topic != "" {
			text = append(text, html.Span(html.Props{Class: "browse-topic", Text: topic}))
		}
		rows = append(rows, html.WithKey(html.Li(html.Props{Class: "browse-row", Data: map[string]string{"conversation-id": c.ID}},
			kindGlyph(m, c, m.t(kindKey(c.Kind))), html.Div(html.Props{Class: "browse-text"}, text...), action), "browse:"+c.ID))
	}
	var body ui.Node = html.Ul(html.Props{Class: "browse-list"}, rows...)
	if len(rows) == 0 {
		message := m.t(KeyBrowseEmpty)
		if q := strings.TrimSpace(m.BrowseQuery); q != "" {
			message = m.tf(KeyBrowseNoMatch, map[string]string{"query": q})
		}
		body = html.Div(html.Props{Class: "dialog-empty"},
			html.Div(html.Props{Class: "empty-icon", Aria: map[string]string{"hidden": "true"}}, icon("browse")),
			html.P(html.Props{Text: message}))
	}
	return html.Div(html.Props{Class: "chat-dialog-backdrop", Role: "presentation"},
		html.Dialog(html.Props{Class: "chat-dialog browse-dialog", Open: true, Role: "dialog", Aria: map[string]string{"label": m.t(KeyBrowseTitle), "modal": "true"}},
			html.Div(html.Props{Class: "side-heading"}, html.Div(html.Props{Class: "browse-heading"}, html.H2(html.Props{Text: m.t(KeyBrowseTitle)}), html.Span(html.Props{Class: "browse-count", Text: m.tf(KeyBrowseCount, map[string]string{"n": m.nz(len(entries))})})), actionButton("icon-button", "close-browse", "", m.t(KeyClose), m.Callbacks.CloseBrowse == nil, icon("close"))),
			html.Div(html.Props{Class: "rail-search browse-filter"},
				html.Label(html.Props{Class: "sr-only", For: "browse-filter"}, ui.Text(m.t(KeyBrowseFilter))),
				icon("search"),
				html.Input(html.Props{ID: "browse-filter", Class: "chat-search", Type: "search", Placeholder: m.t(KeyBrowseFilter), Data: map[string]string{"chat-value": m.BrowseQuery}, AutoComplete: "off", AutoFocus: true, OnInput: h.browseFilter})),
			body,
			html.Div(html.Props{Class: "dialog-actions browse-footer"},
				html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: m.Callbacks.OpenCreate == nil, Data: map[string]string{"action": "browse-to-create"}}, icon("plus"), ui.Text(m.t(KeyBrowseCreate)))),
		),
	)
}
