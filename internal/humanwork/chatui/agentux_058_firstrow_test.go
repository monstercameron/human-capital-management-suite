package chatui

import (
	"strings"
	"testing"
)

// agentux058Specificity counts the class, attribute and pseudo-class parts of
// a selector: the middle number of its specificity. None of the selectors
// compared here has an id, and a type counts below any class.
func agentux058Specificity(selector string) int {
	selector = strings.ReplaceAll(selector, "::", "")
	return strings.Count(selector, ".") + strings.Count(selector, "[") + strings.Count(selector, ":")
}

// TestTodo_AGENTUX_058_FirstRow: on a desktop the bar of the first message is
// drawn inside its row, not above the list's top edge. The rule that says so
// must beat the rule that lifts every bar above its message, in the plain list
// and in the virtualised one, and the row's author line keeps room for it.
func TestTodo_AGENTUX_058_FirstRow(t *testing.T) {
	const lifted = ".chat-workspace .message-list .message .message-actions"
	firstRows := []string{
		".chat-workspace .message-list>.message:first-of-type",
		`.chat-workspace .message-list>[data-virtual-spacer="before"]+.virtual-row>.message`,
	}
	desktop := ChatBug076Styles[strings.Index(ChatBug076Styles, "@media(min-width:768px) and (pointer:fine){"):]
	for _, row := range firstRows {
		bar := row + " .message-actions"
		if top, transform := chatbugCascadeValue(Stylesheet, bar, "top"), chatbugCascadeValue(Stylesheet, bar, "transform"); top != "4px" || transform != "none" {
			t.Errorf("%s: top=%q transform=%q, want the bar 4px under the row's top edge and not lifted", bar, top, transform)
		}
		if !strings.Contains(desktop, bar) {
			t.Errorf("%s is not placed in the desktop rules of the hover bar", bar)
		}
		if got, floor := agentux058Specificity(bar), agentux058Specificity(lifted); got <= floor {
			t.Errorf("%s has specificity %d, not above the %d of the rule that lifts every bar: the later rule would win again", bar, got, floor)
		}
		meta := row + " .message-meta"
		if got := chatbugCascadeValue(Stylesheet, meta, "padding-inline-end"); got != "84px" {
			t.Errorf("%s keeps %q free in a narrow column, want 84px for the three buttons left there", meta, got)
		}
		if !strings.Contains(desktop, meta+"{padding-inline-end:196px}") && !strings.Contains(desktop, meta+",") {
			t.Errorf("%s keeps no room for the bar drawn over it", meta)
		}
		if got, floor := agentux058Specificity(meta), agentux058Specificity(".chat-workspace .message-list .message .message-meta"); got <= floor {
			t.Errorf("%s has specificity %d, not above the %d of the rule that frees the author line", meta, got, floor)
		}
	}
	if !strings.Contains(desktop, ".message .message-meta{padding-inline-end:196px}") {
		t.Fatalf("the first row's author line does not keep 196px for the whole bar: %s", desktop)
	}
	// The virtualised list really does put its first message in the row after
	// the leading spacer.
	if !strings.Contains(Stylesheet, ".virtual-spacer{") {
		t.Fatal("premise changed: the virtualised list no longer has spacers")
	}
}

// TestTodo_CHATUX_014_Hint: the thread composer's Enter hint is whole or absent,
// never cut with an ellipsis.
func TestTodo_CHATUX_014_Hint(t *testing.T) {
	const selector = ".chat-workspace .thread-composer .composer-toolbar .composer-help"
	start := strings.Index(ChatLane4Styles, "@media(min-width:600px){"+selector+"{")
	if start < 0 {
		t.Fatal("the thread hint's rule is gone")
	}
	rule := ChatLane4Styles[start:]
	rule = rule[:strings.Index(rule, "}")]
	for _, want := range []string{"display:flex", "flex-wrap:wrap", "align-content:flex-start", "block-size:1.5em", "overflow:hidden", "white-space:nowrap"} {
		if !strings.Contains(rule, want) {
			t.Errorf("the thread hint's rule misses %q, so a sentence that does not fit is not clipped whole: %s", want, rule)
		}
	}
	if strings.Contains(rule, "ellipsis") {
		t.Errorf("the thread hint is cut with an ellipsis again: %s", rule)
	}
	if !strings.Contains(ChatLane4Styles, selector+`::before{content:"";flex:none;inline-size:0;block-size:1.5em}`) {
		t.Error("the thread hint has no strut, so it does not wrap out of sight when it does not fit")
	}
	if got := chatbugCascadeValue(Stylesheet, selector, "text-overflow"); got != "clip" {
		t.Errorf("a later sheet cuts the thread hint: text-overflow=%q", got)
	}
}
