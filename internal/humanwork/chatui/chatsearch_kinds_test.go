package chatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// TestTodo_CHATSEARCH_002_KindChoices: the kind filter offers the kinds the
// service says it can search, in the registry's order, and no kind that
// nothing in this deployment answers (choosing one returned nothing with no
// reason given).
func TestTodo_CHATSEARCH_002_KindChoices(t *testing.T) {
	served := []chatsearch.Kind{chatsearch.Voice, chatsearch.Message, chatsearch.Todo, chatsearch.Poll, chatsearch.Person}
	got := chatsearchKindChoices(chatsearch.Response{Kinds: served})
	want := []chatsearch.Kind{chatsearch.Message, chatsearch.Person, chatsearch.Todo, chatsearch.Poll, chatsearch.Voice}
	if len(got) != len(want) {
		t.Fatalf("choices = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("choices = %v, want the served kinds in the registry's order %v", got, want)
		}
	}
	// No answer yet: every declared kind, as before.
	if all := chatsearchKindChoices(chatsearch.Response{}); len(all) != len(chatsearch.Declarations()) {
		t.Fatalf("with no answer %d kinds are offered, want all %d", len(all), len(chatsearch.Declarations()))
	}

	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(RenderChatSearch(locale, ChatSearchView{Query: "budget", Response: chatsearch.Response{Kinds: served}}))
		if err != nil {
			t.Fatal(err)
		}
		field := regexp.MustCompile(`<select[^>]*id="chatsearch-kind"[^>]*>.*?</select>`).FindString(markup)
		if field == "" {
			t.Fatalf("%s: no kind filter: %s", locale, markup)
		}
		// "All kinds" and the five served ones.
		if n := strings.Count(field, "<option"); n != len(served)+1 {
			t.Errorf("%s: the kind filter offers %d choices, want %d: %s", locale, n, len(served)+1, field)
		}
		for _, kind := range served {
			if !strings.Contains(field, `value="`+string(kind)+`"`) || chatsearchKind(locale, kind) == "" {
				t.Errorf("%s: the kind filter lacks %s, or it has no name", locale, kind)
			}
		}
		for _, absent := range []chatsearch.Kind{chatsearch.Reminder, chatsearch.SourceTitle, chatsearch.Announcement} {
			if strings.Contains(field, `value="`+string(absent)+`"`) {
				t.Errorf("%s: the kind filter offers %s, which nothing here can find", locale, absent)
			}
		}
	}
}
