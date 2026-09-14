package productui

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/forms"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/wcag"
	xhtml "golang.org/x/net/html"
)

// TestFrontendUnitEveryPageRendersInEverySupportedLocale is deliberately
// registry-driven: adding a production route adds its unit coverage without
// relying on a maintainer to remember a second page list.
func TestFrontendUnitEveryPageRendersInEverySupportedLocale(t *testing.T) {
	for _, definition := range PageDefinitions() {
		definition := definition
		for _, code := range SupportedProductLocales() {
			code := code
			t.Run(string(definition.ID)+"/"+code, func(t *testing.T) {
				view := ApplyLocale(testView(definition.ID), ResolveProductLocale(code))
				doc, err := Render(view)
				if err != nil {
					t.Fatal(err)
				}
				locale := ResolveProductLocale(code)
				for _, want := range []string{
					`<html lang="` + locale.Resolved + `" dir="` + string(locale.Direction) + `"`,
					`data-hcm-catalog="product-ui.v1"`,
					`data-hcm-page-title="` + escapeTitle(ResolveDocumentPageTitle(view)) + `"`,
					`id="workspace-navigation"`,
					`id="main-content"`,
				} {
					if !strings.Contains(doc, want) {
						t.Errorf("document missing %q", want)
					}
				}
				if strings.Contains(doc, "⟦") {
					t.Error("document exposed an unresolved message key")
				}
			})
		}
	}
}

// TestFrontendI18nAccessibilityGateEveryRegisteredPage is the combined
// production matrix: its page inventory comes from the real route registry,
// its locales come from the real catalog registry, and accessibility scoring
// reuses the shared WCAG/qualification criteria used by other human routes.
func TestFrontendI18nAccessibilityGateEveryRegisteredPage(t *testing.T) {
	for _, definition := range PageDefinitions() {
		definition := definition
		for _, code := range SupportedProductLocales() {
			code := code
			t.Run(string(definition.ID)+"/"+code, func(t *testing.T) {
				view := ApplyLocale(testView(definition.ID), ResolveProductLocale(code))
				if definition.ID == PageWorkerIDs {
					view.WorkerIDValidation = ValidationState{SubmissionAttempted: true, Issues: []ValidationIssue{{FieldID: "worker-prefix", MessageKey: "validation.required"}}}
				}
				doc, err := Render(view)
				if err != nil {
					t.Fatal(err)
				}
				for _, result := range []qual.CriterionResult{
					qual.CheckKeyboard(doc),
					wcag.CheckReducedMotion(doc),
				} {
					if !result.Pass {
						t.Errorf("%s: %s", result.Name, result.Detail)
					}
				}
				if definition.ID == PageWorkerIDs {
					result := forms.CheckErrorAssociation(doc, []string{"worker-prefix"})
					if !result.Pass {
						t.Errorf("%s: %s", result.Name, result.Detail)
					}
				}
				for _, problem := range localizedDocumentIntegrity(doc, view.Locale) {
					t.Error(problem)
				}
			})
		}
	}
}

// localizedDocumentIntegrity parses references rather than looking for
// snippets. It catches duplicate IDs, dangling ARIA/fragment relationships,
// incorrect RTL metadata, and catalog keys exposed in human-facing text.
func localizedDocumentIntegrity(doc string, locale LocaleContext) []string {
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		return []string{"parse document: " + err.Error()}
	}
	ids := map[string]int{}
	labels := map[string]bool{}
	var controls []*xhtml.Node
	type reference struct{ owner, attribute, target string }
	var references []reference
	var problems []string
	knownKeys := map[string]bool{}
	for key := range productMessages[DefaultProductLocale] {
		knownKeys[key] = true
	}
	walkElements(root, func(node *xhtml.Node) {
		id := attr(node, "id")
		if id != "" {
			ids[id]++
		}
		if node.Data == "label" && attr(node, "for") != "" {
			labels[attr(node, "for")] = true
		}
		if isUserFacingControl(node) {
			controls = append(controls, node)
		}
		owner := "<" + node.Data + ">"
		if id != "" {
			owner = fmt.Sprintf("<%s id=%q>", node.Data, id)
		}
		for _, attribute := range []string{"aria-describedby", "aria-errormessage", "aria-labelledby"} {
			for _, target := range strings.Fields(attr(node, attribute)) {
				references = append(references, reference{owner, attribute, target})
			}
		}
		if href := attr(node, "href"); strings.HasPrefix(href, "#") && len(href) > 1 {
			references = append(references, reference{owner, "href", strings.TrimPrefix(href, "#")})
		}
		for _, attribute := range []string{"aria-label", "title", "placeholder", "alt"} {
			value := strings.TrimSpace(attr(node, attribute))
			if strings.Contains(value, "⟦") || knownKeys[value] {
				problems = append(problems, fmt.Sprintf("%s exposes untranslated %s %q", owner, attribute, value))
			}
		}
	})
	var walkText func(*xhtml.Node)
	walkText = func(node *xhtml.Node) {
		if node.Type == xhtml.TextNode {
			value := strings.TrimSpace(node.Data)
			if strings.Contains(value, "⟦") || knownKeys[value] {
				problems = append(problems, fmt.Sprintf("text exposes untranslated catalog key %q", value))
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walkText(child)
		}
	}
	walkText(root)
	for id, count := range ids {
		if count != 1 {
			problems = append(problems, fmt.Sprintf("id %q occurs %d times", id, count))
		}
	}
	for _, ref := range references {
		if ids[ref.target] != 1 {
			problems = append(problems, fmt.Sprintf("%s %s references %q, which occurs %d times", ref.owner, ref.attribute, ref.target, ids[ref.target]))
		}
	}
	for _, control := range controls {
		if !controlHasAccessibleName(control, labels, ids) {
			problems = append(problems, fmt.Sprintf("<%s id=%q> has no accessible name", control.Data, attr(control, "id")))
		}
	}
	if htmlNode := firstNamedElement(root, "html"); htmlNode == nil {
		problems = append(problems, "missing html element")
	} else {
		if got := attr(htmlNode, "lang"); got != locale.Resolved {
			problems = append(problems, fmt.Sprintf("html lang=%q, want %q", got, locale.Resolved))
		}
		if got := attr(htmlNode, "dir"); got != string(locale.Direction) {
			problems = append(problems, fmt.Sprintf("html dir=%q, want %q", got, locale.Direction))
		}
	}
	return problems
}

