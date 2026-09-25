package productui

import (
	"strings"
	"testing"
)

// TestNavigationHeightStepsReachServedStylesheet guards S-5: the full admin
// rail (Projects included) only fits a 1440x900 or 1180x780 sidebar because
// top-level rows tighten at max-height 960px and 820px. The rules must reach
// the served sheet with their :is() selector list intact, and the coarse
// pointer 44px floor must be declared after them so it wins the tie.
func TestNavigationHeightStepsReachServedStylesheet(t *testing.T) {
	css := Stylesheet()
	const rows = `.sidebar :is(.primary-nav>ul>li>.nav-link,.primary-nav>ul>li>.nav-group>.nav-group-summary,.nav-bottom .nav-link)`
	step960 := `@media (min-width:761px) and (max-height:960px){` + rows + `{margin-block:1px;min-height:40px;}}`
	step820 := `@media (min-width:761px) and (max-height:820px){` + rows + `{min-height:38px;padding-block:9px;}}`
	coarse := `@media (pointer:coarse){` + rows + `{min-height:44px;}}`
	at := map[string]int{}
	for name, want := range map[string]string{"960": step960, "820": step820, "coarse": coarse} {
		i := strings.Index(css, want)
		if i < 0 {
			t.Fatalf("served stylesheet is missing the %s nav height step %q", name, want)
		}
		at[name] = i
	}
	if at["960"] > at["820"] || at["820"] > at["coarse"] {
		t.Fatalf("nav height steps out of cascade order: 960@%d 820@%d coarse@%d", at["960"], at["820"], at["coarse"])
	}
	if !strings.Contains(css, `@media (min-width:761px) and (max-height:960px){.primary-nav>ul{padding-block-end:8px;}}`) {
		t.Fatal("served stylesheet is missing the short-viewport nav list end padding")
	}
}
