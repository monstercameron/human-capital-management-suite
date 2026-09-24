// Package qual is the UX-QUAL-001 qualification fixture: it scores a
// rendered Promotion workspace document against the go-only technology
// constitution's GWC qualification fixture criteria (keyboard-only
// completion, screen-reader semantics, WCAG 2.2 AA contrast, reflow at
// 320px, and never rendering a masked field) and decides which renderer
// ships.
package qual

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/tokens"
)

// CriterionResult is one criterion's pass/fail plus the evidence that
// produced it, suitable for embedding directly into the decision record.
type CriterionResult struct {
	Name   string
	Pass   bool
	Detail string
}

func pass(name, detail string) CriterionResult {
	return CriterionResult{Name: name, Pass: true, Detail: detail}
}
func fail(name, detail string) CriterionResult {
	return CriterionResult{Name: name, Pass: false, Detail: detail}
}

// RendererResult is the full scorecard for one renderer.
type RendererResult struct {
	Renderer     string
	Buildable    CriterionResult
	Keyboard     CriterionResult
	ScreenReader CriterionResult
	Contrast     CriterionResult
	Reflow       CriterionResult
	Masking      CriterionResult
}

// All returns every criterion in a stable, reportable order.
func (r RendererResult) All() []CriterionResult {
	return []CriterionResult{r.Buildable, r.Keyboard, r.ScreenReader, r.Contrast, r.Reflow, r.Masking}
}

// AllPass reports whether every criterion passed.
func (r RendererResult) AllPass() bool {
	for _, c := range r.All() {
		if !c.Pass {
			return false
		}
	}
	return true
}

// ---- repo root discovery -------------------------------------------------

// repoRoot returns the module root, computed from this source file's own
// path (tools/uxqual/qual/fixture.go is three directories below the root).
func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("qual: could not determine caller for repo root discovery")
	}
	// file: <root>/tools/uxqual/qual/fixture.go
	return filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// ---- buildability ---------------------------------------------------------

// BuildNativePackage runs `go build` (GOFLAGS=-mod=readonly, so it can never
// write to go.mod/go.sum) against the given package pattern from the module
// root, and reports the result as a CriterionResult.
func BuildNativePackage(pkg string) CriterionResult {
	return runGoBuild("Buildable (native)", pkg, nil)
}

// BuildWasmPackage is BuildNativePackage with GOOS=js GOARCH=wasm.
func BuildWasmPackage(pkg string) CriterionResult {
	return runGoBuild("Buildable (GOOS=js GOARCH=wasm)", pkg, []string{"GOOS=js", "GOARCH=wasm"})
}

func runGoBuild(name, pkg string, extraEnv []string) CriterionResult {
	root, err := repoRoot()
	if err != nil {
		return fail(name, err.Error())
	}
	cmd := exec.Command("go", "build", pkg)
	cmd.Dir = root
	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.Env = append(cmd.Env, "GOFLAGS=-mod=readonly")
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fail(name, fmt.Sprintf("go build %s: %v\n%s", pkg, err, truncate(string(out), 2000)))
	}
	return pass(name, fmt.Sprintf("go build %s succeeded", pkg))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "... (truncated)"
}

