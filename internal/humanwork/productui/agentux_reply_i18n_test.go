package productui

import (
	"strings"
	"testing"
)

func TestTodo_AGENTUX_027_AgentReplyCatalogIsComplete(t *testing.T) {
	keys := []string{
		"chat.agent.badge", "chat.agent.name", "chat.agent.response_region", "chat.agent.working", "chat.agent.elapsed_seconds", "chat.agent.only_visible", "chat.agent.open_conversation", "chat.agent.open_original", "chat.agent.sources", "chat.agent.try_again",
		"chat.agent.failed_model_unavailable", "chat.agent.failed_no_permission", "chat.agent.failed_nothing_found", "chat.agent.failed_timeout", "chat.agent.failed_stopped", "chat.agent.failed_generic",
	}
	for _, locale := range SupportedProductLocales() {
		catalog := ResolveProductLocale(locale)
		for _, key := range keys {
			text := catalog.Text(key)
			if strings.TrimSpace(text) == "" || strings.Contains(text, key) || strings.Contains(text, "⟦") {
				t.Fatalf("%s key %s = %q", locale, key, text)
			}
		}
	}
}
