package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_CHAT_033 pins the accessible resize contract exposed by the chat
// workspace. The widths are page data (the CSP disallows inline styles), and
// both separators expose their current bounded value to assistive technology.
func TestTodo_CHAT_033(t *testing.T) {
	markup, err := ui.RenderToString(Build(Model{
		State:         StateReady,
		SelectedID:    "room-1",
		ShowDetails:   true,
		Pane:          PaneSizes{Rail: 300, Details: 360},
		Conversations: []Conversation{{ID: "room-1", Name: "People Ops", Kind: PublicChannel}},
		Callbacks: Callbacks{
			SelectConversation: func(string) {},
			ResizeRail:         func(int) {},
			ResizeDetails:      func(int) {},
			RestorePanes:       func() {},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-rail-width="300"`, `data-details-width="360"`,
		`class="pane-handle"`, `role="separator"`,
		`aria-orientation="vertical"`, `aria-valuenow="300"`,
		`aria-valuemin="220"`, `aria-valuemax="420"`,
		`aria-valuenow="360"`, `aria-valuemin="240"`, `aria-valuemax="440"`,
		`tabindex="0"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("pane resize workspace is missing %q", want)
		}
	}
	if got := strings.Count(markup, `class="pane-handle"`); got != 2 {
		t.Fatalf("rendered %d pane separators, want rail and details", got)
	}
}

// TestTodo_CHAT_033_Accessibility verifies keyboard resizing in both reading
// directions and the Home restore path without requiring a pointer.
func TestTodo_CHAT_033_Accessibility(t *testing.T) {
	for _, tc := range []struct {
		name, direction, pane, key string
		want                       int
	}{
		{name: "rail right", direction: "ltr", pane: "rail", key: "ArrowRight", want: 316},
		{name: "rail rtl right", direction: "rtl", pane: "rail", key: "ArrowRight", want: 284},
		{name: "details right", direction: "ltr", pane: "details", key: "ArrowRight", want: 344},
		{name: "details rtl right", direction: "rtl", pane: "details", key: "ArrowRight", want: 376},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := 0
			m := Model{Direction: tc.direction, Pane: PaneSizes{Rail: 300, Details: 360}, Callbacks: Callbacks{
				ResizeRail:    func(px int) { got = px },
				ResizeDetails: func(px int) { got = px },
			}}
			if !m.resizeFromKey(tc.pane, tc.key) || got != tc.want {
				t.Fatalf("keyboard resize = %d, want %d", got, tc.want)
			}
		})
	}
	restores := 0
	m := Model{Callbacks: Callbacks{RestorePanes: func() { restores++ }}}
	if !m.resizeFromKey("rail", "Home") || restores != 1 {
		t.Fatal("Home did not restore default pane widths")
	}
}
