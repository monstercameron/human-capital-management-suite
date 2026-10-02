package chatui

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// moderationWhen writes a time the way the conversation does: a day and a clock,
// in the reader's time zone.
func moderationWhen(locale string, at time.Time, zone *time.Location, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	if zone == nil {
		zone = time.UTC
	}
	at = at.In(zone)
	clock := at.Format("3:04 PM")
	switch {
	case strings.HasPrefix(locale, "de"):
		clock = at.Format("15:04")
	case strings.HasPrefix(locale, "ar"):
		clock = arabicDigits(at.Format("15:04"))
	}
	return formatDay(locale, at, at.Year() != now.In(zone).Year()) + ", " + clock
}

func moderationFill(text string, pairs ...string) string {
	for i := 0; i+1 < len(pairs); i += 2 {
		text = strings.ReplaceAll(text, pairs[i], pairs[i+1])
	}
	return text
}

// moderationMessage draws a message the way the conversation does: the same
// renderer, so the author, the time and the body read as they do in the room.
// Nothing in it is clickable here; its toolbar is not drawn on this page.
func moderationMessage(m ModerationPageModel, id, authorID, body string, at time.Time, now time.Time) ui.Node {
	model := Model{Locale: m.Locale, Direction: direction(m.Locale)}
	msg := Message{ID: id, AuthorID: authorID, Author: chatremoveName(m, authorID), Body: body, TimeLabel: moderationWhen(m.Locale, at, m.TimeZone, now)}
	return html.Div(html.Props{Class: "chatmod005-message"}, message(model, handlers{}, msg, false))
}

func moderationKindOrder(kind string) int {
	switch kind {
	case "appeal":
		return 0
	case "report":
		return 1
	case "filter":
		return 2
	}
	return 3
}

// ModerationPageHeading is the heading row of the page: its name and the close
// control the details and thread panels use.
func moderationHeading(locale string) ui.Node {
	label := chatremoveText(locale, "close_moderation")
	return html.Header(html.Props{Class: "side-heading chatmod005-heading"},
		html.H2(html.Props{ID: "chatremove-title", Text: chatremoveText(locale, "moderation")}),
		html.Button(html.Props{Class: "icon-button", Type: "button", Title: label, Aria: map[string]string{"label": label}, Data: map[string]string{"chatremove-close": "true"}}, icon("close")))
}

func moderationTabs(m ModerationPageModel) ui.Node {
	t := func(key string) string { return chatremoveText(m.Locale, key) }
	open := t("tab_open")
	if m.OpenCount > 0 {
		open += " · " + strconv.Itoa(m.OpenCount)
	}
	tab := func(name, label string) ui.Node {
		selected := (m.Tab == "resolved") == (name == "resolved")
		return html.Button(html.Props{Type: "button", Class: "chatmod005-tab", Role: "tab", Text: label, Aria: map[string]string{"selected": boolString(selected)}, Data: map[string]string{"chatremove-open": ModerationPageHref + "?" + url.Values{"tab": {name}, "locale": {m.Locale}}.Encode()}})
	}
	return html.Div(html.Props{Class: "chatmod005-tabs", Role: "tablist", Aria: map[string]string{"label": t("moderation")}}, tab("open", open), tab("resolved", t("tab_resolved")))
}

func moderationSearch(locale, query string) ui.Node {
	label := chatremoveText(locale, "search_placeholder")
	return html.Form(html.Props{Class: "chatmod005-search", Role: "search", Data: map[string]string{"chatremove": "filter"}},
		html.Div(html.Props{Class: "member-filter"},
			html.Label(html.Props{Class: "sr-only", For: "chatremove-search", Text: label}), icon("search"),
			html.Input(html.Props{ID: "chatremove-search", Class: "chat-search", Name: "query", Type: "search", Placeholder: label, AutoComplete: "off", Data: map[string]string{"chat-value": query}})))
}

