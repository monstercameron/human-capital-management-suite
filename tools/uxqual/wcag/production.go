package wcag

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

// InputMode is an input modality covered by the production qualification
// matrix.  The rendered contract is deliberately modality-neutral: native
// controls and their names/states are what make keyboard, touch, voice and
// switch access equivalent.  Device-driver runs remain evidence, not a
// second implementation of the UI contract.
type InputMode string

const (
	InputModeKeyboard InputMode = "keyboard"
	InputModeTouch    InputMode = "touch"
	InputModeVoice    InputMode = "voice"
	InputModeSwitch   InputMode = "switch"
)

// SupportedInputModes returns a fresh copy of the modes required by the
// production support matrix.
func SupportedInputModes() []InputMode {
	return []InputMode{InputModeKeyboard, InputModeTouch, InputModeVoice, InputModeSwitch}
}

// CheckLocale verifies that a full document carries the resolved locale and
// direction at the document boundary, and that no unresolved catalog marker
// can be announced by a browser or assistive technology.
func CheckLocale(doc, language, direction string) qual.CriterionResult {
	const name = "Locale and direction"
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return qual.CriterionResult{Name: name, Detail: "parse error: " + err.Error()}
	}
	var document *html.Node
	var unresolved bool
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.DataAtom == atom.Html && document == nil {
			document = node
		}
		if node.Type == html.TextNode && strings.Contains(node.Data, "⟦") {
			unresolved = true
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if document == nil {
		return qual.CriterionResult{Name: name, Detail: "missing html element"}
	}
	attrs := attrs(document)
	if strings.TrimSpace(language) == "" || attrs["lang"] != language {
		return qual.CriterionResult{Name: name, Detail: fmt.Sprintf("html lang=%q, want %q", attrs["lang"], language)}
	}
	if direction != "ltr" && direction != "rtl" {
		return qual.CriterionResult{Name: name, Detail: fmt.Sprintf("unsupported expected direction %q", direction)}
	}
	if attrs["dir"] != direction {
		return qual.CriterionResult{Name: name, Detail: fmt.Sprintf("html dir=%q, want %q", attrs["dir"], direction)}
	}
	if unresolved {
		return qual.CriterionResult{Name: name, Detail: "unresolved catalog marker is exposed in document text"}
	}
	return qual.CriterionResult{Name: name, Pass: true, Detail: "resolved language, direction and catalog text are explicit"}
}

// CheckFocusIndicator verifies that the browser-visible focus boundary is
// present in emitted CSS.  A markup-only check cannot catch a theme that
// accidentally removes the indicator for every control.
func CheckFocusIndicator(doc string) qual.CriterionResult {
	const name = "Visible focus indicator"
	css := qual.ExtractInlineCSS(doc)
	if !strings.Contains(css, ":focus-visible") {
		return qual.CriterionResult{Name: name, Detail: "stylesheet has no :focus-visible rule"}
	}
	focusRule := regexp.MustCompile(`(?is):focus-visible[^{}]*\{[^}]*\}`)
	normalRule := regexp.MustCompile(`(?is)(?:^|})[^{}]*:focus-visible[^{}]*\{[^}]*\}`).FindString(css)
	rule := focusRule.FindString(normalRule)
	if rule == "" || (!strings.Contains(rule, "outline") && !strings.Contains(rule, "box-shadow")) {
		return qual.CriterionResult{Name: name, Detail: "focus-visible rule does not paint an outline or ring"}
	}
	if !strings.Contains(css, "@media (forced-colors:active)") {
		return qual.CriterionResult{Name: name, Detail: "focus stylesheet lacks forced-colors fallback"}
	}
	return qual.CriterionResult{Name: name, Pass: true, Detail: "focus-visible paints a ring and has a forced-colors fallback"}
}

