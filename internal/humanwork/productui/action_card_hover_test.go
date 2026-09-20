package productui

import (
	"strings"
	"testing"
)

// TestActionCardsTakeNoHoverAccent: on a journey detail the first action
// card showed an accent border only because the pointer rested on it. Action
// cards are static forms; the hover accent stays on cards that open
// something.
func TestActionCardsTakeNoHoverAccent(t *testing.T) {
	css := Stylesheet()
	if strings.Contains(css, ".jn-embedded .jn-card:hover{") {
		t.Fatal("every embedded journey card, including static action forms, still takes the hover accent")
	}
	if !strings.Contains(css, ".jn-embedded .jn-card:not(.jn-action):hover{") {
		t.Fatal("the hover accent for cards that open something was removed")
	}
}
