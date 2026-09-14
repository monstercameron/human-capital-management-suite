package productui

import (
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_001(t *testing.T) {
	css := Stylesheet()
	for _, role := range []string{"display", "page-title", "section", "body", "label", "helper", "table", "code"} {
		if !strings.Contains(css, "--hcm-type-"+role) {
			t.Fatalf("stylesheet missing semantic type role %q", role)
		}
	}
}

func TestTodo_UIPOLISH_001_Golden(t *testing.T) {
	css := uipolish001TypographyStylesheet()
	for _, fragment := range []string{"font-size:var(--hcm-type-page-title)", "font-size:var(--hcm-type-section)", "font-family:var(--hcm-font-mono)", "max-inline-size:var(--hcm-measure-prose)", "text-wrap:balance"} {
		if !strings.Contains(css, fragment) {
			t.Errorf("typography contract missing %q", fragment)
		}
	}
}

func TestTodo_UIPOLISH_001_Regression(t *testing.T) {
	css := Stylesheet()
	for _, selector := range []string{".page-head h1", "section", ".table-scroll", "pre", ".field .error"} {
		if !strings.Contains(css, selector) {
			t.Errorf("cross-page stylesheet contract lost selector %q", selector)
		}
	}
	if !strings.Contains(uipolish001TypographyStylesheet(), "text-transform:none") {
		t.Fatal("typography contract does not neutralize competing all-caps metadata")
	}
}

func TestTodo_UIPOLISH_001_Accessibility(t *testing.T) {
	css := uipolish001TypographyStylesheet()
	for _, fragment := range []string{"overflow-wrap:anywhere", "max-inline-size:100%", "min-inline-size:0"} {
		if !strings.Contains(css, fragment) {
			t.Errorf("zoom/locale resilience missing %q", fragment)
		}
	}
	if strings.Contains(css, "body{") || strings.Contains(css, "h1,h2,h3,h4,h5,h6{") {
		t.Fatal("typography contract leaked global selectors outside product shells")
	}
	if !strings.Contains(css, ":where(.app-shell,.jn-embedded)") {
		t.Fatal("typography contract is not scoped to production shell/content roots")
	}
}

func TestTodo_UIPOLISH_001_ExpandedNavigationLabels(t *testing.T) {
	css := uipolish001TypographyStylesheet()
	selector := ":where(.app-shell) .sidebar .nav-group-summary>.nav-label{"
	start := strings.Index(css, selector)
	if start < 0 {
		t.Fatal("sidebar group labels have no scoped expansion rule")
	}
	end := strings.IndexByte(css[start:], '}')
	if end < 0 {
		t.Fatal("sidebar group label rule is malformed")
	}
	rule := css[start : start+end]
	for _, fragment := range []string{"white-space:normal", "max-inline-size:none", "overflow:visible", "min-inline-size:0"} {
		if !strings.Contains(rule, fragment) {
			t.Errorf("German/RTL navigation label can still clip: missing %q", fragment)
		}
	}
}

func TestTodo_UIPOLISH_001_FallbackAndMeasureResilience(t *testing.T) {
	css := uipolish001TypographyStylesheet()
	for _, fragment := range []string{
		"font-family:var(--hcm-font-sans)",
		"font-size-adjust:.52",
		"clamp(1.75rem,var(--hcm-font-size-heading,1.35rem),2.5rem)",
		"p,li,dd,dt,blockquote",
	} {
		if !strings.Contains(css, fragment) {
			t.Errorf("typography fallback/measure contract missing %q", fragment)
		}
	}
	if strings.Contains(css, "1.35rem + 1.5vw") {
		t.Fatal("page-title fallback contains an invalid unparenthesized sum")
	}
}
