package chatui

import (
	"strings"
	"testing"
)

// A person or channel whose name has spaces, typed without quotes the way the
// local search accepts it, used to leave the service search with the first word
// as a nonexistent author and the rest as text words: no results, no hint.
func TestR1_ReviewUnquotedMultiWordFilterNames(t *testing.T) {
	m := Model{
		CurrentUser: "u-camila", CurrentUserName: "Camila Morales",
		Conversations: []Conversation{{ID: "g1", Name: "General Chat", Kind: PublicChannel}},
	}
	got, err := ResolveChatSearchQuery(m, "budget from:@Camila Morales review in:#General Chat")
	if err != nil {
		t.Fatal(err)
	}
	if want := `budget from:u-camila review in:g1`; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// A word after a one-word name that is not part of a name stays a text word.
	got, _ = ResolveChatSearchQuery(m, "from:@Camila budget")
	if got != "from:Camila budget" {
		t.Fatalf("unknown short name changed: %q", got)
	}
}

// A long post whose match sits inside one unbroken run (an address, an encoded
// blob) left the excerpt with nothing but two ellipses.
func TestR1_ReviewSnippetInsideUnbrokenRun(t *testing.T) {
	body := "see " + strings.Repeat("a", 500) + "needle" + strings.Repeat("b", 500)
	out := searchSnippet(body, "needle", 320)
	if strings.Trim(out, "…") == "" {
		t.Fatalf("excerpt is only ellipses: %q", out)
	}
	if !strings.Contains(out, "needle") {
		t.Fatalf("excerpt lost the match: %q", out)
	}
	if n := len([]rune(out)); n > 320+2 {
		t.Fatalf("excerpt too long: %d", n)
	}
}
