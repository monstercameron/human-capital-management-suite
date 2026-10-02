package chatui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// s11Env describes the page a cascade is computed for: the viewport width (the
// chat container is as wide as the viewport at phone width, so the container
// queries use it too) and the input devices.
type s11Env struct {
	width  int
	coarse bool // pointer:coarse and hover:none
}

type s11Rule struct {
	conds    []string // enclosing @media / @container conditions, outermost first
	selector string
	body     string
	order    int
}

var s11RuleParts = regexp.MustCompile(`@(media|container|supports|layer)\s*([^{]*)$`)

// s11ParseRules flattens a stylesheet into rules, remembering the at-rule
// conditions around each one. Keyframes and font faces are skipped.
func s11ParseRules(css string) []s11Rule {
	var rules []s11Rule
	type frame struct{ cond string }
	var stack []frame
	start := 0
	order := 0
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '{':
			head := strings.TrimSpace(css[start:i])
			if strings.HasPrefix(head, "@") {
				cond := ""
				if m := s11RuleParts.FindStringSubmatch(head); m != nil {
					cond = m[1] + " " + strings.TrimSpace(m[2])
				} else {
					cond = "skip " + head
				}
				stack = append(stack, frame{cond})
				start = i + 1
				continue
			}
			end := strings.IndexByte(css[i:], '}')
			if end < 0 {
				return rules
			}
			body := css[i+1 : i+end]
			var conds []string
			for _, f := range stack {
				conds = append(conds, f.cond)
			}
			for _, sel := range s11SplitSelectors(head) {
				rules = append(rules, s11Rule{conds: conds, selector: sel, body: body, order: order})
			}
			order++
			i += end
			start = i + 1
		case '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			start = i + 1
		}
	}
	return rules
}

// s11SplitSelectors splits on commas that are not inside parentheses.
func s11SplitSelectors(list string) []string {
	var out []string
	depth, from := 0, 0
	for i, r := range list {
		switch r {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(list[from:i]))
				from = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(list[from:]))
}

var s11Feature = regexp.MustCompile(`\(\s*([a-z-]+)\s*:\s*([^)]+?)\s*\)`)

