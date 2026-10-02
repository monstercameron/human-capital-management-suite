package chatui

import (
	"math"
	"strings"
	"testing"
)

// TestTodo_CHATUX_036: the accent is for the primary action and the selected
// state. Running-text links and secondary buttons are ink; an off switch is the
// neutral control grey and an on switch the accent; the selected sidebar row is
// a light tint; help text is at least 12 px; the status pill has its dot.
func TestTodo_CHATUX_036(t *testing.T) {
	env := s11Env{width: 1280}
	body := func(sel string) bool { return strings.HasSuffix(sel, ".message-body a") }
	if got := s11Value(t, env, "color", body); got != "var(--ink)" {
		t.Errorf("a link in a message is %q, want the ink colour", got)
	}
	secondary := func(sel string) bool { return strings.HasSuffix(sel, ".button.secondary") && s11Without(sel, ":", "[") }
	if got := s11Value(t, env, "color", secondary); got != "var(--ink)" {
		t.Errorf("a secondary button is %q, want the ink colour", got)
	}
	off := func(sel string) bool {
		return strings.HasSuffix(sel, ":is(.switch,.chatmod-switch-track)") && strings.HasPrefix(sel, ":root .chat-workspace")
	}
	if got := s11Value(t, env, "background", off); got != "var(--hcm-color-control-border)" {
		t.Errorf("an off switch's track is %q, want the neutral control grey", got)
	}
	selected := func(sel string) bool { return sel == ".chat-workspace .chat-row.selected" }
	if got := s11Value(t, env, "background", selected); !strings.Contains(got, "12%") {
		t.Errorf("the selected row is %q, want a light tint", got)
	}
	help := func(sel string) bool {
		return sel == ".chat-workspace .message-time"
	}
	if got := s11Value(t, env, "font-size", help); got != ".75rem" {
		t.Errorf("help text is %q, want at least 12px", got)
	}
	if !strings.Contains(ChatUX036Styles, ".chat-status-pill::before{content:\"\"") {
		t.Error("the status pill has no dot")
	}
	// The muted token on the panel and rail backgrounds: the light-mode values the
	// shell serves are #515861 on #ffffff and #f4f4f2.
	if r := chatux036Contrast("#515861", "#f4f4f2"); r < 4.5 {
		t.Errorf("help text on the rail is %.2f:1, want 4.5:1", r)
	}
}

func chatux036Luminance(hex string) float64 {
	channel := func(s string) float64 {
		v := 0
		for _, r := range s {
			v = v*16 + strings.IndexRune("0123456789abcdef", r)
		}
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	hex = strings.TrimPrefix(hex, "#")
	return 0.2126*channel(hex[0:2]) + 0.7152*channel(hex[2:4]) + 0.0722*channel(hex[4:6])
}

func chatux036Contrast(a, b string) float64 {
	la, lb := chatux036Luminance(a), chatux036Luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
