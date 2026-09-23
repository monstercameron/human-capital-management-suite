package chatui

import (
	"strings"
	"testing"
)

func TestChatMotionUsesTokensAndHonorsMotionPreferences(t *testing.T) {
	css := ScopedStylesheet()
	for _, want := range []string{
		"var(--hcm-motion-fast) var(--hcm-motion-easing)",
		"var(--hcm-motion-normal) var(--hcm-motion-easing)",
		".chat-details.collapsed{opacity:0;transform:translateX(var(--hcm-space-2))}",
		"@starting-style",
		`:scope[data-sidebar-open="true"] .chat-rail{transform:translateX(calc(0px - var(--hcm-space-2)))}`,
		"@keyframes chat-popover-in",
		"@media(prefers-reduced-motion:reduce)",
		`data-hcm-motion-preference="reduce"`,
		`data-hcm-motion-preference="limited"`,
		`:scope :is(.rail-row-menu,.message-menu,.reaction-picker,.emoji-picker:not([hidden]),.giphy-picker:not([hidden])){animation:chat-popover-in`,
		`@keyframes chat-popover-in{from{opacity:0;transform:translateY(var(--hcm-space-1)) scale(.99);background-color:var(--surface);border-color:var(--line);box-shadow:var(--hcm-shadow-raised)}to{opacity:1;transform:translateY(0) scale(1);background-color:var(--surface);border-color:var(--line);box-shadow:var(--hcm-shadow-raised)}}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("chat motion stylesheet missing %q", want)
		}
	}
	if strings.Contains(css, "animation:chat-popover-in") && !strings.Contains(css, "animation:none;transform:none") {
		t.Fatal("chat motion has no reduced-motion override")
	}
}
