package productui

import (
	"strings"
	"testing"
)

func TestVisualFoundationKeepsSharedTokensSemantic(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		"--hcm-control-height:44px",
		"--hcm-control-height-compact:44px",
		"--hcm-focus-ring:0 0 0 3px color-mix(in srgb,var(--hcm-color-focus) 24%,transparent)",
		"min-height:var(--hcm-control-height)",
		"border:1px solid var(--control-border)",
		"background-color:var(--surface)",
		"color:var(--ink)",
		"background-color:var(--success-bg)",
		"background-color:var(--info-bg)",
		"scrollbar-color:var(--hcm-scrollbar-thumb) var(--hcm-scrollbar-track)",
		"@media (prefers-reduced-motion:reduce)",
		"animation-iteration-count:1!important",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("visual foundation missing %q", want)
		}
	}
}

func TestVisualFoundationPreservesThemeBoundaries(t *testing.T) {
	css, err := StylesheetForTheme(map[string]string{
		"color.brand.primary": "#7a1f5c",
		"color.brand.hover":   "#5a1241",
		"color.brand.soft":    "#f7eaf1",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"--hcm-color-brand-primary:#7a1f5c",
		"--accent:var(--hcm-color-brand-primary)",
		"--hcm-color-focus:#102238",
		`@media (forced-colors:active)`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("themed foundation missing %q", want)
		}
	}
	if !strings.HasSuffix(css, finalThemeCoverageLayer()) {
		t.Fatal("visual foundation displaced the terminal theme coverage layer")
	}
}
