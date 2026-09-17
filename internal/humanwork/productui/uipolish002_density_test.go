package productui

import (
	"strings"
	"testing"
)

// TestTodo_UIPOLISH_002_Regression_SideStackFollowsDensityModes pins the
// compact/spacious density contract for the home composition's inner rails.
//
// RED: the compact and spacious overrides restyle .home-grid, .workbench
// and .insights-grid, but .side-stack -- the rail that actually holds the
// cards inside .home-grid -- keeps its fixed 20px gap in every density
// mode, so dense mode crowds sibling grids while the rails beside them do
// not move, and spacious mode stretches grids while rails stay tight.
// GREEN (UIPOLISH-002): card gaps follow the density modes uniformly;
// every card composition in the group moves together.
func TestTodo_UIPOLISH_002_Regression_SideStackFollowsDensityModes(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`:root[data-hcm-density="compact"] .home-grid,:root[data-hcm-density="compact"] .side-stack,:root[data-hcm-density="compact"] .workbench,:root[data-hcm-density="compact"] .insights-grid`,
		`:root[data-hcm-density="spacious"] .home-grid,:root[data-hcm-density="spacious"] .side-stack,:root[data-hcm-density="spacious"] .workbench,:root[data-hcm-density="spacious"] .insights-grid`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("density override omits .side-stack: %q", want)
		}
	}
}
