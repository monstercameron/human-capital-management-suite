package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Disclosures share a labelled button and a hidden body; the Chat event
// delegate also handles fragments rendered by the saved/search clients.
func chatPolishDisclosureLabel(props html.Props, children ...ui.Node) ui.Node {
	props.Type = "button"
	props.Class += " chat-disclosure-button"
	if props.Data == nil {
		props.Data = map[string]string{}
	}
	props.Data["chat-disclosure-toggle"] = "true"
	if props.Aria == nil {
		props.Aria = map[string]string{}
	}
	props.Aria["expanded"] = "false"
	if props.Text != "" {
		children = append(children, html.Span(html.Props{Text: props.Text}))
		props.Text = ""
	}
	if !strings.Contains(props.Class, "tool-button") {
		children = append(children, icon("chevron-down"))
	}
	return html.Button(props, children...)
}

func chatPolishDisclosure(props html.Props, children ...ui.Node) ui.Node {
	if len(children) == 0 {
		return nil
	}
	if props.Data == nil {
		props.Data = map[string]string{}
	}
	props.Data["chat-disclosure"] = "true"
	body := html.Props{Class: "chat-disclosure-body", Hidden: true, Data: map[string]string{"chat-disclosure-body": "true"}}
	var content ui.Node
	if strings.Contains(props.Class, "rail-prefs") || strings.Contains(props.Class, "format-tools") {
		kind := "quiet-hours"
		if strings.Contains(props.Class, "chatrender-personal") {
			kind = "reading-languages"
		}
		if strings.Contains(props.Class, "format-tools") {
			kind = "formatting"
		}
		body.Class += " chat-settings-layer"
		body.Role = "dialog"
		content = anchoredChatLayer(body, kind, children[1:]...)
	} else {
		content = html.Div(body, children[1:]...)
	}
	return html.Div(props, children[0], content)
}

func chatPolishUnavailable(locale string) string {
	switch direction(locale) {
	case "rtl":
		return "هذه الميزة غير متاحة هنا بعد."
	}
	if strings.HasPrefix(locale, "de") {
		return "Diese Funktion ist hier noch nicht verfügbar."
	}
	return "This feature is not available here yet."
}

func chatPolishPolicyScope(locale string) string {
	if strings.HasPrefix(locale, "de") {
		return "Liest Richtliniendokumente"
	}
	if direction(locale) == "rtl" {
		return "يقرأ وثائق السياسات"
	}
	return "Reads policy documents"
}

func ChatFeatureUnavailable(locale string) string { return chatPolishUnavailable(locale) }

func chatPolishSendReady(capable bool, draft string) bool {
	return capable && composerMessageReady(draft)
}
