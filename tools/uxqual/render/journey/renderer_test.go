package journey

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func mustDocument(t *testing.T, p Page) string {
	t.Helper()
	doc, err := Document(p)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	return doc
}

// ----------------------------------------------------------------------
// Landmarks and document structure
// ----------------------------------------------------------------------

// TestLandmarksAreComplete checks the four regions a screen-reader user
// navigates by, plus the skip link that lets a keyboard user get past the
// masthead without tabbing through every nav item on every page.
func TestLandmarksAreComplete(t *testing.T) {
	for _, name := range sampleNames() {
		out := mustRender(t, samplePages()[name])
		t.Run(name, func(t *testing.T) {
			wants := map[string]string{
				"a banner landmark":      `<header class="jn-masthead">`,
				"a navigation landmark":  `<nav aria-label="Primary"`,
				"the main landmark":      `<main class="jn-main" id="main-content">`,
				"a contentinfo landmark": `<footer class="jn-footer">`,
				"the skip link":          `<a class="jn-skip" href="#main-content">Skip to main content</a>`,
				"a polite status region": `role="status"`,
			}
			for what, want := range wants {
				if !strings.Contains(out, want) {
					t.Errorf("%s page is missing %s (%q)", name, what, want)
				}
			}
			if idx := strings.Index(out, `class="jn-skip"`); idx == -1 || idx > strings.Index(out, "<header") {
				t.Error("the skip link is not the first thing in the document")
			}
		})
	}
}

func TestTheDetailPageHasAComplementaryLandmarkForItsRail(t *testing.T) {
	out := mustRender(t, SampleDetailPage())
	if !strings.Contains(out, `<aside aria-label="Actions and history"`) {
		t.Error("the right rail is not a labelled complementary landmark")
	}
}

var headingPattern = regexp.MustCompile(`<h([1-6])\b[^>]*>`)

func headingLevels(html string) []int {
	var levels []int
	for _, m := range headingPattern.FindAllStringSubmatch(html, -1) {
		n, _ := strconv.Atoi(m[1])
		levels = append(levels, n)
	}
	return levels
}

func TestExactlyOneH1PerPage(t *testing.T) {
	for _, name := range sampleNames() {
		out := mustRender(t, samplePages()[name])
		t.Run(name, func(t *testing.T) {
			count := 0
			for _, level := range headingLevels(out) {
				if level == 1 {
					count++
				}
			}
			if count != 1 {
				t.Errorf("%s page has %d h1 elements, want exactly 1", name, count)
			}
		})
	}
}

func TestEmbeddedContentLeavesApplicationLandmarksToHost(t *testing.T) {
	for _, name := range sampleNames() {
		out, err := ui.RenderToString(BuildContent(samplePages()[name]))
		if err != nil {
			t.Fatal(err)
		}
		t.Run(name, func(t *testing.T) {
			for _, forbidden := range []string{"<main", "jn-masthead", "jn-footer", "jn-skip"} {
				if strings.Contains(out, forbidden) {
					t.Errorf("embedded content contains host-owned chrome %q", forbidden)
				}
			}
			if strings.Count(out, "<h1") != 1 || !strings.Contains(out, `class="jn-embedded"`) {
				t.Errorf("embedded content does not own exactly one heading: %s", firstN(out, 300))
			}
		})
	}
}

// TestHeadingOrderNeverSkipsALevel: an outline that jumps h1 to h3 tells a
// screen-reader user a section is missing (WCAG 1.3.1, 2.4.10).
func TestHeadingOrderNeverSkipsALevel(t *testing.T) {
	for _, name := range sampleNames() {
		out := mustRender(t, samplePages()[name])
		t.Run(name, func(t *testing.T) {
			levels := headingLevels(out)
			if len(levels) == 0 {
				t.Fatal("the page has no headings at all")
			}
			if levels[0] != 1 {
				t.Errorf("the first heading is an h%d, want the h1", levels[0])
			}
			for i := 1; i < len(levels); i++ {
				if levels[i] > levels[i-1]+1 {
					t.Errorf("heading %d jumps from h%d to h%d", i, levels[i-1], levels[i])
				}
			}
		})
	}
}

var controlPattern = regexp.MustCompile(`<(input|select|textarea)\b[^>]*>`)

