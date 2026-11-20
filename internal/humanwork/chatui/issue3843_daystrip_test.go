//go:build !(js && wasm)

package chatui

import (
	"strings"
	"testing"
)

// TestIssue3843_DayPillHasItsOwnOpaqueStrip: the sticky day pill floated over the
// author and time of the message scrolled to the top of the list, and the message
// showed around it. The pill's row is now a full-width strip painted in the list's
// own colour, so a message passing under it is clipped cleanly at the strip's
// edge and the pill never sits on top of a visible name or time. The strip also
// paints over the list's header fade above it, and it takes the pointer, so
// nothing hidden under it can be clicked.
func TestIssue3843_DayPillHasItsOwnOpaqueStrip(t *testing.T) {
	const strip = ".chat-workspace .message-list .day-divider"
	if got := chatbugCascadeValue(Stylesheet, strip, "background"); got != "var(--surface)" {
		t.Fatalf("the day pill's row is %q, want an opaque strip in the list colour (--surface, the chat column background)", got)
	}
	// The strip must be the colour the list is really drawn on, or it shows as a
	// differently coloured band (--canvas is the page colour, not the column's).
	if main := chatbugCascadeValue(Stylesheet, ".chat-main", "background"); main != chatbugCascadeValue(Stylesheet, strip, "background") {
		t.Fatalf("the strip is %q but the message column is drawn on %q", chatbugCascadeValue(Stylesheet, strip, "background"), main)
	}
	if got := chatbugCascadeValue(Stylesheet, strip, "pointer-events"); got == "none" {
		t.Fatal("the strip lets clicks through to messages hidden under it")
	}
	if got := chatbugCascadeValue(Stylesheet, strip, "box-shadow"); !strings.Contains(got, "var(--surface)") || !strings.HasPrefix(got, "0 -") {
		t.Fatalf("the strip does not cover the header fade above it: box-shadow=%q", got)
	}
	// The shadows reach past the strip's sides (the list's padding is 20px, 12px on
	// a phone) so no message edge shows beside it.
	shadow := chatbugCascadeValue(Stylesheet, strip, "box-shadow")
	for _, want := range []string{"-24px 0 0 0 var(--surface)", "24px 0 0 0 var(--surface)"} {
		if !strings.Contains(shadow, want) {
			t.Fatalf("the strip does not reach the list's side edges: box-shadow=%q lacks %q", shadow, want)
		}
	}
	// Right-to-left: only logical properties are used for the strip's sides.
	for _, physical := range []string{"margin-left", "margin-right", "padding-left", "padding-right"} {
		if got := chatbugCascadeValue(Stylesheet, strip, physical); got != "" {
			t.Fatalf("the strip uses %s=%q, which breaks in right-to-left", physical, got)
		}
	}
}
