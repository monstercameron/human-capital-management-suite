package qual

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// CheckDocumentLocale verifies the document-level language and writing
// direction metadata that assistive technology uses before it reads any
// translated content. The exact BCP-47 tag is intentional: a generic `en`
// tag does not qualify an en-US or de-DE experience, and RTL content needs a
// direction boundary so embedded IDs and currency values remain readable.
func CheckDocumentLocale(doc, wantLanguage, wantDirection string) CriterionResult {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return fail("Document locale and direction", "parse error: "+err.Error())
	}
	var document *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Html && document == nil {
			document = n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if document == nil {
		return fail("Document locale and direction", "document has no <html> element")
	}
	attrs := attrMap(document)
	gotLanguage := attrs["lang"]
	gotDirection := strings.ToLower(strings.TrimSpace(attrs["dir"]))
	wantDirection = strings.ToLower(strings.TrimSpace(wantDirection))
	var problems []string
	if !strings.EqualFold(strings.TrimSpace(gotLanguage), strings.TrimSpace(wantLanguage)) {
		problems = append(problems, fmt.Sprintf("lang=%q, want %q", gotLanguage, wantLanguage))
	}
	if wantDirection != "" && gotDirection != wantDirection {
		problems = append(problems, fmt.Sprintf("dir=%q, want %q", gotDirection, wantDirection))
	}
	if len(problems) > 0 {
		return fail("Document locale and direction", strings.Join(problems, "; "))
	}
	return pass("Document locale and direction", fmt.Sprintf("lang=%s dir=%s", gotLanguage, gotDirection))
}

// CheckAssistiveNames verifies that every interactive element has a usable
// accessible name. It deliberately follows the renderer's semantic output
// rather than a component implementation: this catches an icon-only action,
// an unlabeled form control, and an aria-labelledby reference to an absent
// node in the same way a browser accessibility tree would.
func CheckAssistiveNames(doc string) CriterionResult {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return fail("Assistive names", "parse error: "+err.Error())
	}
	byID := map[string]*html.Node{}
	labels := map[string]string{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attrs := attrMap(n)
			if id := attrs["id"]; id != "" {
				byID[id] = n
			}
			if n.DataAtom == atom.Label && attrs["for"] != "" {
				labels[attrs["for"]] = visibleText(n)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	var unnamed []string
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attrs := attrMap(n)
			interactive := n.DataAtom == atom.Button || n.DataAtom == atom.Select || n.DataAtom == atom.Textarea ||
				(n.DataAtom == atom.A && attrs["href"] != "") ||
				(n.DataAtom == atom.Input && attrs["type"] != "hidden")
			if interactive && accessibleName(n, byID, labels) == "" {
				id := attrs["id"]
				if id == "" {
					id = n.Data
				}
				unnamed = append(unnamed, "<"+n.Data+" id=\""+id+"\">")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if len(unnamed) > 0 {
		return fail("Assistive names", "unnamed interactive elements: "+strings.Join(unnamed, ", "))
	}
	return pass("Assistive names", "every interactive element has visible or ARIA naming")
}

func accessibleName(n *html.Node, byID map[string]*html.Node, labels map[string]string) string {
	attrs := attrMap(n)
	if value := strings.TrimSpace(attrs["aria-label"]); value != "" {
		return value
	}
	if refs := strings.Fields(attrs["aria-labelledby"]); len(refs) > 0 {
		var parts []string
		for _, ref := range refs {
			if target := byID[ref]; target != nil {
				if text := visibleText(target); text != "" {
					parts = append(parts, text)
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}
	if n.DataAtom == atom.Input || n.DataAtom == atom.Select || n.DataAtom == atom.Textarea {
		id := attrs["id"]
		if text := labels[id]; text != "" {
			return text
		}
	}
	return visibleText(n)
}

func visibleText(n *html.Node) string {
	if n.Type == html.ElementNode && attrMap(n)["aria-hidden"] == "true" {
		return ""
	}
	if n.Type == html.TextNode {
		return strings.TrimSpace(n.Data)
	}
	var parts []string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if text := visibleText(c); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

// CheckLocalizedValues verifies that values already localized by the
// projection reach the document byte-for-byte. The renderer must not parse,
// round, or replace these strings; that keeps money, dates and percentages
// under the locale-aware projector's authority.
func CheckLocalizedValues(doc string, values map[string]string) CriterionResult {
	var missing []string
	for name, value := range values {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name+" is empty")
			continue
		}
		if !strings.Contains(doc, value) {
			missing = append(missing, name+"="+strconv.Quote(value))
		}
	}
	if len(missing) > 0 {
		return fail("Localized values preserved", "missing values: "+strings.Join(missing, ", "))
	}
	return pass("Localized values preserved", fmt.Sprintf("%d projected localized values present unchanged", len(values)))
}

// CheckResponsiveAtWidths applies the qualification's 1440/390/320 viewport
// matrix to stylesheet evidence. CSS is static here, so the check rejects
// fixed widths beyond the narrowest viewport and requires the page's
// max-width, wrapping, and horizontal-overflow protections to be present.
func CheckResponsiveAtWidths(css string, widths ...int) CriterionResult {
	if len(widths) == 0 {
		return fail("Responsive viewport matrix", "no viewport widths supplied")
	}
	minWidth := widths[0]
	for _, width := range widths[1:] {
		if width < minWidth {
			minWidth = width
		}
	}
	fixed := regexp.MustCompile(`(?i)(?:^|[^-\w])(?:width|min-width)\s*:\s*(\d+)px`).FindAllStringSubmatch(css, -1)
	var offenders []string
	for _, match := range fixed {
		n, err := strconv.Atoi(match[1])
		if err == nil && n > minWidth {
			offenders = append(offenders, match[0])
		}
	}
	for _, required := range []string{"max-width:100%", "flex-wrap:wrap", "overflow-x:auto"} {
		if !strings.Contains(strings.ReplaceAll(css, " ", ""), required) {
			offenders = append(offenders, "missing "+required)
		}
	}
	if len(offenders) > 0 {
		return fail("Responsive viewport matrix", strings.Join(offenders, "; "))
	}
	return pass("Responsive viewport matrix", fmt.Sprintf("relative layout protections cover %vpx", widths))
}

// CheckReducedMotion verifies that animation and scrolling have an explicit
// reduced-motion override, not merely a no-preference enhancement.
func CheckReducedMotion(css string) CriterionResult {
	compact := strings.ReplaceAll(css, " ", "")
	if !strings.Contains(compact, "prefers-reduced-motion:reduce") {
		return fail("Reduced motion", "stylesheet has no prefers-reduced-motion:reduce rule")
	}
	var overrides []string
	for _, required := range []string{"animation-duration:.001ms!important", "animation-iteration-count:1!important", "scroll-behavior:auto!important"} {
		if !strings.Contains(compact, required) {
			overrides = append(overrides, required)
		}
	}
	if len(overrides) > 0 {
		return fail("Reduced motion", "missing overrides: "+strings.Join(overrides, ", "))
	}
	return pass("Reduced motion", "animations and smooth scrolling are disabled for reduce")
}
