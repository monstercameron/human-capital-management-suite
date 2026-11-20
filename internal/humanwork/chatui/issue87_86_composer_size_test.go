package chatui

import (
	"strings"
	"testing"
)

// TestIssue87_EmptyDesktopComposerStaysWithinTarget pins the empty fine-pointer
// desktop composer to CHATUX-050's 56-80 px range while the formatting row
// stays available: the row is rendered (data-format-row="auto"), and only an
// empty draft hides it, in every focus state, so it comes back as soon as the
// reader types. Coarse pointer and phone widths keep their own sizing.
func TestIssue87_EmptyDesktopComposerStaysWithinTarget(t *testing.T) {
	out := composerToolsMarkup(t, composerToolsModel("en-US", false), localUI{})
	for _, want := range []string{`data-format-row="auto"`, `data-draft-empty="true"`, `composer-format-row`, `composer-format-toggle`} {
		if !strings.Contains(out, want) {
			t.Fatalf("the default empty composer lacks %q (the Aa control must stay available): %s", want, out)
		}
	}

	row := func(sel string) bool {
		return strings.HasSuffix(sel, ".composer-format-row") && strings.Contains(sel, `.chat-composer[data-format-row="auto"]`)
	}
	empty := func(sel string) bool { return row(sel) && strings.Contains(sel, `[data-draft-empty="true"]`) }
	for _, width := range []int{800, 1440} {
		env := s11Env{width: width}
		if got := s11Value(t, env, "display", empty); got != "none" {
			t.Errorf("%dpx fine pointer: the automatic formatting row of an empty draft is %q, want hidden", width, got)
		}
		// The hide rule must not be conditional on focus: a focused empty
		// composer measured 111 px in the report.
		if got := s11Value(t, env, "display", func(sel string) bool { return empty(sel) && strings.Contains(sel, ":focus") }); got != "" {
			t.Errorf("%dpx: the empty-draft hide rule depends on focus (%q), so the focused composer grows", width, got)
		}
		if got := s11Value(t, env, "display", func(sel string) bool { return row(sel) && !strings.Contains(sel, "data-draft-empty") }); got != "flex" {
			t.Errorf("%dpx: a draft with text shows the formatting row as %q, want flex", width, got)
		}
	}
	// A phone never opens the automatic row.
	if got := s11Value(t, s11Env{width: 390}, "display", func(sel string) bool {
		return strings.HasSuffix(sel, ".composer-format-row") && strings.Contains(sel, ".chat-composer") && !strings.Contains(sel, "[data-") && !strings.Contains(sel, "thread-composer") && !strings.Contains(sel, ".format-")
	}); got != "none" {
		t.Errorf("390px: the automatic formatting row is %q, want hidden", got)
	}
	// The user's own choice (Aa pressed) still opens it on an empty draft.
	if got := s11Value(t, s11Env{width: 1440}, "display", func(sel string) bool {
		return strings.HasSuffix(sel, ".composer-format-row") && strings.Contains(sel, `[data-format-row="shown"]`) && strings.Contains(sel, ".chat-composer")
	}); got != "flex" {
		t.Errorf("the pressed Aa toggle shows the row as %q, want flex", got)
	}
}

// TestIssue86_CoarsePointerToolsKeep44px proves the later CHATUX-050 desktop
// rule (30 px minimum) does not defeat the 44 px coarse-pointer target, that
// fine-pointer sizing is unchanged and that Send keeps its sizes.
func TestIssue86_CoarsePointerToolsKeep44px(t *testing.T) {
	tool := func(class string) func(string) bool {
		return func(sel string) bool {
			return strings.Contains(sel, ".chat-composer") &&
				s11Without(sel, "thread-composer", ".chatvoice-tool", ".chatmap-control", ".composer-format-row", ".format-tools") &&
				(strings.Contains(sel, ".tool-button") || strings.Contains(sel, "."+class))
		}
	}
	send := func(sel string) bool {
		return strings.HasSuffix(sel, ".send-button") && strings.Contains(sel, "form.chat-composer") && s11Without(sel, "thread-composer")
	}
	phoneSend := func(sel string) bool {
		return strings.HasSuffix(sel, ".send-button") && strings.Contains(sel, ".chat-composer") && s11Without(sel, "thread-composer")
	}
	classes := []string{"composer-add-trigger", "composer-mention-button", "emoji-trigger", "giphy-trigger", "composer-format-toggle"}
	for _, width := range []int{1440, 800, 390, 320} {
		env := s11Env{width: width, coarse: true}
		for _, class := range classes {
			if got := s11Value(t, env, "min-height|min-block-size", tool(class)); s11Atoi(got) < 44 {
				t.Errorf("%dpx coarse: .%s minimum block size is %q, want at least 44px", width, class, got)
			}
			if got := s11Value(t, env, "min-width|min-inline-size", tool(class)); s11Atoi(got) < 44 {
				t.Errorf("%dpx coarse: .%s minimum inline size is %q, want at least 44px", width, class, got)
			}
		}
		if got := s11Value(t, env, "min-height|min-block-size", phoneSend); s11Atoi(got) < 44 {
			t.Errorf("%dpx coarse: Send minimum block size is %q, want at least 44px", width, got)
		}
	}
	// The formatting row's own buttons keep the 44 px target on touch screens.
	formatTool := func(sel string) bool {
		return strings.Contains(sel, ".composer-format-row .tool-button") && strings.Contains(sel, ".chat-composer") && !strings.Contains(sel, "thread-composer")
	}
	if got := s11Value(t, s11Env{width: 1440, coarse: true}, "min-height|min-block-size", formatTool); got != "44px" {
		t.Errorf("1440px coarse: formatting row buttons are %q, want 44px", got)
	}
	// Fine pointer is unchanged: compact tools and a 32 px Send.
	fine := s11Env{width: 1440}
	for _, class := range classes {
		if got := s11Value(t, fine, "min-height|min-block-size", tool(class)); got != "30px" {
			t.Errorf("1440px fine: .%s minimum block size is %q, want the compact 30px", class, got)
		}
	}
	if got := s11Value(t, fine, "min-height|min-block-size", send); got != "32px" {
		t.Errorf("1440px fine: Send minimum block size is %q, want 32px", got)
	}
}
