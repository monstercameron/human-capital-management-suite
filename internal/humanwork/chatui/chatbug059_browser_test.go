package chatui_test

import (
	"html"
	"regexp"
	"sort"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// chatbug059Segments is everything a person reads on a rendered surface: each
// text node, and the attributes that are read aloud or shown (name, tooltip,
// placeholder, picture description). Hidden disclosure content is included.
func chatbug059Segments(t *testing.T, page string) []string {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	add := func(text string) {
		if text = strings.Join(strings.Fields(html.UnescapeString(text)), " "); text != "" {
			out = append(out, text)
		}
	}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		switch n.Type {
		case xhtml.TextNode:
			if n.Parent == nil || (n.Parent.Data != "script" && n.Parent.Data != "style") {
				add(n.Data)
			}
		case xhtml.ElementNode:
			for _, a := range n.Attr {
				switch a.Key {
				case "aria-label", "title", "placeholder", "alt", "aria-description", "aria-roledescription", "aria-valuetext":
					add(a.Val)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

// chatbug059Fixture is text the surfaces' fixtures carry as data, which is the
// person's own writing, a name or a technical token and not the product's
// words: it is English in every language, as it should be.
var chatbug059Fixture = map[string]bool{
	"Alice": true, "Bob": true, "Question": true, "Walt": true, "Walt Brennan": true,
	"Check this policy": true, "Thanks!": true, "Policy assistant": true, "Policy Helper": true,
	"Original": true, "Reviewed": true, "Reviewed translation": true, "Please check this policy": true,
	"Read this policy": true, "general": true, "Office": true, "Launch": true, "Send invites": true,
	"Where?": true, "Here": true, "There": true, "A message": true, "GIF": true, "Giphy": true,
	"OK": true, "AI": true, "PDF": true, "@": true, "Aa": true, "H": true, "B": true, "I": true,
	// Text the test hands the page: a notice and an error line a caller writes.
	"Something went wrong.": true, "The service did not answer.": true,
	// Language names are written in their own language.
	"Deutsch": true, "English": true,
	"Powered by GIPHY": true,
}

// chatbug059Data are the fixture's own words (a message, an agent's name and
// purpose, a channel, a person): a string that contains one, or is part of one,
// is the person's data and not the product's copy.
var chatbug059Data = []string{
	"Check this policy", "Carry over up to 40 hours", "Paid time off policy", "explain our PTO policy", "Policy Helper",
	"Answer policy questions", "People Operations", "Send invites", "Launch", "example.com", "people-ops", "payroll",
	"announcements", "general", "Engineer",
}

// chatbug059SharedGerman are single words that German writes the way English
// does, so the same word on the German page is not a leak.
var chatbug059SharedGerman = map[string]bool{
	"Agent": true, "Hindi": true, "Emoji": true, "Code": true, "Link": true, "Apps": true, "Details": true,
	"Filter": true, "Name": true, "Thread": true, "Moderation": true, "Person": true, "Status": true, "Version:": true,
}

// chatbug059Token matches what is not a sentence in any language: a slash
// command, an emoji short name, a time zone, a key chord, an identifier.
var chatbug059Token = regexp.MustCompile(`^(?:/[a-z]+(?: [a-z]+)?|:[a-z0-9_+-]+:|[A-Z][A-Za-z]+/[A-Za-z_ ]+|UTC|\p{Lu}[\p{L} .'-]+ \(UTC[+\x{2212}-]\d{1,2}(?::\d{2})?\)|Ctrl\+K|Cmd\+K|in #[a-z-]+|item-\d+: .*|sha256:.*|[a-z]+(?:_[a-z]+)+|\x{2068}?#[a-z-]+\x{2069}?|@[a-z-]+|Emoji \S+)$`)

// chatbug059Words are English words no German sentence uses; two of them in one
// string, or one in a string of three words or more, mark it as English.
var chatbug059Words = map[string]bool{
	"the": true, "of": true, "to": true, "and": true, "you": true, "your": true, "this": true, "that": true, "is": true,
	"are": true, "for": true, "with": true, "not": true, "can": true, "has": true, "have": true,
	"it": true, "be": true, "by": true, "or": true, "we": true, "our": true, "no": true,
	"try": true, "again": true, "loading": true, "failed": true, "new": true, "all": true, "any": true, "what": true,
}

var (
	chatbug059Latin  = regexp.MustCompile(`[A-Za-z]{2,}`)
	chatbug059Arabic = regexp.MustCompile(`[\x{0600}-\x{06FF}]`)
	chatbug059Word   = regexp.MustCompile(`[A-Za-z']+`)
)

// chatbug059English reports why a string on a German or Arabic page looks
// English: it is the very string the English page shows, or it reads as an
// English sentence.
func chatbug059English(text, locale string, english map[string]bool) string {
	if !chatbug059Latin.MatchString(text) || chatbug059Fixture[text] || chatbug059Token.MatchString(text) {
		return ""
	}
	for _, data := range chatbug059Data {
		if strings.Contains(text, data) || len(text) >= 4 && strings.Contains(data, text) {
			return ""
		}
	}
	words := strings.Fields(text)
	if english[text] && (len(words) >= 2 || locale == "ar") {
		return "the same text as the English page"
	}
	if english[text] && !chatbug059SharedGerman[text] {
		return "a word the English page shows too; if German uses it as it is, add it to chatbug059SharedGerman"
	}
	hits := 0
	for _, word := range chatbug059Word.FindAllString(strings.ToLower(text), -1) {
		if chatbug059Words[word] {
			hits++
		}
	}
	if hits >= 2 || hits >= 1 && len(words) >= 3 {
		return "reads as an English sentence"
	}
	if locale == "ar" && !chatbug059Arabic.MatchString(text) && len(words) >= 2 {
		return "Latin words alone on an Arabic page"
	}
	return ""
}

// chatbug059Surfaces renders every Chat surface in one locale with the
// product's own catalog.
func chatbug059Surfaces(t *testing.T, locale string) map[string][]string {
	ctx := productui.ResolveProductLocale(locale)
	pages := chatui.Chatbug039SurfacesForTest(t, chatui.Model{Locale: ctx.Resolved, Direction: string(ctx.Direction), Text: func(key string) string { return ctx.Text(key) }})
	out := map[string][]string{}
	for name, page := range pages {
		out[name] = chatbug059Segments(t, page)
	}
	catalog := chatui.Model{Locale: ctx.Resolved, Direction: string(ctx.Direction), Text: func(key string) string { return ctx.Text(key) }}
	for name, page := range chatui.Chatbug059MoreSurfacesForTest(t, catalog) {
		out["page: "+name] = chatbug059Segments(t, page)
	}
	return out
}

// TestTodo_CHATBUG_059_Browser renders every Chat surface (sidebar, header,
// composer and its menus, dialogs, details, search, moderation, settings, the
// agent surfaces) in English, German and Arabic and fails on any English
// sentence that shows in a German or Arabic render: a string the English page
// shows word for word, or text that reads as English. What the fixtures carry as
// data (names, a message body) is listed in chatbug059Fixture.
func TestTodo_CHATBUG_059_Browser(t *testing.T) {
	english := map[string]bool{}
	for _, segments := range chatbug059Surfaces(t, "en-US") {
		for _, text := range segments {
			english[text] = true
		}
	}
	for _, locale := range []string{"de-DE", "ar"} {
		surfaces := chatbug059Surfaces(t, locale)
		if len(surfaces) < 50 {
			t.Fatalf("%s: only %d surfaces rendered", locale, len(surfaces))
		}
		names := make([]string, 0, len(surfaces))
		for name := range surfaces {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			seen := map[string]bool{}
			for _, text := range surfaces[name] {
				if why := chatbug059English(text, locale, english); why != "" && !seen[text] {
					seen[text] = true
					t.Errorf("%s, %s: %q (%s)", locale, name, text, why)
				}
			}
		}
	}
}
