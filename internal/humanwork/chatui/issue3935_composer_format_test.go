package chatui

import (
	"strings"
	"testing"
)

// #3903: a second Bold click takes the markers off instead of nesting them.
func TestIssue3903_RepeatedBoldRemovesMarkers(t *testing.T) {
	value, s, e := formatSelection("Please review this", "bold", 7, 13)
	if value != "Please **review** this" || s != 9 || e != 15 {
		t.Fatalf("first bold = %q,%d,%d", value, s, e)
	}
	value, s, e = formatSelection(value, "bold", s, e)
	if value != "Please review this" || s != 7 || e != 13 {
		t.Fatalf("second bold = %q,%d,%d; want the markers removed and the word still selected", value, s, e)
	}
	// The markers inside the selection come off too.
	if got, _, _ := formatSelection("a **b** c", "bold", 2, 7); got != "a b c" {
		t.Fatalf("selection including markers = %q", got)
	}
	// An empty pair with the caret inside it closes again.
	if got, c, _ := formatSelection("****", "bold", 2, 2); got != "" || c != 0 {
		t.Fatalf("empty pair = %q,%d", got, c)
	}
	// Italic and code toggle the same way; a different mark still wraps.
	if got, _, _ := formatSelection("_x_", "italic", 1, 2); got != "x" {
		t.Fatalf("italic toggle = %q", got)
	}
	if got, _, _ := formatSelection("`x`", "code", 1, 2); got != "x" {
		t.Fatalf("code toggle = %q", got)
	}
	if got, _, _ := formatSelection("**x**", "italic", 2, 3); got != "**_x_**" {
		t.Fatalf("italic inside bold = %q", got)
	}
}

// #3913: lists and quotes take only the selected lines; the next unselected
// line stays a paragraph of its own, in the draft and in the preview.
func TestIssue3913_ListAndQuoteLeaveFollowingLineAlone(t *testing.T) {
	draft := "Intro\nFirst item\nSecond item\nClosing"
	for _, c := range []struct{ kind, want, tag, outside string }{
		{"bullets", "Intro\n- First item\n- Second item\n\nClosing", "<li>", "</li>"},
		{"quote", "Intro\n> First item\n> Second item\n\nClosing", "<blockquote>", "</blockquote>"},
	} {
		got, _, _ := formatSelection(draft, c.kind, 6, 28)
		if got != c.want {
			t.Fatalf("%s draft = %q; want %q", c.kind, got, c.want)
		}
		html := renderNode(t, spanOf(markdownMessageBody(Model{}, got)))
		closing := strings.Index(html, "Closing")
		if closing < 0 || strings.Contains(html[closing:], c.outside) {
			t.Fatalf("%s: Closing is still inside the block: %s", c.kind, html)
		}
		if !strings.Contains(html, c.tag) {
			t.Fatalf("%s: the selected lines lost their block: %s", c.kind, html)
		}
	}
	// A following line that is already a list item stays one list.
	if got, _, _ := formatSelection("a\nb\n- c", "bullets", 0, 3); got != "- a\n- b\n- c" {
		t.Fatalf("joined list = %q", got)
	}
	// Selecting to the end of the draft adds nothing.
	if got, _, _ := formatSelection("a\nb", "bullets", 0, 3); got != "- a\n- b" {
		t.Fatalf("whole draft = %q", got)
	}
}

// #3935: a link destination that will not render is named before sending.
func TestIssue3935_LinkProblemsAreNamedInPreview(t *testing.T) {
	cases := []struct{ draft, kind, fix string }{
		{"Please review the [plan](example.com)", "web", "https://example.com"},
		{"Please review the [plan](url)", "empty", ""},
		{"Open [file](javascript:alert(1))", "blocked", ""},
		{"Open [file](ftp://example.com/x)", "blocked", ""},
	}
	for _, c := range cases {
		got := composerLinkProblems(c.draft)
		if len(got) != 1 || got[0].Label == "" || got[0].Kind != c.kind || got[0].Fix != c.fix {
			t.Fatalf("%q problems = %+v; want kind %s fix %q", c.draft, got, c.kind, c.fix)
		}
	}
	for _, ok := range []string{"[plan](https://example.com)", "[x](/chat/a)", "[x](#top)", "no link", "[a](https://a.example) and `[b](url)`"} {
		if got := composerLinkProblems(ok); len(got) != 0 {
			t.Fatalf("%q wrongly flagged: %+v", ok, got)
		}
	}

	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale}
		markup := renderNode(t, composerPreviewBody(m, "Please review the [plan](example.com)", "Message", "chat-composer"))
		for _, want := range []string{`role="status"`, "plan", "https://example.com", `data-action="composer-link-edit"`, `data-id="chat-composer"`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s preview lacks %q: %s", locale, want, markup)
			}
		}
		if locale != "en-US" && strings.Contains(markup, issue3935Text(Model{}, issue3935KeyEdit)) {
			t.Fatalf("%s preview fell back to English: %s", locale, markup)
		}
	}
	// The visible text stays and nothing unsafe becomes a link.
	markup := renderNode(t, composerPreviewBody(Model{}, "[x](javascript:alert(1))", "Message", "chat-composer"))
	if strings.Contains(markup, `href="javascript`) {
		t.Fatalf("unsafe scheme made clickable: %s", markup)
	}
	// A clean draft shows no warning.
	if clean := renderNode(t, composerPreviewBody(Model{}, "[ok](https://example.com)", "Message", "chat-composer")); strings.Contains(clean, `role="status"`) {
		t.Fatalf("clean draft warned: %s", clean)
	}
}

// Review additions: edges of the toggle and of the line boundary.
func TestIssue3903_ToggleEdges(t *testing.T) {
	// Two separate bold spans are not one pair around what lies between.
	if got, _, _ := formatSelection("**a** x **b**", "bold", 5, 8); got != "**a**** x ****b**" {
		t.Fatalf("between two spans = %q", got)
	}
	if got, _, _ := formatSelection("**a** and **b**", "bold", 0, 15); got != "****a** and **b****" {
		t.Fatalf("across two spans = %q", got)
	}
	// Inside a span the markers still come off.
	if got, _, _ := formatSelection("**a** **x** b", "bold", 8, 9); got != "**a** x b" {
		t.Fatalf("second span = %q", got)
	}
	// snake_case is not emphasis.
	if got, _, _ := formatSelection("snake_case_name", "italic", 6, 10); got != "snake__case__name" {
		t.Fatalf("snake_case = %q", got)
	}
}

func TestIssue3913_TrailingNewlineInSelection(t *testing.T) {
	// A selection that includes the newline after its last line leaves the next line alone.
	got, _, _ := formatSelection("Intro\nFirst\nClosing\nMore", "bullets", 6, 12)
	if got != "Intro\n- First\n\nClosing\nMore" {
		t.Fatalf("trailing newline = %q", got)
	}
}
