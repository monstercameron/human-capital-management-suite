package productui

import (
	"strings"
	"testing"
)

func TestTodo_UXSCAN_010(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`--hcm-nav-scrollbar-size-rail:6px;`,
		`--hcm-nav-scrollbar-thumb:color-mix(in srgb,var(--muted) 38%,var(--surface));`,
		`scroll-padding-block:12px 24px;`,
		`.primary-nav>ul{padding-block-end:20px;}`,
		`scroll-padding-block:16px;`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("sidebar reachability/rail treatment missing %q", want)
		}
	}
	for _, forbidden := range []string{`.subnav{overflow-y:auto`, `.nav-group{overflow-y:auto`, `.nav-bottom{overflow-y:auto`} {
		if strings.Contains(css, forbidden) {
			t.Fatalf("nested navigation scroll owner found: %s", forbidden)
		}
	}
}

func TestTodo_UXSCAN_010_Accessibility(t *testing.T) {
	view := testView(PageAdmin)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(doc, `id="primary-nav"`) != 1 || strings.Count(doc, `id="workspace-navigation"`) != 1 {
		t.Fatal("navigation lost its single named primary scroll landmark")
	}
	for _, want := range []string{`tabIndex="0"`, `aria-label="Main"`, `aria-label="Workspace navigation"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("keyboard navigation contract missing %q", want)
		}
	}
}