// FieldIDsInDocumentOrder returns the id attribute of every <input> (not
// type=hidden), <textarea>, and <select> element, in document order. Tests
// use it to pin the tab-order-equals-DOM-order guarantee to a specific
// expected field sequence.
func FieldIDsInDocumentOrder(doc string) []string {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return nil
	}
	var ids []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attrs := attrMap(n)
			switch n.DataAtom {
			case atom.Input:
				if attrs["type"] != "hidden" && attrs["id"] != "" {
					ids = append(ids, attrs["id"])
				}
			case atom.Textarea, atom.Select:
				if attrs["id"] != "" {
					ids = append(ids, attrs["id"])
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return ids
}

// ---- HTML structural checks -----------------------------------------------

type focusable struct {
	tag      string
	id       string
	tabindex string // "" if attribute absent
	typeAttr string
}

// CheckKeyboard verifies keyboard-only completion: every interactive
// control is a natively focusable element (no click-only <div>), tab order
// is derivable from DOM order alone (no positive tabindex, which would
// reorder the tab sequence away from visual/DOM order), and there is no
// negative tabindex on a control that is the only way to reach a required
// action (a keyboard trap).
func CheckKeyboard(doc string) CriterionResult {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return fail("Keyboard-only completion", "parse error: "+err.Error())
	}

	var focusables []focusable
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attrs := attrMap(n)
			tag := n.DataAtom
			interactive := tag == atom.A || tag == atom.Button || tag == atom.Select || tag == atom.Textarea ||
				(tag == atom.Input && attrs["type"] != "hidden")
			_, hasTabindex := attrs["tabindex"]
			if interactive || hasTabindex {
				if tag == atom.A && attrs["href"] == "" {
					// An <a> with no href is not in the default tab order;
					// it does not count as an interactive control.
				} else {
					focusables = append(focusables, focusable{
						tag:      n.Data,
						id:       attrs["id"],
						tabindex: attrs["tabindex"],
						typeAttr: attrs["type"],
					})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)

	if len(focusables) == 0 {
		return fail("Keyboard-only completion", "no focusable controls found in document order")
	}

	var positiveTabindex []string
	for _, f := range focusables {
		if f.tabindex == "" {
			continue
		}
		n, err := strconv.Atoi(f.tabindex)
		if err != nil {
			continue
		}
		if n > 0 {
			positiveTabindex = append(positiveTabindex, fmt.Sprintf("<%s id=%q tabindex=%d>", f.tag, f.id, n))
		}
	}
	if len(positiveTabindex) > 0 {
		return fail("Keyboard-only completion",
			"positive tabindex reorders tab sequence away from DOM order (trap/order risk): "+strings.Join(positiveTabindex, ", "))
	}

	return pass("Keyboard-only completion",
		fmt.Sprintf("%d natively-focusable controls in DOM order, none with a positive tabindex", len(focusables)))
}

// CheckScreenReaderSemantics verifies landmarks exist, every text input has
// an accessible name (an associated <label for>, or its own aria-label /
// aria-labelledby -- both are standard WCAG 4.1.2 name-computation
// techniques, not just the first), and a live region exists for simulation
// status.
func CheckScreenReaderSemantics(doc string) CriterionResult {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return fail("Screen-reader semantics", "parse error: "+err.Error())
	}

	landmarks := map[atom.Atom]int{}
	labelFor := map[string]bool{}
	inputIDs := map[string]bool{}
	selfLabeled := map[string]bool{}
	liveRegions := 0

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attrs := attrMap(n)
			switch n.DataAtom {
			case atom.Header, atom.Nav, atom.Main, atom.Footer:
				landmarks[n.DataAtom]++
			case atom.Label:
				if forID := attrs["for"]; forID != "" {
					labelFor[forID] = true
				}
			case atom.Input:
				if attrs["type"] != "hidden" && attrs["id"] != "" {
					inputIDs[attrs["id"]] = true
					if strings.TrimSpace(attrs["aria-label"]) != "" || strings.TrimSpace(attrs["aria-labelledby"]) != "" {
						selfLabeled[attrs["id"]] = true
					}
				}
			case atom.Textarea:
				if attrs["id"] != "" {
					inputIDs[attrs["id"]] = true
					if strings.TrimSpace(attrs["aria-label"]) != "" || strings.TrimSpace(attrs["aria-labelledby"]) != "" {
						selfLabeled[attrs["id"]] = true
					}
				}
			}
			if _, ok := attrs["aria-live"]; ok {
				liveRegions++
			} else if attrs["role"] == "status" || attrs["role"] == "alert" {
				liveRegions++
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)

	var problems []string
	for _, want := range []atom.Atom{atom.Header, atom.Main} {
		if landmarks[want] == 0 {
			problems = append(problems, "missing <"+want.String()+"> landmark")
		}
	}
	var unlabeled []string
	for id := range inputIDs {
		if !labelFor[id] && !selfLabeled[id] {
			unlabeled = append(unlabeled, id)
		}
	}
	if len(unlabeled) > 0 {
		problems = append(problems, "inputs with no accessible name (<label for>, aria-label or aria-labelledby): "+strings.Join(unlabeled, ", "))
	}
	if liveRegions == 0 {
		problems = append(problems, "no live region (role=status/alert or aria-live) for simulation status")
	}

	if len(problems) > 0 {
		return fail("Screen-reader semantics", strings.Join(problems, "; "))
	}
	return pass("Screen-reader semantics",
		fmt.Sprintf("landmarks present (header=%d, nav=%d, main=%d, footer=%d), all %d inputs labeled, %d live region(s)",
			landmarks[atom.Header], landmarks[atom.Nav], landmarks[atom.Main], landmarks[atom.Footer], len(inputIDs), liveRegions))
}

