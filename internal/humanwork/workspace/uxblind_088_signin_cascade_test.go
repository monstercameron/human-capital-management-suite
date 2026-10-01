package workspace

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// UXBLIND-086 asserted that certain colour strings appeared in the sheet, and
// passed while the page stayed unreadable: the dark surface rules were emitted
// before the light base rules of the same specificity, so the light surface won
// the cascade and the dark-mode text sat on it. These tests therefore resolve
// the cascade the way a browser does (media, specificity, source order,
// inheritance) over the stylesheet the page actually serves, and measure the
// contrast of what wins.

type cssNode struct {
	tag     string
	classes []string
	attrs   map[string]string
}

type cssDecl struct {
	spec  [3]int
	order int
	value string
}

type cssRule struct {
	selector string
	dark     bool
	decls    map[string]string
}

var (
	cssMediaOpen  = regexp.MustCompile(`^@media\s*([^{]*)\{`)
	cssCompoundRe = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9]*)?((?:\.[\w-]+|\[[^\]]+\])*)$`)
	cssPartRe     = regexp.MustCompile(`\.([\w-]+)|\[([\w-]+)(?:="([^"]*)")?\]`)
)

// parseCSS flattens a stylesheet into ordered rules. Only the dark colour
// scheme query is tracked; any other @media (width queries) is skipped, which
// models a desktop viewport wide enough for neither narrow rule to apply.
func parseCSS(t *testing.T, sheet string) []cssRule {
	t.Helper()
	var rules []cssRule
	var walk func(src string, dark, skip bool)
	walk = func(src string, dark, skip bool) {
		for {
			src = strings.TrimSpace(src)
			if src == "" {
				return
			}
			if m := cssMediaOpen.FindStringSubmatch(src); m != nil {
				depth, end := 1, len(m[0])
				for end < len(src) && depth > 0 {
					switch src[end] {
					case '{':
						depth++
					case '}':
						depth--
					}
					end++
				}
				query := strings.ReplaceAll(strings.TrimSpace(m[1]), " ", "")
				inner := src[len(m[0]) : end-1]
				if query == "(prefers-color-scheme:dark)" {
					walk(inner, true, skip)
				} else {
					walk(inner, dark, true)
				}
				src = src[end:]
				continue
			}
			open := strings.Index(src, "{")
			closeAt := strings.Index(src, "}")
			if open < 0 || closeAt < open {
				return
			}
			selector, body := strings.TrimSpace(src[:open]), src[open+1:closeAt]
			src = src[closeAt+1:]
			if skip {
				continue
			}
			decls := map[string]string{}
			for _, decl := range strings.Split(body, ";") {
				if name, value, ok := strings.Cut(decl, ":"); ok {
					decls[strings.TrimSpace(name)] = strings.TrimSpace(value)
				}
			}
			rules = append(rules, cssRule{selector: selector, dark: dark, decls: decls})
		}
	}
	walk(sheet, false, false)
	return rules
}

func (n cssNode) matches(compound string) (bool, [3]int) {
	m := cssCompoundRe.FindStringSubmatch(compound)
	if m == nil {
		return false, [3]int{}
	}
	var spec [3]int
	if m[1] != "" {
		if n.tag != m[1] {
			return false, spec
		}
		spec[2]++
	}
	for _, part := range cssPartRe.FindAllStringSubmatch(m[2], -1) {
		spec[1]++
		if part[1] != "" {
			found := false
			for _, class := range n.classes {
				found = found || class == part[1]
			}
			if !found {
				return false, spec
			}
			continue
		}
		value, has := n.attrs[part[2]]
		if !has || (strings.Contains(part[0], "=") && value != part[3]) {
			return false, spec
		}
	}
	return true, spec
}

// matchChain reports whether selector matches the last node of chain (body
// first) through descendant combinators, and the selector's specificity.
func matchChain(selector string, chain []cssNode) (bool, [3]int) {
	if strings.ContainsAny(selector, ">+~:") {
		return false, [3]int{}
	}
	parts := strings.Fields(selector)
	var total [3]int
	at := len(chain) - 1
	for i := len(parts) - 1; i >= 0; i-- {
		found := false
		for ; at >= 0; at-- {
			if ok, spec := chain[at].matches(parts[i]); ok {
				found = true
				for k := range total {
					total[k] += spec[k]
				}
				at--
				break
			}
			if i == len(parts)-1 {
				return false, total // the subject compound must match the element itself
			}
		}
		if !found {
			return false, total
		}
	}
	return true, total
}

func specGreater(a, b [3]int) bool {
	for k := range a {
		if a[k] != b[k] {
			return a[k] > b[k]
		}
	}
	return false
}

func cascade(rules []cssRule, chain []cssNode, dark bool, property string) (string, bool) {
	var best cssDecl
	have := false
	for order, rule := range rules {
		value, ok := rule.decls[property]
		if !ok || (rule.dark && !dark) {
			continue
		}
		for _, selector := range strings.Split(rule.selector, ",") {
			match, spec := matchChain(strings.TrimSpace(selector), chain)
			if !match {
				continue
			}
			// Later order wins a tie, so equal specificity replaces.
			if !have || !specGreater(best.spec, spec) {
				best, have = cssDecl{spec: spec, order: order, value: value}, true
			}
		}
	}
	return best.value, have
}

