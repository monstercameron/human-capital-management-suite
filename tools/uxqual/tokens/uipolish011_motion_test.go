package tokens_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/tokens"
)

func TestTodo_UIPOLISH_011(t *testing.T) {
	css := tokens.MotionCSS()
	for _, token := range []string{"--motion-duration-instant:", "--motion-duration-fast:", "--motion-duration-normal:", "--motion-duration-slow:", "--motion-easing-standard:", "--motion-distance-drawer:"} {
		if !strings.Contains(css, token) {
			t.Errorf("motion token %q is missing", token)
		}
	}
	for _, surface := range []string{`data-motion="navigation"`, `data-motion="drawer"`, `data-motion="popover"`, `data-motion="list"`, `data-motion="async"`} {
		if !strings.Contains(css, surface) {
			t.Errorf("motion surface %q is missing", surface)
		}
	}
}

func TestTodo_UIPOLISH_011_CSSContract(t *testing.T) {
	css := tokens.MotionCSS()
	for _, state := range []string{`data-motion-state="closed"`, `data-motion-state="open"`} {
		if !strings.Contains(css, state) {
			t.Errorf("motion state %q is missing", state)
		}
	}
	if !strings.Contains(css, "transition:transform var(--motion-duration-normal)") {
		t.Fatal("base motion does not declare an interruption-safe transition")
	}
}

func TestTodo_UIPOLISH_011_Accessibility(t *testing.T) {
	css := tokens.MotionCSS()
	if !strings.Contains(css, `data-hcm-motion-preference="limited"`) || !strings.Contains(css, "transition:opacity var(--motion-duration-fast)") || !strings.Contains(css, "transform:none!important") {
		t.Fatal("limited motion does not remove spatial movement while retaining brief feedback")
	}
	if !strings.Contains(css, `data-hcm-motion-preference="reduce"`) || !strings.Contains(css, "transition:none") || !strings.Contains(css, "transform:none!important") {
		t.Fatal("reduce motion does not remove animation and spatial movement")
	}
}

func TestTodo_UIPOLISH_011_SystemReducedMotionRemovesSpatialMovement(t *testing.T) {
	css := tokens.MotionCSS()
	const media = "@media (prefers-reduced-motion:reduce)"
	start := strings.Index(css, media)
	if start < 0 {
		t.Fatalf("system reduced-motion media query is missing")
	}
	mediaCSS := css[start:]
	if !strings.Contains(mediaCSS, `:where([data-motion]){transform:none!important;transition:none;}`) {
		t.Fatalf("system reduced-motion media query does not remove spatial motion: %s", mediaCSS)
	}
}

func TestTodo_UIPOLISH_011_RTL(t *testing.T) {
	css := tokens.MotionCSS()
	if strings.Contains(css, `data-motion="navigation"][data-motion-state="closed"]{transform:`) || strings.Contains(css, `data-motion="drawer"][data-motion-state="closed"]{transform:`) {
		t.Fatal("navigation and drawer motion must not encode a physical direction")
	}
}

func TestTodo_UIPOLISH_011_CSSBudget(t *testing.T) {
	css := tokens.MotionCSS()
	if strings.Contains(css, "animation-iteration-count:infinite") {
		t.Fatal("motion contract permits unbounded animation")
	}

	baseCSS := css
	if mediaStart := strings.Index(baseCSS, "@media (prefers-reduced-motion:reduce)"); mediaStart >= 0 {
		baseCSS = baseCSS[:mediaStart]
	}
	durations := regexp.MustCompile(`--motion-duration-(?:fast|normal|slow):([0-9]+(?:\.[0-9]+)?)(ms|s);`).FindAllStringSubmatch(baseCSS, -1)
	if len(durations) != 3 {
		t.Fatalf("motion contract must declare fast, normal, and slow durations; found %d", len(durations))
	}
	for _, match := range durations {
		value, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			t.Fatalf("duration %q is not numeric: %v", match[0], err)
		}
		if match[2] == "s" {
			value *= 1000
		}
		if value > 280 {
			t.Fatalf("duration %q exceeds the 280ms motion budget", match[0])
		}
	}
}

func TestTodo_UIPOLISH_011_Regression(t *testing.T) {
	first, second := tokens.MotionCSS(), tokens.MotionCSS()
	if first != second {
		t.Fatal("motion stylesheet is not deterministic across calls")
	}
}
