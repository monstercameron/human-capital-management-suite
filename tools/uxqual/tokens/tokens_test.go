package tokens_test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/gwc"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/ssr"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/testdata"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/tokens"
)

func TestTokensSmoke(t *testing.T) {
	if css := tokens.WorkspaceCSS(); css == "" {
		t.Fatal("WorkspaceCSS returned an empty stylesheet")
	}
}

func TestTokensNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("WorkspaceCSS panicked: %v", r)
		}
	}()
	_ = tokens.WorkspaceCSS()
}

// TestTodo_UIPOLISH_001 proves the shared stylesheet exposes one semantic,
// fluid hierarchy for every renderer rather than page-specific font values.
func TestTodo_UIPOLISH_001(t *testing.T) {
	css := tokens.WorkspaceCSS()
	for _, token := range []string{
		"--font-family-sans:", "--font-family-mono:", "--font-size-display:",
		"--font-size-page-title:", "--font-size-section:", "--font-size-body:",
		"--font-size-label:", "--font-size-helper:", "--font-size-table:", "--font-size-code:",
		"--line-height-display:", "--line-height-heading:", "--line-height-body:",
		"--measure-readable:", "--measure-prose:",
	} {
		if !strings.Contains(css, token) {
			t.Errorf("typography token %q is missing", token)
		}
	}
	for _, role := range []string{"display", "page-title", "section", "label", "helper", "table", "code"} {
		if !strings.Contains(css, `data-type-role="`+role+`"`) {
			t.Errorf("semantic type role %q is missing", role)
		}
	}
}

func TestTodo_UIPOLISH_001_Golden(t *testing.T) {
	css := tokens.WorkspaceCSS()
	for _, value := range []string{
		"clamp(2rem,1.5rem + 2vw,3.5rem)",
		"clamp(1.75rem,1.35rem + 1.5vw,2.5rem)",
		`ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif`,
		`ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,"Liberation Mono","Courier New",monospace`,
	} {
		if !strings.Contains(css, value) {
			t.Errorf("typography golden value %q is missing", value)
		}
	}
}

func TestTodo_UIPOLISH_001_Browser(t *testing.T) {
	css := tokens.WorkspaceCSS()
	if !strings.Contains(css, "max-inline-size:var(--measure-prose)") {
		t.Fatal("prose measure is not bounded for narrow and wide viewports")
	}
	if strings.Contains(css, "font-size:") && strings.Contains(css, "font-size:0px") {
		t.Fatal("typography must not collapse text to zero size")
	}
}

func TestTodo_UIPOLISH_001_Accessibility(t *testing.T) {
	css := tokens.WorkspaceCSS()
	for _, lineHeight := range []string{"var(--line-height-display)", "var(--line-height-heading)", "var(--line-height-body)", "var(--line-height-tight)"} {
		if !strings.Contains(css, "line-height:"+lineHeight) {
			t.Errorf("line-height role %q is not applied", lineHeight)
		}
	}
	if strings.Contains(strings.ToLower(css), "text-transform:uppercase") {
		t.Fatal("typography tokens must not force all-caps metadata")
	}
}

func TestTodo_UIPOLISH_001_I18N(t *testing.T) {
	css := tokens.WorkspaceCSS()
	if !strings.Contains(css, "overflow-wrap:anywhere") || !strings.Contains(css, "text-wrap:balance") {
		t.Fatal("long localized copy lacks resilient wrapping")
	}
}

func TestTodo_UIPOLISH_001_Regression(t *testing.T) {
	css := tokens.WorkspaceCSS()
	for _, selector := range []string{".field label", ".field .error", ".table-scroll-cue", "code,kbd,pre"} {
		if !strings.Contains(css, selector) {
			t.Errorf("existing selector %q lost its typography contract", selector)
		}
	}
}

// TestTodo_UIPOLISH_003 proves shape, boundary, surface and depth decisions
// are named once and consumed by shared component selectors.
func TestTodo_UIPOLISH_003(t *testing.T) {
	css := tokens.WorkspaceCSS()
	for _, token := range []string{
		"--radius-control:", "--radius-surface:", "--radius-overlay:", "--radius-status:",
		"--border-width-boundary:", "--border-color-control:", "--border-color-focus:",
		"--border-color-selection:", "--surface-canvas:", "--surface-raised:",
		"--surface-overlay:", "--elevation-flat:", "--elevation-overlay:", "--elevation-dialog:",
	} {
		if !strings.Contains(css, token) {
			t.Errorf("semantic token %q is missing", token)
		}
	}
	for _, selector := range []string{".surface", ".popover", ".dialog", ".badge,.tag,.status", "section", ".field input,.field textarea", "button"} {
		if !strings.Contains(css, selector) {
			t.Errorf("shared shape selector %q is missing", selector)
		}
	}
}