// computed resolves color (inherited) and the painted background (nearest
// ancestor with one) for the last node of chain.
func computed(t *testing.T, rules []cssRule, chain []cssNode, dark bool) (fg, bg string) {
	t.Helper()
	for end := len(chain); end > 0 && fg == ""; end-- {
		fg, _ = cascade(rules, chain[:end], dark, "color")
		fg = resolveVars(rules, dark, fg)
	}
	for end := len(chain); end > 0 && (bg == "" || bg == "transparent"); end-- {
		bg, _ = cascade(rules, chain[:end], dark, "background-color")
		bg = resolveVars(rules, dark, bg)
	}
	return normalizeHex(t, fg), normalizeHex(t, bg)
}

var cssVarRe = regexp.MustCompile(`^var\(\s*(--[\w-]+)\s*(?:,\s*([^)]*))?\)$`)

// resolveVars substitutes var(--x) from the :root custom properties in force
// for the mode (a dark :root declaration overrides the light one).
func resolveVars(rules []cssRule, dark bool, value string) string {
	for depth := 0; depth < 8; depth++ {
		m := cssVarRe.FindStringSubmatch(strings.TrimSpace(value))
		if m == nil {
			return value
		}
		next, found := "", false
		for _, rule := range rules {
			if rule.dark && !dark {
				continue
			}
			for _, selector := range strings.Split(rule.selector, ",") {
				if v, ok := rule.decls[m[1]]; ok && strings.TrimSpace(selector) == ":root" {
					next, found = v, true
				}
			}
		}
		if !found {
			next = m[2]
		}
		value = next
	}
	return value
}

func normalizeHex(t *testing.T, value string) string {
	t.Helper()
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) == 3 {
		value = string([]byte{value[0], value[0], value[1], value[1], value[2], value[2]})
	}
	if len(value) != 6 {
		t.Fatalf("unresolved colour %q", value)
	}
	return value
}

// styleOf extracts the served <style> element and body attributes from a page.
func styleOf(t *testing.T, page string) (sheet string, bodyAttrs map[string]string) {
	t.Helper()
	m := regexp.MustCompile(`(?s)<style>(.*?)</style>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("page has no <style>")
	}
	bodyAttrs = map[string]string{}
	if b := regexp.MustCompile(`<body([^>]*)>`).FindStringSubmatch(page); b != nil {
		for _, a := range regexp.MustCompile(`([\w-]+)="([^"]*)"`).FindAllStringSubmatch(b[1], -1) {
			bodyAttrs[a[1]] = a[2]
		}
	}
	return m[1], bodyAttrs
}

type signinTarget struct {
	name    string
	chain   func(body map[string]string) []cssNode
	present string // markup the rendered page must contain
}

func signinTargets() []signinTarget {
	body := func(attrs map[string]string) cssNode { return cssNode{tag: "body", attrs: attrs} }
	card := cssNode{tag: "section", classes: []string{"login-card"}}
	span := func(class string) cssNode { return cssNode{tag: "span", classes: []string{class}} }
	option := func(selected bool, leaf cssNode) func(map[string]string) []cssNode {
		return func(attrs map[string]string) []cssNode {
			node := cssNode{tag: "a", classes: []string{"company-option"}, attrs: map[string]string{}}
			if selected {
				node.attrs["aria-current"] = "true"
			}
			return []cssNode{body(attrs), card, node, leaf}
		}
	}
	persona := func(leaf cssNode) func(map[string]string) []cssNode {
		return func(attrs map[string]string) []cssNode {
			return []cssNode{body(attrs), card, {tag: "form", classes: []string{"persona"}}, leaf}
		}
	}
	var targets []signinTarget
	for _, selected := range []bool{false, true} {
		state := map[bool]string{false: "unselected", true: "selected"}[selected]
		targets = append(targets,
			signinTarget{"company name " + state, option(selected, span("company-name")), `class="company-name"`},
			signinTarget{"company description " + state, option(selected, span("company-description")), `class="company-description"`},
			signinTarget{"company headcount " + state, option(selected, span("company-headcount")), `class="company-headcount"`},
		)
	}
	targets = append(targets,
		signinTarget{"persona name", persona(cssNode{tag: "strong"}), `<strong>`},
		signinTarget{"persona description", persona(cssNode{tag: "span"}), `class="persona"`},
		signinTarget{"persona access", persona(span("persona-access")), `class="persona-access"`},
		signinTarget{"directory intro", func(attrs map[string]string) []cssNode {
			return []cssNode{body(attrs), card, {tag: "section", classes: []string{"directory"}}, {tag: "p", classes: []string{"login-intro"}}}
		}, `class="directory"`},
		signinTarget{"card intro", func(attrs map[string]string) []cssNode {
			return []cssNode{body(attrs), card, {tag: "p", classes: []string{"login-intro"}}}
		}, `class="login-intro"`},
	)
	return targets
}

