package productui

import (
	stdhtml "html"
	"strings"
	"testing"
)

// TestTodo_UXBLIND_091 verifies the served navigation projection keeps the
// complete registered page name in the ordinary rail. It checks rendered DOM
// content rather than a hand-maintained copy of the catalogue.
func TestTodo_UXBLIND_091(t *testing.T) {
	for _, code := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(testView(PageWorkflowDesigner), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("%s: render navigation: %v", code, err)
		}
		item, ok := navigationItemForPage(PageWorkflowDesigner, view.Locale)
		if !ok || strings.TrimSpace(item.Label) == "" {
			t.Fatalf("%s: workflow designer is not a registered navigation item", code)
		}
		encodedLabel := stdhtml.EscapeString(item.Label)
		if !strings.Contains(doc, encodedLabel) || !strings.Contains(doc, `aria-label="`+encodedLabel+`"`) {
			t.Fatalf("%s: ordinary navigation lost full label %q", code, item.Label)
		}
		if strings.Contains(doc, item.Label+"…") {
			t.Fatalf("%s: ordinary navigation ellipsized registered label %q", code, item.Label)
		}
	}
}

// TestTodo_UXBLIND_091_Browser checks the component DOM in each navigation
// state. A live browser must still verify layout at the requested widths;
// this test cannot measure painted pixels or CSS line wrapping.
func TestTodo_UXBLIND_091_Browser(t *testing.T) {
	cases := []struct {
		name string
		view View
		want []string
	}{
		{
			name: "ordinary",
			view: testView(PageWorkflowDesigner),
			want: []string{`class="sidebar"`, `class="nav-label">Workflow Designer</span>`, `aria-label="Workflow Designer"`},
		},
		{
			name: "favorited",
			view: ApplyRequest(testView(PageWorkflowDesigner), PageRequest{FavoritePages: []PageID{PageWorkflowDesigner}}),
			want: []string{`>Favorites</li>`, `aria-label="Remove Workflow Designer from favorites"`, `title="Remove Workflow Designer from favorites"`},
		},
		{
			name: "collapsed",
			view: ApplyRequest(testView(PageWorkflowDesigner), PageRequest{NavCollapsed: true}),
			want: []string{`class="sidebar collapsed"`, `aria-label="Workflow Designer"`, `title="Workflow Designer"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Render(tc.view)
			if err != nil {
				t.Fatalf("render navigation: %v", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(doc, want) {
					t.Fatalf("navigation state %s missing %q", tc.name, want)
				}
			}
			if strings.Contains(doc, "Workflow Desig…") {
				t.Fatalf("navigation state %s truncated Workflow Designer", tc.name)
			}
		})
	}
}

// TestTodo_UXBLIND_091_Regression ensures every registered localized label is
// emitted intact in an ordinary navigation projection. This catches future
// long-label regressions beyond the current Workflow Designer example.
func TestTodo_UXBLIND_091_Regression(t *testing.T) {
	for _, code := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(code)
		view := ApplyLocale(testView(PageHome), locale)
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("%s: render navigation: %v", code, err)
		}
		_, projected := projectNavigation(view)
		var labels []string
		var collect func([]NavigationItemProps)
		collect = func(items []NavigationItemProps) {
			for _, item := range items {
				if len(item.Children) == 0 {
					labels = append(labels, item.Label)
					continue
				}
				collect(item.Children)
			}
		}
		collect(projected)
		for _, label := range labels {
			encodedLabel := stdhtml.EscapeString(label)
			if strings.TrimSpace(label) == "" || !strings.Contains(doc, encodedLabel) {
				t.Errorf("%s: projected label %q is absent from navigation DOM", code, label)
			}
			if strings.Contains(doc, label+"…") {
				t.Errorf("%s: projected label %q is truncated", code, label)
			}
		}
	}
}
