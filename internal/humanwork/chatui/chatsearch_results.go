package chatui

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

type ChatSearchView struct {
	Conversation *Model
	Query        string
	Response     chatsearch.Response
	Recent       []string
	Loading      bool
	Error        string
	// Locate names where a result sits: its conversation, author and time. It
	// is set by ChatSearchMount from the conversation list and directory the
	// page already holds; nil leaves each result labelled by its kind alone.
	Locate func(chatsearch.Row) string
}

// ChatSearchMount draws the search outcome from the model, so the progress
// line, the results and the one failure line all come from the same state the
// rest of the page renders from and nothing is written into the page by hand.
func ChatSearchMount(m Model) ui.Node {
	if m.ChatSearch == nil {
		return html.Div(html.Props{ID: "chatsearch-results", Data: map[string]string{"locale": m.Locale}})
	}
	view := *m.ChatSearch
	view.Locate = func(row chatsearch.Row) string { return chatSearchWhere(m, row) }
	return html.Div(html.Props{ID: "chatsearch-results", Data: map[string]string{"locale": m.Locale, "active": "true"}}, RenderChatSearch(m.Locale, view))
}

// searching reports that the results, not a conversation, are on screen: there
// are words in the search box and no result has been opened.
func (m Model) searching() bool { return strings.TrimSpace(m.Search) != "" && !m.SearchOpened }

// searchReturnBar is the in-flow bar above an opened result's conversation that
// leads back to the results. It exists only while the query is in the box.
func searchReturnBar(m Model) ui.Node {
	return html.Div(html.Props{Class: "chatsearch-return", Data: map[string]string{"chatsearch-return": "true"}}, RenderChatSearchReturn(m.Locale))
}

// chatSearchCount is the number of results on the page, and false while the
// search is still running or failed.
func chatSearchCount(v *ChatSearchView) (int, bool) {
	if v == nil || v.Loading || (v.Error != "" && v.Error != "invalid" && v.Error != "meaning") {
		return 0, false
	}
	total := 0
	for _, g := range v.Response.Groups {
		total += len(g.Rows)
	}
	return total, true
}

