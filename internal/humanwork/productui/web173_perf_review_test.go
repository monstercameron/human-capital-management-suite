package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-173: performance-review workspace. The
// registry owns every product surface, but no review
// workspace exists: running performance reviews has no
// exposure point and the first surface invents review
// data by convention. The compiler needs the registered
// surface — canonical identity, route, and an honest
// fallback that reviews nothing until the governed growth
// service publishes, with review truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_173(t *testing.T) {
	definition, ok := LookupPage(PagePerfReview)
	if !ok {
		t.Fatal("performance-review workspace unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("performance-review workspace incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PagePerfReview {
		t.Fatal("performance-review route does not round-trip")
	}
	doc, err := Render(testView(PagePerfReview))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("performance-review workspace exposes an unresolved message key")
	}
	bodyText, err := web173RenderedBodyText(doc)
	if err != nil {
		t.Fatalf("parse performance-review document: %v", err)
	}
	for _, invented := range []string{"review:", "rating:", "section:", "signed ✓"} {
		if strings.Contains(bodyText, invented) {
			t.Fatalf("performance-review workspace invents review data: %q", invented)
		}
	}
}

// web173RenderedBodyText keeps the invented-data assertion on user-visible
// output. Stylesheet selectors are production render input, not review
// records; scanning the complete document would make a valid CSS selector
// such as "section:" look like fabricated data.
func web173RenderedBodyText(doc string) (string, error) {
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		return "", err
	}
	bodies := collectElements(root, "body")
	if len(bodies) != 1 {
		return "", fmt.Errorf("document has %d body elements, want exactly one", len(bodies))
	}
	var text strings.Builder
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && (node.Data == "style" || node.Data == "script") {
			return
		}
		if node.Type == xhtml.TextNode {
			text.WriteString(node.Data)
			text.WriteByte(' ')
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(bodies[0])
	return text.String(), nil
}

// Golden: the registered performance-review definition
// and its fallback copy.
func TestTodo_WEB_173_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePerfReview)
	if !ok {
		t.Fatal("performance-review workspace unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("perf_review.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("perf_review.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "106c79450e41ab279c4aa70c7a542434a8c9355fc341608c56c37bef3b92301f"
	if got != want {
		t.Fatalf("performance-review digest = %s, want %s", got, want)
	}
}

// Browser: the performance-review workspace renders
// deterministically and round-trips its route.
func TestTodo_WEB_173_Browser(t *testing.T) {
	first, err := Render(testView(PagePerfReview))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PagePerfReview))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("performance-review workspace renders nondeterministically")
	}
	definition, _ := LookupPage(PagePerfReview)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PagePerfReview {
		t.Fatal("performance-review route does not round-trip")
	}
}

// Conformance: the performance-review workspace keeps the
// registry contract — visible to managers, hidden from
// the role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_173_Conformance(t *testing.T) {
	if !PageVisible(PagePerfReview, []string{"manager"}) {
		t.Fatal("performance-review workspace hidden from managers")
	}
	if PageVisible(PagePerfReview, nil) {
		t.Fatal("performance-review workspace visible without roles")
	}
	definition, _ := LookupPage(PagePerfReview)
	if definition.PrimaryNav {
		t.Fatal("performance-review workspace claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PagePerfReview), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("performance-review workspace leaks a key in %s", code)
		}
	}
}
