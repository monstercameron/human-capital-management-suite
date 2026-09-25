package projectui

import (
	"strconv"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Sharing a board or a ticket. Every action is a button carrying the
// in-app address and title; the host copies the absolute link, copies a
// Markdown line for Docs (which unfurls into a card there), or opens the
// send-to-chat dialog. Nothing here talks to a service.

// shareItems are the three share actions, as menu items.
func shareItems(copy Copy, href, title string) []ui.Node {
	board := copy.Board
	item := func(action, icon, label, done string) ui.Node {
		data := map[string]string{"projectui-action": action, "href": href, "title": title}
		if done != "" {
			data["copied"] = done
		}
		return html.Button(html.Props{Type: "button", Class: "projectui-menu-item", Role: "menuitem", Data: data},
			html.Span(html.Props{Class: "projectui-menu-icon", Data: map[string]string{"icon": icon}, Aria: map[string]string{"hidden": "true"}}),
			html.Span(html.Props{Class: "projectui-menu-item-label", Text: label}))
	}
	return []ui.Node{
		item("copy-link", "link", copy.CopyLink, copy.LinkCopied),
		item("share-chat", "chat", board.SendToChat, ""),
		item("copy-docs", "docs", board.CopyForDocs, board.CopiedForDocs),
	}
}

// ShareMenu is the Share button and its menu for a board or a ticket.
func ShareMenu(copy Copy, share *Share) ui.Node {
	if share == nil || share.Href == "" {
		return nil
	}
	copy.Board = localizedBoardCopy(copy.Board, formatOrItoa(copy.FormatNumber))
	return html.Details(html.Props{Class: "projectui-share"},
		html.Summary(html.Props{Class: "projectui-share-trigger", Aria: map[string]string{"haspopup": "menu"}},
			html.Span(html.Props{Class: "projectui-share-icon", Aria: map[string]string{"hidden": "true"}}),
			html.Span(html.Props{Text: copy.Board.Share})),
		html.Div(html.Props{Class: "projectui-card-menu-panel projectui-share-panel", Role: "menu", Aria: map[string]string{"label": copy.Board.Share}}, shareItems(copy, share.Href, share.Title)...),
	)
}

// ShareDialog is the send-to-chat dialog and the toast that follows a
// send. The host opens it with the item's address and title.
func ShareDialog(copy Copy, share *Share) ui.Node {
	if share == nil {
		return nil
	}
	copy.Board = localizedBoardCopy(copy.Board, formatOrItoa(copy.FormatNumber))
	board := copy.Board
	channels, direct := []ui.Node{}, []ui.Node{}
	for _, target := range share.Targets {
		option := html.Option(html.Props{Value: target.ID, Text: target.Label})
		if target.Direct {
			direct = append(direct, option)
		} else {
			channels = append(channels, option)
		}
	}
	options := []ui.Node{}
	if len(channels) > 0 {
		options = append(options, html.Tag("optgroup", html.Props{Raw: map[string]any{"label": board.Channels}}, channels...))
	}
	if len(direct) > 0 {
		options = append(options, html.Tag("optgroup", html.Props{Raw: map[string]any{"label": board.DirectMessages}}, direct...))
	}
	body := []ui.Node{
		html.Div(html.Props{Class: "projectui-share-head"},
			html.H2(html.Props{ID: "projectui-share-title", Text: board.SendTitle}),
			html.Button(html.Props{Type: "button", Class: "projectui-modal-close", Data: map[string]string{"projectui-action": "close-share"}, Aria: map[string]string{"label": copy.Close}}, html.Span(html.Props{Class: "projectui-modal-close-icon", Aria: map[string]string{"hidden": "true"}})),
		),
		html.P(html.Props{ID: "projectui-share-item", Class: "projectui-share-item", Dir: "auto"}),
	}
	if len(options) == 0 && share.Fill {
		options = append(options, html.Option(html.Props{Raw: map[string]any{"value": ""}, Text: "…"}))
	}
	if len(options) == 0 {
		body = append(body, html.P(html.Props{Class: "projectui-muted", Text: board.NoConversations}))
	} else {
		body = append(body,
			html.Label(html.Props{For: "projectui-share-conversation", Class: "projectui-share-label", Text: board.Conversation}),
			html.Select(html.Props{ID: "projectui-share-conversation", Name: "conversation", Class: "projectui-share-select", Data: map[string]string{"fill": boolString(share.Fill), "channels": board.Channels, "direct": board.DirectMessages}}, options...),
			html.Label(html.Props{For: "projectui-share-note", Class: "projectui-share-label", Text: board.ShareNote}),
			html.Textarea(html.Props{ID: "projectui-share-note", Name: "note", Rows: 2, MaxLength: 2000, Class: "projectui-share-note", Dir: "auto"}),
		)
	}
	body = append(body,
		html.Input(html.Props{Type: "hidden", Name: "href"}),
		html.Input(html.Props{Type: "hidden", Name: "title"}),
		html.P(html.Props{Class: "projectui-share-error", Role: "alert", Hidden: true, Text: board.ShareFailed}),
		html.Div(html.Props{Class: "projectui-share-actions"},
			html.Button(html.Props{Type: "button", Class: "projectui-button", Data: map[string]string{"projectui-action": "close-share"}, Text: copy.Cancel}),
			html.Button(html.Props{Type: "submit", Class: "projectui-button projectui-button-primary", Disabled: len(options) == 0, Text: board.Send}),
		),
	)
	return html.Div(html.Props{Class: "projectui-share-host"},
		html.Tag("dialog", html.Props{ID: "projectui-share-dialog", Class: "projectui-share-dialog", Aria: map[string]string{"labelledby": "projectui-share-title"}},
			html.Form(html.Props{Class: "projectui-share-form", Data: map[string]string{"projectui-action": "share-send", "sent": board.SentTo("{name}")}}, body...)),
		shareToast("projectui-toast", board),
	)
}

// shareToast is the confirmation after a send. The modal renders its own,
// because a modal dialog makes the page behind it inert.
func shareToast(id string, board BoardCopy) ui.Node {
	return html.Div(html.Props{ID: id, Class: "projectui-toast", Role: "status", Hidden: true},
		html.Span(html.Props{Class: "projectui-toast-text"}),
		html.A(html.Props{Class: "projectui-toast-link", Href: "/workspace/app/chat", Text: board.OpenConversation}))
}

func formatOrItoa(format func(int) string) func(int) string {
	if format != nil {
		return format
	}
	return strconv.Itoa
}
