package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_016_Browser(t *testing.T) {
	page := Page{Title: "Journeys", TenantLabel: "HarborCare Health Services", Locale: "en-US", Notice: &Notice{Busy: true, MessageKey: "journey.busy_list"}, List: &ListView{}}
	markup, err := ui.RenderToString(Build(page))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `jn-network-stale`) {
		t.Fatalf("busy navigation did not retain a stale-content stage: %s", markup)
	}
	if !strings.Contains(markup, `inert`) || !strings.Contains(markup, `jn-network-proxy`) {
		t.Fatalf("busy navigation did not replace stale controls with an inert progress proxy: %s", markup)
	}
}

func TestTodo_UXBLIND_027_Browser(t *testing.T) {
	page := Page{Title: "Journeys", Locale: "en-US", List: &ListView{}}
	markup, err := ui.RenderToString(Build(page))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, ">Journeys<") || strings.Contains(markup, "Promotion requests") || strings.Contains(markup, ">WORKFLOWS<") {
		t.Fatalf("journey list has more than one page name: %s", markup)
	}
}