func isUserFacingControl(node *xhtml.Node) bool {
	switch node.Data {
	case "input":
		return attr(node, "type") != "hidden"
	case "textarea", "select", "button":
		return true
	default:
		return false
	}
}

func controlHasAccessibleName(node *xhtml.Node, labels map[string]bool, ids map[string]int) bool {
	if strings.TrimSpace(attr(node, "aria-label")) != "" {
		return true
	}
	if labelledBy := strings.Fields(attr(node, "aria-labelledby")); len(labelledBy) > 0 {
		for _, target := range labelledBy {
			if ids[target] != 1 {
				return false
			}
		}
		return true
	}
	if id := attr(node, "id"); id != "" && labels[id] {
		return true
	}
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor.Type == xhtml.ElementNode && ancestor.Data == "label" {
			return true
		}
	}
	if node.Data == "button" && strings.TrimSpace(elementText(node)) != "" {
		return true
	}
	typeName := attr(node, "type")
	return node.Data == "input" && (typeName == "submit" || typeName == "button") && strings.TrimSpace(attr(node, "value")) != ""
}

func elementText(node *xhtml.Node) string {
	var text strings.Builder
	var walk func(*xhtml.Node)
	walk = func(current *xhtml.Node) {
		if current.Type == xhtml.TextNode {
			text.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return text.String()
}

func firstNamedElement(root *xhtml.Node, name string) *xhtml.Node {
	var result *xhtml.Node
	walkElements(root, func(node *xhtml.Node) {
		if result == nil && node.Data == name {
			result = node
		}
	})
	return result
}

// TestFrontendRegressionEveryPageSupportsNetworkLifecycle protects the
// browser's three observable states. Cold loads must show a shaped proxy;
// warm refreshes must retain the resolved component tree; ready pages must
// remain deterministic.
func TestFrontendRegressionEveryPageSupportsNetworkLifecycle(t *testing.T) {
	for _, definition := range PageDefinitions() {
		definition := definition
		t.Run(string(definition.ID), func(t *testing.T) {
			view := testView(definition.ID)
			ready, err := ui.RenderToString(Build(view))
			if err != nil {
				t.Fatal(err)
			}
			loading, err := ui.RenderToString(BuildLoading(view))
			if err != nil {
				t.Fatal(err)
			}
			refreshing, err := ui.RenderToString(BuildRefreshing(view))
			if err != nil {
				t.Fatal(err)
			}

			for state, markup := range map[string]string{"ready": ready, "loading": loading, "refreshing": refreshing} {
				if strings.Count(markup, `id="main-content"`) != 1 {
					t.Errorf("%s state has %d main-content targets, want one", state, strings.Count(markup, `id="main-content"`))
				}
				if !strings.Contains(markup, `id="workspace-navigation"`) {
					t.Errorf("%s state dropped the stable navigation shell", state)
				}
			}
			for _, want := range []string{`aria-busy="true"`, `aria-live="polite"`, `class="loading-progress"`} {
				if !strings.Contains(loading, want) {
					t.Errorf("cold-loading state missing %q", want)
				}
			}
			for _, want := range []string{`aria-busy="true"`, `data-network-state="refreshing"`, escapeTitle(ResolveDocumentPageTitle(view))} {
				if !strings.Contains(refreshing, want) {
					t.Errorf("warm-refresh state missing %q", want)
				}
			}
			if strings.Contains(refreshing, "loading-proxy") {
				t.Error("warm refresh replaced resolved content with a cold-loading proxy")
			}
			again, err := ui.RenderToString(Build(view))
			if err != nil {
				t.Fatal(err)
			}
			if ready != again {
				t.Error("ready component tree is not deterministic")
			}
		})
	}
}

// TestFrontendRegressionAllProductLinksResolve prevents a reusable component
// from shipping a stale page path. Query state may vary, but every link and
// form action within the product subtree must resolve through the canonical
// registry used by the server and WASM history router.
func TestFrontendRegressionAllProductLinksResolve(t *testing.T) {
	for _, definition := range PageDefinitions() {
		definition := definition
		t.Run(string(definition.ID), func(t *testing.T) {
			doc, err := Render(testView(definition.ID))
			if err != nil {
				t.Fatal(err)
			}
			root, err := xhtml.Parse(strings.NewReader(doc))
			if err != nil {
				t.Fatal(err)
			}
			walkElements(root, func(node *xhtml.Node) {
				for _, attribute := range []string{"href", "action"} {
					raw := attr(node, attribute)
					if raw == "" {
						continue
					}
					parsed, parseErr := url.Parse(raw)
					if parseErr != nil {
						t.Errorf("<%s> has invalid %s %q: %v", node.Data, attribute, raw, parseErr)
						continue
					}
					if !strings.HasPrefix(parsed.Path, "/workspace/app/") {
						continue
					}
					if _, ok := LookupRoute(parsed.Path); !ok {
						t.Errorf("<%s> %s %q does not resolve through the product page registry", node.Data, attribute, raw)
					}
				}
			})
		})
	}
}
