package productui

import (
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_011(t *testing.T) {
	css := UIPolish011MotionStylesheet()
	for _, selector := range []string{".app-shell :is(.sidebar,.sidebar.collapsed)", `data-hcm-motion-preference="limited"`} {
		if !strings.Contains(css, selector) {
			t.Errorf("real motion selector %q is missing", selector)
		}
	}
}

func TestTodo_UIPOLISH_011_Accessibility(t *testing.T) {
	css := UIPolish011MotionStylesheet()
	if !strings.Contains(css, `@media (max-width:760px){:root[data-hcm-motion-preference="limited"] .sidebar,:root[data-hcm-motion-preference="limited"] .sidebar.collapsed{transition:none!important;}}`) {
		t.Fatal("limited motion does not remove logical drawer travel")
	}
	if !strings.Contains(css, `data-hcm-motion-preference="reduce"`) || !strings.Contains(css, "prefers-reduced-motion:reduce") || !strings.Contains(css, "transition:none!important") {
		t.Fatal("reduced motion does not win the late drawer cascade")
	}
	if strings.Contains(css, ".popover-root[open]>.popover-surface") || strings.Contains(css, ".network-stage-refreshing") {
		t.Fatal("new motion layer duplicated existing popover or async preference contracts")
	}
}

func TestTodo_UIPOLISH_011_RTL(t *testing.T) {
	css := UIPolish011MotionStylesheet()
	if !strings.Contains(css, `@media (max-width:760px){.app-shell :is(.sidebar,.sidebar.collapsed){transition:inset-inline-start`) {
		t.Fatal("drawer transition is not attached to the logical inline axis")
	}
	for _, physical := range []string{"left:", "right:", "translateX("} {
		if strings.Contains(css, physical) {
			t.Errorf("motion contract uses physical RTL-sensitive property %q", physical)
		}
	}
}

func TestTodo_UIPOLISH_011_Performance(t *testing.T) {
	css := UIPolish011MotionStylesheet()
	if strings.Contains(css, "will-change:") {
		t.Fatal("motion layer must not force permanent compositing")
	}
	if strings.Contains(css, "animation-iteration-count:infinite") {
		t.Fatal("motion layer must not add an unbounded animation")
	}
}

func TestTodo_UIPOLISH_011_Regression(t *testing.T) {
	if first, second := UIPolish011MotionStylesheet(), UIPolish011MotionStylesheet(); first != second {
		t.Fatal("motion stylesheet is not deterministic")
	}
	if !strings.Contains(Stylesheet(), UIPolish011MotionStylesheet()) {
		t.Fatal("production stylesheet omitted the reviewed motion layer")
	}
}