// CheckInputModes qualifies the shared HTML contract used by pointer and
// non-pointer drivers.  It rejects click-only pseudo-controls, malformed
// tabindex values, and unsupported inputmode values.  Native controls remain
// the source of keyboard/voice/switch semantics for every supported mode.
func CheckInputModes(doc string) qual.CriterionResult {
	const name = "Input-mode compatibility"
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return qual.CriterionResult{Name: name, Detail: "parse error: " + err.Error()}
	}
	validModes := map[string]bool{"none": true, "text": true, "decimal": true, "numeric": true, "tel": true, "search": true, "email": true, "url": true}
	var problems []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			a := attrs(node)
			if mode := strings.ToLower(strings.TrimSpace(a["inputmode"])); mode != "" && !validModes[mode] {
				problems = append(problems, fmt.Sprintf("<%s id=%q> has unsupported inputmode %q", node.Data, a["id"], mode))
			}
			if raw, ok := a["tabindex"]; ok {
				if strings.TrimSpace(raw) == "" {
					problems = append(problems, fmt.Sprintf("<%s id=%q> has empty tabindex", node.Data, a["id"]))
				} else if _, parseErr := parseTabIndex(raw); parseErr != nil {
					problems = append(problems, fmt.Sprintf("<%s id=%q> has invalid tabindex %q", node.Data, a["id"], raw))
				}
			}
			if a["onclick"] != "" && !nativeInteractive(node) {
				role := strings.ToLower(strings.TrimSpace(a["role"]))
				if (role != "button" && role != "link") || strings.TrimSpace(a["tabindex"]) == "" {
					problems = append(problems, fmt.Sprintf("click-only <%s id=%q> has no keyboard entry", node.Data, a["id"]))
				} else if strings.TrimSpace(a["onkeydown"]) == "" && strings.TrimSpace(a["onkeyup"]) == "" {
					problems = append(problems, fmt.Sprintf("click-only <%s id=%q> has no keyboard activation handler", node.Data, a["id"]))
				}
			}
			if a["role"] == "button" && !nativeInteractive(node) {
				value, parseErr := parseTabIndex(a["tabindex"])
				if parseErr != nil || value < 0 {
					problems = append(problems, fmt.Sprintf("role=button <%s id=%q> is not keyboard-focusable", node.Data, a["id"]))
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if len(problems) > 0 {
		return qual.CriterionResult{Name: name, Detail: strings.Join(problems, "; ")}
	}
	return qual.CriterionResult{Name: name, Pass: true, Detail: "native controls and declared input modes are available to keyboard, touch, voice and switch drivers"}
}

// CheckMotionContract checks the relationship between motion declarations and
// the user preference override. Merely mentioning prefers-reduced-motion in a
// stylesheet is insufficient when the media block does not actually disable
// animation, transition, or smooth scrolling.
func CheckMotionContract(doc string) qual.CriterionResult {
	const name = "Reduced-motion contract"
	css := qual.ExtractInlineCSS(doc)
	motion := regexp.MustCompile(`(?i)(?:animation(?:-name|-duration|-delay)?\s*:|transition(?:-duration|-delay)?\s*:|scroll-behavior\s*:)`).MatchString(css)
	if !motion {
		return qual.CriterionResult{Name: name, Pass: true, Detail: "document declares no motion or smooth-scroll behavior"}
	}
	reduce := regexp.MustCompile(`(?is)@media\s*\([^)]*prefers-reduced-motion\s*:\s*reduce\s*\)\s*\{.*?\}`).FindString(css)
	if reduce == "" {
		return qual.CriterionResult{Name: name, Detail: "motion declaration has no prefers-reduced-motion:reduce override"}
	}
	if !regexp.MustCompile(`(?i)(animation\s*:\s*none|animation-duration\s*:\s*(?:0|\.0?1ms)|transition\s*:\s*none|transition-duration\s*:\s*(?:0|\.0?1ms)|scroll-behavior\s*:\s*auto)`).MatchString(reduce) {
		return qual.CriterionResult{Name: name, Detail: "reduced-motion media block does not disable motion"}
	}
	return qual.CriterionResult{Name: name, Pass: true, Detail: "motion declarations have a reduced-motion disablement policy"}
}

// CheckScreenReaderContract adds production-surface checks to the generic
// qualification fixture: duplicate IDs and dangling ARIA references are
// failures even when the page happens to contain a live region.
func CheckScreenReaderContract(doc string) qual.CriterionResult {
	const name = "Screen-reader names, states and references"
	base := qual.CheckScreenReaderSemantics(doc)
	if !base.Pass {
		return qual.CriterionResult{Name: name, Detail: base.Detail}
	}
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return qual.CriterionResult{Name: name, Detail: "parse error: " + err.Error()}
	}
	ids := map[string]int{}
	var references []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			a := attrs(node)
			if id := strings.TrimSpace(a["id"]); id != "" {
				ids[id]++
			}
			for _, key := range []string{"aria-labelledby", "aria-describedby", "aria-errormessage", "aria-controls"} {
				references = append(references, strings.Fields(a[key])...)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	for id, count := range ids {
		if count != 1 {
			return qual.CriterionResult{Name: name, Detail: fmt.Sprintf("id %q occurs %d times", id, count)}
		}
	}
	for _, id := range references {
		if ids[id] != 1 {
			return qual.CriterionResult{Name: name, Detail: fmt.Sprintf("ARIA reference %q does not resolve uniquely", id)}
		}
	}
	return qual.CriterionResult{Name: name, Pass: true, Detail: base.Detail + "; IDs and ARIA references resolve uniquely"}
}

// CheckFocusContract is the production-strength focus gate. It validates
// every natively focusable control (including links and summaries), not only
// form inputs, and rejects duplicate IDs, dangling label references, and
// positive or malformed tabindex values.
func CheckFocusContract(doc string) qual.CriterionResult {
	const name = "Production focus order and names"
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return qual.CriterionResult{Name: name, Detail: "parse error: " + err.Error()}
	}
	labels := map[string]string{}
	ids := map[string]int{}
	var controls []*html.Node
	var problems []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			a := attrs(node)
			if id := strings.TrimSpace(a["id"]); id != "" {
				ids[id]++
			}
			if node.DataAtom == atom.Label && strings.TrimSpace(a["for"]) != "" {
				labels[a["for"]] = strings.TrimSpace(nodeText(node))
			}
			if focusableElement(node) {
				controls = append(controls, node)
			}
			if raw, ok := a["tabindex"]; ok {
				value, parseErr := parseTabIndex(raw)
				if parseErr != nil {
					problems = append(problems, fmt.Sprintf("invalid tabindex %q on <%s id=%q>", raw, node.Data, a["id"]))
				} else if value > 0 {
					problems = append(problems, fmt.Sprintf("positive tabindex %d on <%s id=%q>", value, node.Data, a["id"]))
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	for id, count := range ids {
		if count != 1 {
			problems = append(problems, fmt.Sprintf("id %q occurs %d times", id, count))
		}
	}
	for _, node := range controls {
		a := attrs(node)
		if !hasAccessibleName(node, labels, ids) {
			problems = append(problems, fmt.Sprintf("unnamed <%s id=%q>", node.Data, a["id"]))
		}
		for _, ref := range strings.Fields(a["aria-labelledby"]) {
			if ids[ref] != 1 {
				problems = append(problems, fmt.Sprintf("aria-labelledby reference %q does not resolve uniquely", ref))
			}
		}
	}
	if len(problems) > 0 {
		return qual.CriterionResult{Name: name, Detail: strings.Join(problems, "; ")}
	}
	return qual.CriterionResult{Name: name, Pass: true, Detail: fmt.Sprintf("%d focusable controls are named and preserve DOM order", len(controls))}
}

// CheckInputModeCompatibility applies the same semantic contract for one
// supported driver.  This makes the matrix explicit without pretending that
// a static HTML check is evidence of a particular operating-system driver.
func CheckInputModeCompatibility(doc string, mode InputMode) qual.CriterionResult {
	const name = "Input-mode compatibility"
	for _, supported := range SupportedInputModes() {
		if mode == supported {
			return CheckInputModes(doc)
		}
	}
	return qual.CriterionResult{Name: name, Detail: fmt.Sprintf("unsupported input mode %q", mode)}
}

// SurfaceScore is the production scorecard. Locale and modality are passed
// explicitly because they cannot be inferred safely from arbitrary markup.
func SurfaceScore(doc, language, direction string) []qual.CriterionResult {
	// Score is intentionally the UX-003 promotion fixture scorecard and has
	// fixture-specific authorization/masking assertions. Production pages
	// have their own server-resolved action set, so the reusable surface gate
	// contains only document-level WCAG criteria.
	return []qual.CriterionResult{
		qual.CheckKeyboard(doc),
		CheckScreenReaderContract(doc),
		qual.CheckContrastAA(),
		CheckZoomReflow(doc),
		CheckReducedMotion(doc),
		CheckFocusIndicator(doc),
		CheckFocusContract(doc),
		CheckLocale(doc, language, direction),
		CheckInputModes(doc),
		CheckMotionContract(doc),
	}
}

func nativeInteractive(node *html.Node) bool {
	switch node.DataAtom {
	case atom.A, atom.Button, atom.Input, atom.Select, atom.Textarea, atom.Summary:
		return true
	default:
		return false
	}
}

func focusableElement(node *html.Node) bool {
	if node.Type != html.ElementNode {
		return false
	}
	a := attrs(node)
	if a["aria-hidden"] == "true" || strings.TrimSpace(a["tabindex"]) == "-1" {
		return false
	}
	switch node.DataAtom {
	case atom.A:
		return strings.TrimSpace(a["href"]) != ""
	case atom.Button, atom.Select, atom.Textarea, atom.Summary:
		return true
	case atom.Input:
		return strings.ToLower(a["type"]) != "hidden"
	default:
		return strings.TrimSpace(a["tabindex"]) != ""
	}
}

func hasAccessibleName(node *html.Node, labels map[string]string, ids map[string]int) bool {
	a := attrs(node)
	if strings.TrimSpace(a["aria-label"]) != "" {
		return true
	}
	if refs := strings.Fields(a["aria-labelledby"]); len(refs) > 0 {
		for _, ref := range refs {
			if ids[ref] != 1 {
				return false
			}
		}
		return true
	}
	if id := strings.TrimSpace(a["id"]); id != "" && strings.TrimSpace(labels[id]) != "" {
		return true
	}
	if strings.TrimSpace(visibleNodeText(node)) != "" {
		return true
	}
	return node.DataAtom == atom.Input && (a["type"] == "submit" || a["type"] == "button") && strings.TrimSpace(a["value"]) != ""
}

func visibleNodeText(node *html.Node) string {
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.ElementNode && current != node && attrs(current)["aria-hidden"] == "true" {
			return
		}
		if current.Type == html.TextNode {
			text.WriteString(current.Data)
			text.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return text.String()
}

func parseTabIndex(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty")
	}
	sign := 1
	if value[0] == '+' || value[0] == '-' {
		if value[0] == '-' {
			sign = -1
		}
		value = value[1:]
	}
	if value == "" {
		return 0, fmt.Errorf("missing digits")
	}
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not an integer")
		}
		n = n*10 + int(r-'0')
	}
	return sign * n, nil
}
