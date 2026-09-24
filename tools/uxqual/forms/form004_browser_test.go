package forms

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/gwc"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/ssr"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/testdata"
)

// TestTodo_FORM_004_Browser is the BROWSER matrix test for FORM-004. Like
// tools/uxqual/qual's own TestTodo_UX_QUAL_001_Browser, it verifies
// keyboard-only completion structurally against both renderers' real
// output: tab order is derivable from DOM order alone, and it pins the
// expected order to the fixture's own field sequence so a reordering
// regression is caught even if the generic keyboard check does not trip.
//
// A real-browser (Chromium/AT) pass against the Promotion form is not
// exercised here -- see definitions/ux/forms/promotion-form-accessibility-
// decision.yaml's PENDING items, which name this gap explicitly rather than
// fabricate automated coverage this lane cannot run.
func TestTodo_FORM_004_Browser(t *testing.T) {
	fixture := FixtureWithValidationError()

	var wantOrder []string
	for _, f := range fixture.Request.Fields {
		if f.Kind == contract.FieldKindReadOnly {
			continue // rendered as static text, not a focusable control
		}
		wantOrder = append(wantOrder, f.ID)
	}

	ssrDoc, err := ssr.Render(fixture)
	if err != nil {
		t.Fatalf("ssr.Render: %v", err)
	}
	gwcDoc, err := gwc.Document(fixture)
	if err != nil {
		t.Fatalf("gwc.Document: %v", err)
	}

	for name, doc := range map[string]string{"ssr": ssrDoc, "gwc": gwcDoc} {
		t.Run(name, func(t *testing.T) {
			res := qual.CheckKeyboard(doc)
			if !res.Pass {
				t.Errorf("keyboard-only completion failed: %s", res.Detail)
			}
			got := qual.FieldIDsInDocumentOrder(doc)
			if len(got) < len(wantOrder) {
				t.Fatalf("expected at least %d focusable fixture fields in DOM order, found %v", len(wantOrder), got)
			}
			if strings.Join(got[:len(wantOrder)], ",") != strings.Join(wantOrder, ",") {
				t.Errorf("tab order (DOM order) mismatch: want %v, got %v", wantOrder, got[:len(wantOrder)])
			}
		})
	}

	t.Run("the errored field is reachable by keyboard, not just visually flagged", func(t *testing.T) {
		for name, doc := range map[string]string{"ssr": ssrDoc, "gwc": gwcDoc} {
			order := qual.FieldIDsInDocumentOrder(doc)
			found := false
			for _, id := range order {
				if id == erroredFieldID {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("[%s] errored field %q is not among the focusable controls in DOM order: %v", name, erroredFieldID, order)
			}
		}
	})

	t.Run("the clean fixture (no error) renders identically-ordered fields", func(t *testing.T) {
		clean, err := ssr.Render(testdata.PromotionFixture())
		if err != nil {
			t.Fatalf("ssr.Render: %v", err)
		}
		got := qual.FieldIDsInDocumentOrder(clean)
		if len(got) < len(wantOrder) || strings.Join(got[:len(wantOrder)], ",") != strings.Join(wantOrder, ",") {
			t.Errorf("adding a validation error changed field order: want %v, got %v", wantOrder, got)
		}
	})
}