// moderationQueuePage is the Moderation page: a heading row with the close
// control, Open and Resolved, a search once there is something to search, then
// the items, each with the message as the conversation draws it, why it is
// here, what can be done with it, and the conversation around it on request.
func moderationQueuePage(m ModerationPageModel) ui.Node {
	t := func(key string) string { return chatremoveText(m.Locale, key) }
	now := time.Now()
	resolved := m.Tab == "resolved"
	nodes := []ui.Node{moderationHeading(m.Locale)}
	if m.NoQueue {
		nodes = append(nodes, html.H3(html.Props{Class: "chatmod005-subheading", Text: t("notices_title")}))
	} else {
		nodes = append(nodes, moderationTabs(m))
		if len(m.Items) > 0 && m.State == StateReady {
			nodes = append(nodes, moderationSearch(m.Locale, m.Query))
		}
	}
	switch {
	case m.State == StateLoading:
		nodes = append(nodes, html.P(html.Props{Role: "status", Aria: map[string]string{"busy": "true"}, Text: t("loading")}))
	case m.State == StateError:
		key := "error"
		if m.ErrorCode == "conflict" {
			key = "conflict"
		}
		nodes = append(nodes, html.P(html.Props{Role: "alert", Text: t(key)}), html.Button(html.Props{Type: "button", Class: "chatremove-link", Text: t("retry"), Data: map[string]string{"chatremove-open": ModerationPageHref + "?" + url.Values{"tab": {m.Tab}, "locale": {m.Locale}}.Encode()}}))
	case m.NoQueue && len(m.Notices) == 0:
		nodes = append(nodes, html.P(html.Props{Class: "chatmod005-empty", Text: t("notices_empty")}))
	case !m.NoQueue && len(m.Items) == 0:
		first, hint := "nothing", "nothing_hint"
		if resolved {
			first, hint = "nothing_resolved", "nothing_resolved_hint"
		}
		nodes = append(nodes, html.Div(html.Props{Class: "chatmod005-empty", Role: "status"}, html.P(html.Props{Text: t(first)}), html.P(html.Props{Class: "chatmod005-muted", Text: t(hint)})))
	}
	if m.State == StateReady && !m.NoQueue && len(m.Items) > 0 {
		items := append([]chat.ModerationItem(nil), m.Items...)
		sort.SliceStable(items, func(i, j int) bool {
			if resolved {
				return items[i].At.After(items[j].At)
			}
			if items[i].At.Equal(items[j].At) {
				return moderationKindOrder(items[i].Kind) < moderationKindOrder(items[j].Kind)
			}
			return items[i].At.Before(items[j].At)
		})
		list := []ui.Node{}
		for _, item := range items {
			list = append(list, moderationQueueItem(m, item, now))
		}
		nodes = append(nodes, html.Div(html.Props{Class: "chatmod005-items", Data: map[string]string{"chatremove-items": "true"}}, list...),
			html.P(html.Props{Class: "chatmod005-muted chatmod005-nomatch", Hidden: true, Role: "status", Text: t("no_match")}))
	}
	if m.State == StateReady && len(m.Notices) > 0 {
		if !m.NoQueue {
			nodes = append(nodes, html.H3(html.Props{Class: "chatmod005-subheading", Text: t("notices_title")}))
		}
		for _, notice := range m.Notices {
			nodes = append(nodes, ModerationAuthorNotice(m.Locale, notice))
		}
	}
	return html.Main(html.Props{ID: "chatremove-moderation", Class: "chatremove chatmod005-page", Lang: m.Locale, Dir: direction(m.Locale)}, nodes...)
}