// chatSearchWhere says where a result sits: "#general · Walt Brennan · Today
// 9:41 AM". A part nobody can name yet (an author the directory has not
// returned) is left out; an identifier is never shown in its place.
func chatSearchWhere(m Model, row chatsearch.Row) string {
	kind := chatsearchKind(m.Locale, row.Kind)
	if row.Kind == chatsearch.Person || row.Kind == chatsearch.Conversation {
		return kind
	}
	parts := []string{}
	for _, c := range m.Conversations {
		if c.ID != row.Target.ConversationID {
			continue
		}
		if name := displayName(m, c); name != "" {
			if c.Kind == PublicChannel || c.Kind == PrivateChannel {
				name = "#" + name
			}
			parts = append(parts, name)
		}
		break
	}
	if row.AuthorID != "" {
		for _, p := range mentionIndex(m) {
			if p.id == row.AuthorID {
				parts = append(parts, p.name)
				break
			}
		}
	}
	if !row.At.IsZero() {
		parts = append(parts, dayLabel(m, row.At)+" "+chat5Clock(m.Locale, row.At))
	}
	if len(parts) == 0 {
		return kind
	}
	return strings.Join(parts, " · ")
}
func RenderChatSearchReturn(locale string) ui.Node {
	return html.Div(html.Props{Class: "chatsearch-view"}, html.Button(html.Props{Type: "button", Text: chatsearchText(locale, "back"), Data: map[string]string{"chatsearch-action": "back"}}))
}
func RenderChatSearch(locale string, v ChatSearchView) ui.Node {
	t := func(key string) string { return chatsearchText(locale, key) }
	button := func(action, label, extra string) ui.Node {
		return html.Button(html.Props{Type: "button", Text: label, Data: map[string]string{"chatsearch-action": action, "extra": extra}, Disabled: v.Loading})
	}
	if v.Error != "" && v.Error != "invalid" && v.Error != "meaning" {
		if len(v.Response.Groups) == 0 {
			notice := html.P(html.Props{Class: "search-status", Role: "status", Text: t("unavailable")})
			if v.Conversation == nil {
				return notice
			}
			rows := []ui.Node{}
			for _, msg := range v.Conversation.Messages {
				rows = append(rows, message(*v.Conversation, handlers{}, msg, false))
			}
			return html.Div(html.Props{Class: "chat-search-fallback"}, notice, html.Div(html.Props{Class: "chat-search-fallback-conversation", Role: "log", Aria: map[string]string{"label": v.Conversation.t(KeyConversation)}}, rows...))
		}
		previous := v
		previous.Error = ""
		return html.Div(html.Props{}, html.P(html.Props{Class: "search-status", Role: "status", Text: t("unavailable")}), RenderChatSearch(locale, previous))
	}
	parsed, _ := chatsearch.Parse(v.Query)
	children := []ui.Node{}
	errorAssociation := map[string]string{}
	if v.Error != "" {
		errorAssociation["describedby"] = "chatsearch-error"
	}
	controls := []ui.Node{}
	for _, field := range []string{"channel", "person", "before", "after", "on"} {
		typ := "text"
		if field == "before" || field == "after" || field == "on" {
			typ = "date"
		}
		controls = append(controls, html.Label(html.Props{Text: t(field), For: "chatsearch-" + field}, html.Input(html.Props{ID: "chatsearch-" + field, Name: field, Type: typ, Aria: errorAssociation})))
	}
	options := []ui.Node{html.Option(html.Props{Value: "", Text: t("all")})}
	for _, d := range chatsearch.Declarations() {
		options = append(options, html.Option(html.Props{Value: string(d.Kind), Text: chatsearchKind(locale, d.Kind)}))
	}
	controls = append(controls, html.Label(html.Props{Text: t("kind"), For: "chatsearch-kind"}, html.Select(html.Props{ID: "chatsearch-kind", Name: "kind"}, options...)))
	for _, field := range []string{"file", "link", "reactions", "threads", "mentions", "agent", "mine", "voice"} {
		controls = append(controls, html.Label(html.Props{Class: "chatsearch-check", For: "chatsearch-" + field}, html.Input(html.Props{ID: "chatsearch-" + field, Name: field, Type: "checkbox"}), ui.Text(t(field))))
	}
	children = append(children, chatPolishDisclosure(html.Props{}, chatPolishDisclosureLabel(html.Props{Text: t("filters")}), html.Form(html.Props{Data: map[string]string{"chatsearch-form": "filters"}}, html.Div(html.Props{Class: "chatsearch-controls"}, controls...), html.Button(html.Props{Type: "submit", Class: "chatsearch-primary", Text: t("apply"), Disabled: v.Loading}))))
	chips := []ui.Node{}
	for _, chip := range parsed.Chips {
		key, value, _ := strings.Cut(chip, ":")
		key = strings.ToLower(key)
		label := t("remove") + ": "
		switch key {
		case "in":
			label += t("channel") + " " + value
		case "from":
			if value == "agent" {
				label += t("agent")
			} else {
				label += t("person") + " " + value
			}
		case "kind":
			label += chatsearchKind(locale, chatsearch.Kind(value))
		case "has":
			label += t(value)
		case "is":
			if value == "thread" {
				value = "threads"
			}
			label += t(value)
		case "mentions":
			label += t("mentions")
		default:
			label += t(key) + " " + value
		}
		chips = append(chips, button("remove", label, chip))
	}
	children = append(children, html.Div(html.Props{Class: "chatsearch-chips"}, chips...))
	if v.Loading {
		children = append(children, html.P(html.Props{Role: "status", Text: t("loading")}))
	} else if v.Error != "" {
		key := v.Error
		if key != "invalid" && key != "meaning" {
			key = "error"
		}
		children = append(children, html.P(html.Props{Role: "alert", ID: "chatsearch-error", Text: t(key)}), button("retry", t("retry"), ""))
	} else {
		if n, ok := chatSearchCount(&v); ok && n > 0 {
			text := strings.ReplaceAll(t("count"), "{n}", strconv.Itoa(n))
			if n == 1 {
				text = t("countOne")
			}
			children = append(children, html.P(html.Props{Class: "chatsearch-count", Role: "status", Text: text}))
		}
		if len(v.Response.Groups) == 0 {
			wider := parsed.Text
			if wider == "" {
				wider = "*"
			}
			children = append(children, html.P(html.Props{Role: "status", Text: strings.ReplaceAll(t("empty"), "{query}", v.Query)}), button("widen", t("widen"), wider))
		}
		for _, g := range v.Response.Groups {
			rows := []ui.Node{html.H3(html.Props{Text: chatux010GroupName(locale, g.Kind) + " · " + strconv.Itoa(g.Count) + " " + t("shown")})}
			for _, row := range g.Rows {
				target, _ := json.Marshal(row.Target)
				label := chatsearchKind(locale, row.Kind)
				if v.Locate != nil {
					label = v.Locate(row)
				}
				if row.Private {
					label += " · " + t("only")
				}
				rows = append(rows, html.Button(html.Props{Type: "button", Class: "chatsearch-result", Data: map[string]string{"chatsearch-action": "open", "target": base64.RawURLEncoding.EncodeToString(target), "kind": string(row.Kind)}}, html.Strong(html.Props{Text: label}), html.Span(html.Props{}, highlightText(chatDisplayText(row.Text), parsed.Text)...)))
			}
			children = append(children, html.Section(html.Props{}, rows...))
		}
		if v.Response.NextCursor != "" {
			children = append(children, button("more", t("more"), v.Response.NextCursor))
		}
		if len(v.Response.Unavailable) > 0 {
			names := []string{}
			for _, kind := range v.Response.Unavailable {
				names = append(names, chatsearchKind(locale, kind))
			}
			children = append(children, html.P(html.Props{Role: "status", Text: strings.ReplaceAll(t("missing"), "{kinds}", strings.Join(names, ", "))}))
		}
	}
	if len(v.Recent) > 0 {
		recent := []ui.Node{html.H3(html.Props{Text: t("recent")})}
		for _, query := range v.Recent {
			recent = append(recent, button("recent", query, query))
		}
		recent = append(recent, button("clear-recent", t("clear"), ""))
		children = append(children, html.Div(html.Props{Class: "chatsearch-chips"}, recent...))
	}
	dir := "ltr"
	if strings.HasPrefix(locale, "ar") {
		dir = "rtl"
	}
	return html.Div(html.Props{Class: "chatsearch-view", Dir: dir, Role: "region", Aria: map[string]string{"label": t("search"), "busy": strconv.FormatBool(v.Loading)}}, children...)
}

