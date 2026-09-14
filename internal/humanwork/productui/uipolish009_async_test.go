package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UIPOLISH_009(t *testing.T) {
	view := testView(PagePeople)
	loading, err := ui.RenderToString(BuildLoading(view))
	if err != nil {
		t.Fatal(err)
	}
	refreshing, err := ui.RenderToString(BuildRefreshing(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(loading, `data-async-region="page-content"`) || !strings.Contains(loading, `aria-live="polite"`) {
		t.Fatal("cold load lost stable async region")
	}
	if !strings.Contains(refreshing, "Avery Patel") || !strings.Contains(refreshing, `data-network-state="refreshing"`) {
		t.Fatal("refresh dropped safe stale projection")
	}
}

func TestTodo_UIPOLISH_009_Golden(t *testing.T) {
	markup, err := ui.RenderToString(BuildLoading(testView(PagePeople)))
	if err != nil {
		t.Fatal(err)
	}
	geometry := LoadingProxyGeometry(PagePeople)
	for _, want := range []string{`data-loading-contract="v1"`, `data-loading-layout="` + geometry.Layout + `"`, `data-preserve-scroll="true"`, `data-preserve-focus="true"`, "loading-table-layout"} {
		if !strings.Contains(markup, want) {
			t.Errorf("loading geometry missing %q", want)
		}
	}
}

func TestTodo_UIPOLISH_009_Browser(t *testing.T) {
	view := testView(PagePeople)
	view.RefreshingRegion = RefreshRegionPeopleDirectory
	markup, err := ui.RenderToString(BuildRefreshing(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="surface people-directory is-refreshing"`) || !strings.Contains(markup, `class="loading-progress people-directory-progress"`) {
		t.Fatal("focused refresh lost actionable region cue")
	}
}

func TestTodo_UIPOLISH_009_Accessibility(t *testing.T) {
	markup, err := ui.RenderToString(BuildContentLoading(testView(PageWork)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`aria-busy="true"`, `data-network-state="pending"`, `aria-live="polite"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("content-loading accessibility missing %q", want)
		}
	}
}

func TestTodo_UIPOLISH_009_Fault(t *testing.T) {
	view := testView(PagePeople)
	view.RefreshingRegion = "unknown-region"
	markup, err := ui.RenderToString(BuildRefreshing(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `data-network-state="refreshing"`) || !strings.Contains(markup, "Avery Patel") {
		t.Fatal("unknown refresh failed closed without retaining safe content")
	}
}

func TestTodo_UIPOLISH_009_Performance(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{".loading-progress", `:root[data-hcm-motion-preference="reduce"]`, ".network-stage-refreshing"} {
		if !strings.Contains(css, want) {
			t.Errorf("production async CSS missing %q", want)
		}
	}
}
