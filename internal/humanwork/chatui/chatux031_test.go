package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	xhtml "golang.org/x/net/html"
)

// TestTodo_CHATUX_031: the reading settings are one row (a label and the select
// that holds the language, with a tick after a save), the translate switch is
// its own row, there is no Change button and no second labelled select, and the
// counts and skin tones are laid out as plain text and one row of six.
func TestTodo_CHATUX_031(t *testing.T) {
	render := func(saved bool) string {
		node := RenderingSettings(RenderingSettingsModel{Locale: "en-US", Preference: chatrender.DefaultPreference("en"), AutoSave: true, Saved: saved})
		out, err := ui.RenderToString(node)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	page := render(false)
	for _, want := range []string{`class="chat-prefs-head chatux031-row"`, `id="chatrender-reading"`, `Reading language`, `id="chatrender-translate"`, `data-state="help"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the settings do not carry %s", want)
		}
	}
	if strings.Contains(page, "Read messages in") || strings.Contains(page, "chat-prefs-change") || strings.Contains(page, "chatux031-tick") {
		t.Error("the form keeps the second label, the Change button or a tick before any save")
	}
	if saved := render(true); !strings.Contains(saved, `chatux031-tick`) || !strings.Contains(saved, `data-state="saved"`) {
		t.Error("a saved change draws no tick")
	}
	for _, want := range []string{"repeat(6,minmax(0,1fr))", ".chatrender-language-count{border:0"} {
		if !strings.Contains(ChatUX031Styles, want) {
			t.Errorf("the preferences styles lack %s", want)
		}
	}
}

// chatux031Selected reports whether an option carries the selected attribute,
// which an HTML parser keeps with an empty value.
func chatux031Selected(n *xhtml.Node) bool {
	for _, a := range n.Attr {
		if a.Key == "selected" {
			return true
		}
	}
	return false
}
