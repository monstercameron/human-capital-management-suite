package chatui

import (
	"strings"
	"testing"
)

func TestIssue900004_RepairIsInTheStylesheet(t *testing.T) {
	if !strings.Contains(Stylesheet, Issue900004Styles) {
		t.Fatal("the phone sheet repair is not part of the Chat stylesheet")
	}
}

// TestIssue900004_ThreadBarSizesToItsButtons: the bar that holds a reply's Save
// and More buttons is not pinned to the 24px height of the desktop bar, so the
// 44px buttons stay inside the reply and cannot sit on the next author row.
func TestIssue900004_ThreadBarSizesToItsButtons(t *testing.T) {
	// The premise of the defect: an earlier rule pins every message bar to 24px.
	if !strings.Contains(AgentUXChat5Styles, ".message-actions{height:24px;") {
		t.Skip("the 24px bar rule is gone; the overlap cannot come back from it")
	}
	want := `.chat-workspace .thread-pane .thread-message-actions{height:auto;min-block-size:44px;`
	if !strings.Contains(Issue900004Styles, want) {
		t.Fatalf("thread bar is not released from the fixed bar height: missing %q", want)
	}
	if !strings.Contains(Issue900004Styles, `.thread-message-body,.chat-workspace .thread-pane .thread-root-body{display:flow-root}`) {
		t.Error("a reply and the root must contain their bar")
	}
	chat5 := strings.Index(Stylesheet, ".message-actions{height:24px;")
	fix := strings.Index(Stylesheet, want)
	if chat5 < 0 || fix < chat5 {
		t.Errorf("the repair (at %d) must follow the fixed-height rule (at %d)", fix, chat5)
	}
	if !strings.HasPrefix(Issue900004Styles, "@media(max-width:767px){") {
		t.Error("the repair must apply below 768px only")
	}
}

// TestIssue900004_RootHeaderWrapsInsteadOfSqueezingTheName: name, date and View
// in channel share a wrapping row; none is absolutely positioned and the name
// is never given a column narrower than itself.
func TestIssue900004_RootHeaderWrapsInsteadOfSqueezingTheName(t *testing.T) {
	for _, want := range []string{
		`.thread-message-body .message-meta{display:flex;flex-wrap:wrap;`,
		`.message-meta .message-author{flex:0 1 auto;min-inline-size:0;`,
		`.thread-view-in-channel{position:static;flex:0 0 auto;margin:0;white-space:nowrap}`,
	} {
		if !strings.Contains(Issue900004Styles, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, physical := range []string{"-left", "-right", "left:", "right:"} {
		if strings.Contains(Issue900004Styles, physical) {
			t.Errorf("physical property %q breaks right-to-left", physical)
		}
	}
}

// TestIssue900004_PreviewSettingsScrollAboveStickyActions: the settings area has
// a floor, can be swiped, and the Post and Cancel row stays outside it.
func TestIssue900004_PreviewSettingsScrollAboveStickyActions(t *testing.T) {
	for _, want := range []string{
		`.chatcmd003-preview>.chatcmd003-body{min-block-size:min(9rem,32dvh);`,
		`touch-action:pan-y`,
		`.chatcmd003-preview>.chatcmd003-actions{padding-block-end:max(12px,env(safe-area-inset-bottom))}`,
		`.chat-workspace .chatcmd003-actions .button{min-block-size:44px;min-inline-size:44px}`,
		`has(.chatcmd003-preview){max-block-size:min(94dvh,calc(100dvh - 56px))}`,
	} {
		if !strings.Contains(Issue900004Styles, want) {
			t.Errorf("missing %q", want)
		}
	}
	// The body scrolls and the actions row does not: the structural rules stay.
	if !strings.Contains(chatcmd002PreviewStyles, `.chatcmd003-body{display:grid;align-content:start;gap:8px;flex:1 1 auto;min-height:0;`) ||
		!strings.Contains(chatcmd002PreviewStyles, `overflow-y:auto`) ||
		!strings.Contains(chatcmd002PreviewStyles, `.chatcmd003-actions{position:relative;z-index:1;flex:none;`) {
		t.Error("the preview no longer scrolls its settings above a fixed actions row")
	}
}

// TestIssue900004_ReplyBarSitsInsideItsReplyBody: the bar is a child of the
// reply's body, so the flow-root containment above holds it, in every language.
func TestIssue900004_ReplyBarSitsInsideItsReplyBody(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := issue3894Model(locale)
		out := renderNode(t, threadPane(m, handlers{}))
		for _, body := range []string{"thread-root-body", "thread-message-body"} {
			start := strings.Index(out, body)
			if start < 0 {
				t.Fatalf("%s: no %s rendered", locale, body)
			}
			if bar := strings.Index(out[start:], "thread-message-actions"); bar < 0 {
				t.Errorf("%s: no bar after %s", locale, body)
			}
		}
	}
}

// TestIssue900004_RepairFollowsTheRulesItOverrides: every rule of the repair ties
// or beats the rule it overrides on specificity only by coming later, so the
// repair must sit after the rules it overrides in the assembled stylesheet: the
// fixed-height bar rule, the thread bar sizing, the root header grid, and the
// preview's body and composer-height rules.
func TestIssue900004_RepairFollowsTheRulesItOverrides(t *testing.T) {
	fix := strings.Index(Stylesheet, Issue900004Styles)
	if fix < 0 {
		t.Fatal("the repair is not in the stylesheet")
	}
	for _, earlier := range []string{
		".message-actions{height:24px;",
		".chat-workspace .thread-pane .thread-message-actions .message-action,.chat-workspace .thread-pane .thread-message-actions .thread-more{display:inline-flex",
		"@media(max-width:760px){.thread-root-body .message-meta,.thread-message-body .message-meta{display:grid",
		".chatcmd003-preview>.chatcmd003-body{display:grid",
		"form.chat-composer.chat-composer:has(.chatcmd003-preview){display:flex",
	} {
		at := strings.Index(Stylesheet, earlier)
		if at < 0 {
			t.Errorf("rule the repair overrides is gone or changed: %q", earlier)
			continue
		}
		if at > fix {
			t.Errorf("%q (at %d) comes after the repair (at %d) and would win the cascade", earlier, at, fix)
		}
	}
}
