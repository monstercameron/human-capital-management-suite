package productui

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_CHATUX_037 renders the Agents regions in their loading state in the
// three languages and fails on any loading sentence outside the status element.
func TestTodo_CHATUX_037(t *testing.T) {
	bare := regexp.MustCompile(`(?i)loading|please wait|wird geladen|werden geladen|جار[ٍ]? تحميل`)
	for _, tag := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(tag)
		nodes := map[string]ui.Node{
			"operations": AgentControlsMount(locale),
			"cards":      AgentLoadingFrame(AgentLoadingProps{Locale: locale, Shape: AgentLoadingCards, Status: "Loading agent setup", RetryRaw: map[string]any{"data-persona-admin-retry": "page"}}),
			"table":      AgentLoadingFrame(AgentLoadingProps{Locale: locale, Shape: AgentLoadingTable, Status: "Loading costs"}),
		}
		for name, node := range nodes {
			markup, err := ui.RenderToString(node)
			if err != nil {
				t.Fatal(err)
			}
			visible := html.UnescapeString(markup)
			visible = regexp.MustCompile(`(?s)<span class="sr-only"[^>]*>.*?</span>`).ReplaceAllString(visible, " ")
			visible = strings.Join(strings.Fields(regexp.MustCompile(`(?s)<[^>]*>`).ReplaceAllString(visible, " ")), " ")
			if found := bare.FindString(visible); found != "" {
				t.Errorf("%s %s: a loading sentence is printed: %q", tag, name, visible)
			}
			for _, want := range []string{"chatux037-loading", `aria-busy="true"`, "chatux037-slow", AgentLoadingText(locale, "slow"), `role="status"`} {
				if !strings.Contains(markup, want) {
					t.Errorf("%s %s: missing %q", tag, name, want)
				}
			}
		}
		if ops, _ := ui.RenderToString(AgentControlsMount(locale)); !strings.Contains(ops, "data-owner-refresh") || !strings.Contains(ops, AgentLoadingText(locale, "retry")) {
			t.Errorf("%s: Operations has no Try again", tag)
		}
	}
	if !strings.Contains(AgentLoadingStyles, "8s forwards") {
		t.Error("the eight-second reveal is missing")
	}
	if !strings.Contains(agentUX073Stylesheet(), "chatux037-reveal") {
		t.Error("the Agents stylesheet does not carry the loading styles")
	}
}