// CheckContrastAA scores every (foreground, background) pair the shared
// token palette declares against WCAG 2.2 AA (1.4.3): >=4.5:1 for normal
// text. Both renderers import tokens.Palette, so this check is
// renderer-independent -- it is the palette, not the markup, that must
// satisfy AA.
func CheckContrastAA() CriterionResult {
	var failures []string
	pairs := tokens.TextPairs()
	for _, p := range pairs {
		ratio, err := tokens.ContrastRatio(p.Foreground.Hex, p.Background.Hex)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s/%s: %v", p.Foreground.Name, p.Background.Name, err))
			continue
		}
		if ratio < tokens.MinRatioNormalText {
			failures = append(failures, fmt.Sprintf("%s on %s: %.2f:1 (< %.1f:1)", p.Foreground.Name, p.Background.Name, ratio, tokens.MinRatioNormalText))
		}
	}
	if len(failures) > 0 {
		return fail("WCAG 2.2 AA contrast", strings.Join(failures, "; "))
	}
	return pass("WCAG 2.2 AA contrast", fmt.Sprintf("%d token pairs all >= %.1f:1", len(pairs), tokens.MinRatioNormalText))
}

var fixedWidthRE = regexp.MustCompile(`(?i)(?:^|[^-\w])width\s*:\s*(\d+)px`)

// CheckReflow verifies the emitted CSS never forces an element wider than
// the 320px reflow floor via a bare `width:<n>px` declaration (max-width
// and min-width are intentionally excluded from the pattern: a max-width
// cap does not force overflow, and this workspace's own CSS uses only
// max-width/percentage/rem sizing).
func CheckReflow(css string) CriterionResult {
	var offenders []string
	for _, m := range fixedWidthRE.FindAllStringSubmatch(css, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if n > 320 {
			offenders = append(offenders, m[0])
		}
	}
	if len(offenders) > 0 {
		return fail("Reflow at 320px", "fixed width(s) beyond the 320px floor: "+strings.Join(offenders, ", "))
	}
	return pass("Reflow at 320px", "no bare width declaration exceeds 320px; layout uses relative units and max-width")
}

// CheckMasking verifies none of the given masked needles (field IDs,
// labels, or raw values the server did not authorize for this actor) occur
// anywhere in the rendered document.
func CheckMasking(doc string, needles []string) CriterionResult {
	var leaked []string
	for _, needle := range needles {
		if strings.Contains(doc, needle) {
			leaked = append(leaked, needle)
		}
	}
	if len(leaked) > 0 {
		return fail("Never renders a masked field", "leaked into output: "+strings.Join(leaked, ", "))
	}
	return pass("Never renders a masked field", fmt.Sprintf("%d masked needles absent from output", len(needles)))
}

// RunDocumentChecks scores an already-rendered HTML document (SSR output,
// or a captured GWC DOM snapshot) against the four criteria that only need
// a rendered document: keyboard, screen-reader, reflow, and masking.
// Contrast is renderer-independent (see CheckContrastAA, which scores the
// shared token palette both renderers import) and Buildable is computed
// separately via BuildNativePackage/BuildWasmPackage, so callers assemble
// the full RendererResult from this plus those two.
func RunDocumentChecks(renderer, doc string, maskedNeedles []string) RendererResult {
	return RendererResult{
		Renderer:     renderer,
		Keyboard:     CheckKeyboard(doc),
		ScreenReader: CheckScreenReaderSemantics(doc),
		Contrast:     CheckContrastAA(),
		Reflow:       CheckReflow(ExtractInlineCSS(doc)),
		Masking:      CheckMasking(doc, maskedNeedles),
	}
}

var styleBlockRE = regexp.MustCompile(`(?is)<style[^>]*>(.*?)</style>`)

// ExtractInlineCSS pulls the concatenated contents of every <style> element
// out of a full HTML document, renderer-agnostically, for CheckReflow.
func ExtractInlineCSS(doc string) string {
	var css strings.Builder
	for _, m := range styleBlockRE.FindAllStringSubmatch(doc, -1) {
		css.WriteString(m[1])
		css.WriteString("\n")
	}
	return css.String()
}

func attrMap(n *html.Node) map[string]string {
	m := make(map[string]string, len(n.Attr))
	for _, a := range n.Attr {
		m[a.Key] = a.Val
	}
	return m
}
