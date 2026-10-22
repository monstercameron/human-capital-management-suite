package chatui

import (
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
	// Context is the page's own model. ChatSearchMount sets it so a result can
	// be drawn like a message (people, conversations, documents and agents the
	// page already knows); nil draws a result from its own fields alone.
	Context *Model
}

// ChatSearchMount draws the search outcome from the model, so the progress
// line, the results and the one failure line all come from the same state the
// rest of the page renders from and nothing is written into the page by hand.
func ChatSearchMount(m Model) ui.Node {
	if m.ChatSearch == nil {
		return html.Div(html.Props{ID: "chatsearch-results", Data: map[string]string{"locale": m.Locale}})
	}
	view := *m.ChatSearch
	view.Context = &m
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
	groups, _ := chatsearchDedupe(v.Response.Groups)
	for _, g := range groups {
		total += len(g.Rows)
	}
	return total, true
}

func RenderChatSearchReturn(locale string) ui.Node {
	return html.Div(html.Props{Class: "chatsearch-view"}, html.Button(html.Props{Type: "button", Text: chatsearchText(locale, "back"), Data: map[string]string{"chatsearch-action": "back"}}))
}

// chatsearchKindChoices are the kinds the "kind" filter offers, in the
// registry's order: those the service says it can search. A kind that is
// declared but has nothing behind it in this deployment (reminders, sources)
// was offered too, and choosing it returned nothing with no word of why. An
// answer that names no kinds (an older service, or none yet) offers them all.
func chatsearchKindChoices(r chatsearch.Response) []chatsearch.Kind {
	served := map[chatsearch.Kind]bool{}
	for _, kind := range r.Kinds {
		served[kind] = true
	}
	var out []chatsearch.Kind
	for _, d := range chatsearch.Declarations() {
		if len(served) == 0 || served[d.Kind] {
			out = append(out, d.Kind)
		}
	}
	// CHATSEARCH-003: the agent kinds are offered only where the service
	// answers them; a deployment without agents never names them.
	for _, d := range chatsearch.AgentDeclarations() {
		if served[d.Kind] {
			out = append(out, d.Kind)
		}
	}
	return out
}

func RenderChatSearch(locale string, v ChatSearchView) ui.Node {
	t := func(key string) string { return chatsearchText(locale, key) }
	button := func(action, label, extra string) ui.Node {
		return html.Button(html.Props{Type: "button", Text: label, Data: map[string]string{"chatsearch-action": action, "extra": extra}, Disabled: v.Loading})
	}
	if v.Error != "" && v.Error != "invalid" && v.Error != "meaning" {
		if len(v.Response.Groups) == 0 {
			// CHATBUG-022: the failure says so once and offers the retry, so a
			// person is never left looking at a box that did nothing.
			notice := html.Div(html.Props{Class: "search-status", Role: "status"}, html.P(html.Props{Text: t("unavailable")}), button("retry", t("retry"), ""))
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
		return html.Div(html.Props{}, html.Div(html.Props{Class: "search-status", Role: "status"}, html.P(html.Props{Text: t("unavailable")}), button("retry", t("retry"), "")), RenderChatSearch(locale, previous))
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
	for _, kind := range chatsearchKindChoices(v.Response) {
		options = append(options, html.Option(html.Props{Value: string(kind), Text: chatsearchKind(locale, kind)}))
	}
	controls = append(controls, html.Label(html.Props{Text: t("kind"), For: "chatsearch-kind"}, html.Select(html.Props{ID: "chatsearch-kind", Name: "kind"}, options...)))
	controls = append(controls, chatsearchWrittenControl(locale))
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
		case "lang", "language":
			label += chattoneW10Text(locale, "search_written") + " " + chatlangLanguageName(locale, value)
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
		children = append(children, ChatLoadingFrame(LoadingFrame{Locale: locale, Shape: LoadingShapeList, Status: t("loading"), RetryData: map[string]string{"chatsearch-action": "retry", "extra": ""}}))
	} else if v.Error != "" {
		key := v.Error
		if key != "invalid" && key != "meaning" {
			key = "error"
		}
		children = append(children, html.P(html.Props{Role: "alert", ID: "chatsearch-error", Text: t(key)}), button("retry", t("retry"), ""))
	} else {
		// The count is printed once, in the page header; the results start here.
		groups, saved := chatsearchDedupe(v.Response.Groups)
		if len(groups) == 0 {
			wider := parsed.Text
			if wider == "" {
				wider = "*"
			}
			children = append(children, html.P(html.Props{Role: "status", Text: strings.ReplaceAll(t("empty"), "{query}", v.Query)}), button("widen", t("widen"), wider))
		}
		context := Model{}
		if v.Context != nil {
			context = *v.Context
		}
		context.Locale = locale
		people := chatsearchPeopleOf(context)
		for _, g := range groups {
			rows := []ui.Node{html.H3(html.Props{Text: chatux010GroupName(locale, g.Kind)})}
			for _, row := range g.Rows {
				rows = append(rows, chatsearchResult(context, people, row, parsed.Text, saved[chatsearchMessageKey(row)]))
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
	// Recent searches are offered for an empty box; once there are words in it the
	// page is about those words.
	if len(v.Recent) > 0 && strings.TrimSpace(v.Query) == "" {
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
	out := make([]string, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		key, value, ok := strings.Cut(token, ":")
		key = strings.ToLower(key)
		if !ok {
			out = append(out, token)
			continue
		}
		if key == "in" {
			value = strings.TrimPrefix(value, "#")
			resolved, used := resolveSearchFilterName(tokens[i+1:], value, func(name string) string {
				for _, c := range m.Conversations {
					if strings.EqualFold(displayName(m, c), name) || strings.EqualFold(c.Name, name) {
						return c.ID
					}
				}
				return ""
			})
			value, i = resolved, i+used
		}
		if key == "from" && value != "agent" {
			value = strings.TrimPrefix(value, "@")
			people := mentionIndex(m)
			resolved, used := resolveSearchFilterName(tokens[i+1:], value, func(name string) string {
				for _, p := range people {
					if strings.EqualFold(p.name, name) {
						return p.id
					}
				}
				return ""
			})
			value, i = resolved, i+used
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
		out = append(out, token)
	}
	return strings.Join(out, " "), nil
}

// searchFilterNameWords is how many words after the first a channel or person
// name typed without quotes may run on.
const searchFilterNameWords = 4

// resolveSearchFilterName finds the channel or person a filter names. A name with
// spaces typed without quotes (from:@Camila Morales) reached here as the first
// word and the words after it; the longest run of them that is exactly one known
// name wins, and a word that is not part of a name is left for the search. It
// returns the id and how many of the following words it used, or value unchanged
// and none when nothing is known by that name.
func resolveSearchFilterName(following []string, value string, find func(string) string) (string, int) {
	most := searchFilterNameWords
	for used := 0; used < len(following) && used < searchFilterNameWords; used++ {
		if strings.Contains(following[used], ":") {
			// The next filter starts here.
			most = used
			break
		}
	}
	if most > len(following) {
		most = len(following)
	}
	for used := most; used >= 0; used-- {
		name := strings.Join(append([]string{value}, following[:used]...), " ")
		if id := find(name); id != "" {
			return id, used
		}
	}
	return value, 0
}
