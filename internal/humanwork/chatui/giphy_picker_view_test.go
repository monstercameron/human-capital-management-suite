package chatui

import (
	"strings"
	"testing"
)

func TestGiphyPickerRendersLocalizedStatusAndAccessibleNoKeyState(t *testing.T) {
	model := Model{State: StateReady, SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "People"}}, Text: func(key string) string {
		switch key {
		case KeyGiphyUnavailable:
			return "GIFs require an administrator key"
		case KeyGiphyLoading:
			return "GIFs loading"
		case KeyGiphyLoadError:
			return "GIF error"
		case KeyGiphyNoResults:
			return "No GIF matches"
		case KeyGiphyClose:
			return "Close GIFs"
		default:
			return key
		}
	}}
	markup := render(t, model)
	for _, want := range []string{
		`data-action="giphy-toggle"`, `disabled`,
		`aria-label="GIFs require an administrator key"`, `GIFs require an administrator key`,
		`data-loading="GIFs loading"`, `data-load-error="GIF error"`,
		`data-no-results="No GIF matches"`, `data-close="Close GIFs"`,
		`Powered by GIPHY`, `maxLength="50"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("GIPHY picker is missing %q", want)
		}
	}
}

func TestGiphyPickerConfiguredControlIsEnabled(t *testing.T) {
	model := Model{State: StateReady, SelectedID: "room", GiphyAPIKey: "public-key", Conversations: []Conversation{{ID: "room", Name: "People"}}}
	markup := render(t, model)
	start := strings.Index(markup, `data-action="giphy-toggle"`)
	if start < 0 {
		t.Fatal("configured GIPHY picker trigger was not rendered")
	}
	end := strings.Index(markup[start:], ">")
	if end < 0 || strings.Contains(markup[start:start+end], "disabled") {
		t.Fatalf("configured GIPHY trigger is disabled: %s", markup[start:start+end])
	}
}