// TestEveryControlIsLabelled walks every rendered control and demands a
// <label for> that matches its id (or an aria-label). A control that has
// only a placeholder or an adjacent visual caption is unusable with a
// screen reader and unclickable by its caption (WCAG 1.3.1, 3.3.2, 2.5.3).
func TestEveryControlIsLabelled(t *testing.T) {
	labelFor := regexp.MustCompile(`<label[^>]*\bfor="([^"]+)"`)
	attr := func(tag, name string) string {
		m := regexp.MustCompile(name + `="([^"]*)"`).FindStringSubmatch(tag)
		if m == nil {
			return ""
		}
		return m[1]
	}
	for _, name := range sampleNames() {
		out := mustRender(t, samplePages()[name])
		t.Run(name, func(t *testing.T) {
			labelled := map[string]bool{}
			for _, m := range labelFor.FindAllStringSubmatch(out, -1) {
				labelled[m[1]] = true
			}
			for _, tag := range controlPattern.FindAllString(out, -1) {
				if attr(tag, "type") == "hidden" {
					continue
				}
				if attr(tag, "aria-label") != "" {
					continue
				}
				id := attr(tag, "id")
				if id == "" {
					t.Errorf("control has no id and no aria-label, so nothing can label it: %s", tag)
					continue
				}
				if !labelled[id] {
					t.Errorf("control %q has no <label for=%q>: %s", id, id, tag)
				}
			}
		})
	}
}

// TestEveryButtonHasAnAccessibleName: a submit button whose only content
// were an icon would announce as "button".
func TestEveryButtonHasAnAccessibleName(t *testing.T) {
	buttonPattern := regexp.MustCompile(`(?s)<button\b[^>]*>(.*?)</button>`)
	stripTags := regexp.MustCompile(`<[^>]*>`)
	for _, name := range sampleNames() {
		out := mustRender(t, samplePages()[name])
		t.Run(name, func(t *testing.T) {
			matches := buttonPattern.FindAllStringSubmatch(out, -1)
			if len(matches) == 0 {
				t.Fatal("the page has no buttons; every fixture should offer at least one submit")
			}
			for _, m := range matches {
				if strings.TrimSpace(stripTags.ReplaceAllString(m[1], "")) == "" {
					t.Errorf("button has no text content: %s", m[0])
				}
			}
		})
	}
}

// TestEveryLinkHasAnAccessibleName covers the stretched card link, whose
// name comes from the worker's name plus a visually-hidden suffix.
func TestEveryLinkHasAnAccessibleName(t *testing.T) {
	linkPattern := regexp.MustCompile(`(?s)<a\b[^>]*>(.*?)</a>`)
	stripTags := regexp.MustCompile(`<[^>]*>`)
	for _, name := range sampleNames() {
		out := mustRender(t, samplePages()[name])
		t.Run(name, func(t *testing.T) {
			for _, m := range linkPattern.FindAllStringSubmatch(out, -1) {
				if strings.TrimSpace(stripTags.ReplaceAllString(m[1], "")) == "" {
					t.Errorf("link has no text content: %s", m[0])
				}
			}
		})
	}
}

func TestCurrentNavLinkIsMarkedForAssistiveTechnology(t *testing.T) {
	for _, name := range sampleNames() {
		out := mustRender(t, samplePages()[name])
		t.Run(name, func(t *testing.T) {
			if got := strings.Count(out, `aria-current="page"`); got != 1 {
				t.Errorf("%d links are marked aria-current=\"page\", want exactly 1", got)
			}
		})
	}
}

// ----------------------------------------------------------------------
// Document
// ----------------------------------------------------------------------

func TestDocumentShape(t *testing.T) {
	for _, name := range sampleNames() {
		p := samplePages()[name]
		doc := mustDocument(t, p)
		t.Run(name, func(t *testing.T) {
			for _, want := range []string{
				"<!doctype html>",
				`<html lang="en-US" dir="ltr">`,
				`<meta charset="utf-8">`,
				`<meta name="viewport" content="width=device-width, initial-scale=1">`,
				"<title>" + p.Title + "</title>",
				"<style>", "</style></head><body>", "</body></html>",
			} {
				if !strings.Contains(doc, want) {
					t.Errorf("document is missing %q", want)
				}
			}
			if !strings.HasPrefix(doc, "<!doctype html>") {
				t.Error("the document does not start with its doctype")
			}
		})
	}
}

// TestDocumentInlinesExactlyTheHashedStylesheet is the CSP contract: the
// serving package pins sha256(Stylesheet()) in style-src, so the inline
// block must be that string, byte for byte, once.
func TestDocumentInlinesExactlyTheHashedStylesheet(t *testing.T) {
	css := Stylesheet()
	for _, name := range sampleNames() {
		doc := mustDocument(t, samplePages()[name])
		t.Run(name, func(t *testing.T) {
			if got := strings.Count(doc, css); got != 1 {
				t.Fatalf("the stylesheet appears %d times in the document, want exactly 1", got)
			}
			if !strings.Contains(doc, "<style>"+css+"</style>") {
				t.Error("the style element does not contain exactly the stylesheet")
			}
			if got := strings.Count(doc, "<style"); got != 1 {
				t.Errorf("the document has %d style elements, want exactly 1", got)
			}
		})
	}
}