func TestTodo_UIPOLISH_003_Golden(t *testing.T) {
	css := tokens.WorkspaceCSS()
	for _, want := range []string{
		`:root[data-hcm-shape="precise"]`, `--radius-control:4px`, `--radius-surface:6px`,
		`:root[data-hcm-shape="rounded"]`, `--radius-control:12px`, `--radius-surface:18px`,
		"box-shadow:var(--elevation-flat)", "box-shadow:var(--elevation-overlay)", "box-shadow:var(--elevation-dialog)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("shape golden %q is missing", want)
		}
	}
}

func TestTodo_UIPOLISH_003_Browser(t *testing.T) {
	css := tokens.WorkspaceCSS()
	for _, selector := range []string{".surface{", ".popover,.menu,[role=\"menu\"],[role=\"listbox\"]{", ".dialog,[role=\"dialog\"]{"} {
		start := strings.Index(css, selector)
		if start < 0 || !strings.Contains(css[start:strings.Index(css[start:], "}")+start], "box-shadow:var(--elevation-") {
			t.Errorf("%s does not consume a named elevation token", selector)
		}
	}
	if !strings.Contains(css, ":where(a[href],input,select,textarea,button,summary):focus-visible{box-shadow:0 0 0 var(--border-width-focus)") {
		t.Fatal("focus state does not use the semantic focus ring contract")
	}
}

func TestTodo_UIPOLISH_003_Accessibility(t *testing.T) {
	css := tokens.WorkspaceCSS()
	if !strings.Contains(css, "--border-width-focus:2px") || !strings.Contains(css, "outline:var(--border-width-focus) solid var(--border-color-focus)") {
		t.Fatal("focus boundary is not a persistent two-pixel semantic indicator")
	}
	if strings.Contains(css, "border-radius:999") {
		t.Fatal("shape contract must not force pill geometry")
	}
}

func TestTodo_UIPOLISH_003_Regression(t *testing.T) {
	css := tokens.WorkspaceCSS()
	for _, selector := range []string{".status-banner", ".finding", ".check", ".table-scroll", ".visually-hidden"} {
		if !strings.Contains(css, selector) {
			t.Errorf("existing selector %q lost while adding shape semantics", selector)
		}
	}
}

// TestTodo_WEB_024 proves the mode rules are present in the exact stylesheet
// both production candidates embed, and that their evidence selectors match
// elements in each candidate's real rendered document.
func TestTodo_WEB_024(t *testing.T) {
	css := tokens.WorkspaceCSS()
	for _, media := range []string{"@media (prefers-contrast:more)", "@media (forced-colors:active)", "@media print"} {
		if block := mediaBlock(t, css, media); len(block) == 0 {
			t.Fatalf("empty %s contract", media)
		}
	}
	for name, doc := range renderedDocuments(t) {
		root := parseDocument(t, doc)
		for _, class := range []string{"status-banner", "finding", "check", "provenance", "print-evidence"} {
			if countClass(root, class) == 0 {
				t.Errorf("%s renderer has no .%s element for the mode contract", name, class)
			}
		}
	}
}

// TestTodo_WEB_024_Golden pins the complete mode contracts rather than a few
// easy-to-satisfy substrings. Any selector or protected declaration change is
// therefore an explicit golden review.
func TestTodo_WEB_024_Golden(t *testing.T) {
	css := tokens.WorkspaceCSS()
	modeCSS := mediaBlock(t, css, "@media (prefers-contrast:more)") +
		mediaBlock(t, css, "@media (forced-colors:active)") +
		mediaBlock(t, css, "@media print")
	const wantSHA256 = "d9f29f3d47fbaae828dcd89d8dbae15e04363894d616a04b141d9f2ed3998575"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(modeCSS))); got != wantSHA256 {
		t.Fatalf("mode contract golden = %s, want %s", got, wantSHA256)
	}
}