// ResolveChatSearchQuery resolves only visible channel and person names. An
// unresolved name remains a nonmatching filter; it never widens to all rooms.
func ResolveChatSearchQuery(m Model, query string) (string, error) {
	tokens, err := chatsearch.Tokens(query)
	if err != nil {
		return "", err
	}
	for i, token := range tokens {
		key, value, ok := strings.Cut(token, ":")
		key = strings.ToLower(key)
		if !ok {
			continue
		}
		if key == "in" {
			value = strings.TrimPrefix(value, "#")
			for _, c := range m.Conversations {
				if strings.EqualFold(displayName(m, c), value) || strings.EqualFold(c.Name, value) {
					value = c.ID
					break
				}
			}
		}
		if key == "from" && value != "agent" {
			value = strings.TrimPrefix(value, "@")
			for _, p := range mentionIndex(m) {
				if strings.EqualFold(p.name, value) {
					value = p.id
					break
				}
			}
		}
		if key == "in" || key == "from" {
			token = key + ":" + value
		}
		if strings.Contains(token, " ") {
			key, value, ok = strings.Cut(token, ":")
			if ok {
				token = key + `:"` + value + `"`
			} else {
				token = `"` + token + `"`
			}
		}
		tokens[i] = token
	}
	return strings.Join(tokens, " "), nil
}
