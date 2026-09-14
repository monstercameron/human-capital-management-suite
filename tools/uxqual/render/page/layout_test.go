package page

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_UIPOLISH_002 proves the five page primitives share one spacing
// contract, preserve caller attributes, and expose semantic hooks for the
// renderer-owned responsive stylesheet.
func TestTodo_UIPOLISH_002(t *testing.T) {
	data := map[string]string{"landmark": "content"}
	cases := []struct {
		name      string
		got       ui.Node
		wantClass string
		wantKind  string
	}{
		{"stack", Stack(LayoutProps{Props: html.Props{Data: data}, Spacing: "spacing.3"}, ui.Text("one")), "layout-stack layout-spacing-3", "stack"},
		{"cluster", Cluster(LayoutProps{Spacing: "spacing.3"}, ui.Text("one")), "layout-cluster layout-spacing-3", "cluster"},
		{"grid", Grid(LayoutProps{Spacing: "spacing.3"}, ui.Text("one")), "layout-grid layout-spacing-3", "grid"},
		{"split", Split(LayoutProps{Spacing: "spacing.3"}, ui.Text("one")), "layout-split layout-spacing-3", "split"},
		{"page-frame", PageFrame(LayoutProps{Spacing: "spacing.3"}, ui.Text("one")), "layout-page-frame layout-spacing-3", "page-frame"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ui.RenderToString(tc.got)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				`class="` + tc.wantClass + `"`,
				`data-layout-primitive="` + tc.wantKind + `"`,
				`data-spacing-token="spacing.3"`,
				`layout-spacing-3`,
				`one`,
			} {
				if !strings.Contains(out, want) {
					t.Errorf("rendered %s missing %q: %s", tc.name, want, out)
				}
			}
		})
	}
	if got := data["landmark"]; got != "content" {
		t.Fatalf("primitive mutated caller Data map: landmark=%q", got)
	}
}

func TestLayoutPrimitivesRejectUncontrolledSpacing(t *testing.T) {
	for _, spacing := range []string{"", "spacing.5", "spacing.3; color:red", "--custom", "space.3"} {
		out, err := ui.RenderToString(Stack(LayoutProps{Spacing: spacing, Density: "experimental"}, ui.Text("x")))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, `data-spacing-token=`) || strings.Contains(out, `data-density=`) {
			t.Errorf("uncontrolled layout values emitted for %q: %s", spacing, out)
		}
	}
}

func TestLayoutPrimitivePreservesExistingClassAndData(t *testing.T) {
	out, err := ui.RenderToString(Grid(LayoutProps{
		Props:   html.Props{Class: "customer-region", Data: map[string]string{"region": "primary"}},
		Spacing: " spacing.2 ", Density: "dense",
	}, ui.Text("content")))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="customer-region layout-grid layout-spacing-2"`, `data-region="primary"`, `data-spacing-token="spacing.2"`, `data-density="dense"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
}

func TestLayoutPrimitiveEmitsComfortableDensity(t *testing.T) {
	out, err := ui.RenderToString(PageFrame(LayoutProps{Density: "comfortable"}, ui.Text("content")))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="layout-page-frame"`, `data-density="comfortable"`} {
		if !strings.Contains(out, want) {
			t.Errorf("comfortable page frame missing %q: %s", want, out)
		}
	}
}

func TestLayoutCSSConsumesProductionSpacingScale(t *testing.T) {
	css := LayoutCSS()
	for _, want := range []string{"--hcm-space-1", "--hcm-space-2", "--hcm-space-3", "--hcm-space-4", "--hcm-density", ".layout-spacing-3", "@media (max-width:48rem)"} {
		if !strings.Contains(css, want) {
			t.Errorf("LayoutCSS missing production hook %q", want)
		}
	}
}

func TestLayoutCSSDensityAndFrameRhythmAreComplete(t *testing.T) {
	css := LayoutCSS()
	for _, want := range []string{
		`.layout-page-frame{display:flex;flex-direction:column;`,
		`var(--hcm-space-2,.5rem)`,
		`var(--hcm-density,1)`,
		`.layout-page-frame[data-density="dense"]`,
		`--hcm-density:1.25`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("LayoutCSS missing shared layout guarantee %q", want)
		}
	}
}
