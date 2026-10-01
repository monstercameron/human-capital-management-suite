package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderJourneyActionsR(t *testing.T, availability JourneyCapabilityAvailability) string {
	t.Helper()
	markup, err := ui.RenderToString(ProjectJourneyActions("en-US", "Ana Silva", availability))
	if err != nil {
		t.Fatalf("render journey actions: %v", err)
	}
	return markup
}

func TestTodo_UXBLIND_058(t *testing.T) {
	markup := renderJourneyActionsR(t, JourneyCapabilityAvailability{})
	if strings.Contains(markup, "Send to chat") || strings.Contains(markup, "Link to ticket") {
		t.Fatalf("unpublished share targets were rendered: %s", markup)
	}
	if strings.Contains(markup, "projectui-action") {
		t.Fatalf("an unavailable project action left an interactive host hook: %s", markup)
	}

	available := renderJourneyActionsR(t, JourneyCapabilityAvailability{Chat: true, Projects: true})
	for _, want := range []string{"Send to chat", "Link to ticket"} {
		if !strings.Contains(available, want) {
			t.Errorf("published target %q was not rendered: %s", want, available)
		}
	}
}

func TestTodo_UXBLIND_058_Browser(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		markup, err := ui.RenderToString(ProjectJourneyActions(locale, "Ana Silva", JourneyCapabilityAvailability{Projects: true}))
		if err != nil {
			t.Fatalf("%s render journey actions: %v", locale, err)
		}
		if strings.Contains(markup, `data-projectui-action="share-chat"`) {
			t.Fatalf("%s rendered the unavailable chat target: %s", locale, markup)
		}
		if !strings.Contains(markup, "projectui-action=\"open-ticket-linker\"") {
			t.Fatalf("%s did not retain the published ticket target: %s", locale, markup)
		}
	}
}