// s11CondActive reports whether one at-rule condition holds in env. Features it
// does not model make the rule inactive, so an unmodelled rule never wins a
// cascade silently; the callers list the rules they rely on.
func s11CondActive(cond string, env s11Env) bool {
	kind, query, _ := strings.Cut(cond, " ")
	switch kind {
	case "skip", "supports", "layer":
		return kind == "supports" || kind == "layer"
	case "container":
		// "chat (max-width:760px)" or "chatmain (max-width:560px)": the main column
		// of the conversation is narrower than the viewport by the open panels, but
		// at phone width the details panel overlays rather than shares the row.
		if query = strings.TrimSpace(query); !strings.HasPrefix(query, "(") {
			_, query, _ = strings.Cut(query, " ")
		}
	}
	for _, alt := range strings.Split(query, ",") {
		ok := true
		if strings.Contains(alt, "not ") || strings.Contains(alt, "only ") {
			ok = false
		}
		matches := s11Feature.FindAllStringSubmatch(alt, -1)
		if len(matches) == 0 {
			ok = false
		}
		for _, f := range matches {
			name, value := f[1], strings.TrimSpace(f[2])
			switch name {
			case "max-width":
				n, err := strconv.Atoi(strings.TrimSuffix(value, "px"))
				ok = ok && err == nil && env.width <= n
			case "min-width":
				n, err := strconv.Atoi(strings.TrimSuffix(value, "px"))
				ok = ok && err == nil && env.width >= n
			case "pointer":
				ok = ok && (value == "coarse") == env.coarse
			case "hover":
				ok = ok && (value == "none") == env.coarse
			default:
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func s11RuleActive(r s11Rule, env s11Env) bool {
	for _, c := range r.conds {
		if !s11CondActive(c, env) {
			return false
		}
	}
	return true
}

// s11Specificity counts (ids, classes+attributes+pseudo-classes, elements). A
// :not/:is/:has takes the specificity of its argument, as the standard says.
func s11Specificity(sel string) [3]int {
	var s [3]int
	for i := 0; i < len(sel); i++ {
		switch c := sel[i]; {
		case c == '#':
			s[0]++
			i = s11SkipIdent(sel, i+1)
		case c == '.':
			s[1]++
			i = s11SkipIdent(sel, i+1)
		case c == '[':
			s[1]++
			if j := strings.IndexByte(sel[i:], ']'); j > 0 {
				i += j
			}
		case c == ':':
			j := i + 1
			if j < len(sel) && sel[j] == ':' {
				s[2]++
				i = s11SkipIdent(sel, j+1)
				continue
			}
			end := s11SkipIdent(sel, j)
			name := sel[j : end+1]
			if end+1 < len(sel) && sel[end+1] == '(' {
				depth, k := 0, end+1
				for ; k < len(sel); k++ {
					if sel[k] == '(' {
						depth++
					} else if sel[k] == ')' {
						depth--
						if depth == 0 {
							break
						}
					}
				}
				if name == "where" {
					i = k
					continue
				}
				if name == "not" || name == "is" || name == "has" {
					arg := s11SplitSelectors(sel[end+2 : k])
					best := [3]int{}
					for _, a := range arg {
						if sp := s11Specificity(a); s11Less(best, sp) {
							best = sp
						}
					}
					for n := range s {
						s[n] += best[n]
					}
					i = k
					continue
				}
				s[1]++
				i = k
				continue
			}
			s[1]++
			i = end
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			s[2]++
			i = s11SkipIdent(sel, i)
		}
	}
	return s
}

func s11SkipIdent(sel string, from int) int {
	i := from
	for i < len(sel) {
		c := sel[i]
		if c == '-' || c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			i++
			continue
		}
		break
	}
	return i - 1
}

func s11Less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

var s11Decl = func(prop string) *regexp.Regexp {
	return regexp.MustCompile(`(?:^|;)\s*(?:` + prop + `)\s*:\s*([^;!]+?)\s*(!important)?\s*(?:;|$)`)
}

// s11Winner returns the value the cascade gives property (any of the names in
// the alternation, written as a regexp such as "min-height|min-block-size") for
// an element that the match predicate says each selector applies to.
func s11Winner(rules []s11Rule, env s11Env, prop string, match func(selector string) bool) (value string, selector string) {
	re := s11Decl(prop)
	var bestSpec [3]int
	bestOrder, bestImportant := -1, false
	for _, r := range rules {
		if !s11RuleActive(r, env) || !match(r.selector) {
			continue
		}
		found := re.FindAllStringSubmatch(r.body, -1)
		if len(found) == 0 {
			continue
		}
		last := found[len(found)-1]
		important := last[2] != ""
		spec := s11Specificity(r.selector)
		better := false
		switch {
		case important != bestImportant:
			better = important
		case bestOrder < 0:
			better = true
		case s11Less(bestSpec, spec):
			better = true
		case spec == bestSpec && r.order >= bestOrder:
			better = true
		}
		if better {
			value, selector, bestSpec, bestOrder, bestImportant = strings.TrimSpace(last[1]), r.selector, spec, r.order, important
		}
	}
	return value, selector
}

func TestS11CascadeHelper(t *testing.T) {
	css := `.a .b{color:red}.b{color:blue}@media(max-width:600px){.b{color:green}}@media(min-width:700px){.a .b{color:black}}`
	rules := s11ParseRules(css)
	any := func(string) bool { return true }
	if v, _ := s11Winner(rules, s11Env{width: 390}, "color", any); v != "red" {
		t.Fatalf("phone: %q", v)
	}
	if v, _ := s11Winner(rules, s11Env{width: 390}, "color", func(s string) bool { return s == ".b" }); v != "green" {
		t.Fatalf("media order: %q", v)
	}
	if v, _ := s11Winner(rules, s11Env{width: 390}, "color", func(s string) bool { return s == ".a .b" }); v != "red" {
		t.Fatalf("specificity: %q", v)
	}
	if v, _ := s11Winner(rules, s11Env{width: 800}, "color", any); v != "black" {
		t.Fatalf("wide: %q", v)
	}
	if got := s11Specificity(`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) .composer-draft`); got != [3]int{0, 6, 0} {
		t.Fatalf("specificity %v", got)
	}
}
