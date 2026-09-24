package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestTodo_REV_069_03(t *testing.T) {
	tests := []struct {
		name         string
		layout       ListDetailLayout
		selected     string
		wantList     bool
		wantDetail   bool
		wantBackLink bool
	}{
		{name: "wide without route selection", layout: ListDetailWide, wantList: true, wantDetail: true},
		{name: "wide with route selection", layout: ListDetailWide, selected: "intent-1", wantList: true, wantDetail: true},
		{name: "narrow without selection", layout: ListDetailNarrow, wantList: true},
		{name: "narrow with selection", layout: ListDetailNarrow, selected: "intent-1", wantDetail: true, wantBackLink: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			view := testView(PageWork)
			view.WorkLayout = test.layout
			view.SelectedWork = test.selected
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			wantPane(t, doc, `class="surface work-list"`, test.wantList)
			wantPane(t, doc, `class="surface work-preview"`, test.wantDetail)
			wantPane(t, doc, `>Show all work</a>`, test.wantBackLink)
			if test.wantDetail && !strings.Contains(doc, `data-work-layout="`+workLayoutName(test.layout)+`"`) {
				t.Fatalf("My Work did not identify %s layout", workLayoutName(test.layout))
			}
		})
	}

	view := testView(PageWork)
	view.WorkLayout = ListDetailNarrow
	view.SelectedWork = "forged-or-filtered-out"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `class="surface work-list"`) || strings.Contains(doc, `class="surface work-preview"`) {
		t.Fatal("an unresolved selection opened a narrow detail pane")
	}
}

func TestTodo_REV_069_03_Golden(t *testing.T) {
	var material strings.Builder
	for _, test := range []struct {
		layout   ListDetailLayout
		selected string
	}{{ListDetailWide, ""}, {ListDetailWide, "intent-1"}, {ListDetailNarrow, ""}, {ListDetailNarrow, "intent-1"}} {
		view := testView(PageWork)
		view.WorkLayout, view.SelectedWork = test.layout, test.selected
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range []string{`data-work-layout="` + workLayoutName(test.layout) + `"`, `data-work-selection="`, `class="surface work-list"`, `class="surface work-preview"`} {
			material.WriteString(strconvBool(strings.Contains(doc, fragment)))
			material.WriteByte(0)
		}
	}
	digest := sha256.Sum256([]byte(material.String()))
	got := hex.EncodeToString(digest[:])
	const want = "b577bfac94380700206eabbfd844459c44a6354169c6c2d0ec5fb79f389342ac"
	if got != want {
		t.Fatalf("My Work pane golden = %s, want %s", got, want)
	}
}

// Browser coverage verifies the route-level contract the interactive
// component consumes at each measured layout. The WASM browser harness also
// drives the live matchMedia change event and verifies resize rerendering.
func TestTodo_REV_069_03_Browser(t *testing.T) {
	for _, width := range []struct {
		name   string
		layout ListDetailLayout
	}{{"desktop", ListDetailWide}, {"390px", ListDetailNarrow}, {"320px", ListDetailNarrow}} {
		t.Run(width.name, func(t *testing.T) {
			view := testView(PageWork)
			view.WorkLayout = width.layout
			view.SelectedWork = "intent-1"
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			if width.layout == ListDetailNarrow {
				if strings.Contains(doc, `class="surface work-list"`) || !strings.Contains(doc, `class="surface work-preview"`) || !strings.Contains(doc, `href="/workspace/app/work?filter="`) {
					t.Fatalf("%s selected state did not render one navigable detail pane", width.name)
				}
				return
			}
			if !strings.Contains(doc, `class="surface work-list"`) || !strings.Contains(doc, `class="surface work-preview"`) {
				t.Fatal("desktop layout did not render both panes")
			}
		})
	}
}

func wantPane(t *testing.T, doc, fragment string, want bool) {
	t.Helper()
	got := strings.Contains(doc, fragment)
	if got != want {
		t.Fatalf("pane presence %q = %t, want %t", fragment, got, want)
	}
}

func strconvBool(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
