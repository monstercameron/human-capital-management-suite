package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATBUG_053_Entities: a character reference in message text needs
// its semicolon. The names HTML still accepts without one ("&copy", "&reg",
// "&not", "&para") are ordinary query parameters in an address.
func TestTodo_CHATBUG_053_Entities(t *testing.T) {
	for value, want := range map[string]string{
		"?a=1&copy=2":             "?a=1&copy=2",
		"?a=1&amp;copy=2":         "?a=1&copy=2",
		"&reg=eu&not=1&para":      "&reg=eu&not=1&para",
		"&copy; 2026":             "© 2026",
		"Q&amp;A":                 "Q&A",
		"&#38; &#x26; &#X26;":     "& & &",
		"&lt;b&gt;":               "<b>",
		"&unknown; & &; &#; &#x;": "&unknown; & &; &#; &#x;",
		"a \\* b &amp":            "a * b &amp",
		"no reference":            "no reference",
		"trailing &":              "trailing &",
	} {
		if got := markdownUnescape(value); got != want {
			t.Errorf("markdownUnescape(%q) = %q, want %q", value, got, want)
		}
	}

	m := chatbug037Model("none")
	const address = "https://example.com/report?a=1&copy=2&reg=eu"
	markup := chatbug053Body(t, m, "see "+address+" today")
	if !strings.Contains(markup, `href="`+address+`"`) || !strings.Contains(markup, ">"+address+"</a>") {
		t.Fatalf("the address lost its query: %s", markup)
	}
	if strings.ContainsAny(markup, "©®") {
		t.Fatalf("a query parameter was read as a character reference: %s", markup)
	}
	if text := chatbug053Tags.ReplaceAllString(markup, ""); text != "see "+address+" today" {
		t.Fatalf("the text of the message changed: %q", text)
	}
}
