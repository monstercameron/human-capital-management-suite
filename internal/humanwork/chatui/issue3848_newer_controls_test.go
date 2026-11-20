//go:build !(js && wasm)

package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// TestIssue3848_NewerControlsNameDifferentEffects: after a search result opens
// on an older page, the footer control loads the next page in place and the
// floating one skips to the last message. Their labels must not read alike.
func TestIssue3848_NewerControlsNameDifferentEffects(t *testing.T) {
	m := chat4Fixture("en-US", "sent", false)
	m.HasNewer = true
	markup := render(t, m)
	jump := regexp.MustCompile(`<button[^>]*class="jump-newest"[^>]*>`).FindString(markup)
	if jump == "" {
		t.Fatalf("no jump control: %s", markup)
	}
	if strings.Contains(jump, `aria-label="Jump to newest"`) {
		t.Fatalf("jump control keeps the ambiguous label while newer pages exist: %s", jump)
	}
	if !strings.Contains(jump, "Skip to the latest message") {
		t.Fatalf("jump control does not say it skips to the latest message: %s", jump)
	}
	if !strings.Contains(markup, "Show newer messages") {
		t.Fatal("the in-place paging control is gone")
	}
	if !strings.Contains(markup, `title="Loads the next messages here; you stay where you are"`) {
		t.Fatalf("paging control does not explain its effect: %s", markup)
	}
}

// TestIssue3848_JumpLabelUnchangedWhenFullyLoaded: with no newer page the jump
// control is the only one, and keeps its plain name.
func TestIssue3848_JumpLabelUnchangedWhenFullyLoaded(t *testing.T) {
	m := chat4Fixture("en-US", "sent", false)
	if markup := render(t, m); !strings.Contains(markup, "Jump to newest") || strings.Contains(markup, "Skip to the latest message") {
		t.Fatalf("fully loaded timeline changed the jump label: %s", markup)
	}
}
