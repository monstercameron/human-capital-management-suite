package chatui

import (
	"strings"
	"testing"
)

// TestIssue3861_PreviewHasRoomInComposer: the composer is held to a third of the
// window, so a populated poll or to-do preview grew past the form and its draft
// field was drawn over the formatting toolbar. With a preview open the form must
// lift that cap, give the preview the room that is left (it scrolls inside
// itself, Post and Cancel stay under it), and keep the draft and the toolbar
// out of the shrinking part so they never overlap.
func TestIssue3861_PreviewHasRoomInComposer(t *testing.T) {
	rules := map[string][]string{
		`form.chat-composer.chat-composer:has(.chatcmd003-preview){`:                                             {"max-block-size:min(80dvh", "min-block-size:0"},
		`form.chat-composer.chat-composer:has(.chatcmd003-preview) .chatcmd003-slot{`:                            {"flex:1 1 auto", "min-block-size:0", "display:flex", "flex-direction:column"},
		`form.chat-composer.chat-composer .chatcmd003-slot>.chatcmd003-preview{`:                                 {"flex:1 1 auto", "min-block-size:0", "max-height:none"},
		`form.chat-composer.chat-composer:has(.chatcmd003-preview) .composer-draft{`:                             {"flex:none"},
		`form.chat-composer.chat-composer:has(.chatcmd003-preview) :is(.composer-format-row,.composer-toolbar){`: {"flex:none"},
	}
	for selector, wants := range rules {
		at := strings.Index(ChatLane2Styles, selector)
		if at < 0 {
			t.Errorf("no rule %q", selector)
			continue
		}
		body := ChatLane2Styles[at+len(selector):]
		body = body[:strings.Index(body, "}")]
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s lacks %q (has %q)", selector, want, body)
			}
		}
	}
	// The form must not clip: the mention, command and add menus open above it.
	form := `form.chat-composer.chat-composer:has(.chatcmd003-preview){`
	if at := strings.Index(ChatLane2Styles, form); at >= 0 {
		body := ChatLane2Styles[at+len(form):]
		if body = body[:strings.Index(body, "}")]; strings.Contains(body, "overflow") {
			t.Errorf("composer form with a preview clips its popup menus: %q", body)
		}
	}
	// The preview's own body is still the scroll owner and the actions stay
	// pinned under it.
	for _, want := range []string{`.chatcmd003-preview>.chatcmd003-body{`, `overflow-y:auto`, `.chatcmd003-preview>.chatcmd003-actions{`} {
		if !strings.Contains(ChatLane2Styles, want) {
			t.Errorf("preview styles lack %q", want)
		}
	}
}