func moderationQueueItem(m ModerationPageModel, item chat.ModerationItem, now time.Time) ui.Node {
	t := func(key string) string { return chatremoveText(m.Locale, key) }
	when := moderationWhen(m.Locale, item.At, m.TimeZone, now)
	head := []ui.Node{html.Span(html.Props{Class: "chatmod005-kind", Text: moderationKindLabel(m.Locale, item.Kind)})}
	if item.Message.Deleted && item.PostID != "" {
		head = append(head, html.Span(html.Props{Class: "chatmod005-kind chatmod005-removed", Text: t("removed_badge")}))
	}
	if item.ConversationName != "" {
		head = append(head, html.A(html.Props{Class: "chatmod005-channel", Dir: "auto", Href: ChannelReferenceURL(item.ConversationID), Text: "#" + item.ConversationName}))
	}
	children := []ui.Node{html.Div(html.Props{Class: "chatmod005-item-head"}, head...)}
	switch {
	case item.PostID == "" || item.Message.ID == "":
		children = append(children, html.P(html.Props{Class: "chatmod005-muted", Text: t("no_message")}))
	default:
		children = append(children, moderationMessage(m, item.PostID, item.AuthorID, item.Message.Body, item.Message.CreatedAt, now))
	}
	if item.Rule != "" {
		children = append(children, html.P(html.Props{Class: "chatmod005-muted chatmod005-why", Dir: "auto", Text: t("rule") + ": " + item.Rule}))
	} else if item.Kind != "appeal" {
		children = append(children, html.P(html.Props{Class: "chatmod005-muted chatmod005-why", Dir: "auto", Text: t("reason") + ": " + chatremoveReason(m.Locale, item.Reason)}))
	}
	switch item.Kind {
	case "report":
		children = append(children, html.P(html.Props{Class: "chatmod005-muted", Dir: "auto", Text: moderationFill(t("reported_line"), "{name}", chatremoveName(m, item.ReporterID), "{when}", when)}))
	case "appeal":
		children = append(children, html.P(html.Props{Class: "chatmod005-muted", Dir: "auto", Text: moderationFill(t("appeal_line"), "{name}", chatremoveName(m, item.ReporterID), "{when}", when)}))
	case "filter":
		children = append(children, html.P(html.Props{Class: "chatmod005-muted", Text: moderationFill(t("flag_line"), "{when}", when)}))
	case "removal":
		children = append(children, html.P(html.Props{Class: "chatmod005-muted", Dir: "auto", Text: moderationFill(t("removal_line"), "{name}", chatremoveName(m, item.DecidedBy), "{when}", when)}))
	}
	if item.State != "OPEN" && item.Decision != "" && item.Kind != "removal" {
		line := moderationFill(t("decided_"+item.Decision), "{name}", chatremoveName(m, item.DecidedBy), "{when}", moderationWhen(m.Locale, item.DecidedAt, m.TimeZone, now))
		if item.DecisionReason != "" {
			line += " — " + chatremoveReason(m.Locale, item.DecisionReason)
		}
		children = append(children, html.P(html.Props{Class: "chatmod005-muted chatmod005-decision", Dir: "auto", Text: line}))
	}
	if len(item.Context) > 1 {
		around := []ui.Node{html.Summary(html.Props{Text: t("show_context")})}
		for _, post := range item.Context {
			body := post.Body
			if post.ID == item.PostID {
				continue
			}
			if body == chat.RemovedByAdministrator {
				body = t("removed")
			}
			around = append(around, moderationMessage(m, post.ID, post.AuthorID, body, post.CreatedAt, now))
		}
		children = append(children, html.Details(html.Props{Class: "chatmod005-context"}, around...))
	}
	if actions := moderationItemActions(m, item); actions != nil {
		children = append(children, actions)
	}
	return html.Article(html.Props{Class: "chatmod005-item", Data: map[string]string{"kind": item.Kind, "item-id": item.ID}}, children...)
}

// moderationItemActions is the one row of buttons an item offers, the action
// that suits its kind first: Remove for a report or a flagged message, Restore
// for an appeal. Remove and Message the author ask for what they need in a
// dialog; Dismiss and Restore need nothing and happen at once.
func moderationItemActions(m ModerationPageModel, item chat.ModerationItem) ui.Node {
	t := func(key string) string { return chatremoveText(m.Locale, key) }
	dialog := func(action, label string, primary bool) ui.Node {
		q := url.Values{"conversation": {item.ConversationID}, "post": {item.PostID}, "action": {action}, "case": {item.ID}, "locale": {m.Locale}}
		class := "chatmod005-action"
		if primary {
			class += " primary"
		}
		return html.Button(html.Props{Type: "button", Class: class, Text: label, Data: map[string]string{"chatremove-open": ModerationPageHref + "?" + q.Encode()}})
	}
	direct := func(action, label string, primary bool) ui.Node {
		class := "chatmod005-action"
		if primary {
			class += " primary"
		}
		return html.Button(html.Props{Type: "button", Class: class, Text: label, Data: map[string]string{"chatremove-act": action, "case-id": item.ID}})
	}
	var buttons []ui.Node
	if item.State != "OPEN" {
		if item.CanRestore {
			buttons = append(buttons, direct("restore", t("restore"), false))
		}
	} else {
		switch {
		case item.Kind == "appeal" && item.CanRestore:
			buttons = append(buttons, direct("restore", t("restore"), true))
		case item.CanRemove:
			buttons = append(buttons, dialog("remove", t("remove_short"), true))
		}
		buttons = append(buttons, direct("dismiss", t("dismiss"), false))
		if item.PostID != "" {
			buttons = append(buttons, dialog("message", t("message_author"), false))
		}
	}
	if len(buttons) == 0 {
		return nil
	}
	return html.Div(html.Props{Class: "chatmod005-actions"}, buttons...)
}
