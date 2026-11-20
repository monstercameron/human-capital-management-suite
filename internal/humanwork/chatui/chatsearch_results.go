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
	// DraftBefore and DraftAfter are the dates the person typed into Refine
	// search when Error is "daterange" (ISSUE-3859): they are not applied, so the
	// fields keep them while the chips keep showing the last applied query.
	DraftBefore, DraftAfter string
	// Context is the page's own model. ChatSearchMount sets it so a result can
	// be drawn like a message (people, conversations, documents and agents the
	// page already knows); nil draws a result from its own fields alone.
	Context *Model
	// LoadingMore is true while "Show next results" is fetching a page: the
	// results already read stay on screen (#3899) and the control says it is busy.
	LoadingMore bool
	// Fresh lists the results (kind, NUL, id) the last page added, so the first
	// of them can be marked and announced (#3899).
	Fresh []string
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
	return html.Div(html.Props{ID: "chatsearch-results", Data: map[string]string{"locale": m.Locale, "active": "true"}}, issue3929SaveControl(m), RenderChatSearch(m.Locale, view))
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
	if v == nil || v.Loading || (v.Error != "" && !chatsearchInputError(v.Error)) {
		return 0, false
	}
	// ISSUE-3859: results are hidden while a filter is rejected, so the count of
	// the answer they came from is not stated either.
	if v.Error == "invalid" || v.Error == "daterange" {
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
	if v.Error != "" && !chatsearchInputError(v.Error) {
		if len(v.Response.Groups) == 0 {
			// CHATBUG-022: the failure says so once and offers the retry, so a
			// person is never left looking at a box that did nothing.
			notice := html.Div(html.Props{Class: "search-status", Role: "status"}, html.P(html.Props{ID: Issue3857SummaryID, TabIndex: -1, Text: t("unavailable")}), button("retry", t("retry"), ""))
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
	// ISSUE-3691: names typed without quotes are read back as the one person or
	// channel they name, so chips and fields never show a raw id or half a name.
	parsed, _ := chatsearch.Parse(chatsearchDisplayQuery(v))
	children := []ui.Node{}
	errorAssociation := map[string]string{}
	if v.Error != "" {
		errorAssociation["describedby"] = "chatsearch-error"
	}
	// ISSUE-3690: every control mirrors the filter the query already holds. An
	// unmirrored control kept the DOM value of an earlier search, so a stale box
	// was re-applied and the form hid the active filters.
	active := parsed.Filters
	controlValues := map[string]string{"channel": issue3937InFieldValue(v, active.Conversation), "person": chatsearchPersonName(v, active.Person), "before": active.Before, "after": active.After, "on": active.On}
	controlChecked := map[string]bool{"file": active.File, "link": active.Link, "reactions": active.Reactions, "threads": active.Threads, "mentions": active.MentionsMe, "agent": active.Agent, "mine": active.Mine, "voice": active.Voice}
	controls := []ui.Node{}
	for _, field := range []string{"channel", "person", "before", "after", "on"} {
		typ := "text"
		if field == "before" || field == "after" || field == "on" {
			typ = "date"
		}
		if v.Error == "daterange" && (field == "before" || field == "after") {
			// ISSUE-3859: the fields hold what was typed, say which one to
			// change, and are marked invalid for assistive technology.
			draft, key := v.DraftBefore, "daterangeBefore"
			if field == "after" {
				draft, key = v.DraftAfter, "daterangeAfter"
			}
			id := "chatsearch-" + field + "-error"
			controls = append(controls, html.Label(html.Props{For: "chatsearch-" + field}, html.Span(html.Props{Text: t(field)}), html.Input(html.Props{ID: "chatsearch-" + field, Name: field, Type: typ, Value: draft, Aria: map[string]string{"invalid": "true", "describedby": id + " chatsearch-error"}}), html.Span(html.Props{ID: id, Class: "chatsearch-field-error", Text: t(key)})))
			continue
		}
		controls = append(controls, html.Label(html.Props{For: "chatsearch-" + field}, html.Span(html.Props{Text: t(field)}), html.Input(html.Props{ID: "chatsearch-" + field, Name: field, Type: typ, Value: controlValues[field], Aria: errorAssociation})))
	}
	options := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Selected: active.Kind == "", Text: t("all")})}
	for _, kind := range chatsearchKindChoices(v.Response) {
		options = append(options, html.Option(html.Props{Value: string(kind), Selected: kind == active.Kind, Text: chatsearchKind(locale, kind)}))
	}
	controls = append(controls, html.Label(html.Props{For: "chatsearch-kind"}, html.Span(html.Props{Text: t("kind")}), html.Select(html.Props{ID: "chatsearch-kind", Name: "kind", Data: map[string]string{"chat-select-value": string(active.Kind)}}, options...)))
	controls = append(controls, chatsearchWrittenControl(locale, active.Language))
	for _, field := range []string{"file", "link", "reactions", "threads", "mentions", "agent", "mine", "voice"} {
		controls = append(controls, html.Label(html.Props{Class: "chatsearch-check", For: "chatsearch-" + field}, html.Input(html.Props{ID: "chatsearch-" + field, Name: field, Type: "checkbox", Checked: controlChecked[field]}), ui.Text(t(field))))
	}
	children = append(children, chatPolishDisclosure(html.Props{}, chatPolishDisclosureLabel(html.Props{Text: t("filters")}), html.Form(html.Props{Data: map[string]string{"chatsearch-form": "filters"}}, html.Div(html.Props{Class: "chatsearch-controls"}, controls...), html.Button(html.Props{Type: "submit", Class: "chatsearch-primary", Text: t("apply"), Disabled: v.Loading}))))
	chips := []ui.Node{}
	for _, chip := range parsed.Chips {
		key, value, _ := strings.Cut(chip, ":")
		key = strings.ToLower(key)
		label := t("remove") + ": "
		switch key {
		case "in":
			label += issue3937InChipLabel(v, locale, t("channel"), value)
		case "from":
			if value == "agent" {
				label += t("agent")
			} else {
				label += t("person") + " " + chatsearchPersonName(v, strings.TrimPrefix(value, "@"))
			}
		case "kind":
			label += chatsearchKind(locale, chatsearch.Kind(value))
		case "sort":
			label += t("order") + " " + issue3945ChipLabel(locale, strings.ToLower(value))
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
		chips = append(chips, issue3860Chip(t("remove")+": ", label, chip, v.Loading))
	}
	if scope := issue3860Scope(locale, parsed.Text); scope != nil {
		children = append(children, scope)
	}
	children = append(children, html.Div(html.Props{Class: "chatsearch-chips"}, chips...))
	if strings.TrimSpace(v.Query) != "" && !chatsearchInputError(v.Error) {
		children = append(children, issue3945OrderSwitch(locale, issue3945Oldest(active)))
	}
	if v.Loading {
		children = append(children, ChatLoadingFrame(LoadingFrame{Locale: locale, Shape: LoadingShapeList, Status: t("loading"), RetryData: map[string]string{"chatsearch-action": "retry", "extra": ""}}))
	} else if v.Error != "" {
		key := v.Error
		if !chatsearchInputError(key) {
			key = "error"
		}
		if key == "daterange" {
			// ISSUE-3859: the fix is editing the dates, so there is no generic
			// retry; the note tells the chips (applied) from the fields (edited).
			children = append(children, html.P(html.Props{Role: "alert", ID: "chatsearch-error", Text: t(key)}), html.P(html.Props{Class: "chatsearch-draft-note", Text: t("draftnote")}))
		} else {
			children = append(children, html.P(html.Props{Role: "alert", ID: "chatsearch-error", Text: t(key)}), button("retry", t("retry"), ""))
		}
	} else {
		// The count is printed once, in the page header; the results start here.
		groups, saved := chatsearchDedupe(v.Response.Groups)
		if len(groups) == 0 {
			wider := parsed.Text
			if wider == "" {
				wider = "*"
			}
			if issue3945Oldest(active) {
				wider += " sort:oldest"
			}
			// #3897: widening removes filters, so it is offered only while one
			// is applied; an unfiltered search says what to try instead.
			restricted := issue3945Restricted(parsed.Chips)
			emptyKey := "empty"
			if !restricted {
				emptyKey = "emptyplain"
			}
			children = append(children, html.P(html.Props{Role: "status", Text: strings.ReplaceAll(t(emptyKey), "{query}", v.Query)}))
			if chatsearchUnknownPerson(v, active.Person) {
				// ISSUE-3691: say why a first name found nobody.
				children = append(children, html.P(html.Props{Class: "chatsearch-person-hint", Text: t("personhint")}))
			}
			if restricted {
				children = append(children, button("widen", t("widen"), wider))
			}
		}
		context := Model{}
		if v.Context != nil {
			context = *v.Context
		}
		context.Locale = locale
		people := chatsearchPeopleOf(context)
		boundaryDrawn := false
		for _, g := range groups {
			rows := []ui.Node{html.H3(html.Props{Text: chatux010GroupName(locale, g.Kind)})}
			for _, row := range g.Rows {
				if !boundaryDrawn && issue3899Fresh(v, row) {
					boundaryDrawn = true
					rows = append(rows, issue3899Boundary(locale, v))
				}
				rows = append(rows, chatsearchResult(context, people, row, parsed.Text, saved[chatsearchMessageKey(row)]))
			}
			children = append(children, html.Section(html.Props{}, rows...))
		}
		if v.Response.NextCursor != "" {
			if v.LoadingMore {
				children = append(children, html.Button(html.Props{Type: "button", Class: "chatsearch-more", Text: t("loading"), Disabled: true, Aria: map[string]string{"busy": "true"}}))
			} else {
				children = append(children, button("more", t("more"), v.Response.NextCursor))
			}
		}
		if len(v.Response.Unavailable) > 0 {
			names := []string{}
			for _, kind := range v.Response.Unavailable {
				names = append(names, chatsearchKind(locale, kind))
			}
			// CHATBUG-096: the partial failure names its sources in one
			// sentence with one Retry beside it, like the full failure above.
			children = append(children, html.Div(html.Props{Class: "search-status", Role: "status"}, html.P(html.Props{Text: strings.ReplaceAll(t("missing"), "{kinds}", strings.Join(names, ", "))}), button("retry", t("retry"), "")))
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
				// ISSUE-3937: two rooms can carry one name; the open one is meant.
				first := ""
				for _, c := range m.Conversations {
					if strings.EqualFold(displayName(m, c), name) || strings.EqualFold(c.Name, name) {
						if c.ID == m.SelectedID {
							return c.ID
						}
						if first == "" {
							first = c.ID
						}
					}
				}
				return first
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
