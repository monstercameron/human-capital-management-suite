package chatui

import (
	"strings"
	"testing"
)

// Issue 3766: the Saved empty state must not tell a touch user to hover.
func TestIssue3766_SavedEmptyStateIsInputNeutral(t *testing.T) {
	banned := map[string][]string{
		"en-US": {"hover"},
		"de-DE": {"fahren sie über"},
		"ar":    {"مرّر المؤشر"},
	}
	for locale, words := range banned {
		c := SavedMessagesCopy(locale)
		for name, text := range map[string]string{"todo": c.EmptyTodo, "all": c.EmptyAll} {
			if strings.TrimSpace(text) == "" {
				t.Fatalf("%s %s empty copy is blank", locale, name)
			}
			for _, w := range words {
				if strings.Contains(strings.ToLower(text), w) {
					t.Fatalf("%s %s empty copy %q still tells people to hover", locale, name, text)
				}
			}
		}
	}
	if got := SavedMessagesCopy("en-US").EmptyTodo; got != "Use the bookmark on a message to keep it here." {
		t.Fatalf("en-US empty copy = %q", got)
	}
	out := renderNode(t, RenderSavedMessages(SavedMessagesView{Locale: "en-US", Tab: "todo"}))
	if strings.Contains(strings.ToLower(out), "hover") || !strings.Contains(out, "Use the bookmark on a message") {
		t.Fatalf("rendered empty Saved page: %s", out)
	}
}