// TestTodo_WEB_024_Browser checks the DOM/CSS boundary a browser actually
// consumes: non-colour state text, print-retained evidence, form values, and
// machine-readable timestamps exist in both representative documents.
func TestTodo_WEB_024_Browser(t *testing.T) {
	documents := renderedDocuments(t)
	for name, doc := range documents {
		root := parseDocument(t, doc)
		if got := strings.TrimSpace(textOf(firstClass(root, "status-banner"))); !strings.HasPrefix(got, "Status: Ready.") {
			t.Errorf("%s status is colour-dependent or unnamed: %q", name, got)
		}
		if got := countClass(root, "severity-label"); got != 6 {
			t.Errorf("%s rendered %d severity labels, want one for every finding/check", name, got)
		}
		for _, node := range nodesWithClass(root, "severity-label") {
			if strings.TrimSpace(textOf(node)) == "" {
				t.Errorf("%s rendered an empty non-colour severity label", name)
			}
		}
		assertFormValues(t, name, root)
		assertEvidence(t, name, root)
	}
	ssrEvidence := evidenceSignature(parseDocument(t, documents["ssr"]))
	gwcEvidence := evidenceSignature(parseDocument(t, documents["gwc"]))
	if ssrEvidence != gwcEvidence {
		t.Fatalf("print evidence differs between SSR and GWC:\nSSR: %s\nGWC: %s", ssrEvidence, gwcEvidence)
	}

	css := tokens.WorkspaceCSS()
	if strings.Contains(css, "content:") || strings.Contains(css, "::before") || strings.Contains(css, "::after") {
		t.Fatal("mode/status meaning must be DOM text, not CSS-generated accessibility content")
	}
	print := mediaBlock(t, css, "@media print")
	for _, evidence := range []string{".status-banner", ".finding", ".check", ".print-evidence", ":where(input,select,textarea)"} {
		if !strings.Contains(print, evidence) {
			t.Errorf("print contract does not explicitly retain %s", evidence)
		}
	}
	if strings.Contains(print, ".visually-hidden{position:static") || strings.Contains(print, ".provenance{display:none") {
		t.Fatal("print contract broadly exposes hidden controls or hides provenance")
	}
}

// TestTodo_WEB_024_Conformance proves brand tokens cannot override protected
// high-contrast semantics and print removes only interactive action surfaces.
func TestTodo_WEB_024_Conformance(t *testing.T) {
	css := tokens.WorkspaceCSS()
	contrast := mediaBlock(t, css, "@media (prefers-contrast:more)")
	forced := mediaBlock(t, css, "@media (forced-colors:active)")
	for name, block := range map[string]string{"prefers contrast": contrast, "forced colors": forced} {
		if strings.Contains(block, "var(--color-") {
			t.Errorf("%s lets customer colour tokens redefine protected semantics", name)
		}
		for _, systemColor := range []string{"Canvas", "CanvasText", "Highlight"} {
			if !strings.Contains(block, systemColor) {
				t.Errorf("%s omits system colour %s", name, systemColor)
			}
		}
	}
	print := mediaBlock(t, css, "@media print")
	if !strings.Contains(print, "nav,.skip-link,.actions,.interactive-only,[data-print=\"interactive-only\"]{display:none!important;}") {
		t.Fatal("print hiding is not bounded to navigation/actions explicitly marked interactive-only")
	}
	if strings.Contains(print, "*::before") || strings.Contains(print, "*::after") {
		t.Fatal("print contract must not globally rewrite pseudo-element evidence")
	}
}

func TestTodo_WEB_024_Security(t *testing.T) {
	css := strings.ToLower(tokens.WorkspaceCSS())
	for _, forbidden := range []string{"url(", "expression(", "javascript:", "position:fixed", "position:sticky"} {
		if strings.Contains(css, forbidden) {
			t.Errorf("unsafe or viewport-capturing CSS %q", forbidden)
		}
	}
	for name, doc := range renderedDocuments(t) {
		for _, masked := range testdata.MaskedNeedles() {
			if strings.Contains(doc, masked) {
				t.Errorf("%s printed unauthorized value %q", name, masked)
			}
		}
	}
}

func TestTodo_WEB_024_RTL(t *testing.T) {
	css := modeCSS(t)
	for _, physical := range []string{"margin-left", "margin-right", "padding-left", "padding-right", "border-left", "border-right", "left:", "right:"} {
		if strings.Contains(css, physical) {
			t.Errorf("mode contract uses physical property %q", physical)
		}
	}
}

func TestTodo_WEB_024_I18n(t *testing.T) {
	for name, doc := range renderedDocuments(t) {
		root := parseDocument(t, doc)
		if htmlNode := firstElement(root, "html"); attr(htmlNode, "lang") != "en" {
			t.Errorf("%s document does not identify its content language", name)
		}
		for _, tm := range elements(root, "time") {
			if value := attr(tm, "datetime"); value == "" || !strings.Contains(value, "T") {
				t.Errorf("%s time lacks a machine-readable instant: %q", name, value)
			}
		}
	}
}

func TestTodo_WEB_024_Performance(t *testing.T) {
	css := tokens.WorkspaceCSS()
	if len(css) > 24000 {
		t.Fatalf("WorkspaceCSS grew to %d bytes; keep shared mode CSS bounded", len(css))
	}
	allocs := testing.AllocsPerRun(100, func() {
		_ = tokens.WorkspaceCSS()
	})
	if allocs > 200 {
		t.Fatalf("WorkspaceCSS uses %.0f allocations per call; keep shared mode CSS bounded", allocs)
	}
}

func BenchmarkWorkspaceModeContracts(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = tokens.WorkspaceCSS()
	}
}

