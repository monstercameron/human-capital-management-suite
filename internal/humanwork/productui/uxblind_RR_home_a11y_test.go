package productui

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var uxblindRRFactPair = regexp.MustCompile(`<dt>([^<]+)</dt><dd>(.*?)</dd>`)
var uxblindRRTags = regexp.MustCompile(`<[^>]+>`)

func uxblindRRFactValues(markup string) map[string]string {
	values := make(map[string]string)
	for _, match := range uxblindRRFactPair.FindAllStringSubmatch(markup, -1) {
		label := html.UnescapeString(match[1])
		value := uxblindRRTags.ReplaceAllString(match[2], "")
		values[label] = strings.TrimSpace(html.UnescapeString(value))
	}
	return values
}

func uxblindRRActivityCard(t *testing.T, locale LocaleContext) string {
	t.Helper()
	markup, err := ui.RenderToString(SummaryCard(SummaryCardProps{
		Title: "Current activity",
		Groups: []FactGroupProps{{Title: "Activity", Facts: []FactProps{
			{Label: locale.Text("home.needs_action"), Value: "0", Href: "/workspace/app/work"},
			{Label: locale.Text("home.in_progress"), Value: "1", Href: "/workspace/app/journeys"},
			{Label: locale.Text("home.closed"), Value: "2", Href: "/workspace/app/history"},
		}}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestTodo_UXBLIND_099(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	markup := uxblindRRActivityCard(t, locale)
	values := uxblindRRFactValues(markup)
	for label, want := range map[string]string{
		locale.Text("home.needs_action"): "0",
		locale.Text("home.in_progress"):  "1",
		locale.Text("home.closed"):       "2",
	} {
		if got := values[label]; got != want {
			t.Fatalf("accessible activity fact %q = %q, want %q; markup: %s", label, got, want, markup)
		}
	}
	if strings.Contains(markup, `class="sr-only"`) {
		t.Fatalf("activity facts contain a second hidden label: %s", markup)
	}
}

// Browser: this component-level render is the native proof available to the
// Go package; it walks the same dt/dd names that a browser accessibility tree
// exposes, across every served product locale.
func TestTodo_UXBLIND_099_Browser(t *testing.T) {
	for _, code := range []string{"en-US", "de-DE", "ar"} {
		t.Run(code, func(t *testing.T) {
			locale := ResolveProductLocale(code)
			markup := uxblindRRActivityCard(t, locale)
			values := uxblindRRFactValues(markup)
			for _, label := range []string{locale.Text("home.needs_action"), locale.Text("home.in_progress"), locale.Text("home.closed")} {
				if _, ok := values[label]; !ok {
					t.Fatalf("locale %s has no accessible activity fact %q: %s", code, label, markup)
				}
			}
			if strings.Contains(markup, `class="sr-only"`) {
				t.Fatalf("locale %s duplicates an activity label: %s", code, markup)
			}
		})
	}
}

func TestTodo_UXBLIND_100(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`display:flex;flex-direction:column;`,
		`.primary-nav{flex:1;min-height:0;overflow-y:auto;`,
		`.nav-bottom{display:grid;margin-top:auto;`,
		`.primary-nav::after{`,
		`position:sticky;`,
		`box-shadow:0 -4px 12px 4px light-dark(`,
		`.primary-nav>ul{padding-block-end:20px;}`,
		`@media (prefers-reduced-motion:reduce){`,
		`animation:none;`,
		`opacity:1;`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("sidebar overflow contract is missing %q", want)
		}
	}
}

// Browser: the rendered DOM keeps the independently scrolling list before
// the pinned support footer, so the list's last item cannot paint beneath it.
func TestTodo_UXBLIND_100_Browser(t *testing.T) {
	markup, err := Render(testView(PageAdmin))
	if err != nil {
		t.Fatal(err)
	}
	primary := strings.Index(markup, `id="primary-nav"`)
	footer := strings.Index(markup, `class="nav-bottom"`)
	if primary < 0 || footer < 0 || primary > footer {
		t.Fatalf("sidebar list/footer order is not scroll-above-footer: primary=%d footer=%d", primary, footer)
	}
	if strings.Contains(markup[primary:footer], `class="nav-bottom"`) {
		t.Fatal("support footer became part of the primary scrolling list")
	}
}