func assertSigninContrast(t *testing.T, label, page string) {
	t.Helper()
	sheet, attrs := styleOf(t, page)
	rules := parseCSS(t, sheet)
	results := map[string]float64{}
	for _, target := range signinTargets() {
		if !strings.Contains(page, target.present) {
			t.Fatalf("%s page lacks markup for %s (%s)", label, target.name, target.present)
		}
		for _, dark := range []bool{false, true} {
			fg, bg := computed(t, rules, target.chain(attrs), dark)
			mode := map[bool]string{false: "light", true: "dark"}[dark]
			results[target.name+" "+mode+" #"+fg+" on #"+bg] = contrastRatio(fg, bg)
		}
	}
	keys := make([]string, 0, len(results))
	for key := range results {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		t.Logf("%s: %s = %.2f", label, key, results[key])
		if results[key] < 4.5 {
			t.Errorf("%s: %s has contrast %.2f, want at least 4.5", label, key, results[key])
		}
	}
}

func signinPages(t *testing.T) map[string]string {
	t.Helper()
	h := newCompanyLoginHandler(t)
	pages := map[string]string{}
	for name, target := range map[string]string{
		"harborcare": PathLogin,
		"ironridge":  PathLogin + "?company=ironridge-demo",
	} {
		_, pages[name] = getCompanyLoginPage(t, h, target)
	}
	return pages
}

func TestTodo_UXBLIND_088(t *testing.T) {
	for name, page := range signinPages(t) {
		assertSigninContrast(t, name, page)
	}
}

// TestTodo_UXBLIND_088_Browser resolves the served page's own stylesheet for
// both colour schemes and requires each card's winning surface to belong to
// the mode: dark in dark mode, light in light mode. A contrasting pair
// somewhere in the sheet is not enough; the winner must be the pair used.
func TestTodo_UXBLIND_088_Browser(t *testing.T) {
	for name, page := range signinPages(t) {
		sheet, attrs := styleOf(t, page)
		rules := parseCSS(t, sheet)
		card := cssNode{tag: "section", classes: []string{"login-card"}}
		for _, dark := range []bool{false, true} {
			for _, class := range []string{"company-option", "persona", "login-card"} {
				chain := []cssNode{{tag: "body", attrs: attrs}, {tag: "div", classes: []string{class}}}
				if class != "login-card" {
					chain = []cssNode{{tag: "body", attrs: attrs}, card, {tag: "div", classes: []string{class}}}
				}
				_, bg := computed(t, rules, chain, dark)
				luminance := colorLuminance(bg)
				if dark && luminance > 0.1 {
					t.Errorf("%s dark: .%s surface #%s is light (luminance %.2f)", name, class, bg, luminance)
				}
				if !dark && luminance < 0.5 {
					t.Errorf("%s light: .%s surface #%s is dark (luminance %.2f)", name, class, bg, luminance)
				}
			}
		}
		assertSigninContrast(t, name, page)
	}
}

// TestTodo_UXBLIND_088_Regression pins the cascade defect itself, proving the
// resolver can fail, and covers the single-company page.
func TestTodo_UXBLIND_088_Regression(t *testing.T) {
	broken := `body{background-color:#111315;color:#f2f0ec;}` +
		`.persona{background-color:#fbfcfb;}@media (prefers-color-scheme:dark){.persona{background-color:#1b1e22;}}` +
		`.persona{background-color:#fbfcfb;}`
	chain := []cssNode{{tag: "body"}, {tag: "form", classes: []string{"persona"}}}
	if _, bg := computed(t, parseCSS(t, broken), chain, true); bg != "fbfcfb" {
		t.Fatalf("resolver did not reproduce the light-wins cascade, got #%s", bg)
	}

	page := getLoginPage(t, newDirectoryHandler(t), "")
	sheet, attrs := styleOf(t, page)
	if len(attrs) != 0 {
		t.Fatalf("single-company body gained attributes %v", attrs)
	}
	rules := parseCSS(t, sheet)
	name := []cssNode{{tag: "body"}, {tag: "section", classes: []string{"login-card"}}, {tag: "form", classes: []string{"persona"}}, {tag: "strong"}}
	for _, dark := range []bool{false, true} {
		fg, bg := computed(t, rules, name, dark)
		if ratio := contrastRatio(fg, bg); ratio < 4.5 {
			t.Errorf("single-company persona name dark=%v #%s on #%s = %.2f", dark, fg, bg, ratio)
		}
		if dark && colorLuminance(bg) > 0.1 {
			t.Errorf("single-company dark persona surface #%s is light", bg)
		}
	}

	// The company sheet ends with the dark rules so they follow every light one.
	company := loginCompanyStylesheet()
	lastLight := strings.LastIndex(company, `{background-color:#fdeee5;`)
	if firstDark := strings.Index(company[lastLight:], "@media (prefers-color-scheme:dark)"); firstDark < 0 {
		t.Error("no dark rule follows the last light company-option rule")
	}
}
