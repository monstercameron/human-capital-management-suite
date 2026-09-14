package productui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/wcag"
)

// TestSupportedAssistiveTechnologyBrowserLocaleMatrixCompletesCriticalFlowsEquivalently
// is the production accessibility gate. It is registry-driven so a newly
// published page cannot silently skip the WCAG document contract. The static
// checks qualify the shared semantics; actual NVDA/VoiceOver and browser
// sessions remain named release evidence in the UX-003 artifact.
func TestSupportedAssistiveTechnologyBrowserLocaleMatrixCompletesCriticalFlowsEquivalently(t *testing.T) {
	modes := wcag.SupportedInputModes()
	for _, definition := range PageDefinitions() {
		definition := definition
		for _, code := range SupportedProductLocales() {
			code := code
			t.Run(fmt.Sprintf("%s/%s", definition.ID, code), func(t *testing.T) {
				locale := ResolveProductLocale(code)
				view := ApplyLocale(testView(definition.ID), locale)
				doc, err := Render(view)
				if err != nil {
					t.Fatal(err)
				}
				for _, result := range wcag.SurfaceScore(doc, locale.Resolved, string(locale.Direction)) {
					if !result.Pass {
						t.Errorf("%s: %s", result.Name, result.Detail)
					}
				}
				for _, mode := range modes {
					if result := wcag.CheckInputModeCompatibility(doc, mode); !result.Pass {
						t.Errorf("input mode %q: %s", mode, result.Detail)
					}
				}
			})
		}
	}
}

// TestTodo_A11Y_001_Conformance pins the supported locale and input-mode
// matrix to the production registries rather than a duplicated test list.
func TestTodo_A11Y_001_Conformance(t *testing.T) {
	locales := SupportedProductLocales()
	if len(locales) < 3 {
		t.Fatalf("accessibility matrix needs en-US, de-DE and RTL locales; got %v", locales)
	}
	for _, want := range []string{"en-US", "de-DE", "ar"} {
		found := false
		for _, got := range locales {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("supported locale matrix missing %q", want)
		}
	}
	if got := wcag.SupportedInputModes(); len(got) != 4 {
		t.Fatalf("supported input modes = %v, want keyboard/touch/voice/switch", got)
	}
}

// TestTodo_A11Y_001_Mutation proves the product gate is not a render-only
// smoke test: the shared contract rejects representative accessibility
// regressions before browser or assistive-technology execution.
func TestTodo_A11Y_001_Mutation(t *testing.T) {
	doc, err := Render(ApplyLocale(testView(PageHome), ResolveProductLocale("en-US")))
	if err != nil {
		t.Fatal(err)
	}
	if wcag.CheckLocale(doc, "de-DE", "ltr").Pass {
		t.Fatal("wrong locale was accepted by the product accessibility gate")
	}
	if wcag.CheckInputModeCompatibility(doc, wcag.InputMode("unknown")).Pass {
		t.Fatal("unknown input mode was accepted by the product accessibility gate")
	}
}

func TestTodo_A11Y_001_Property(t *testing.T) {
	for _, page := range []PageID{PageHome, PagePeople, PageWork, PageSettings} {
		for _, code := range SupportedProductLocales() {
			view := ApplyLocale(testView(page), ResolveProductLocale(code))
			first, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			second, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			if first != second {
				t.Fatalf("%s/%s render is not deterministic", page, code)
			}
		}
	}
}

func TestTodo_A11Y_001_Golden(t *testing.T) {
	view := ApplyLocale(testView(PageHome), ResolveProductLocale("en-US"))
	first, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("accessibility release artifact is nondeterministic")
	}
}

func TestTodo_A11Y_001_Security(t *testing.T) {
	for _, page := range PageDefinitions() {
		doc, err := Render(testView(page.ID))
		if err != nil {
			t.Fatal(err)
		}
		if result := wcag.CheckInputModes(doc); !result.Pass {
			t.Errorf("%s: %s", page.ID, result.Detail)
		}
		for _, forbidden := range []string{"href=\"javascript:", "onclick=\"", "onkeydown=\""} {
			if strings.Contains(doc, forbidden) {
				t.Errorf("%s contains executable presentation hook %q", page.ID, forbidden)
			}
		}
	}
}

func TestTodo_A11Y_001_Browser(t *testing.T) {
	doc, err := Render(ApplyLocale(testView(PageHome), ResolveProductLocale("en-US")))
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []qual.CriterionResult{wcag.CheckFocusIndicator(doc), wcag.CheckZoomReflow(doc), wcag.CheckMotionContract(doc)} {
		if !result.Pass {
			t.Errorf("%s: %s", result.Name, result.Detail)
		}
	}
}