// TestDocumentLoadsNothing restates `default-src 'none'`: any of these tags
// would be a request the browser refuses, so the page would ship visibly
// broken.
func TestDocumentLoadsNothing(t *testing.T) {
	for _, name := range sampleNames() {
		doc := strings.ToLower(mustDocument(t, samplePages()[name]))
		t.Run(name, func(t *testing.T) {
			for _, forbidden := range []string{"<script", "<img", "<link", "<iframe", "<object", "<embed", "@import"} {
				if strings.Contains(doc, forbidden) {
					t.Errorf("document contains %q, which the content-security-policy blocks", forbidden)
				}
			}
			for _, handler := range []string{" onclick=", " onload=", " onerror=", " onsubmit="} {
				if strings.Contains(doc, handler) {
					t.Errorf("document contains the inline handler %q; script-src is 'none'", handler)
				}
			}
		})
	}
}

// TestEveryFormWorksWithoutScripting: the page has no JavaScript at all, so
// every form must be a plain POST to a real route with a submit button.
func TestEveryFormWorksWithoutScripting(t *testing.T) {
	for _, name := range sampleNames() {
		out := mustRender(t, samplePages()[name])
		t.Run(name, func(t *testing.T) {
			forms := formsIn(out)
			if len(forms) == 0 {
				t.Fatal("the page offers no form; nothing can be done without scripting")
			}
			for _, form := range forms {
				if !strings.Contains(form, `method="post"`) {
					t.Errorf("form is not a POST: %s", firstN(form, 200))
				}
				action := formAction(form)
				if action == "" || action == "#" {
					t.Errorf("form posts to %q, which goes nowhere", action)
				}
				if !strings.Contains(form, `type="submit"`) {
					t.Errorf("form has no submit button: %s", firstN(form, 200))
				}
			}
		})
	}
}

// ----------------------------------------------------------------------
// Determinism and degradation
// ----------------------------------------------------------------------

// TestRenderIsDeterministic runs the whole document repeatedly. Map
// iteration order is the usual culprit; anything else non-deterministic
// (time, randomness, pointer identity) would show up here too.
func TestRenderIsDeterministic(t *testing.T) {
	for _, name := range sampleNames() {
		p := samplePages()[name]
		t.Run(name, func(t *testing.T) {
			first := mustDocument(t, p)
			for i := 1; i < 10; i++ {
				if again := mustDocument(t, p); again != first {
					t.Fatalf("render %d differs from the first; the page is not deterministic", i)
				}
			}
		})
	}
}

// TestBuildDegradesRatherThanPanicking: a projection bug that leaves both
// views nil should cost the reader the content, not the page. The chrome
// still has to navigate.
func TestBuildDegradesRatherThanPanicking(t *testing.T) {
	cases := map[string]Page{
		"entirely empty":     {},
		"chrome but no view": {Title: "T", Brand: "B", Nav: []NavLink{{Label: "L", Href: "/", Current: true}}},
		"empty list view":    {Title: "T", List: &ListView{}},
		"empty detail view":  {Title: "T", Detail: &DetailView{}},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := RenderToString(p)
			if err != nil {
				t.Fatalf("RenderToString: %v", err)
			}
			if !strings.Contains(out, `id="main-content"`) {
				t.Error("the main landmark did not survive an empty page")
			}
			if !strings.Contains(out, `class="jn-skip"`) {
				t.Error("the skip link did not survive an empty page")
			}
			if _, err := Document(p); err != nil {
				t.Fatalf("Document: %v", err)
			}
		})
	}
}

// TestDetailWinsWhenBothViewsAreSet documents the precedence Build uses, so
// a projection that sets both does something predictable.
func TestDetailWinsWhenBothViewsAreSet(t *testing.T) {
	p := SampleDetailPage()
	p.List = SampleListPage().List
	out := mustRender(t, p)
	if !strings.Contains(out, `id="journey-heading"`) {
		t.Error("the detail view did not render")
	}
	if strings.Contains(out, `id="propose-heading"`) {
		t.Error("the list view rendered alongside the detail view")
	}
}

// TestStylesheetIsNotInTheComponentTree restates why Document assembles the
// CSS as a string: GWC escapes text nodes, so a <style> child holding the
// sheet would arrive with its selectors entity-encoded and the CSP hash
// would not match either.
func TestStylesheetIsNotInTheComponentTree(t *testing.T) {
	out := mustRender(t, SampleListPage())
	if strings.Contains(out, "<style") || strings.Contains(out, "--jn-canvas") {
		t.Error("the stylesheet leaked into the component tree")
	}
}
