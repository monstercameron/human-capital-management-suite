//go:build !(js && wasm)

package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// TestIssue3987_JumpToNewestIsNotHiddenByAMountedTarget: a pinned or searched
// message that is still marked as the target must not hide the shortcut once the
// reader has scrolled away from the newest messages.
func TestIssue3987_JumpToNewestIsNotHiddenByAMountedTarget(t *testing.T) {
	for _, rule := range regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`).FindAllStringSubmatch(Stylesheet, -1) {
		selector := rule[1]
		if strings.Contains(selector, "search-target") && strings.Contains(selector, "jump-newest") {
			t.Fatalf("a mounted target hides the Jump to newest shortcut: %s{%s}", strings.TrimSpace(selector), rule[2])
		}
	}
	if got := chatbugCascadeValue(Stylesheet, ".timeline-frame.away .jump-newest", "pointer-events"); got != "auto" {
		t.Fatalf("the away timeline does not make the shortcut clickable: %q", got)
	}
}

// TestIssue3987_TargetStillMarkedAndShortcutStillRendered: the pin keeps its
// highlight, and the shortcut is in the markup with its accessible name in every
// language, whether or not a target is mounted.
func TestIssue3987_TargetStillMarkedAndShortcutStillRendered(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "sent", false)
		m.FocusMessageID = m.Messages[0].ID
		markup := render(t, m)
		if !strings.Contains(markup, "search-target") {
			t.Fatalf("%s: the target lost its highlight", locale)
		}
		if !regexp.MustCompile(`<button[^>]*aria-label="[^"]+"[^>]*class="jump-newest"`).MatchString(markup) {
			t.Fatalf("%s: the shortcut has no accessible name: %s", locale, markup)
		}
	}
}
