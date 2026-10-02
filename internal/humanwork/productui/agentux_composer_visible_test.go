package productui

import (
	"regexp"
	"strings"
	"testing"
)

// An empty textarea matches the CSS :empty pseudo-class. A rule that hides
// every empty child of the composer therefore hides the question field
// itself, which left the Agents page with no way to type a question.
func TestAgentsComposerQuestionFieldIsNotHiddenWhenEmpty(t *testing.T) {
	css := agentsStylesheet()
	hidesEmpty := regexp.MustCompile(`([^{}]*:empty[^{}]*)\{[^{}]*display\s*:\s*none`)
	found := false
	for _, match := range hidesEmpty.FindAllStringSubmatch(css, -1) {
		for _, selector := range strings.Split(match[1], ",") {
			selector = strings.TrimSpace(selector)
			if !strings.Contains(selector, ":empty") || !strings.Contains(selector, ".agents-composer") {
				continue
			}
			found = true
			for _, control := range []string{":not(textarea)", ":not(input)", ":not(select)"} {
				if !strings.Contains(selector, control) {
					t.Fatalf("composer rule %q hides empty children without excluding form controls (%s)", selector, control)
				}
			}
		}
	}
	if !found {
		t.Fatalf("expected the composer's empty-child rule in the Agents stylesheet")
	}
	if !strings.Contains(css, ".agents-composer textarea") {
		t.Fatalf("composer textarea has no layout rule: %s", css)
	}
}