func renderedDocuments(t *testing.T) map[string]string {
	t.Helper()
	fixture := testdata.PromotionFixture()
	ssrDoc, err := ssr.Render(fixture)
	if err != nil {
		t.Fatalf("render SSR: %v", err)
	}
	gwcDoc, err := gwc.Document(fixture)
	if err != nil {
		t.Fatalf("render GWC: %v", err)
	}
	return map[string]string{"ssr": ssrDoc, "gwc": gwcDoc}
}

func modeCSS(t *testing.T) string {
	t.Helper()
	css := tokens.WorkspaceCSS()
	return mediaBlock(t, css, "@media (prefers-contrast:more)") +
		mediaBlock(t, css, "@media (forced-colors:active)") +
		mediaBlock(t, css, "@media print")
}

func mediaBlock(t *testing.T, css, query string) string {
	t.Helper()
	start := strings.Index(css, query)
	if start < 0 {
		t.Fatalf("missing media query %q", query)
	}
	open := strings.IndexByte(css[start:], '{')
	if open < 0 {
		t.Fatalf("media query %q has no block", query)
	}
	open += start
	depth := 0
	for i := open; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return css[start : i+1]
			}
		}
	}
	t.Fatalf("unterminated media query %q", query)
	return ""
}

func parseDocument(t *testing.T, doc string) *html.Node {
	t.Helper()
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("parse rendered document: %v", err)
	}
	return root
}

func walk(n *html.Node, visit func(*html.Node)) {
	visit(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		walk(child, visit)
	}
}

func nodesWithClass(root *html.Node, class string) []*html.Node {
	var found []*html.Node
	walk(root, func(n *html.Node) {
		if n.Type == html.ElementNode && hasClass(n, class) {
			found = append(found, n)
		}
	})
	return found
}

func countClass(root *html.Node, class string) int { return len(nodesWithClass(root, class)) }

func firstClass(root *html.Node, class string) *html.Node {
	nodes := nodesWithClass(root, class)
	if len(nodes) == 0 {
		return nil
	}
	return nodes[0]
}

func hasClass(n *html.Node, class string) bool {
	for _, item := range strings.Fields(attr(n, "class")) {
		if item == class {
			return true
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func elements(root *html.Node, tag string) []*html.Node {
	var found []*html.Node
	walk(root, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag {
			found = append(found, n)
		}
	})
	return found
}

func firstElement(root *html.Node, tag string) *html.Node {
	nodes := elements(root, tag)
	if len(nodes) == 0 {
		return nil
	}
	return nodes[0]
}

func textOf(n *html.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	walk(n, func(child *html.Node) {
		if child.Type == html.TextNode {
			b.WriteString(child.Data)
		}
	})
	return b.String()
}

func assertFormValues(t *testing.T, renderer string, root *html.Node) {
	t.Helper()
	want := map[string]string{
		"proposedJobTitle":     "Registered Nurse III",
		"proposedCompensation": "$98,000.00",
		"effectiveDate":        "2026-10-01",
	}
	for _, input := range elements(root, "input") {
		if expected, ok := want[attr(input, "id")]; ok {
			if got := attr(input, "value"); got != expected {
				t.Errorf("%s input %s value = %q, want %q", renderer, attr(input, "id"), got, expected)
			}
			delete(want, attr(input, "id"))
		}
	}
	if len(want) != 0 {
		t.Errorf("%s omitted valued inputs: %v", renderer, want)
	}
	textareas := elements(root, "textarea")
	if len(textareas) != 1 || !strings.Contains(textOf(textareas[0]), "retention risk") {
		t.Errorf("%s textarea does not retain its printable value", renderer)
	}
}

func assertEvidence(t *testing.T, renderer string, root *html.Node) {
	t.Helper()
	provenance := firstClass(root, "provenance")
	got := strings.Join(strings.Fields(textOf(provenance)), " ")
	for _, evidence := range []string{"Source: hcm-next", "Capability people.promote@v1", "As of"} {
		if !strings.Contains(got, evidence) {
			t.Errorf("%s provenance omits %q: %q", renderer, evidence, got)
		}
	}
	if len(elements(provenance, "time")) != 1 || len(nodesWithClass(root, "simulation-generated")) != 1 {
		t.Errorf("%s omits source/simulation timestamp evidence", renderer)
	}
}

func evidenceSignature(root *html.Node) string {
	parts := []string{
		strings.Join(strings.Fields(textOf(firstClass(root, "status-banner"))), " "),
		strings.Join(strings.Fields(textOf(firstClass(root, "simulation-generated"))), " "),
		attr(firstElement(firstClass(root, "simulation-generated"), "time"), "datetime"),
		strings.Join(strings.Fields(textOf(firstClass(root, "provenance"))), " "),
		attr(firstElement(firstClass(root, "provenance"), "time"), "datetime"),
	}
	return strings.Join(parts, "|")
}
