package chatsearch

import (
	"errors"
	"testing"
)

// TestTodo_CHATLANG_005_WrittenIn: "lang:de" (or "language:de") narrows a search
// to messages written in that language, a rubbish tag fails closed, and a result
// whose language is not known is not shown under a language filter.
func TestTodo_CHATLANG_005_WrittenIn(t *testing.T) {
	parsed, err := Parse("budget lang:de")
	if err != nil || parsed.Filters.Language != "de" || parsed.Text != "budget" || len(parsed.Chips) != 1 || parsed.Chips[0] != "lang:de" {
		t.Fatalf("lang:de: %+v %v", parsed, err)
	}
	if parsed, err = Parse(`language:AR`); err != nil || parsed.Filters.Language != "ar" {
		t.Fatalf("language:AR: %+v %v", parsed, err)
	}
	for _, bad := range []string{"lang:", "lang:german", "lang:d3", "lang:d", "lang:de lang:fr"} {
		if _, err := Parse(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q was accepted: %v", bad, err)
		}
	}
	if (Filters{Language: "de"}).Empty() {
		t.Fatal("a language filter alone is an empty search")
	}
	request := Request{Query: "budget", Filters: Filters{Language: "de"}}
	german := Row{Kind: Message, ID: "a", Text: "Das Budget ist knapp", Language: "de"}
	english := Row{Kind: Message, ID: "b", Text: "The budget is tight", Language: "en"}
	unknown := Row{Kind: Message, ID: "c", Text: "budget"}
	if !Match(german, request) || Match(english, request) || Match(unknown, request) {
		t.Fatal("the language filter does not narrow by the language a message was written in")
	}
	// No filter: nothing is excluded by language.
	if !Match(english, Request{Query: "budget"}) || !Match(unknown, Request{Query: "budget"}) {
		t.Fatal("a search without the filter lost results")
	}
	// Removing the chip rebuilds the query without it.
	if rest, err := RemoveChip("budget lang:de", "lang:de"); err != nil || rest != "budget" {
		t.Fatalf("remove chip: %q %v", rest, err)
	}
}
