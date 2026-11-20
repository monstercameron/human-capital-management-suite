package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// Issue 3781: the classification-backed detectors are offered in the sensitive
// data editor, saved as detector rules the filter engine accepts, and named in
// plain words in every language.
func TestIssue3781_EditorOffersClassificationDetectors(t *testing.T) {
	for _, name := range chatfilter.DLPDetectorNames {
		if !modContains(ModDetectors, name) {
			t.Fatalf("%s is not offered in the editor", name)
		}
		form := ModForm{Name: "Pay talk", Kind: ModKindSensitive, Detector: name, Action: "block", Scope: "channel", Channel: "room"}
		d, problem := ModDefinitionFromForm(form, modTestEntropy(1))
		if problem != nil {
			t.Fatalf("%s: %v", name, problem)
		}
		if _, err := chatfilter.NewRegistry().Compile([]chatfilter.Definition{d}); err != nil {
			t.Fatalf("%s does not compile: %v", name, err)
		}
		for _, loc := range []string{"en-US", "de-DE", "ar"} {
			text := modadminText(Model{Locale: loc}, "d_"+name)
			if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "dlp-") {
				t.Fatalf("%s has no %s label: %q", name, loc, text)
			}
		}
	}
}

// Issue 3781 (review): a saved classification filter reads in words in the
// filters list, never as a blank line, in every language.
func TestIssue3781_FilterListLineNamesTheClass(t *testing.T) {
	for _, name := range chatfilter.DLPDetectorNames {
		d := chatfilter.Definition{ID: "pay-ab12cd", Name: "Pay talk", Version: "1.0.0", Kind: "detector", Action: "block", Match: []string{name}}
		for _, loc := range []string{"en-US", "de-DE", "ar"} {
			line := modDescribe(Model{Locale: loc}, d)
			if line == "" || strings.Contains(line, "{what}") || strings.Contains(line, "⟦") {
				t.Fatalf("%s %s: the listing line is %q", name, loc, line)
			}
		}
	}
}
