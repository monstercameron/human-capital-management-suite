package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func giphyPickerControl(m Model, targetID string, disabled bool) ui.Node {
	pickerID := targetID + "-giphy-picker"
	configured := GiphyConfigured(GiphyPickerConfig{APIKey: m.GiphyAPIKey})
	label := m.t(KeyGiphyPicker)
	// Round 3 C-6: with no GIPHY key the trigger is left out rather than
	// shown permanently disabled. A greyed "GIF" in every composer read as a
	// broken control (2.03:1 light, 2.85:1 dark) and offered nothing to do;
	// the key is a deployment setting, not something the reader can fix.
	// Typing "/giphy" still explains the missing key (KeyGiphyUnavailable,
	// render.go), and the hidden picker stays so that path has a target.
	var children []ui.Node
	if configured {
		children = append(children, html.Button(html.Props{Class: "tool-button giphy-trigger", Type: "button", Disabled: disabled,
			Data:  map[string]string{"action": "giphy-toggle", "id": targetID},
			Aria:  map[string]string{"label": label, "expanded": "false", "controls": pickerID, "haspopup": "dialog"},
			Title: label}, html.Span(html.Props{Class: "giphy-trigger-label", Text: "GIF"})))
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
