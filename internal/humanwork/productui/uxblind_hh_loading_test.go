package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_016_LoadingOutletIsInert(t *testing.T) {
	view := testView(PagePeople)
	markup, err := ui.RenderToString(BuildContentLoading(view))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="app-shell is-content-loading"`,
		`data-network-state="pending"`,
		`aria-busy="true"`,
		`inert=""`,
		`data-preserve-scroll="true"`,
		`data-preserve-focus="true"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("navigation loading outlet missing %q: %s", want, markup)
		}
	}
}
