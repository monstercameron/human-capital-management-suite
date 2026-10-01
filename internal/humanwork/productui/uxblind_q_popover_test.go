package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_057(t *testing.T) {
	cases := []struct {
		event              string
		inside, selectable bool
		want               bool
	}{
		{uxblindQEventEscape, true, false, true},
		{uxblindQEventEscape, false, false, true},
		{uxblindQEventOutside, false, false, true},
		{uxblindQEventOutside, true, false, false},
		{uxblindQEventSelect, true, true, true},
		{uxblindQEventSelect, true, false, false},
		{uxblindQEventSelect, false, true, false},
	}
	for _, tc := range cases {
		if got := uxblindQShouldDismiss(tc.event, tc.inside, tc.selectable); got != tc.want {
			t.Errorf("dismiss(%q, inside=%t, selectable=%t) = %t, want %t", tc.event, tc.inside, tc.selectable, got, tc.want)
		}
	}
}

func TestTodo_UXBLIND_057_Browser(t *testing.T) {
	doc, err := Render(testView(PageWork))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-hcm-popover-controller="uxblind-q"`,
		`id="action-launcher"`,
		`data-hcm-transient-popover="action-launcher"`,
		`data-hcm-transient-popover="locale"`,
		`data-hcm-transient-popover="notification"`,
		`data-hcm-popover-surface="true"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("shared popover surface missing %q", want)
		}
	}
}

func TestTodo_UXBLIND_057_Accessibility(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(TransientPopover, TransientPopoverProps{
		Kind: "accessibility", Label: "Open menu", Trigger: []ui.Node{ui.Text("Menu")},
		Children: []ui.Node{html.Button(html.Props{Type: "button"}, ui.Text("Select"))},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<summary`, `aria-label="Open menu"`, `data-hcm-popover-surface="true"`, `type="button"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("popover lost keyboard-accessible contract %q: %s", want, markup)
		}
	}
}
