package chatui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// #3771: with the product navigation collapsed to its icon rail (Chat's
// default) and a side panel open, the message column keeps a comfortable
// reading width at 1280 and 1440. The widths come from the served stylesheet,
// taking the widest conversation list and side panel it ever declares so the
// result is the worst case.
func TestIssue3771_MessageColumnKeepsReadingWidthWithAPanelOpen(t *testing.T) {
	const navRail = 72 // the icon rail plus its gutter, in px
	const comfortable = 600
	largest := func(pattern string) int {
		t.Helper()
		matches := regexp.MustCompile(pattern).FindAllStringSubmatch(Stylesheet, -1)
		if len(matches) == 0 {
			t.Fatalf("stylesheet does not match %q", pattern)
		}
		best := 0
		for _, match := range matches {
			if value, _ := strconv.Atoi(match[1]); value > best {
				best = value
			}
		}
		return best
	}
	rail := largest(`--chat-rail:(\d+)px`)
	narrowRail := largest(`@container chat \(max-width:1350px\)\{[^@]*?minmax\(200px,min\(var\(--chat-rail\),(\d+)px\)\)`)
	details := largest(`--chat-details:(\d+)px`)
	narrowDetails := largest(`@container chat \(max-width:1350px\)\{[^@]*?minmax\(280px,(\d+)px\)`)
	if !strings.Contains(Stylesheet, `.chat-workspace{container:chat/inline-size}`) {
		t.Fatal("the chat workspace is no longer the container the narrower grid reads")
	}
	for _, c := range []struct{ viewport, rail, details int }{{1440, rail, details}, {1280, narrowRail, narrowDetails}} {
		if message := c.viewport - navRail - c.rail - c.details; message < comfortable {
			t.Errorf("at %dpx with a side panel the message column is %dpx (list %d, panel %d), below %dpx", c.viewport, message, c.rail, c.details, comfortable)
		}
	}
}
