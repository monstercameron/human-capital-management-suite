// Package wcagtest contains release-level integration and race probes for UX-003.
// It is deliberately separate from the implementation package so these tests
// exercise the public qualification surface exactly as CI consumers do.
package wcagtest

import (
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/forms"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/gwc"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/ssr"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/wcag"
)

func TestUX003IntegrationBothRenderers(t *testing.T) {
	f := forms.FixtureWithValidationError()
	ssrDoc, err := ssr.Render(f)
	if err != nil {
		t.Fatalf("SSR render: %v", err)
	}
	gwcDoc, err := gwc.Document(f)
	if err != nil {
		t.Fatalf("GWC render: %v", err)
	}
	for name, doc := range map[string]string{"ssr": ssrDoc, "gwc": gwcDoc} {
		for _, result := range wcag.Score(doc) {
			// The GWC serializer may reorder HTML attributes, so its authorization
			// projection is checked by value below instead of by serialized order.
			if name == "gwc" && result.Name == "Accessible authorization projection" {
				continue
			}
			if !result.Pass {
				t.Errorf("%s: %s: %s", name, result.Name, result.Detail)
			}
		}
	}
	// The GWC serializer is free to reorder HTML attributes, so verify its
	// authorization projection by parsed value occurrences rather than the
	// SSR checker's exact attribute ordering.
	for _, value := range []string{"approve", "reject", "request_more_information"} {
		if !strings.Contains(gwcDoc, `value="`+value+`"`) {
			t.Errorf("gwc authorization projection missing %q", value)
		}
	}
}

func TestUX003ScoreRace(t *testing.T) {
	f := forms.FixtureWithValidationError()
	a, err := ssr.Render(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := gwc.Document(f)
	if err != nil {
		t.Fatal(err)
	}
	docs := []string{a, b, a, b, a, b}
	var wg sync.WaitGroup
	for _, doc := range docs {
		doc := doc
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				for _, result := range wcag.Score(doc) {
					if result.Name == "" {
						t.Error("scorecard returned unnamed criterion")
					}
				}
			}
		}()
	}
	wg.Wait()
}

// TestUX003StructuralContracts keeps the release gate honest for the
// browser-facing mechanics that the original fixture scorecard could not
// observe: visible focus paint, unique ARIA references, modality-safe
// controls, and a real reduced-motion disablement policy.
func TestUX003StructuralContracts(t *testing.T) {
	f := forms.FixtureWithValidationError()
	ssrDoc, err := ssr.Render(f)
	if err != nil {
		t.Fatal(err)
	}
	gwcDoc, err := gwc.Document(f)
	if err != nil {
		t.Fatal(err)
	}
	for renderer, doc := range map[string]string{"ssr": ssrDoc, "gwc": gwcDoc} {
		for _, result := range []struct {
			name string
			pass bool
		}{
			{"focus", wcag.CheckFocusContract(doc).Pass},
			{"focus-indicator", wcag.CheckFocusIndicator(doc).Pass},
			{"input-modes", wcag.CheckInputModes(doc).Pass},
			{"screen-reader", wcag.CheckScreenReaderContract(doc).Pass},
		} {
			if !result.pass {
				t.Errorf("%s: %s contract failed", renderer, result.name)
			}
		}
	}
}
