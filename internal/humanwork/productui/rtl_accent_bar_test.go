package productui

import (
	"strings"
	"testing"
)

// TestEveryLeadingAccentBarIsMirrored: the shell marks a selected or current
// item with a 3px accent bar on its leading edge, drawn as an inset shadow.
// A shadow's offset is physical, so each bar needs a [dir=rtl] rule that moves
// it to the right; without one, Arabic readers see the bar on the far edge of
// the row. Every selector drawing the bar must have a mirror.
func TestEveryLeadingAccentBarIsMirrored(t *testing.T) {
	css := Stylesheet()
	mirrored := map[string]bool{}
	var drawn []string
	for chunk := range strings.SplitSeq(css, "}") {
		open := strings.Index(chunk, "{")
		if open < 0 || strings.Contains(chunk[:open], "@") {
			continue
		}
		body := chunk[open+1:]
		for _, selector := range splitSelectorList(chunk[:open]) {
			selector = strings.TrimSpace(selector)
			switch {
			case strings.Contains(body, "inset -3px 0 0 0 var(--accent)") && strings.HasPrefix(selector, "[dir=rtl] "):
				mirrored[strings.TrimPrefix(selector, "[dir=rtl] ")] = true
			case strings.Contains(body, "inset 3px 0 0 0 var(--accent)") && !strings.HasPrefix(selector, "[dir=rtl]"):
				drawn = append(drawn, selector)
			}
		}
	}
	if len(drawn) == 0 {
		t.Fatal("found no leading accent bars; the scan no longer matches how the sheet draws them")
	}
	for _, selector := range drawn {
		if !mirrored[selector] {
			t.Errorf("%s draws a leading accent bar with no [dir=rtl] mirror", selector)
		}
	}
}

// TestTextAlignmentIsLogical: text-align:left/right are physical and do not
// mirror, so in Arabic every table header and cell sat on the wrong side of
// its column. Alignment is written start/end.
func TestTextAlignmentIsLogical(t *testing.T) {
	css := Stylesheet()
	for _, physical := range []string{"text-align:left", "text-align:right"} {
		if at := strings.Index(css, physical); at >= 0 {
			from := strings.LastIndex(css[:at], "}") + 1
			t.Errorf("physical %s in %.120s", physical, css[from:at])
		}
	}
}
