package chatui

import (
	"strings"
	"testing"
)

// issue3872SendTag is the opening tag of the thread's Reply button.
func issue3872SendTag(t *testing.T, markup string) string {
	t.Helper()
	start := strings.Index(markup, `class="send-button"`)
	if start < 0 {
		t.Fatal("no reply button in the thread pane")
	}
	end := strings.Index(markup[start:], ">")
	return markup[start : start+end]
}

// TestIssue3872_ReplyStaysAvailableWhilePreviewing: a nonempty draft being
// previewed keeps Reply enabled, as the channel Send does; an empty draft and a
// whitespace-only draft keep it disabled.
func TestIssue3872_ReplyStaysAvailableWhilePreviewing(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, tc := range []struct {
			draft   string
			preview bool
			enabled bool
		}{
			{"Temporary **preview** draft", true, true},
			{"Temporary **preview** draft", false, true},
			{"", true, false},
			{"   \n", true, false},
		} {
			m := issue3894Model(locale)
			m.ThreadDrafts = map[string]string{"p1": tc.draft}
			markup := renderNode(t, threadPane(m, handlers{local: localUI{threadComposerPreview: tc.preview}}))
			tag := issue3872SendTag(t, markup)
			if got := !strings.Contains(tag, "disabled"); got != tc.enabled {
				t.Errorf("%s draft %q preview=%v: reply enabled=%v, want %v (%s)", locale, tc.draft, tc.preview, got, tc.enabled, tag)
			}
		}
	}
}
