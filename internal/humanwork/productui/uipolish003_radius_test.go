package productui

import (
	"strings"
	"testing"
)

// TestTodo_UIPOLISH_003_Regression_StandardContainersUseNamedSurfaceRadius
// pins the named shape scale for every standard container.
//
// RED: sixteen standard-container rules (.surface, .global-search-panel,
// .popover-surface, .popover, .people-workspace, .settings-shell,
// .loading-panel, .people-filter, .studio-shell, four appearance panels,
// two settings contexts and two settings task cards) resolve
// border-radius through var(--hcm-radius-panel, var(--panel)) or
// var(--panel), but neither variable is defined anywhere, so every one
// computes to square corners in every browser -- overlays and cards fall
// off the named shape scale while the rest of the product uses
// --hcm-radius-control/--hcm-radius-surface/--hcm-radius-status.
// GREEN (UIPOLISH-003): standard containers select the named surface
// radius; shared primitives own shape instead of emitting dangling
// variables.
func TestTodo_UIPOLISH_003_Regression_StandardContainersUseNamedSurfaceRadius(t *testing.T) {
	css := Stylesheet()
	if strings.Contains(css, "var(--panel)") || strings.Contains(css, "var(--panel,") || strings.Contains(css, "--hcm-radius-panel") {
		t.Fatal("production CSS still references the undefined panel radius variable")
	}
	for _, want := range []string{
		`.surface{`,
		`.global-search-panel{`,
		`.popover-surface{`,
		`.popover{`,
		`.people-workspace{`,
		`.settings-shell{`,
		`.loading-panel{`,
		`.people-filter{`,
		`.studio-shell{`,
		`.appearance-preview-window{`,
		`.appearance-preview-card{`,
		`.appearance-actions-sticky{`,
		`.appearance-preview-modal{`,
		`.settings-overview-grid>.settings-context{`,
		`.settings-account-group .settings-group-content>:is(.settings-context,.settings-task-card,.settings-signout){`,
		`.settings-organization-group .settings-task-card{`,
	} {
		start := strings.Index(css, want)
		if start < 0 {
			t.Fatalf("standard container rule missing: %s", want)
		}
		block := css[start : start+strings.Index(css[start:], "}")]
		if !strings.Contains(block, "border-radius:var(--hcm-radius-surface)") {
			t.Errorf("%s does not use the named surface radius: %s}", want, block)
		}
	}
}
