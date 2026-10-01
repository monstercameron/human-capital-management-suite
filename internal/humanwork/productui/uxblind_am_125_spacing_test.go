package productui

import (
	"strings"
	"testing"
)

func TestTodo_UXBLIND_125_SearchCompactBoxKeepsHeaderControlsSeparated(t *testing.T) {
	css := Stylesheet()
	compact := "@media (max-width:1100px){.global-search .global-search-input{padding:0;}"
	if !strings.Contains(css, compact) {
		t.Fatalf("compact search padding override missing: %q", compact)
	}

	wide := strings.Index(css, ".global-search .global-search-input{padding-block:10px;padding-inline:48px 14px;")
	compactIndex := strings.Index(css, compact)
	if wide < 0 || compactIndex < 0 || compactIndex <= wide {
		t.Fatalf("compact override must follow the wide refinement rule (wide=%d compact=%d)", wide, compactIndex)
	}
}
