package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func giphyPickerControl(m Model, targetID string, disabled bool) ui.Node {
	pickerID := targetID + "-giphy-picker"
	configured := GiphyConfigured(GiphyPickerConfig{APIKey: m.GiphyAPIKey})
	label := m.t(KeyGiphyPicker)
	if !configured {
		label = m.t(KeyGiphyUnavailable)
	}
	children := []ui.Node{
		html.Button(html.Props{Class: "tool-button giphy-trigger", Type: "button", Disabled: disabled || !configured,
			Data:  map[string]string{"action": "giphy-toggle", "id": targetID},
			Aria:  map[string]string{"label": label, "expanded": "false", "controls": pickerID, "haspopup": "dialog"},
			Title: label}, html.Span(html.Props{Class: "giphy-trigger-label", Text: "GIF"})),
	}
	if !configured {
		children = append(children, html.Span(html.Props{Class: "giphy-unavailable", Text: label, Role: "status"}))
	}
	children = append(children,
		html.Div(html.Props{ID: pickerID, Class: "giphy-picker", Role: "dialog", Hidden: true,
			Data: map[string]string{"loading": m.t(KeyGiphyLoading), "load-error": m.t(KeyGiphyLoadError), "no-results": m.t(KeyGiphyNoResults), "close": m.t(KeyGiphyClose)},
			Aria: map[string]string{"label": m.t(KeyGiphyPickerTitle)}},
			html.Label(html.Props{Class: "sr-only", For: pickerID + "-query"}, ui.Text(m.t(KeyGiphySearch))),
			html.Input(html.Props{ID: pickerID + "-query", Class: "chat-input giphy-query", Type: "search", MaxLength: GiphyMaxQuery, Placeholder: m.t(KeyGiphySearch), AutoComplete: "off", Aria: map[string]string{"controls": pickerID + "-results"}}),
			html.Div(html.Props{ID: pickerID + "-status", Class: "giphy-status", Role: "status", Aria: map[string]string{"live": "polite"}}),
			html.Div(html.Props{ID: pickerID + "-results", Class: "giphy-results"}),
			html.Button(html.Props{Class: "giphy-more", Type: "button", Data: map[string]string{"action": "giphy-more", "id": targetID}, Hidden: true, Text: m.t(KeyGiphyMore)}),
			html.Div(html.Props{Class: "giphy-attribution", Text: "Powered by GIPHY"}),
		),
	)
	return html.Div(html.Props{Class: "giphy-control"}, children...)
}
