package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_AGENTUX_037_RemovedPlacementsHistory: Agent setup lists the
// conversations a person removed an agent from, newest first, as "Removed by
// <name> on <date>"; a removal whose remover the viewer may not name says only
// when; nothing is drawn for an agent that was never removed.
func TestTodo_AGENTUX_037_RemovedPlacementsHistory(t *testing.T) {
	persona := PersonaAdminPersona{ID: "policy-helper", Name: "Policy Helper", Lifecycle: PersonaPublished, RemovedPlacements: []PersonaAdminRemovedPlacement{
		{ConversationID: "general", Conversation: "general", Kind: "channel", RemovedBy: "Wanda Reyes", RemovedAt: time.Date(2026, 10, 2, 14, 5, 0, 0, time.UTC), Version: "4"},
		{ConversationID: "benefits", Conversation: "benefits", Kind: "channel", RemovedAt: time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC), Version: "3"},
	}}
	render := func(locale string, persona PersonaAdminPersona) string {
		markup, err := ui.RenderToString(personaAdminPlacements(ResolveProductLocale(locale), persona, PersonaAdminSnapshot{}))
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(strings.Fields(strings.NewReplacer("<!-- -->", "").Replace(agentux037MarkupText(markup))), " ")
	}
	en := render("en-US", persona)
	if !strings.Contains(en, "Removed by Wanda Reyes on") || !strings.Contains(en, "#general") || !strings.Contains(en, "Removed on") || !strings.Contains(en, "#benefits") {
		t.Fatalf("english history = %q", en)
	}
	if strings.Index(en, "#general") > strings.Index(en, "#benefits") {
		t.Fatalf("the history is not newest first: %q", en)
	}
	if de := render("de-DE", persona); !strings.Contains(de, "Entfernt von Wanda Reyes am") {
		t.Fatalf("german history = %q", de)
	}
	if ar := render("ar", persona); !strings.Contains(ar, "أزاله Wanda Reyes في") {
		t.Fatalf("arabic history = %q", ar)
	}
	none := PersonaAdminPersona{ID: "policy-helper", Name: "Policy Helper", Lifecycle: PersonaPublished}
	if text := render("en-US", none); strings.Contains(text, "Removed") {
		t.Fatalf("an agent never removed shows a history: %q", text)
	}
	// Only the five most recent removals are drawn.
	many := none
	for day := 1; day <= 8; day++ {
		many.RemovedPlacements = append(many.RemovedPlacements, PersonaAdminRemovedPlacement{ConversationID: "room", Conversation: "room", Kind: "channel", RemovedBy: "Wanda Reyes", RemovedAt: time.Date(2026, 10, day, 9, 0, 0, 0, time.UTC)})
	}
	if count := strings.Count(render("en-US", many), "Removed by"); count != personaAdminRemovedPlacementsMax {
		t.Fatalf("history lines = %d, want %d", count, personaAdminRemovedPlacementsMax)
	}
}

func agentux037MarkupText(markup string) string {
	var out strings.Builder
	inTag := false
	for _, r := range markup {
		switch {
		case r == '<':
			inTag = true
			out.WriteRune(' ')
		case r == '>':
			inTag = false
		case !inTag:
			out.WriteRune(r)
		}
	}
	return out.String()
}
